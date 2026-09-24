//go:build !windows

package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"hum/internal/process"
)

func runtimeEndpoint(dir string) string { return filepath.Join(dir, "hum.sock") }
func defaultRuntimeDir() string         { return filepath.Join(os.TempDir(), "hum-"+strconv.Itoa(os.Getuid())) }
func checkRuntimeFile(_ string, info os.FileInfo) error {
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("mode is %o, want 600", info.Mode().Perm())
	}
	return nil
}
func openStartupLock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
}
func secureNewArtifact(string) error { return nil }
func lockRuntimeFile(lock *os.File) error {
	if err := lock.Chmod(0o600); err != nil {
		return fmt.Errorf("secure startup lock: %w", err)
	}
	return syscall.Flock(int(lock.Fd()), syscall.LOCK_EX)
}
func unlockRuntimeFile(lock *os.File) error { return syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }
func ensurePrivateDir(dir string) error {
	if dir == "" {
		return errors.New("runtime directory is empty")
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return fmt.Errorf("create runtime directory parent: %w", err)
	}
	if err := os.Mkdir(dir, 0o700); err == nil {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("secure runtime directory: %w", err)
		}
		return nil
	} else if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	return checkPrivateDir(dir)
}
func checkPrivateDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("inspect runtime directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("runtime path is not a directory: %s", dir)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != runtimeUser() {
		return fmt.Errorf("runtime directory is not owned by the current user: %s", dir)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("runtime directory mode %04o permits group or other writes: %s", info.Mode().Perm(), dir)
	}
	return nil
}
func runtimeGroupVerification(group RuntimeGroup) error {
	if !processAlive(group.LeaderPID) {
		return os.ErrNotExist
	}
	identity, err := process.ProcessStartIdentity(group.LeaderPID)
	if err != nil {
		return fmt.Errorf("read leader start identity: %w", err)
	}
	if identity != group.StartIdentity {
		return fmt.Errorf("leader pid %d start identity %q does not match recorded %q", group.LeaderPID, identity, group.StartIdentity)
	}
	pgid, err := syscall.Getpgid(group.LeaderPID)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrNotExist
		}
		return fmt.Errorf("read leader process group: %w", err)
	}
	if pgid != group.PGID || group.PGID != group.LeaderPID {
		return fmt.Errorf("leader pid %d is in pgid %d, recorded leader/pgid %d/%d", group.LeaderPID, pgid, group.LeaderPID, group.PGID)
	}
	return nil
}
func runtimeGroupAlive(pgid int) bool            { return process.ProcessGroupAlive(pgid) }
func recordedGroupAlive(group RuntimeGroup) bool { return runtimeGroupAlive(group.PGID) }
func waitRuntimeGroupGone(pgid int, timeout time.Duration) bool {
	if !runtimeGroupAlive(pgid) {
		return true
	}
	if timeout <= 0 {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !runtimeGroupAlive(pgid) {
			return true
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			return !runtimeGroupAlive(pgid)
		}
	}
}
func reclaimRuntimeGroup(group RuntimeGroup, grace time.Duration) (string, error) {
	if err := runtimeGroupVerification(group); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if runtimeGroupAlive(group.PGID) {
				return "unresolved", errors.New("recorded leader is gone while its process group remains alive")
			}
			return "reclaimed", nil
		}
		return "unresolved", err
	}
	if err := syscall.Kill(-group.PGID, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return "reclaimed", nil
		}
		return "unresolved", fmt.Errorf("send TERM: %w", err)
	}
	if waitRuntimeGroupGone(group.PGID, grace) {
		return "reclaimed", nil
	}
	// A live group retains its PGID after its leader exits; reverify before escalation.
	if err := runtimeGroupVerification(group); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "unresolved", err
	}
	if err := syscall.Kill(-group.PGID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return "unresolved", fmt.Errorf("send KILL: %w", err)
	}
	if waitRuntimeGroupGone(group.PGID, grace) {
		return "reclaimed", nil
	}
	return "unresolved", errors.New("process group remained alive after KILL")
}
func listenRuntime(path string) (net.Listener, error) {
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on daemon socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("secure daemon socket: %w", err)
	}
	return listener, nil
}
func dialRuntime(ctx context.Context, path string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", path)
}
func endpointExists(path string) bool  { return pathExists(path) }
func removeEndpoint(path string) error { return os.Remove(path) }
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
func socketResponds(path string) bool {
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
