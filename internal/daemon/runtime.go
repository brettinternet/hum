// Package daemon adapts the process supervisor to the private local daemon
// transport. Runtime ownership and artifact handling live in this file so the
// wire server can never accidentally start a second supervisor.
package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/process"
	"hum/internal/project"
	"hum/internal/protocol"
)

var (
	// ErrAlreadyRunning is returned when a live daemon still owns the runtime.
	ErrAlreadyRunning = errors.New("hum daemon is already running")
	// ErrRuntimeOwned is returned when an ownership artifact cannot be proved
	// stale. Failing closed is important: a live daemon must never be displaced.
	ErrRuntimeOwned = errors.New("hum daemon runtime is owned")
)

const (
	defaultLogBytes     = 1 << 20
	maxLogBytes         = 64 << 20
	RuntimeStateVersion = 2
)

// RuntimeIdentity is the PID and OS process-start identity of one process.
type RuntimeIdentity struct {
	PID           int    `json:"pid"`
	StartIdentity string `json:"start_identity"`
}

// RuntimeGroup is the persisted identity of one live child process group.
type RuntimeGroup struct {
	Scope         string `json:"scope"`
	ProjectRoot   string `json:"project_root"`
	Name          string `json:"name"`
	LeaderPID     int    `json:"leader_pid"`
	PGID          int    `json:"pgid"`
	StartIdentity string `json:"start_identity"`
}

func (g RuntimeGroup) MarshalJSON() ([]byte, error) {
	if g.Scope == "" {
		g.Scope = "project"
	}
	type runtimeGroupJSON RuntimeGroup
	return json.Marshal(runtimeGroupJSON(g))
}

// RuntimeState is the versioned, atomically replaced runtime-state document.
type RuntimeState struct {
	Version int             `json:"version"`
	Daemon  RuntimeIdentity `json:"daemon"`
	Groups  []RuntimeGroup  `json:"groups"`
}

// RuntimeStateFile is a descriptive alias for RuntimeState.
type RuntimeStateFile = RuntimeState

func runtimeGroupKey(project, name string) string { return project + "\x00" + name }
func scopedRuntimeGroupKey(scope, project, name string) string {
	if scope == app.ScopeGlobal {
		return "global\x00" + name
	}
	return runtimeGroupKey(project, name)
}

