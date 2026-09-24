//go:build windows

package daemon

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"hum/internal/process"
)

func defaultRuntimeDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "hum-runtime")
}

// Pipe names are derived from the absolute runtime path, not a PID. FILE_CREATE
// on the first instance prevents a different process from usurping the name.
func runtimeEndpoint(dir string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(dir))))
	return fmt.Sprintf(`\\.\pipe\hum-%x`, sum[:16])
}

func ensurePrivateDir(dir string) error {
	if dir == "" {
		return errors.New("runtime directory is empty")
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return fmt.Errorf("create runtime directory parent: %w", err)
	}
	sd, err := windows.SecurityDescriptorFromString(privateSDDL())
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	if err := windows.CreateDirectory(name, sa); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
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
	if err := checkPrivateACL(dir); err != nil {
		return fmt.Errorf("runtime directory is not owned exclusively by the current user: %s: %w", dir, err)
	}
	return nil
}
func checkRuntimeFile(path string, _ os.FileInfo) error { return checkPrivateACL(path) }

func secureNewArtifact(path string) error {
	sid, err := currentSID()
	if err != nil {
		return err
	}
	// An elevated token may default new file ownership to Administrators. Give
	// this newly-created artifact the actual user SID before it can be trusted.
	sd, err := windows.SecurityDescriptorFromString(privateSDDL())
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, sid, nil, acl, nil)
}
func openStartupLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err == nil {
		if err := secureNewArtifact(path); err != nil {
			_ = file.Close()
			return nil, err
		}
		return file, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return os.OpenFile(path, os.O_RDWR, 0o600)
}
func lockRuntimeFile(file *os.File) error {
	if err := checkRuntimeFile(file.Name(), nil); err != nil {
		return fmt.Errorf("secure startup lock: %w", err)
	}
	var ov windows.Overlapped
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ov)
}
func unlockRuntimeFile(file *os.File) error {
	var ov windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &ov)
}

func listenRuntime(path string) (net.Listener, error) {
	listener, err := winio.ListenPipe(path, &winio.PipeConfig{SecurityDescriptor: privateSDDL()})
	if err != nil {
		return nil, fmt.Errorf("listen on private daemon pipe: %w", err)
	}
	return listener, nil
}
func dialRuntime(ctx context.Context, path string) (net.Conn, error) {
	// The security descriptor is checked before talking to an endpoint. A pipe
	// created first by another user may permit us to connect; it is not trusted.
	if err := checkPrivateACL(path); err != nil {
		return nil, fmt.Errorf("refusing daemon pipe: %w", err)
	}
	conn, err := winio.DialPipeContext(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := checkPrivateACL(path); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("refusing daemon pipe: %w", err)
	}
	return conn, nil
}
func endpointExists(path string) bool {
	_, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	// An inaccessible endpoint is owned until proved otherwise, never stale.
	return err == nil || !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(err, windows.ERROR_PATH_NOT_FOUND)
}
func removeEndpoint(string) error { return nil } // A named pipe disappears when its listener closes.
func socketResponds(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	conn, err := dialRuntime(ctx, path)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
func processAlive(pid int) bool      { return process.ProcessGroupAlive(pid) }
func runtimeGroupAlive(pid int) bool { return process.ProcessGroupAlive(pid) }
func recordedGroupAlive(group RuntimeGroup) bool {
	identity, err := process.ProcessStartIdentity(group.LeaderPID)
	return err != nil && !errors.Is(err, os.ErrNotExist) || err == nil && identity == group.StartIdentity && runtimeGroupAlive(group.PGID)
}
func reclaimRuntimeGroup(group RuntimeGroup, _ time.Duration) (string, error) {
	if group.PGID != group.LeaderPID {
		return "unresolved", errors.New("recorded Windows job leader identity is inconsistent")
	}
	identity, err := process.ProcessStartIdentity(group.LeaderPID)
	if errors.Is(err, os.ErrNotExist) {
		return "reclaimed", nil
	}
	if err != nil {
		return "unresolved", fmt.Errorf("verify recorded leader: %w", err)
	}
	if identity != group.StartIdentity {
		return "reclaimed", nil
	} // Reused PID is never a termination target.
	if !runtimeGroupAlive(group.LeaderPID) {
		return "reclaimed", nil
	}
	// The daemon's kill-on-close Job Object ordinarily terminates these children.
	// A surviving leader cannot be terminated without the lost job handle.
	return "unresolved", errors.New("recorded child is live but its Job Object ownership cannot be verified")
}
