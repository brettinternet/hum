// Package app owns the in-process supervisor registry.  It deliberately keeps
// process launch details behind internal/process while exposing immutable
// snapshots to callers.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"hum/internal/output"
	"hum/internal/process"
)

// State is the lifecycle state of a supervised process.
type State string

const (
	StateRunning State = "running"
	StateExited  State = "exited"
)

// StartRequest describes one direct-argv launch. Env is copied and is never
// inherited from the supervisor when it is nil or empty.
type StartRequest struct {
	Name string
	Cwd  string
	Argv []string
	Env  []string
}

// Process is an immutable read model. It intentionally has no environment or
// output-store field; callers use Supervisor.Output for output access.
type Process struct {
	Name         string
	Root         string
	PID          int
	PGID         int
	Cwd          string
	Argv         []string
	Start        time.Time
	LaunchCursor output.Cursor
	NextCursor   output.Cursor
	State        State
	Exit         *process.Result
	ExitCode     int
	ExitedAt     time.Time
	RestartCount int
}

// WaitOutcome describes the terminal state observed by Wait.
type WaitOutcome string

const (
	WaitMatched  WaitOutcome = "matched"
	WaitExited   WaitOutcome = "exited"
	WaitTimedOut WaitOutcome = "timed_out"
)

// WaitOptions controls the output and lifecycle condition observed by Wait.
// After is strict-exclusive. A nil After starts at the current launch
// boundary for an explicit same-store restart; fresh launches use the
// initial-cursor rule, which includes output cursor zero.
type WaitOptions struct {
	After *output.Cursor
	Match *regexp.Regexp
}

// WaitResult is the stable result of a Wait call. Exit is populated when the
// process exits before a matching entry is observed.
type WaitResult struct {
	Outcome WaitOutcome
	Cursor  output.Cursor
	Exit    *process.Result
}

// Options configures a Supervisor. A zero OutputLimits value delegates to the
// output package's conservative defaults. A zero StopGrace means that a
// process is killed as soon as the TERM grace check is reached. Use the
// configured project runtime values when a longer default grace is desired.
type Options struct {
	// CompletedLimit is the global bound for terminal records. A zero value
	// uses the default bound; positive values are exact.
	CompletedLimit int
	// CompletedRecords is accepted as a descriptive alias for callers that
	// use the configuration field's name. CompletedLimit takes precedence.
	CompletedRecords int
	StopGrace        time.Duration
	OutputLimits     output.Limits
	MaxLineBytes     int
	Now              func() time.Time

	// StartProcess is the only process seam. It is useful for deterministic
	// lifecycle tests; nil selects process.Start.
	StartProcess func(process.Spec) (Child, error)
	// After supplies the grace timer. It is normally time.After and exists so
	// tests can make TERM/KILL timing deterministic without sleeping.
	After func(time.Duration) <-chan time.Time
}

// Child is the narrow process lifecycle contract consumed by Supervisor.
type Child interface {
	PID() int
	PGID() int
	Done() <-chan struct{}
	Wait() process.Result
	Signal(os.Signal) error
}

var (
	ErrSupervisorClosed = errors.New("supervisor is shut down")
	ErrProcessNotFound  = errors.New("supervised process not found")
	ErrNameInUse        = errors.New("supervised process name is already running")
	ErrInvalidName      = errors.New("invalid supervised process name")
	ErrInvalidRequest   = errors.New("invalid process start request")
	ErrInvalidSignal    = errors.New("invalid process signal")
)

// InvalidNameError identifies a rejected process name.
type InvalidNameError struct {
	Name string
}

func (e *InvalidNameError) Error() string {
	return fmt.Sprintf("%s: %q (want [A-Za-z0-9][A-Za-z0-9._-]{0,63})", ErrInvalidName, e.Name)
}
func (e *InvalidNameError) Unwrap() error { return ErrInvalidName }

// DuplicateError identifies the running process that owns a name.
type DuplicateError struct {
	Root string
	Name string
	PID  int
}

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("%s: %q is already running (PID %d)", ErrNameInUse, e.Name, e.PID)
}
func (e *DuplicateError) Unwrap() error { return ErrNameInUse }

// NotFoundError identifies a project/name lookup that failed.
type NotFoundError struct {
	Root string
	Name string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s: %q in %s", ErrProcessNotFound, e.Name, e.Root)
}
func (e *NotFoundError) Unwrap() error { return ErrProcessNotFound }