// RuntimePaths names every artifact belonging to one daemon instance. Dir is
// private to the current user; all other files are children of Dir.
type RuntimePaths struct {
	Dir    string
	Socket string
	PID    string
	Lock   string
	Ready  string
	Log    string
	State  string

	// Descriptive aliases keep path ownership obvious at command edges.
	RuntimeDir       string
	SocketPath       string
	PIDPath          string
	LockPath         string
	StartupLockPath  string
	ReadyPath        string
	LogPath          string
	StatePath        string
	RuntimeState     string
	RuntimeStatePath string
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
		State: filepath.Join(dir, "hum.state"), StatePath: filepath.Join(dir, "hum.state"), RuntimeState: filepath.Join(dir, "hum.state"), RuntimeStatePath: filepath.Join(dir, "hum.state"),
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

// Config controls a Server. StopGrace is exact, including zero for immediate
// escalation; command callers supply the configured default. Supervisor, when
// non-nil, is owned by the server for its complete lifetime and is shut down
// before runtime artifacts are removed.
type Config struct {
	RuntimeDir string
	// WireVersion overrides the advertised protocol version. It exists for
	// compatibility tests; zero selects protocol.Version.
	WireVersion int

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
	paths         RuntimePaths
	lock          *os.File
	pid           int
	startIdentity string

	stateMu sync.Mutex
	state   RuntimeState
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
	if p.State == "" {
		p.State = p.StatePath
	}
	if p.State == "" {
		p.State = p.RuntimeState
	}
	if p.State == "" {
		p.State = p.RuntimeStatePath
	}
	if p.State == "" {
		p.State = filepath.Join(p.Dir, "hum.state")
	}
	p.SocketPath, p.PIDPath, p.LockPath, p.StartupLockPath = p.Socket, p.PID, p.Lock, p.Lock
	p.ReadyPath, p.LogPath = p.Ready, p.Log
	p.StatePath, p.RuntimeState, p.RuntimeStatePath = p.State, p.State, p.State
	return p
}

func readRuntimeState(path string) (RuntimeState, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RuntimeState{}, false, nil
	}
	if err != nil {
		return RuntimeState{}, true, runtimeStateCorrupt(path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return RuntimeState{}, true, runtimeStateCorrupt(path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return RuntimeState{}, true, runtimeStateCorrupt(path, errors.New("symbolic links are not allowed"))
	}
	if info.Mode().Perm() != 0o600 {
		return RuntimeState{}, true, runtimeStateCorrupt(path, fmt.Errorf("mode is %o, want 600", info.Mode().Perm()))
	}
	var state RuntimeState
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return RuntimeState{}, true, runtimeStateCorrupt(path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return RuntimeState{}, true, runtimeStateCorrupt(path, errors.New("contains more than one JSON value"))
		}
		return RuntimeState{}, true, runtimeStateCorrupt(path, err)
	}
	if state.Version != RuntimeStateVersion {
		return RuntimeState{}, true, runtimeStateCorrupt(path, fmt.Errorf("unsupported version %d", state.Version))
	}
	if state.Daemon.PID <= 0 || state.Daemon.StartIdentity == "" {
		return RuntimeState{}, true, runtimeStateCorrupt(path, errors.New("daemon identity is incomplete"))
	}
	seen := make(map[string]struct{}, len(state.Groups))
	for index, group := range state.Groups {
		if group.Scope == "" {
			group.Scope = app.ScopeProject
		}
		validRoot := group.Scope == app.ScopeGlobal && group.ProjectRoot == "" || group.Scope == app.ScopeProject && group.ProjectRoot != "" && filepath.IsAbs(group.ProjectRoot)
		if (group.Scope != app.ScopeProject && group.Scope != app.ScopeGlobal) || !validRoot || group.Name == "" || group.LeaderPID <= 0 || group.PGID <= 0 || group.StartIdentity == "" {
			return RuntimeState{}, true, runtimeStateCorrupt(path, fmt.Errorf("groups[%d] identity is incomplete", index))
		}
		if group.Scope == app.ScopeProject {
			canonical, canonicalErr := project.CanonicalPath(group.ProjectRoot)
			if canonicalErr != nil {
				return RuntimeState{}, true, runtimeStateCorrupt(path, fmt.Errorf("groups[%d] project root: %w", index, canonicalErr))
			}
			group.ProjectRoot = canonical
		}
		key := scopedRuntimeGroupKey(group.Scope, group.ProjectRoot, group.Name)
		if _, exists := seen[key]; exists {
			return RuntimeState{}, true, runtimeStateCorrupt(path, fmt.Errorf("groups[%d] aliases collapse to one project/name; remove the state after verifying managed processes", index))
		}
		seen[key] = struct{}{}
		state.Groups[index] = group
	}
	return state, true, nil
}

func runtimeStateCorrupt(path string, err error) error {
	return fmt.Errorf("runtime state %s is corrupt: %v; verify no managed processes remain, then remove the file and retry", path, err)
}

func (r *runtimeOwner) writeStateLocked() error {
	r.state.Version = RuntimeStateVersion
	if r.state.Daemon.PID <= 0 || r.state.Daemon.StartIdentity == "" {
		return errors.New("runtime state daemon identity is incomplete")
	}
	groups := make([]RuntimeGroup, len(r.state.Groups))
	copy(groups, r.state.Groups)
	sortRuntimeGroups(groups)
	r.state.Groups = groups
	data, err := json.Marshal(r.state)
	if err != nil {
		return fmt.Errorf("encode runtime state: %w", err)
	}
	if err := writeAtomic(r.paths.State, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write runtime state: %w", err)
	}
	return nil
}

func sortRuntimeGroups(groups []RuntimeGroup) {
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].ProjectRoot != groups[j].ProjectRoot {
			return groups[i].ProjectRoot < groups[j].ProjectRoot
		}
		return groups[i].Name < groups[j].Name
	})
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
	startIdentity, identityErr := process.ProcessStartIdentity(os.Getpid())
	if identityErr != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("read daemon start identity: %w", identityErr)
	}
	owner := &runtimeOwner{paths: paths, lock: lock, pid: os.Getpid(), startIdentity: startIdentity}
	state, stateExists, stateErr := readRuntimeState(paths.State)
	if stateErr != nil {
		owner.release()
		return nil, stateErr
	}
	if stateExists {
		owner.state = state
	}
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

	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("inspect runtime directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("runtime path is not a directory: %s", dir)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("runtime directory mode %04o permits group or other writes: %s", info.Mode().Perm(), dir)
	}
	return nil
}

