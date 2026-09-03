// Package daemon adapts the process supervisor to the private local daemon
// transport. Runtime ownership and artifact handling live in this file so the
// wire server can never accidentally start a second supervisor.
package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"hum/internal/app"
	"hum/internal/output"
)

var (
	// ErrAlreadyRunning is returned when a live daemon still owns the runtime.
	ErrAlreadyRunning = errors.New("hum daemon is already running")
	// ErrRuntimeOwned is returned when an ownership artifact cannot be proved
	// stale. Failing closed is important: a live daemon must never be displaced.
	ErrRuntimeOwned = errors.New("hum daemon runtime is owned")
)

const (
	defaultLogBytes = 1 << 20
	maxLogBytes     = 64 << 20
)

// RuntimePaths names every artifact belonging to one daemon instance. Dir is
// private to the current user; all other files are children of Dir.
type RuntimePaths struct {
	Dir    string
	Socket string
	PID    string
	Lock   string
	Ready  string
	Log    string

	// Descriptive aliases keep path ownership obvious at command edges.
	RuntimeDir      string
	SocketPath      string
	PIDPath         string
	LockPath        string
	StartupLockPath string
	ReadyPath       string
	LogPath         string
}

// NewRuntimePaths resolves an explicit runtime directory into canonical paths.
// An empty directory follows HUM_RUNTIME_DIR, XDG_RUNTIME_DIR, and finally a
// per-user temporary directory in that order.
func NewRuntimePaths(runtimeDir string) RuntimePaths {
	dir := resolveRuntimeDir(runtimeDir)
	return RuntimePaths{
		Dir: dir, RuntimeDir: dir,
		Socket: filepath.Join(dir, "hum.sock"), SocketPath: filepath.Join(dir, "hum.sock"),
		PID: filepath.Join(dir, "hum.pid"), PIDPath: filepath.Join(dir, "hum.pid"),
		Lock: filepath.Join(dir, "hum.startup.lock"), LockPath: filepath.Join(dir, "hum.startup.lock"), StartupLockPath: filepath.Join(dir, "hum.startup.lock"),
		Ready: filepath.Join(dir, "hum.ready"), ReadyPath: filepath.Join(dir, "hum.ready"),
		Log: filepath.Join(dir, "daemon.log"), LogPath: filepath.Join(dir, "daemon.log"),
	}
}

// RuntimePathsFor is a concise alias used by command and integration callers.
func RuntimePathsFor(runtimeDir string) RuntimePaths { return NewRuntimePaths(runtimeDir) }

func resolveRuntimeDir(explicit string) string {
	if explicit == "" {
		explicit = os.Getenv("HUM_RUNTIME_DIR")
	}
	if explicit == "" {
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
			explicit = filepath.Join(xdg, "hum")
		}
	}
	if explicit == "" {
		explicit = filepath.Join(os.TempDir(), "hum-"+strconv.Itoa(os.Getuid()))
	}
	if absolute, err := filepath.Abs(explicit); err == nil {
		explicit = absolute
	}
	return filepath.Clean(explicit)
}

// ResolveRuntimeDir exposes the same precedence used by NewRuntimePaths.
func ResolveRuntimeDir(explicit string) string { return resolveRuntimeDir(explicit) }

// DefaultRuntimeDir resolves the process environment without an override.
func DefaultRuntimeDir() string { return resolveRuntimeDir("") }

// Config controls a Server. The zero value is usable and picks conservative
// defaults. Supervisor, when non-nil, is owned by the server for its complete
// lifetime and is shut down before runtime artifacts are removed.
type Config struct {
	RuntimeDir string
	Version    string

	StopGrace      time.Duration
	OutputLimits   output.Limits
	CompletedLimit int
	MaxLineBytes   int
	LogBytes       int64

	// Supervisor is primarily useful to integration tests and embedding code.
	// A nil value causes NewServer to construct an app.Supervisor from the
	// lifecycle fields above.
	Supervisor *app.Supervisor
}

// ServerConfig is the descriptive spelling used by callers at command edges.
type ServerConfig = Config

// runtimeOwner holds the lock and artifacts for one server. The startup lock is
// intentionally retained on disk after release; its inode is the stable
// serialization point for future contenders.
type runtimeOwner struct {
	paths RuntimePaths
	lock  *os.File
	pid   int
}

func (p RuntimePaths) normalized() RuntimePaths {
	if p.Dir == "" && p.RuntimeDir != "" {
		p.Dir = p.RuntimeDir
	}
	if p.Dir == "" {
		p = NewRuntimePaths("")
	} else {
		p.Dir = resolveRuntimeDir(p.Dir)
	}
	p.RuntimeDir = p.Dir
	if p.Socket == "" {
		p.Socket = p.SocketPath
	}
	if p.Socket == "" {
		p.Socket = filepath.Join(p.Dir, "hum.sock")
	}
	if p.PID == "" {
		p.PID = p.PIDPath
	}
	if p.PID == "" {
		p.PID = filepath.Join(p.Dir, "hum.pid")
	}
	if p.Lock == "" {
		p.Lock = p.LockPath
	}
	if p.Lock == "" {
		p.Lock = p.StartupLockPath
	}
	if p.Lock == "" {
		p.Lock = filepath.Join(p.Dir, "hum.startup.lock")
	}
	if p.Ready == "" {
		p.Ready = p.ReadyPath
	}
	if p.Ready == "" {
		p.Ready = filepath.Join(p.Dir, "hum.ready")
	}
	if p.Log == "" {
		p.Log = p.LogPath
	}
	if p.Log == "" {
		p.Log = filepath.Join(p.Dir, "daemon.log")
	}
	p.SocketPath, p.PIDPath, p.LockPath, p.StartupLockPath = p.Socket, p.PID, p.Lock, p.Lock
	p.ReadyPath, p.LogPath = p.Ready, p.Log
	return p
}