// InvalidName reports whether name is safe to use as a portable process name.
func ValidName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	isAlphaNum := func(c byte) bool {
		return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
	}
	if !isAlphaNum(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !isAlphaNum(c) && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// ValidateName returns a typed error for an unsafe name.
func ValidateName(name string) error {
	if !ValidName(name) {
		return &InvalidNameError{Name: name}
	}
	return nil
}

// record contains private relaunch state in addition to the immutable public
// snapshot. It is never returned to callers.
type record struct {
	key  string
	name string
	root string
	cwd  string
	argv []string
	env  []string

	child        Child
	pid          int
	pgid         int
	store        *output.Store
	start        time.Time
	cursor       output.Cursor
	restartCount int
	// launchBoundary identifies a successful same-store Restart. It is
	// deliberately independent of restartCount because ordinary Start
	// replacement creates a fresh output sequence.
	launchBoundary bool
	restarting     bool
	// pendingExit covers an exit published before a failed restart's child
	// reconciliation completed.
	pendingExit bool

	state      State
	result     process.Result
	terminalAt time.Time
	terminal   bool
	done       chan struct{}
	stopMu     sync.Mutex
}

// Supervisor owns all launches independently of client request lifetimes.
type Supervisor struct {
	mu sync.RWMutex

	records  map[string]*record
	starting map[string]struct{}
	complete []*record

	completedLimit int
	stopGrace      time.Duration
	outputLimits   output.Limits
	maxLineBytes   int
	now            func() time.Time
	after          func(time.Duration) <-chan time.Time
	startProcess   func(process.Spec) (Child, error)

	closed          bool
	launches        sync.WaitGroup
	shutdownStarted bool
	shutdownDone    chan struct{}
	shutdownErr     error
}

const (
	defaultCompletedLimit = 20
	defaultMaxLineBytes   = 64 * 1024
)

// New constructs a Supervisor and validates output limits before any launch.
func New(opts Options) (*Supervisor, error) {
	completedLimit := opts.CompletedLimit
	if completedLimit == 0 {
		completedLimit = opts.CompletedRecords
	}
	if completedLimit == 0 {
		completedLimit = defaultCompletedLimit
	}
	if completedLimit < 0 {
		return nil, fmt.Errorf("completed record limit must not be negative: %d", completedLimit)
	}
	if opts.StopGrace < 0 {
		return nil, fmt.Errorf("stop grace must not be negative: %s", opts.StopGrace)
	}
	maxLineBytes := opts.MaxLineBytes
	if maxLineBytes == 0 {
		maxLineBytes = defaultMaxLineBytes
	}
	if maxLineBytes < 0 {
		return nil, fmt.Errorf("max line bytes must be positive: %d", maxLineBytes)
	}
	if _, err := output.NewStore(opts.OutputLimits); err != nil {
		return nil, fmt.Errorf("output limits: %w", err)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	after := opts.After
	if after == nil {
		after = time.After
	}
	starter := opts.StartProcess
	if starter == nil {
		starter = func(spec process.Spec) (Child, error) {
			return process.Start(spec)
		}
	}
	return &Supervisor{
		records:        make(map[string]*record),
		starting:       make(map[string]struct{}),
		completedLimit: completedLimit,
		stopGrace:      opts.StopGrace,
		outputLimits:   opts.OutputLimits,
		maxLineBytes:   maxLineBytes,
		now:            now,
		after:          after,
		startProcess:   starter,
		shutdownDone:   make(chan struct{}),
	}, nil
}

// DiscoverProjectRoot returns the nearest ancestor containing a .git
// directory or worktree file. If no marker exists, it returns the absolute,
// cleaned cwd.
func DiscoverProjectRoot(cwd string) (string, error) {
	path, err := absoluteClean(cwd)
	if err != nil {
		return "", err
	}
	probe := path
	if info, statErr := os.Stat(probe); statErr == nil && !info.IsDir() {
		probe = filepath.Dir(probe)
	}
	for {
		marker := filepath.Join(probe, ".git")
		if info, statErr := os.Stat(marker); statErr == nil {
			if info.IsDir() || info.Mode().IsRegular() {
				return filepath.Clean(probe), nil
			}
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return filepath.Clean(path), nil
		}
		probe = parent
	}
}

// ProjectRoot is a concise alias for DiscoverProjectRoot.
func ProjectRoot(cwd string) (string, error) { return DiscoverProjectRoot(cwd) }

func absoluteClean(path string) (string, error) {
	if path == "" {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("current directory: %w", err)
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("absolute cwd: %w", err)
	}
	return filepath.Clean(absolute), nil
}

func keyFor(root, name string) string { return root + "\x00" + name }

// Start launches a process with direct argv execution and returns its initial
// immutable snapshot. A client disappearing after Start has no lifecycle
// effect; only Stop or Shutdown sends signals.
func (s *Supervisor) Start(req StartRequest) (Process, error) {
	if err := ValidateName(req.Name); err != nil {
		return Process{}, err
	}
	if len(req.Argv) == 0 || req.Argv[0] == "" {
		return Process{}, fmt.Errorf("%w: argv must not be empty", ErrInvalidRequest)
	}
	cwd, err := absoluteClean(req.Cwd)
	if err != nil {
		return Process{}, err
	}
	root, err := DiscoverProjectRoot(cwd)
	if err != nil {
		return Process{}, err
	}
	argv := append([]string(nil), req.Argv...)
	env := append([]string(nil), req.Env...)
	if env == nil {
		env = []string{}
	}
	key := keyFor(root, req.Name)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Process{}, ErrSupervisorClosed
	}
	if current := s.records[key]; current != nil && !current.terminal {
		pid := current.pid
		s.mu.Unlock()
		return Process{}, &DuplicateError{Root: root, Name: req.Name, PID: pid}
	}
	if _, ok := s.starting[key]; ok {
		s.mu.Unlock()
		return Process{}, fmt.Errorf("%w: %q is being started", ErrNameInUse, req.Name)
	}
	s.starting[key] = struct{}{}
	s.launches.Add(1)
	s.mu.Unlock()
	defer s.launches.Done()

	store, err := output.NewStore(s.outputLimits)
	if err != nil {
		s.mu.Lock()
		delete(s.starting, key)
		s.evictLocked()
		s.mu.Unlock()
		return Process{}, fmt.Errorf("output store: %w", err)
	}
	startedAt := s.now()
	specEnv := make([]string, len(env))
	copy(specEnv, env)
	child, err := s.startProcess(process.Spec{
		Dir:          cwd,
		Argv:         append([]string(nil), argv...),
		Env:          specEnv,
		Output:       store,
		MaxLineBytes: s.maxLineBytes,
		Now:          s.now,
	})
	if err != nil {
		s.mu.Lock()
		delete(s.starting, key)
		s.evictLocked()
		s.mu.Unlock()
		return Process{}, err
	}
	if child == nil {
		s.mu.Lock()
		delete(s.starting, key)
		s.evictLocked()
		s.mu.Unlock()
		return Process{}, fmt.Errorf("%w: process starter returned nil child", ErrInvalidRequest)
	}
	pid, pgid := child.PID(), child.PGID()

	s.mu.Lock()
	delete(s.starting, key)
	if s.closed {
		s.evictLocked()
		s.mu.Unlock()
		// Shutdown won the launch race. Kill this unregistered child and wait
		// without holding the registry lock so it cannot be orphaned.
		_ = child.Signal(syscall.SIGKILL)
		<-child.Done()
		_ = child.Wait()
		return Process{}, ErrSupervisorClosed
	}
	restartCount := 0
	if previous := s.records[key]; previous != nil {
		restartCount = previous.restartCount + 1
		s.removeCompletedLocked(previous)
		previous.env = nil
		previous.store = nil
		previous.child = nil
	}
	rec := &record{
		key:            key,
		name:           req.Name,
		root:           root,
		cwd:            cwd,
		argv:           argv,
		env:            env,
		child:          child,
		pid:            pid,
		pgid:           pgid,
		store:          store,
		start:          startedAt,
		cursor:         0,
		restartCount:   restartCount,
		launchBoundary: false,
		state:          StateRunning,
		done:           make(chan struct{}),
	}
	s.records[key] = rec
	initial := rec.snapshotLocked()
	s.mu.Unlock()

	go s.reconcile(rec)
	return initial, nil
}

// Restart stops and relaunches one retained process with its original launch
// specification. The process name and output sequence remain reserved for the
// entire operation.
func (s *Supervisor) Restart(ctx context.Context, cwd, name string) (Process, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rec, err := s.lookup(cwd, name)
	if err != nil {
		return Process{}, err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Process{}, ErrSupervisorClosed
	}
	if s.records[rec.key] != rec || rec.store == nil {
		s.mu.Unlock()
		return Process{}, &NotFoundError{Root: rec.root, Name: rec.name}
	}
	if _, ok := s.starting[rec.key]; ok {
		s.mu.Unlock()
		return Process{}, fmt.Errorf("%w: %q is being started", ErrNameInUse, rec.name)
	}
	restartStore := rec.store
	s.starting[rec.key] = struct{}{}
	rec.restarting = true
	s.launches.Add(1)
	s.mu.Unlock()
	restartSucceeded := false
	defer func() {
		s.mu.Lock()
		delete(s.starting, rec.key)
		rec.restarting = false
		terminal := rec.terminal
		result := rec.result
		if !restartSucceeded && !terminal {
			rec.pendingExit = true
		}
		if !restartSucceeded {
			s.evictLocked()
		}
		s.mu.Unlock()
		if !restartSucceeded && terminal {
			// A follower may have consumed and suppressed the original exit
			// while the restart was in progress. Re-publish it only after the
			// continuation state is cleared so the follower can terminate.
			restartStore.NotifyExit(output.Exit{
				Code: result.ExitCode,
				Time: result.ExitedAt,
			})
		}
		s.launches.Done()
	}()

	rec.stopMu.Lock()
	defer rec.stopMu.Unlock()
	if err := s.stopRecord(ctx, rec); err != nil {
		return Process{}, err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Process{}, ErrSupervisorClosed
	}
	if s.records[rec.key] != rec || rec.store == nil {
		s.mu.Unlock()
		return Process{}, &NotFoundError{Root: rec.root, Name: rec.name}
	}
	store := rec.store
	argv := append([]string(nil), rec.argv...)
	env := append([]string(nil), rec.env...)
	launchCwd := rec.cwd
	s.mu.Unlock()

	launchCursor, err := store.Append(output.System, s.now(), fmt.Sprintf("%s restarted\n", rec.name))
	if err != nil {
		return Process{}, fmt.Errorf("restart marker: %w", err)
	}
	startedAt := s.now()
	child, err := s.startProcess(process.Spec{
		Dir:          launchCwd,
		Argv:         append([]string(nil), argv...),
		Env:          append([]string(nil), env...),
		Output:       store,
		MaxLineBytes: s.maxLineBytes,
		Now:          s.now,
	})
	if err != nil {
		return Process{}, err
	}
	if child == nil {
		return Process{}, fmt.Errorf("%w: process starter returned nil child", ErrInvalidRequest)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = child.Signal(syscall.SIGKILL)
		<-child.Done()
		_ = child.Wait()
		return Process{}, ErrSupervisorClosed
	}
	if s.records[rec.key] != rec {
		s.mu.Unlock()
		_ = child.Signal(syscall.SIGKILL)
		<-child.Done()
		_ = child.Wait()
		return Process{}, &NotFoundError{Root: rec.root, Name: rec.name}
	}
	s.removeCompletedLocked(rec)
	rec.child = child
	rec.pid = child.PID()
	rec.pgid = child.PGID()
	rec.start = startedAt
	rec.cursor = launchCursor
	rec.restartCount++
	rec.launchBoundary = true
	rec.state = StateRunning
	rec.result = process.Result{}
	rec.terminalAt = time.Time{}
	rec.terminal = false
	rec.done = make(chan struct{})
	restarted := rec.snapshotLocked()
	restartSucceeded = true
	s.mu.Unlock()

	go s.reconcile(rec)
	return restarted, nil
}

// Restarting reports whether name is between its stop and relaunch phases.
func (s *Supervisor) Restarting(cwd, name string) bool {
	rec, err := s.lookup(cwd, name)
	if err != nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.records[rec.key] == rec && rec.restarting
}

// FollowContinuesAfter reports whether a follower should suppress an exit
// notification because this name is still being explicitly restarted or has
// already crossed into a successful same-store restart.
func (s *Supervisor) FollowContinuesAfter(cwd, name string, exitedAt time.Time) bool {
	rec, err := s.lookup(cwd, name)
	if err != nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.records[rec.key] != rec {
		return false
	}
	return rec.restarting || (rec.launchBoundary && rec.start.After(exitedAt))
}

func (s *Supervisor) reconcile(rec *record) {
	result := rec.child.Wait()
	if result.ExitedAt.IsZero() {
		result.ExitedAt = s.now()
	}
	s.mu.Lock()
	if rec.terminal || s.records[rec.key] != rec {
		s.mu.Unlock()
		return
	}
	rec.result = result
	rec.terminalAt = result.ExitedAt
	rec.terminal = true
	rec.state = StateExited
	store := rec.store
	republish := rec.pendingExit
	rec.pendingExit = false
	s.insertCompletedLocked(rec)
	close(rec.done)
	s.evictLocked()
	s.mu.Unlock()
	if republish && store != nil {
		store.NotifyExit(output.Exit{Code: result.ExitCode, Time: result.ExitedAt})
	}
}

func (s *Supervisor) insertCompletedLocked(rec *record) {
	index := sort.Search(len(s.complete), func(i int) bool {
		candidate := s.complete[i]
		if rec.terminalAt.Equal(candidate.terminalAt) {
			return rec.key < candidate.key
		}
		return rec.terminalAt.Before(candidate.terminalAt)
	})
	s.complete = append(s.complete, nil)
	copy(s.complete[index+1:], s.complete[index:])
	s.complete[index] = rec
}

func (s *Supervisor) removeCompletedLocked(rec *record) {
	for i, candidate := range s.complete {
		if candidate != rec {
			continue
		}
		copy(s.complete[i:], s.complete[i+1:])
		s.complete[len(s.complete)-1] = nil
		s.complete = s.complete[:len(s.complete)-1]
		return
	}
}

func (s *Supervisor) evictLocked() {
	for len(s.complete) > s.completedLimit {
		index := -1
		for i, rec := range s.complete {
			if rec == nil || !rec.terminal || s.records[rec.key] != rec {
				index = i
				break
			}
			if rec.restarting {
				continue
			}
			if _, reserved := s.starting[rec.key]; reserved {
				continue
			}
			index = i
			break
		}
		if index < 0 {
			return
		}
		rec := s.complete[index]
		copy(s.complete[index:], s.complete[index+1:])
		s.complete[len(s.complete)-1] = nil
		s.complete = s.complete[:len(s.complete)-1]
		if rec == nil || !rec.terminal || s.records[rec.key] != rec {
			continue
		}
		delete(s.records, rec.key)
		// Clearing these references is part of retention, not merely an
		// optimization: env and output may contain secrets and unbounded bytes.
		rec.env = nil
		rec.store = nil
		rec.child = nil
	}
}

func (s *Supervisor) lookup(cwd, name string) (*record, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	root, err := DiscoverProjectRoot(cwd)
	if err != nil {
		return nil, err
	}
	key := keyFor(root, name)
	s.mu.RLock()
	rec := s.records[key]
	s.mu.RUnlock()
	if rec == nil {
		return nil, &NotFoundError{Root: root, Name: name}
	}
	return rec, nil
}

// Get returns a snapshot for a project-scoped name, including retained
// terminal records.
func (s *Supervisor) Get(cwd, name string) (Process, error) {
	rec, err := s.lookup(cwd, name)
	if err != nil {
		return Process{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.records[rec.key] != rec {
		return Process{}, &NotFoundError{Root: rec.root, Name: rec.name}
	}
	return rec.snapshotLocked(), nil
}

// List returns deterministic snapshots for one project root. By default only
// active records are returned; includeCompleted includes retained terminals.
func (s *Supervisor) List(cwd string, includeCompleted bool) ([]Process, error) {
	root, err := DiscoverProjectRoot(cwd)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	items := make([]Process, 0)
	for _, rec := range s.records {
		if rec.root != root || !includeCompleted && rec.terminal {
			continue
		}
		items = append(items, rec.snapshotLocked())
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Start.Before(items[j].Start)
	})
	return items, nil
}

// Output returns the retained output store for one project-scoped process.
// The returned store is safe for concurrent reads and subscriptions. Once a
// terminal record is evicted, future Output calls fail and the supervisor no
// longer retains its store.
func (s *Supervisor) Output(cwd, name string) (*output.Store, error) {
	rec, err := s.lookup(cwd, name)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.records[rec.key] != rec || rec.store == nil {
		return nil, &NotFoundError{Root: rec.root, Name: rec.name}
	}
	return rec.store, nil
}

// Subscribe is a convenience output-access surface for pull clients. It
// snapshots the current record's store and start time before registering the
// subscription, then replays a terminal notification published before that
// registration when it belongs to this process incarnation.
func (s *Supervisor) Subscribe(cwd, name string, opts output.ReadOptions) (*output.Subscription, error) {
	rec, err := s.lookup(cwd, name)
	if err != nil {
		return nil, err
	}

	// Capture the store and process start under the registry lock. A terminal
	// record may be evicted after this lock is released, but the subscription
	// keeps the captured store alive independently.
	s.mu.RLock()
	if s.records[rec.key] != rec || rec.store == nil {
		root, processName := rec.root, rec.name
		s.mu.RUnlock()
		return nil, &NotFoundError{Root: root, Name: processName}
	}
	store, start := rec.store, rec.start
	s.mu.RUnlock()

	sub := store.Subscribe(opts)
	sub.ReplayLatestExitSince(start)
	return sub, nil
}

// Wait blocks until output matching opts.Match is observed, the process exits,
// or ctx reaches its deadline. It consumes a private subscription from the
// process launch cursor by default, so concurrent waiters do not interfere.
// A nil Match waits for process exit after draining output through its exit
// watermark.
func (s *Supervisor) Wait(ctx context.Context, cwd, name string, opts WaitOptions) (WaitResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rec, err := s.lookup(cwd, name)
	if err != nil {
		return WaitResult{}, err
	}

	// Capture one process incarnation under the registry lock. The record may
	// be evicted or replaced after this point; retaining its store and launch
	// metadata keeps this wait tied to the looked-up incarnation.
	s.mu.RLock()
	if s.records[rec.key] != rec || rec.store == nil {
		root, processName := rec.root, rec.name
		s.mu.RUnlock()
		return WaitResult{}, &NotFoundError{Root: root, Name: processName}
	}
	store, start, launchCursor, launchBoundary := rec.store, rec.start, rec.cursor, rec.launchBoundary
	s.mu.RUnlock()

	after := opts.After
	if after == nil && launchBoundary {
		after = &launchCursor
	}
	sub := store.Subscribe(output.ReadOptions{After: after, Match: opts.Match, MaxBytes: s.maxLineBytes})
	sub.ReplayLatestExitSince(start)
	defer sub.Close()

	for {
		event, err := sub.Next(ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return WaitResult{Outcome: WaitTimedOut, Cursor: sub.Cursor()}, nil
			}
			return WaitResult{}, err
		}
		if event.Read != nil {
			// A filtered read can consume metadata and nonmatching entries
			// without yielding an entry. Only an actual matching entry ends
			// the wait.
			if opts.Match != nil && len(event.Read.Entries) != 0 {
				return WaitResult{Outcome: WaitMatched, Cursor: sub.Cursor()}, nil
			}
			continue
		}
		if event.Exit == nil {
			continue
		}

		// NotifyExit wakes the subscription before Supervisor.reconcile closes
		// rec.done. Prefer the reconciled result when it is already available,
		// and otherwise preserve the exact exit code/time from the event.
		exitResult := process.Result{ExitCode: event.Exit.Code, ExitedAt: event.Exit.Time}
		s.mu.RLock()
		if rec.terminal {
			exitResult = rec.result
		}
		s.mu.RUnlock()
		return WaitResult{
			Outcome: WaitExited,
			Cursor:  sub.Cursor(),
			Exit:    &exitResult,
		}, nil
	}
}

// Signal forwards sig to the process group. Client cancellation is not
// involved; this method is an explicit lifecycle operation.
func (s *Supervisor) Signal(cwd, name string, sig os.Signal) error {
	if sig == nil {
		return ErrInvalidSignal
	}
	rec, err := s.lookup(cwd, name)
	if err != nil {
		return err
	}
	s.mu.RLock()
	if s.records[rec.key] != rec || rec.terminal || rec.child == nil {
		s.mu.RUnlock()
		return nil
	}
	child := rec.child
	s.mu.RUnlock()
	return child.Signal(sig)
}

// Stop sends SIGTERM to one group, waits at most StopGrace, then sends
// SIGKILL only if the group is still active. It always waits for the
// supervisor's terminal reconciliation after a successful stop sequence.
func (s *Supervisor) Stop(ctx context.Context, cwd, name string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	rec, err := s.lookup(cwd, name)
	if err != nil {
		return err
	}
	rec.stopMu.Lock()
	defer rec.stopMu.Unlock()
	return s.stopRecord(ctx, rec)
}

func (s *Supervisor) stopRecord(ctx context.Context, rec *record) error {
	s.mu.RLock()
	if s.records[rec.key] != rec || rec.terminal || rec.child == nil {
		s.mu.RUnlock()
		return nil
	}
	child := rec.child
	s.mu.RUnlock()

	termErr := child.Signal(syscall.SIGTERM)
	if signalMeansDone(termErr) {
		_, waitErr := s.waitForDone(ctx, rec, -1)
		return waitErr
	}
	if done, waitErr := s.waitForDone(ctx, rec, s.stopGrace); done || waitErr != nil {
		if waitErr != nil {
			return waitErr
		}
		if termErr != nil {
			return termErr
		}
		return nil
	}

	s.mu.RLock()
	stillActive := s.records[rec.key] == rec && !rec.terminal && rec.child != nil
	s.mu.RUnlock()
	if stillActive {
		killErr := child.Signal(syscall.SIGKILL)
		if killErr != nil && !signalMeansDone(killErr) {
			// Continue waiting: the child may have exited between the active
			// check and the KILL syscall, but preserve a real signal failure.
			if done, waitErr := s.waitForDone(ctx, rec, -1); waitErr != nil {
				return waitErr
			} else if !done {
				return killErr
			}
			return killErr
		}
	}
	_, waitErr := s.waitForDone(ctx, rec, -1)
	if waitErr != nil {
		return waitErr
	}
	return nil
}

func signalMeansDone(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}

// waitForDone waits for a terminal transition. A negative duration means
// wait without a timer. The context only bounds this method's waiting; it is
// never used as an implicit signal request.
func (s *Supervisor) waitForDone(ctx context.Context, rec *record, duration time.Duration) (bool, error) {
	if duration < 0 {
		select {
		case <-rec.done:
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	if duration == 0 {
		select {
		case <-rec.done:
			return true, nil
		default:
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
			return false, nil
		}
	}
	timer := s.after(duration)
	select {
	case <-rec.done:
		return true, nil
	case <-timer:
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// Shutdown rejects new launches, stops every active group, and waits for each
// terminal reconciliation. Concurrent callers share the first shutdown's
// result; caller cancellation is reported only after shared cleanup finishes.
func (s *Supervisor) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.shutdownStarted {
		done := s.shutdownDone
		s.mu.Unlock()
		<-done
		s.mu.RLock()
		err := s.shutdownErr
		s.mu.RUnlock()
		if callerErr := ctx.Err(); callerErr != nil {
			return errors.Join(err, callerErr)
		}
		return err
	}
	s.shutdownStarted = true
	s.closed = true
	s.mu.Unlock()

	// Let launches which crossed the closed check finish their non-registry
	// cleanup before selecting active records.
	s.launches.Wait()
	s.mu.RLock()
	active := make([]*record, 0)
	for _, rec := range s.records {
		if !rec.terminal {
			active = append(active, rec)
		}
	}
	s.mu.RUnlock()

	// Shutdown owns cleanup after it begins. Do not let the initiating
	// caller's cancellation interrupt TERM, KILL, or terminal reconciliation.
	cleanupCtx := context.Background()
	var wg sync.WaitGroup
	errCh := make(chan error, len(active))
	for _, rec := range active {
		wg.Add(1)
		go func(rec *record) {
			defer wg.Done()
			if err := s.stopRecord(cleanupCtx, rec); err != nil {
				errCh <- err
			}
		}(rec)
	}
	wg.Wait()
	close(errCh)
	var shutdownErr error
	for err := range errCh {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	s.mu.Lock()
	s.shutdownErr = shutdownErr
	close(s.shutdownDone)
	s.mu.Unlock()
	if callerErr := ctx.Err(); callerErr != nil {
		return errors.Join(shutdownErr, callerErr)
	}
	return shutdownErr
}

func (r *record) snapshotLocked() Process {
	model := Process{
		Name:         r.name,
		Root:         r.root,
		PID:          r.pid,
		PGID:         r.pgid,
		Cwd:          r.cwd,
		Argv:         append([]string(nil), r.argv...),
		Start:        r.start,
		LaunchCursor: r.cursor,
		State:        r.state,
		RestartCount: r.restartCount,
	}
	if r.store != nil {
		model.NextCursor = r.store.NextCursor()
	}
	if r.terminal {
		result := r.result
		model.Exit = &result
		model.ExitCode = result.ExitCode
		model.ExitedAt = result.ExitedAt
	}
	return model
}

// String is useful in logs without exposing private launch state.
func (p Process) String() string {
	return strings.Join([]string{p.Root, p.Name, string(p.State)}, ":")
}