func (r *runtimeOwner) recoverStale() error {
	if r.state.Version == RuntimeStateVersion && r.state.Daemon.PID > 0 && processAlive(r.state.Daemon.PID) {
		identity, identityErr := process.ProcessStartIdentity(r.state.Daemon.PID)
		if identityErr != nil {
			return fmt.Errorf("%w: recorded daemon pid %d is live but its start identity cannot be verified", ErrRuntimeOwned, r.state.Daemon.PID)
		}
		if identity == r.state.Daemon.StartIdentity {
			return fmt.Errorf("%w: pid %d", ErrAlreadyRunning, r.state.Daemon.PID)
		}
		// A live process with a different start identity is a reused PID, which
		// positively proves that the recorded daemon incarnation is dead.
	}
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

func runtimeGroupForProcess(item app.Process) (RuntimeGroup, error) {
	if item.Name == "" || item.PID <= 0 || item.PGID <= 0 || item.StartIdentity == "" {
		return RuntimeGroup{}, errors.New("process identity is incomplete")
	}
	if item.Scope == app.ScopeGlobal {
		return RuntimeGroup{Scope: app.ScopeGlobal, Name: item.Name, LeaderPID: item.PID, PGID: item.PGID, StartIdentity: item.StartIdentity}, nil
	}
	if item.Root == "" || !filepath.IsAbs(item.Root) {
		return RuntimeGroup{}, errors.New("process identity is incomplete")
	}
	root, err := project.CanonicalPath(item.Root)
	if err != nil {
		return RuntimeGroup{}, fmt.Errorf("canonical project root: %w", err)
	}
	return RuntimeGroup{Scope: app.ScopeProject, ProjectRoot: root, Name: item.Name, LeaderPID: item.PID, PGID: item.PGID, StartIdentity: item.StartIdentity}, nil
}

func (r *runtimeOwner) persistProcess(item app.Process) error {
	// Deterministic embedders may provide an in-memory Child with no host PID
	// identity. Production process.Child always captures one; leave test-only
	// children outside the OS runtime-state document rather than inventing an
	// identity that could later authorize a signal.
	if item.StartIdentity == "" {
		return nil
	}
	group, err := runtimeGroupForProcess(item)
	if err != nil {
		return fmt.Errorf("persist %s/%s: %w", item.Root, item.Name, err)
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.state.Version == 0 {
		r.state = RuntimeState{Version: RuntimeStateVersion, Daemon: RuntimeIdentity{PID: r.pid, StartIdentity: r.startIdentity}}
	}
	if r.state.Daemon.PID != r.pid || r.state.Daemon.StartIdentity != r.startIdentity {
		return errors.New("runtime state daemon identity changed")
	}
	updated := false
	for index := range r.state.Groups {
		if scopedRuntimeGroupKey(r.state.Groups[index].Scope, r.state.Groups[index].ProjectRoot, r.state.Groups[index].Name) == scopedRuntimeGroupKey(group.Scope, group.ProjectRoot, group.Name) {
			r.state.Groups[index] = group
			updated = true
			break
		}
	}
	if !updated {
		r.state.Groups = append(r.state.Groups, group)
	}
	return r.writeStateLocked()
}

func (r *runtimeOwner) removeProcess(item app.Process) error {
	if item.Name == "" || item.Scope != app.ScopeGlobal && item.Root == "" {
		return nil
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	key := scopedRuntimeGroupKey(item.Scope, filepath.Clean(item.Root), item.Name)
	previous := make([]RuntimeGroup, len(r.state.Groups))
	copy(previous, r.state.Groups)
	filtered := make([]RuntimeGroup, 0, len(previous))
	for _, group := range previous {
		if scopedRuntimeGroupKey(group.Scope, group.ProjectRoot, group.Name) == key && (item.StartIdentity == "" || group.StartIdentity == item.StartIdentity) {
			continue
		}
		filtered = append(filtered, group)
	}
	r.state.Groups = filtered
	if err := r.writeStateLocked(); err != nil {
		r.state.Groups = previous
		return err
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

func runtimeGroupAlive(pgid int) bool {
	return process.ProcessGroupAlive(pgid)
}

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
	// Never escalate a group whose leader changed incarnation or leadership
	// while TERM was in flight. A leader that exited during the grace is not
	// such a change: this group was verified before TERM and was just observed
	// alive, and a PGID cannot be recycled while its group is non-empty, so
	// the survivors are still the recorded group.
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

func (r *runtimeOwner) reconcileStartup(supervisor *app.Supervisor, grace time.Duration) ([]protocol.StartupWarning, error) {
	if supervisor == nil {
		return nil, errors.New("startup reconciliation requires a supervisor")
	}
	if grace < 0 {
		grace = 0
	}
	oldGroups := append([]RuntimeGroup(nil), r.state.Groups...)
	sortRuntimeGroups(oldGroups)
	remaining := make([]RuntimeGroup, 0, len(oldGroups))
	warnings := make([]protocol.StartupWarning, 0, len(oldGroups))
	for _, group := range oldGroups {
		outcome, reclaimErr := reclaimRuntimeGroup(group, grace)
		message := "recorded process group was reclaimed"
		if reclaimErr != nil {
			message = fmt.Sprintf("recorded process group was left unresolved: %v", reclaimErr)
			remaining = append(remaining, group)
			if err := supervisor.AddUnresolvedScoped(group.Scope, group.ProjectRoot, group.Name, group.LeaderPID, group.PGID, group.StartIdentity); err != nil {
				return nil, fmt.Errorf("retain unresolved %s/%s: %w", group.ProjectRoot, group.Name, err)
			}
		}
		warnings = append(warnings, protocol.StartupWarning{Project: group.ProjectRoot, Name: group.Name, Outcome: outcome, Message: message})
	}
	r.stateMu.Lock()
	r.state = RuntimeState{Version: RuntimeStateVersion, Daemon: RuntimeIdentity{PID: r.pid, StartIdentity: r.startIdentity}, Groups: remaining}
	stateErr := r.writeStateLocked()
	r.stateMu.Unlock()
	if stateErr != nil {
		return nil, stateErr
	}
	return warnings, nil
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
		r.stateMu.Lock()
		stateHasGroups := len(r.state.Groups) != 0
		r.stateMu.Unlock()
		if !stateHasGroups {
			if err := os.Remove(r.paths.State); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove %s: %w", r.paths.State, err))
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