func acquireRuntime(paths RuntimePaths) (*runtimeOwner, error) {
	paths = paths.normalized()
	if err := ensurePrivateDir(paths.Dir); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(paths.Lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open startup lock: %w", err)
	}
	if err := lock.Chmod(0o600); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("secure startup lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock runtime: %w", err)
	}
	owner := &runtimeOwner{paths: paths, lock: lock, pid: os.Getpid()}
	if err := owner.recoverStale(); err != nil {
		owner.release()
		return nil, err
	}
	return owner, nil
}

func ensurePrivateDir(dir string) error {
	if dir == "" {
		return errors.New("runtime directory is empty")
	}
	info, err := os.Lstat(dir)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("runtime path is not a directory: %s", dir)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect runtime directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure runtime directory: %w", err)
	}
	return nil
}

func (r *runtimeOwner) recoverStale() error {
	pid, pidExists, err := readPID(r.paths.PID)
	if err != nil {
		return err
	}
	socketExists := pathExists(r.paths.Socket)
	if pidExists && pid > 0 && processAlive(pid) {
		// A live daemon is never displaced. A socket that accepts confirms
		// ownership; a missing/unresponsive socket is still treated as owned
		// because it may be in the startup window.
		if socketExists && socketResponds(r.paths.Socket) {
			return fmt.Errorf("%w: pid %d", ErrAlreadyRunning, pid)
		}
		return fmt.Errorf("%w: pid %d", ErrRuntimeOwned, pid)
	}
	if socketExists {
		if socketResponds(r.paths.Socket) {
			return fmt.Errorf("%w: socket %s", ErrAlreadyRunning, r.paths.Socket)
		}
		if err := os.Remove(r.paths.Socket); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale socket: %w", err)
		}
	}
	for _, path := range []string{r.paths.PID, r.paths.Ready} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale artifact %s: %w", path, err)
		}
	}
	return nil
}

func (r *runtimeOwner) bind() (net.Listener, error) {
	listener, err := net.Listen("unix", r.paths.Socket)
	if err != nil {
		return nil, fmt.Errorf("listen on daemon socket: %w", err)
	}
	if err := os.Chmod(r.paths.Socket, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(r.paths.Socket)
		return nil, fmt.Errorf("secure daemon socket: %w", err)
	}
	if err := writeAtomic(r.paths.PID, []byte(strconv.Itoa(r.pid)+"\n"), 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(r.paths.Socket)
		return nil, fmt.Errorf("write daemon pid: %w", err)
	}
	return listener, nil
}

func (r *runtimeOwner) markReady() error {
	return writeAtomic(r.paths.Ready, []byte(strconv.Itoa(r.pid)+"\n"), 0o600)
}

func (r *runtimeOwner) unlockStartup() {
	if r.lock == nil {
		return
	}
	_ = syscall.Flock(int(r.lock.Fd()), syscall.LOCK_UN)
}

func (r *runtimeOwner) lockStartup() error {
	if r.lock == nil {
		return errors.New("startup lock is closed")
	}
	return syscall.Flock(int(r.lock.Fd()), syscall.LOCK_EX)
}

func (r *runtimeOwner) cleanup() error {
	if err := r.lockStartup(); err != nil {
		r.release()
		return err
	}
	var errs []error
	// Only remove artifacts if the PID file still identifies this owner. This
	// prevents a delayed Close from deleting a replacement daemon's socket.
	if pid, ok, err := readPID(r.paths.PID); err != nil {
		errs = append(errs, err)
	} else if !ok || pid == r.pid {
		for _, path := range []string{r.paths.Socket, r.paths.PID, r.paths.Ready} {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
			}
		}
	}
	r.release()
	return errors.Join(errs...)
}

func (r *runtimeOwner) release() {
	if r.lock == nil {
		return
	}
	_ = syscall.Flock(int(r.lock.Fd()), syscall.LOCK_UN)
	_ = r.lock.Close()
	r.lock = nil
}

func readPID(path string) (pid int, exists bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, true, fmt.Errorf("read daemon pid: %w", err)
	}
	value := strings.TrimSpace(string(data))
	pid, err = strconv.Atoi(value)
	if err != nil || pid <= 0 {
		// A malformed PID is stale, but report no live owner so it can be
		// replaced safely while holding the startup lock.
		return 0, true, nil
	}
	return pid, true, nil
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

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

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hum-artifact-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
