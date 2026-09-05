// Package app owns the in-process supervisor registry.  It deliberately keeps
// process launch details behind internal/process while exposing immutable
// snapshots to callers.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// ReadinessConfig describes an output expression used to mark a manifest
// process ready. Match is compiled once when the process is admitted.
type ReadinessConfig struct {
	Match   string
	Timeout time.Duration
}

// Readiness is the response-safe readiness state of a running resolved
// process. Cursor is the first matching output cursor for this incarnation.
type Readiness struct {
	State  string
	Cursor *output.Cursor
	Time   time.Time
	Match  string
}

const (
	ReadinessStarting          = "starting"
	ReadinessReady             = "ready"
	ReadinessRunningUnverified = "running_unverified"
)

// StartRequest describes one direct-argv launch. Env is copied and is never
// inherited from the supervisor when it is nil or empty. Root, when present,
// is the explicit manifest project root used for record keying; Cwd remains
// only the child working directory.
type StartRequest struct {
	Name    string
	Source  string
	Root    string
	Cwd     string
	Argv    []string
	Env     []string
	Ready   *ReadinessConfig
	TTY     bool
	TTYSize *TTYSize
}

// TTYSize is a pseudo-terminal size in character cells.
type TTYSize struct {
	Columns uint16
	Rows    uint16
}

// Process is an immutable read model. It intentionally has no environment or
// output-store field; callers use Supervisor.Output for output access.
type Process struct {
	Name         string
	Source       string
	Root         string
	TTY          bool
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
	Followers    int
	Readiness    *Readiness
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

// RestartOptions optionally replaces a retained launch specification. Update
// must be true to apply any fields; a zero-value option preserves the
// historical ad-hoc restart behavior. Root, when present, is the explicit
// manifest root used to locate and retain the record key.
type RestartOptions struct {
	Update  bool
	Source  string
	Root    string
	Cwd     string
	Argv    []string
	Env     []string
	Ready   *ReadinessConfig
	TTY     bool
	TTYSize *TTYSize
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

// InputChild is the optional child capability used by TTY input leases. The
// legacy Child seam remains intentionally small so deterministic non-TTY test
// children do not need to implement terminal methods.
type InputChild interface {
	Write([]byte) (int, error)
	Resize(uint16, uint16) error
}

// ContextInputWriter is implemented by children whose PTY write can be
// interrupted without closing the PTY. The legacy InputChild interface remains
// supported for deterministic test children and other callers.
type ContextInputWriter interface {
	WriteContext(context.Context, []byte) (int, error)
}

// ContextInputResizer is the cancellation-aware form of terminal resize.
type ContextInputResizer interface {
	ResizeContext(context.Context, uint16, uint16) error
}

var (
	ErrSupervisorClosed = errors.New("supervisor is shut down")
	ErrProcessNotFound  = errors.New("supervised process not found")
	ErrNameInUse        = errors.New("supervised process name is already running")
	ErrInvalidName      = errors.New("invalid supervised process name")
	ErrInvalidRequest   = errors.New("invalid process start request")
	ErrInvalidSignal    = errors.New("invalid process signal")
	ErrInputConflict    = errors.New("tty input is already owned")
	ErrInputTooLarge    = errors.New("tty input is too large")
	ErrInputClosed      = errors.New("tty input is closed")
	ErrInputStopped     = errors.New("tty input incarnation has stopped")
	ErrInputStale       = errors.New("tty input targets a stale incarnation")
	ErrInputNotTTY      = errors.New("process does not have a tty")
)

const maxInputBytes = 32 * 1024

// InputState identifies the incarnation currently targeted by an input lease.
type InputState string

const (
	InputRunning InputState = "running"
	InputStopped InputState = "stopped"
)

// InputEvent is emitted once on acquisition and whenever the retained session
// crosses a launch or exit boundary.
type InputEvent struct {
	State        InputState
	LaunchCursor output.Cursor
	TTY          bool
}

// InputConflictError identifies the existing owner of a TTY input lease.
type InputConflictError struct{ Name string }

func (e *InputConflictError) Error() string {
	if e == nil || e.Name == "" {
		return ErrInputConflict.Error()
	}
	return fmt.Sprintf("%s: %q", ErrInputConflict, e.Name)
}
func (e *InputConflictError) Unwrap() error { return ErrInputConflict }

// InputTooLargeError reports an atomic input write rejected before any bytes
// reach the child.
type InputTooLargeError struct{ Size, Limit int }

func (e *InputTooLargeError) Error() string {
	if e == nil {
		return ErrInputTooLarge.Error()
	}
	return fmt.Sprintf("%s: %d bytes exceeds %d-byte limit", ErrInputTooLarge, e.Size, e.Limit)
}
func (e *InputTooLargeError) Unwrap() error { return ErrInputTooLarge }

// InputStoppedError identifies input discarded because its process
// incarnation exited. It remains an input-closed error for callers that only
// need the stable closed classification, while allowing the CLI to retain the
// durable lease instead of detaching it.
type InputStoppedError struct{}

func (InputStoppedError) Error() string { return ErrInputStopped.Error() }
func (InputStoppedError) Unwrap() error { return ErrInputClosed }
func (InputStoppedError) Is(target error) bool {
	return target == ErrInputStopped || target == ErrInputClosed
}

// InputStaleError identifies a write or resize for an earlier launch cursor.
type InputStaleError struct{ Want, Current output.Cursor }

func (e *InputStaleError) Error() string {
	if e == nil {
		return ErrInputStale.Error()
	}
	return fmt.Sprintf("%s: launch cursor %d is not current cursor %d", ErrInputStale, e.Want, e.Current)
}
func (e *InputStaleError) Unwrap() error { return ErrInputStale }

// InputLease owns one exclusive TTY attachment. Ordinary process exit leaves
// the lease open so its owner can target a successor incarnation.
type InputLease struct {
	supervisor *Supervisor
	rec        *record

	// events is the public delivery channel. emit only appends to eventQueue;
	// the dispatcher is the sole sender, so a slow owner never makes a
	// supervisor lifecycle transition block and no transition is discarded.
	events      chan InputEvent
	eventMu     sync.Mutex
	eventQueue  []InputEvent
	eventNotify chan struct{}
	eventClosed bool

	closed            chan struct{}
	mu                sync.Mutex
	closedFlag        bool
	incarnationDone   chan struct{}
	incarnationClosed bool
}

func (l *InputLease) Events() <-chan InputEvent {
	if l == nil {
		return nil
	}
	return l.events
}

// Done is closed when the durable input lease is released. Process exit only
// closes the current incarnation and intentionally leaves Done open.
func (l *InputLease) Done() <-chan struct{} {
	if l == nil {
		return nil
	}
	return l.closed
}

func (l *InputLease) Next(ctx context.Context) (InputEvent, error) {
	if l == nil {
		return InputEvent{}, ErrInputClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-l.closed:
		return InputEvent{}, ErrInputClosed
	default:
	}
	select {
	case event, ok := <-l.events:
		if !ok {
			return InputEvent{}, ErrInputClosed
		}
		return event, nil
	case <-l.closed:
		return InputEvent{}, ErrInputClosed
	case <-ctx.Done():
		return InputEvent{}, ctx.Err()
	}
}
func (l *InputLease) Release() {
	if l == nil || l.supervisor == nil || l.rec == nil {
		return
	}
	l.supervisor.releaseInput(l)
}
func (l *InputLease) close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	if !l.closedFlag {
		l.closedFlag = true
		close(l.closed)
		if !l.incarnationClosed {
			close(l.incarnationDone)
			l.incarnationClosed = true
		}
	}
	l.mu.Unlock()

	l.eventMu.Lock()
	l.eventClosed = true
	select {
	case l.eventNotify <- struct{}{}:
	default:
	}
	l.eventMu.Unlock()
}

// dispatchEvents drains the unbounded-in-practice queue into the compatibility
// channel returned by Events. A release may stop delivery of events that were
// queued after the lease was closed, but every launch/exit event is retained
// and delivered while the durable lease remains open.
func (l *InputLease) dispatchEvents() {
	defer close(l.events)
	for {
		l.eventMu.Lock()
		if len(l.eventQueue) != 0 {
			event := l.eventQueue[0]
			l.eventQueue = l.eventQueue[1:]
			if len(l.eventQueue) == 0 {
				l.eventQueue = nil
			}
			l.eventMu.Unlock()
			select {
			case l.events <- event:
			case <-l.closed:
				return
			}
			continue
		}
		closed := l.eventClosed
		l.eventMu.Unlock()
		if closed {
			return
		}
		select {
		case <-l.eventNotify:
		case <-l.closed:
			return
		}
	}
}

func (l *InputLease) beginIncarnation() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closedFlag || !l.incarnationClosed {
		return
	}
	l.incarnationDone = make(chan struct{})
	l.incarnationClosed = false
}

func (l *InputLease) endIncarnation() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.incarnationClosed {
		close(l.incarnationDone)
		l.incarnationClosed = true
	}
}

func (l *InputLease) incarnationChannel() <-chan struct{} {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.incarnationDone
}

func (l *InputLease) Close() { l.Release() }

// Write forwards one cursor-scoped byte payload. The variadic form accepts
// (cursor, bytes) and (context, cursor, bytes), keeping call sites concise.
func (l *InputLease) Write(args ...any) error {
	ctx, cursor, data, err := parseInputWriteArgs(args...)
	if err != nil {
		return err
	}
	return l.supervisor.writeInput(ctx, l, cursor, data)
}

// Resize applies one cursor-scoped terminal size. It accepts
// (cursor, columns, rows) or (context, cursor, columns, rows).
func (l *InputLease) Resize(args ...any) error {
	ctx, cursor, columns, rows, err := parseInputResizeArgs(args...)
	if err != nil {
		return err
	}
	return l.supervisor.resizeInput(ctx, l, cursor, columns, rows)
}

func parseInputWriteArgs(args ...any) (context.Context, output.Cursor, []byte, error) {
	ctx := context.Background()
	if len(args) == 3 {
		if value, ok := args[0].(context.Context); ok && value != nil {
			ctx = value
		}
		args = args[1:]
	}
	if len(args) != 2 {
		return nil, 0, nil, fmt.Errorf("%w: input write arguments", ErrInvalidRequest)
	}
	cursor, ok := inputCursor(args[0])
	if !ok {
		return nil, 0, nil, fmt.Errorf("%w: input cursor", ErrInvalidRequest)
	}
	data, ok := args[1].([]byte)
	if !ok {
		return nil, 0, nil, fmt.Errorf("%w: input bytes", ErrInvalidRequest)
	}
	return ctx, cursor, append([]byte(nil), data...), nil
}
func parseInputResizeArgs(args ...any) (context.Context, output.Cursor, uint16, uint16, error) {
	ctx := context.Background()
	if len(args) == 4 {
		if value, ok := args[0].(context.Context); ok && value != nil {
			ctx = value
		}
		args = args[1:]
	}
	if len(args) != 3 {
		return nil, 0, 0, 0, fmt.Errorf("%w: input resize arguments", ErrInvalidRequest)
	}
	cursor, ok := inputCursor(args[0])
	if !ok {
		return nil, 0, 0, 0, fmt.Errorf("%w: input cursor", ErrInvalidRequest)
	}
	columns, ok := inputUint16(args[1])
	if !ok {
		return nil, 0, 0, 0, fmt.Errorf("%w: input columns", ErrInvalidRequest)
	}
	rows, ok := inputUint16(args[2])
	if !ok {
		return nil, 0, 0, 0, fmt.Errorf("%w: input rows", ErrInvalidRequest)
	}
	return ctx, cursor, columns, rows, nil
}
func inputCursor(value any) (output.Cursor, bool) {
	switch typed := value.(type) {
	case output.Cursor:
		return typed, true
	case uint64:
		return output.Cursor(typed), true
	case int:
		if typed >= 0 {
			return output.Cursor(typed), true
		}
	}
	return 0, false
}
func inputUint16(value any) (uint16, bool) {
	switch typed := value.(type) {
	case uint16:
		return typed, true
	case int:
		if typed >= 0 && typed <= 65535 {
			return uint16(typed), true
		}
	case uint:
		if typed <= 65535 {
			return uint16(typed), true
		}
	}
	return 0, false
}

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

// readinessTracker observes append events directly so the first matching
// cursor is captured before a later append can evict the matching entry.
// observe is called by output.Store while its lock is held and therefore must
// never call back into Store.
type readinessTracker struct {
	pattern  *regexp.Regexp
	after    output.Cursor
	hasAfter bool

	mu       sync.Mutex
	ready    bool
	cursor   output.Cursor
	at       time.Time
	once     sync.Once
	observer *output.AppendObserver
}

func newReadinessTracker(store *output.Store, pattern *regexp.Regexp, after output.Cursor, hasAfter bool) *readinessTracker {
	if store == nil || pattern == nil {
		return nil
	}
	tracker := &readinessTracker{
		pattern: pattern, after: after, hasAfter: hasAfter,
	}
	tracker.observer = store.ObserveAppend(tracker.observe)
	return tracker
}

func (t *readinessTracker) observe(entry output.Entry) {
	if t == nil || (t.hasAfter && entry.Cursor <= t.after) || !t.pattern.MatchString(entry.Text) {
		return
	}
	t.mu.Lock()
	if !t.ready {
		t.ready = true
		t.cursor = entry.Cursor
		t.at = entry.Time
	}
	t.mu.Unlock()
}

func (t *readinessTracker) snapshot() (bool, output.Cursor, time.Time) {
	if t == nil {
		return false, 0, time.Time{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ready, t.cursor, t.at
}

func (t *readinessTracker) close() {
	if t == nil {
		return
	}
	t.once.Do(func() {
		if t.observer != nil {
			t.observer.Close()
		}
	})
}

// record contains private relaunch state in addition to the immutable public
// snapshot. It is never returned to callers.
type record struct {
	key    string
	name   string
	root   string
	cwd    string
	source string
	argv   []string
	env    []string

	readyConfig  *ReadinessConfig
	readyPattern *regexp.Regexp
	tracker      *readinessTracker
	tty          bool
	ttySize      *TTYSize
	input        *InputLease
	inputMu      sync.Mutex
	inputOp      *inputOperation
	// incarnation changes for every successful launch, including same-store
	// restarts. Each incarnation owns one tracker; an old tracker can never
	// update a later launch.
	incarnation uint64

	child        Child
	pid          int
	pgid         int
	store        *output.Store
	start        time.Time
	cursor       output.Cursor
	restartCount int
	followers    int
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

// inputOperation represents one child write or resize. The done channel is
// closed by the operation goroutine after the child call returns. Legacy
// InputChild implementations cannot be interrupted, so their operation stays
// registered until it drains; this prevents a later owner from observing the
// lease as free while an old syscall can still touch the PTY.
type inputOperation struct {
	done   chan struct{}
	legacy bool
}

func (r *record) currentInputOperation() *inputOperation {
	if r == nil {
		return nil
	}
	r.inputMu.Lock()
	operation := r.inputOp
	r.inputMu.Unlock()
	return operation
}

func (r *record) beginInputOperation(lease *InputLease, incarnationDone <-chan struct{}, legacy bool) (*inputOperation, <-chan struct{}, bool) {
	if r == nil || lease == nil || inputLeaseClosed(lease) || inputChannelClosed(incarnationDone) {
		return nil, nil, false
	}
	r.inputMu.Lock()
	defer r.inputMu.Unlock()
	if inputLeaseClosed(lease) || inputChannelClosed(incarnationDone) {
		return nil, nil, false
	}
	if r.inputOp != nil {
		return nil, r.inputOp.done, true
	}
	operation := &inputOperation{done: make(chan struct{}), legacy: legacy}
	r.inputOp = operation
	return operation, nil, true
}

func (r *record) finishInputOperation(operation *inputOperation) {
	if r == nil || operation == nil {
		return
	}
	r.inputMu.Lock()
	if r.inputOp == operation {
		r.inputOp = nil
		close(operation.done)
	}
	r.inputMu.Unlock()
}

// waitInputOperations waits without holding inputMu. It is used before a
// replacement incarnation is launched, after the old incarnation is already
// terminal, so an operation that was admitted before exit cannot reach the
// successor. Remove and shutdown deliberately do not wait on non-context
// legacy calls because their records are discarded and no successor can reuse
// that child.
func (r *record) waitInputOperations() {
	for {
		operation := r.currentInputOperation()
		if operation == nil {
			return
		}
		<-operation.done
	}
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

func (s *Supervisor) trackStore(key string, store *output.Store) {
	store.SetIdleCallback(func() {
		s.mu.Lock()
		rec := s.records[key]
		if rec != nil && rec.store == store && rec.terminal && rec.incarnation == 0 {
			delete(s.records, key)
		} else {
			s.evictLocked()
		}
		s.mu.Unlock()
	})
}

// Start launches a process with direct argv execution and returns its initial
// immutable snapshot. A client disappearing after Start has no lifecycle
// effect; only Stop or Shutdown sends signals.
func (s *Supervisor) Start(req StartRequest) (Process, error) {
	if err := ValidateName(req.Name); err != nil {
		return Process{}, err
	}
	requestCwd, err := absoluteClean(req.Cwd)
	if err != nil {
		return Process{}, err
	}
	root := ""
	if req.Root != "" {
		root, err = absoluteClean(req.Root)
	} else {
		root, err = DiscoverProjectRoot(requestCwd)
	}
	if err != nil {
		return Process{}, err
	}
	key := keyFor(root, req.Name)

	var readyConfig *ReadinessConfig
	var readyPattern *regexp.Regexp
	if req.Ready != nil {
		config := *req.Ready
		compiled, compileErr := regexp.Compile(config.Match)
		if compileErr != nil {
			return Process{}, fmt.Errorf("%w: readiness match: %v", ErrInvalidRequest, compileErr)
		}
		readyConfig, readyPattern = &config, compiled
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Process{}, ErrSupervisorClosed
	}
	if current := s.records[key]; current != nil && !current.terminal {
		pid := current.pid
		if req.TTY && !current.tty {
			s.mu.Unlock()
			return Process{}, fmt.Errorf("%w: %q is running without a tty; stop it and rerun with --tty", ErrInputNotTTY, req.Name)
		}
		s.mu.Unlock()
		return Process{}, &DuplicateError{Root: root, Name: req.Name, PID: pid}
	}
	if _, ok := s.starting[key]; ok {
		s.mu.Unlock()
		return Process{}, fmt.Errorf("%w: %q is being started", ErrNameInUse, req.Name)
	}
	rec := s.records[key]
	var inputToClose *InputLease
	if rec == nil {
		if len(req.Argv) == 0 || req.Argv[0] == "" {
			s.mu.Unlock()
			return Process{}, fmt.Errorf("%w: argv must not be empty", ErrInvalidRequest)
		}
		store, storeErr := output.NewStore(s.outputLimits)
		if storeErr != nil {
			s.mu.Unlock()
			return Process{}, fmt.Errorf("output store: %w", storeErr)
		}
		done := make(chan struct{})
		close(done)
		rec = &record{key: key, name: req.Name, root: root, store: store, state: StateExited, terminal: true, done: done}
		s.trackStore(key, store)
		s.records[key] = rec
	}
	if len(req.Argv) != 0 {
		if req.Argv[0] == "" {
			s.mu.Unlock()
			return Process{}, fmt.Errorf("%w: argv must not be empty", ErrInvalidRequest)
		}
		rec.cwd = requestCwd
		rec.source = req.Source
		rec.argv = append([]string(nil), req.Argv...)
		rec.env = append([]string{}, req.Env...)
		rec.readyConfig, rec.readyPattern = readyConfig, readyPattern
		// An explicit argv replaces a stopped retained definition, including
		// its TTY choice. Calls without argv intentionally retain it.
		rec.tty = req.TTY
		if !req.TTY {
			rec.ttySize = nil
		}
		if req.TTYSize != nil {
			size := *req.TTYSize
			if size.Columns == 0 || size.Rows == 0 {
				s.mu.Unlock()
				return Process{}, fmt.Errorf("%w: tty size must have non-zero columns and rows", ErrInvalidRequest)
			}
			rec.ttySize = &size
		}
		if !req.TTY && rec.input != nil {
			// Changing a retained TTY definition to non-TTY must retire its
			// owner before the new incarnation is started. Keep this under the
			// registry lock so no attach can observe the non-TTY record with an
			// old lease.
			inputToClose = rec.input
			rec.input = nil
			rec.ttySize = nil
			inputToClose.close()
		}
	} else if len(rec.argv) == 0 {
		s.mu.Unlock()
		return Process{}, fmt.Errorf("%w: retained launch specification is empty", ErrInvalidRequest)
	}
	store := rec.store
	argv := append([]string(nil), rec.argv...)
	env := append([]string{}, rec.env...)
	launchCwd := rec.cwd
	pattern := rec.readyPattern
	source := rec.source
	tty := rec.tty
	var ttySize *TTYSize
	if rec.ttySize != nil {
		size := *rec.ttySize
		ttySize = &size
	}
	wasLaunched := rec.incarnation != 0
	s.starting[key] = struct{}{}
	s.launches.Add(1)
	s.mu.Unlock()
	defer s.launches.Done()
	if inputToClose != nil {
		closeInputLease(inputToClose)
	}
	// A stopped retained record may still have a legacy input call draining.
	// Do not launch a successor until that call has returned.
	rec.waitInputOperations()

	launchCursor := output.Cursor(0)
	launchBoundary := wasLaunched
	if next := store.NextCursor(); next != 0 {
		launchCursor = next - 1
	}
	var (
		markerOnce sync.Once
		markerErr  error
		tracker    *readinessTracker
	)
	markStarted := func() error {
		markerOnce.Do(func() {
			if wasLaunched || store.SubscriberCount() != 0 || tty {
				marker := fmt.Sprintf("%s launched\n", req.Name)
				launchCursor, markerErr = store.Append(output.System, s.now(), marker)
				launchBoundary = markerErr == nil
			}
			if markerErr == nil && source != "" && pattern != nil {
				tracker = newReadinessTracker(store, pattern, launchCursor, launchBoundary)
			}
		})
		return markerErr
	}
	startedAt := s.now()
	var processTTYSize *process.TTYSize
	if ttySize != nil {
		processTTYSize = &process.TTYSize{Columns: ttySize.Columns, Rows: ttySize.Rows}
	}
	child, err := s.startProcess(process.Spec{Dir: launchCwd, Argv: argv, Env: env, Output: store, MaxLineBytes: s.maxLineBytes, Now: s.now, Started: markStarted, TTY: tty, TTYSize: processTTYSize})
	if err == nil && child != nil {
		err = markStarted()
	}
	if err != nil || child == nil {
		if tracker != nil {
			tracker.close()
		}
		s.mu.Lock()
		delete(s.starting, key)
		s.evictLocked()
		s.mu.Unlock()
		if err != nil {
			return Process{}, err
		}
		return Process{}, fmt.Errorf("%w: process starter returned nil child", ErrInvalidRequest)
	}

	s.mu.Lock()
	delete(s.starting, key)
	if s.closed || s.records[key] != rec {
		s.mu.Unlock()
		if tracker != nil {
			tracker.close()
		}
		_ = child.Signal(syscall.SIGKILL)
		<-child.Done()
		_ = child.Wait()
		if s.closed {
			return Process{}, ErrSupervisorClosed
		}
		return Process{}, &NotFoundError{Root: root, Name: req.Name}
	}
	s.removeCompletedLocked(rec)
	rec.child, rec.pid, rec.pgid = child, child.PID(), child.PGID()
	rec.start, rec.cursor = startedAt, launchCursor
	if wasLaunched {
		rec.restartCount++
	}
	rec.launchBoundary = launchBoundary
	rec.state, rec.result, rec.terminalAt, rec.terminal = StateRunning, process.Result{}, time.Time{}, false
	rec.done = make(chan struct{})
	rec.incarnation++
	rec.tracker = tracker
	input := rec.input
	if input != nil {
		input.beginIncarnation()
	}
	started := rec.snapshotLocked()
	s.mu.Unlock()
	if input != nil {
		input.emit(InputEvent{State: InputRunning, LaunchCursor: launchCursor, TTY: tty})
	}
	go s.reconcile(rec)
	return started, nil
}

// Restart stops and relaunches one retained process with its original launch
// specification. The process name and output sequence remain reserved for the
// entire operation.
func (s *Supervisor) Restart(ctx context.Context, cwd, name string, options ...RestartOptions) (Process, error) {
	if len(options) > 1 {
		return Process{}, fmt.Errorf("%w: restart accepts at most one options value", ErrInvalidRequest)
	}
	var update RestartOptions
	if len(options) == 1 {
		update = options[0]
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rec, err := s.lookupRoot(cwd, name, update.Root)
	if err != nil {
		return Process{}, err
	}
	var (
		updatedCwd      string
		updatedArgv     []string
		updatedEnv      []string
		updatedReady    *ReadinessConfig
		updatedPattern  *regexp.Regexp
		updatedTTY      bool
		updatedTTYSize  *TTYSize
		previousTracker *readinessTracker
	)
	if update.Update {
		updatedArgv = append([]string(nil), update.Argv...)
		if len(updatedArgv) == 0 || updatedArgv[0] == "" {
			return Process{}, fmt.Errorf("%w: restart argv must not be empty", ErrInvalidRequest)
		}
		updatedCwd = update.Cwd
		if updatedCwd == "" {
			updatedCwd = rec.cwd
		}
		updatedCwd, err = absoluteClean(updatedCwd)
		if err != nil {
			return Process{}, err
		}
		updatedRoot := update.Root
		if updatedRoot != "" {
			updatedRoot, err = absoluteClean(updatedRoot)
		} else {
			updatedRoot, err = DiscoverProjectRoot(updatedCwd)
		}
		if err != nil {
			return Process{}, err
		}
		if updatedRoot != rec.root {
			return Process{}, fmt.Errorf("%w: restart root %q does not match project %q", ErrInvalidRequest, updatedRoot, rec.root)
		}
		updatedEnv = append([]string(nil), update.Env...)
		if updatedEnv == nil {
			updatedEnv = []string{}
		}
		if update.Ready != nil {
			config := *update.Ready
			compiled, compileErr := regexp.Compile(config.Match)
			if compileErr != nil {
				return Process{}, fmt.Errorf("%w: readiness match: %v", ErrInvalidRequest, compileErr)
			}
			updatedReady = &config
			updatedPattern = compiled
		}
		updatedTTY = update.TTY
		if update.TTYSize != nil {
			size := *update.TTYSize
			if size.Columns == 0 || size.Rows == 0 {
				return Process{}, fmt.Errorf("%w: tty size must have non-zero columns and rows", ErrInvalidRequest)
			}
			updatedTTYSize = &size
		}
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
	restartStore := rec.store
	s.starting[rec.key] = struct{}{}
	rec.restarting = true
	previousTracker = rec.tracker
	rec.tracker = nil
	s.launches.Add(1)
	s.mu.Unlock()
	if previousTracker != nil {
		previousTracker.close()
	}
	restartSucceeded := false
	var launchTracker *readinessTracker
	var inputToClose *InputLease
	defer func() {
		if !restartSucceeded && launchTracker != nil {
			launchTracker.close()
		}
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
	// stopRecord has reconciled the old incarnation and closed its cancellation
	// channel. Drain any operation admitted before that boundary before a new
	// child can be assigned to this record.
	rec.waitInputOperations()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Process{}, ErrSupervisorClosed
	}
	if s.records[rec.key] != rec || rec.store == nil {
		s.mu.Unlock()
		return Process{}, &NotFoundError{Root: rec.root, Name: rec.name}
	}
	// A definition update is committed while the name remains reserved by
	// this restart. No concurrent Start or Restart can observe a partially
	// updated launch specification.
	if update.Update {
		rec.cwd = updatedCwd
		rec.source = update.Source
		rec.argv = append([]string(nil), updatedArgv...)
		rec.env = append([]string(nil), updatedEnv...)
		rec.readyConfig = updatedReady
		rec.readyPattern = updatedPattern
		rec.tty = updatedTTY
		if updatedTTYSize != nil {
			rec.ttySize = updatedTTYSize
		} else if !updatedTTY {
			rec.ttySize = nil
		}
		if !updatedTTY && rec.input != nil {
			// A manifest/update transition away from TTY must close the
			// durable owner before this restart's non-TTY incarnation.
			inputToClose = rec.input
			rec.input = nil
			rec.ttySize = nil
			inputToClose.close()
		}
	}
	store := rec.store
	argv := append([]string(nil), rec.argv...)
	env := append([]string(nil), rec.env...)
	launchCwd := rec.cwd
	tty := rec.tty
	var ttySize *TTYSize
	if rec.ttySize != nil {
		size := *rec.ttySize
		ttySize = &size
	}
	s.mu.Unlock()
	if inputToClose != nil {
		closeInputLease(inputToClose)
	}
	rec.waitInputOperations()
	marker := fmt.Sprintf("%s restarted\n", rec.name)
	if limit := s.outputLimits.RetainedBytes; limit > 0 && len(marker) > limit {
		marker = "restarted"
		if len(marker) > limit {
			marker = marker[:limit]
		}
	}
	launchCursor, err := store.Append(output.System, s.now(), marker)
	if err != nil {
		return Process{}, fmt.Errorf("restart marker: %w", err)
	}
	if rec.source != "" && rec.readyPattern != nil {
		launchTracker = newReadinessTracker(store, rec.readyPattern, launchCursor, true)
	}
	startedAt := s.now()
	var processTTYSize *process.TTYSize
	if ttySize != nil {
		processTTYSize = &process.TTYSize{Columns: ttySize.Columns, Rows: ttySize.Rows}
	}
	child, err := s.startProcess(process.Spec{
		Dir:          launchCwd,
		Argv:         append([]string(nil), argv...),
		Env:          append([]string(nil), env...),
		Output:       store,
		MaxLineBytes: s.maxLineBytes,
		Now:          s.now,
		TTY:          tty,
		TTYSize:      processTTYSize,
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
	rec.incarnation++
	rec.tracker = launchTracker
	input := rec.input
	if input != nil {
		input.beginIncarnation()
	}
	restarted := rec.snapshotLocked()
	restartSucceeded = true
	s.mu.Unlock()
	if input != nil {
		input.emit(InputEvent{State: InputRunning, LaunchCursor: launchCursor, TTY: tty})
	}

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
	tracker := rec.tracker
	rec.tracker = nil
	input := rec.input
	cursor := rec.cursor
	tty := rec.tty
	if input != nil {
		input.endIncarnation()
	}
	s.insertCompletedLocked(rec)
	close(rec.done)
	s.evictLocked()
	s.mu.Unlock()
	if input != nil {
		input.emit(InputEvent{State: InputStopped, LaunchCursor: cursor, TTY: tty})
	}
	if tracker != nil {
		tracker.close()
	}
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
			if rec.restarting || rec.input != nil || rec.store != nil && rec.store.SubscriberCount() != 0 {
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
	return s.lookupRoot(cwd, name, "")
}

func (s *Supervisor) lookupRoot(cwd, name, rootHint string) (*record, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	root := rootHint
	var err error
	if root == "" {
		root, err = DiscoverProjectRoot(cwd)
	} else {
		root, err = absoluteClean(root)
	}
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

// ensureSession returns the durable project/name record, creating an empty
// pre-launch session when necessary. The caller may attach before any launch.
func (s *Supervisor) ensureSession(cwd, name string) (*record, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	root, err := DiscoverProjectRoot(cwd)
	if err != nil {
		return nil, err
	}
	key := keyFor(root, name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrSupervisorClosed
	}
	if rec := s.records[key]; rec != nil {
		return rec, nil
	}
	store, err := output.NewStore(s.outputLimits)
	if err != nil {
		return nil, fmt.Errorf("output store: %w", err)
	}
	done := make(chan struct{})
	close(done)
	rec := &record{key: key, name: name, root: root, cwd: root, store: store, state: StateExited, terminal: true, done: done}
	s.trackStore(key, store)
	s.records[key] = rec
	return rec, nil
}

// Subscribe attaches to a durable name rather than one process incarnation.
// It may create an empty session before the first launch. Registration occurs
// under the registry lock so launch and eviction cannot race the attachment.
// PrepareTTY records a known TTY definition before its first launch. It is
// used by an attached client so input ownership can be acquired before the
// child exists without reserving unresolved names.
func (s *Supervisor) PrepareTTY(req StartRequest) error {
	if !req.TTY {
		return ErrInputNotTTY
	}
	if err := ValidateName(req.Name); err != nil {
		return err
	}
	requestCwd, err := absoluteClean(req.Cwd)
	if err != nil {
		return err
	}
	root := req.Root
	if root == "" {
		root, err = DiscoverProjectRoot(requestCwd)
	} else {
		root, err = absoluteClean(root)
	}
	if err != nil {
		return err
	}
	key := keyFor(root, req.Name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSupervisorClosed
	}
	rec := s.records[key]
	if rec == nil {
		store, storeErr := output.NewStore(s.outputLimits)
		if storeErr != nil {
			return fmt.Errorf("output store: %w", storeErr)
		}
		done := make(chan struct{})
		close(done)
		rec = &record{key: key, name: req.Name, root: root, cwd: requestCwd, store: store, state: StateExited, terminal: true, done: done}
		s.trackStore(key, store)
		s.records[key] = rec
	}
	if !rec.terminal {
		if !rec.tty {
			return fmt.Errorf("%w: %q is running without a tty; stop it and rerun with --tty", ErrInputNotTTY, req.Name)
		}
		return nil
	}
	// A retained TTY record already has the authoritative launch
	// specification. Input attach requests intentionally omit Env, so never
	// restage that record (or replace its cwd) while merely acquiring input.
	retained := rec.terminal && rec.tty && len(rec.argv) != 0
	rec.tty = true
	if !retained && req.Cwd != "" {
		rec.cwd = requestCwd
	}
	if !retained && len(req.Argv) != 0 {
		if req.Argv[0] == "" {
			return fmt.Errorf("%w: argv must not be empty", ErrInvalidRequest)
		}
		rec.argv = append([]string(nil), req.Argv...)
		rec.source = req.Source
		rec.env = append([]string(nil), req.Env...)
	}
	if req.TTYSize != nil {
		size := *req.TTYSize
		if size.Columns == 0 || size.Rows == 0 {
			return fmt.Errorf("%w: tty size must have non-zero columns and rows", ErrInvalidRequest)
		}
		rec.ttySize = &size
	}
	return nil
}

// EnsureTTY is a descriptive alias for PrepareTTY.
func (s *Supervisor) EnsureTTY(req StartRequest) error { return s.PrepareTTY(req) }

// AcquireInput claims the one input owner for an existing known TTY session.
// The variadic arguments accept cwd, name, and an optional bool indicating a
// known TTY definition, followed by an optional TTYSize.
func (s *Supervisor) AcquireInput(args ...any) (*InputLease, error) {
	cwd, name, requestedTTY, ttySet, size, err := parseInputAcquireArgs(args...)
	if err != nil {
		return nil, err
	}
	return s.acquireInput("", cwd, name, requestedTTY, ttySet, size)
}

// AcquireInputAt is the explicit-root form used by daemon requests whose
// child working directory differs from the project root.
func (s *Supervisor) AcquireInputAt(root, cwd, name string, requestedTTY bool, size *TTYSize) (*InputLease, error) {
	return s.acquireInput(root, cwd, name, requestedTTY, true, size)
}

func (s *Supervisor) acquireInput(rootHint, cwd, name string, requestedTTY, ttySet bool, size *TTYSize) (*InputLease, error) {
	if cwd == "" || name == "" {
		return nil, fmt.Errorf("%w: input name and cwd are required", ErrInvalidRequest)
	}
	var err error
	if rootHint == "" {
		rootHint, err = DiscoverProjectRoot(cwd)
	} else {
		rootHint, err = absoluteClean(rootHint)
	}
	if err != nil {
		return nil, err
	}
	key := keyFor(rootHint, name)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrSupervisorClosed
	}
	rec := s.records[key]
	if rec == nil || !rec.tty || requestedTTY && !rec.tty {
		s.mu.Unlock()
		return nil, ErrInputNotTTY
	}
	if ttySet && !requestedTTY {
		s.mu.Unlock()
		return nil, ErrInputNotTTY
	}
	if rec.input != nil {
		s.mu.Unlock()
		return nil, &InputConflictError{Name: name}
	}
	if size != nil {
		if size.Columns == 0 || size.Rows == 0 {
			s.mu.Unlock()
			return nil, fmt.Errorf("%w: tty size must have non-zero columns and rows", ErrInvalidRequest)
		}
		copySize := *size
		rec.ttySize = &copySize
	}
	lease := &InputLease{
		supervisor: s, rec: rec, events: make(chan InputEvent, 16),
		eventNotify: make(chan struct{}, 1), closed: make(chan struct{}),
		incarnationDone: make(chan struct{}),
	}
	go lease.dispatchEvents()
	rec.input = lease
	state := InputStopped
	if !rec.terminal {
		state = InputRunning
	}
	cursor := rec.cursor
	tty := rec.tty
	child := rec.child
	s.mu.Unlock()
	lease.emit(InputEvent{State: state, LaunchCursor: cursor, TTY: tty})
	if state == InputRunning && size != nil && child != nil {
		if resizeErr := s.resizeInput(context.Background(), lease, cursor, size.Columns, size.Rows); resizeErr != nil {
			// A child can exit after the lease is acquired but before its
			// initial size is applied. Ordinary exit retains the lease so the
			// owner can continue with the successor incarnation.
			if errors.Is(resizeErr, os.ErrProcessDone) || errors.Is(resizeErr, syscall.EIO) || errors.Is(resizeErr, ErrInputStopped) {
				return lease, nil
			}
			lease.Release()
			return nil, resizeErr
		}
	}
	return lease, nil
}

// AttachInput is the protocol-facing spelling of AcquireInput.
func (s *Supervisor) AttachInput(args ...any) (*InputLease, error) { return s.AcquireInput(args...) }

func parseInputAcquireArgs(args ...any) (cwd, name string, requestedTTY, ttySet bool, size *TTYSize, err error) {
	for _, arg := range args {
		switch value := arg.(type) {
		case context.Context:
		case string:
			if cwd == "" {
				cwd = value
			} else if name == "" {
				name = value
			} else {
				err = fmt.Errorf("%w: too many input strings", ErrInvalidRequest)
			}
		case bool:
			requestedTTY, ttySet = value, true
		case TTYSize:
			copyValue := value
			size = &copyValue
		case *TTYSize:
			if value != nil {
				copyValue := *value
				size = &copyValue
			}
		default:
			err = fmt.Errorf("%w: unsupported input argument", ErrInvalidRequest)
		}
		if err != nil {
			return
		}
	}
	return
}

func (l *InputLease) emit(event InputEvent) {
	if l == nil {
		return
	}
	l.eventMu.Lock()
	if l.eventClosed {
		l.eventMu.Unlock()
		return
	}
	l.eventQueue = append(l.eventQueue, event)
	select {
	case l.eventNotify <- struct{}{}:
	default:
	}
	l.eventMu.Unlock()
}

func (s *Supervisor) clearInputLease(lease *InputLease) {
	if s == nil || lease == nil || lease.rec == nil {
		return
	}
	s.mu.Lock()
	if lease.rec.input == lease {
		lease.rec.input = nil
		// A terminal size belongs to the attached owner. A later launch
		// without an owner must use the PTY default rather than a stale size.
		lease.rec.ttySize = nil
	}
	s.mu.Unlock()
}

func (s *Supervisor) releaseInput(lease *InputLease) {
	if lease == nil || lease.rec == nil {
		return
	}
	// Close first so a pending cancellation-aware child write/resize is
	// interrupted. Context-aware operations are drained synchronously; a legacy
	// operation is kept registered and cleared asynchronously because waiting on
	// an implementation that cannot observe cancellation would deadlock remove
	// and shutdown. The record remains owned until that legacy call returns.
	lease.close()
	rec := lease.rec
	operation := rec.currentInputOperation()
	if operation == nil {
		s.clearInputLease(lease)
		return
	}
	if operation.legacy {
		go func() {
			<-operation.done
			s.clearInputLease(lease)
		}()
		return
	}
	<-operation.done
	s.clearInputLease(lease)
}

func closeInputLease(lease *InputLease) {
	if lease == nil || lease.rec == nil {
		return
	}
	lease.close()
	operation := lease.rec.currentInputOperation()
	if operation != nil && !operation.legacy {
		<-operation.done
	}
}

func acquireInputOperation(ctx context.Context, rec *record, lease *InputLease, incarnationDone <-chan struct{}, legacy bool) (*inputOperation, error) {
	for {
		operation, wait, ok := rec.beginInputOperation(lease, incarnationDone, legacy)
		if !ok {
			if inputLeaseClosed(lease) {
				return nil, ErrInputClosed
			}
			return nil, InputStoppedError{}
		}
		if wait == nil {
			return operation, nil
		}
		select {
		case <-wait:
			continue
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-lease.closed:
			return nil, ErrInputClosed
		case <-incarnationDone:
			return nil, InputStoppedError{}
		}
	}
}

func inputOperationContext(ctx context.Context, lease *InputLease, incarnationDone <-chan struct{}) (context.Context, context.CancelFunc, <-chan struct{}) {
	operationCtx, cancel := context.WithCancel(ctx)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-lease.closed:
			cancel()
		case <-incarnationDone:
			cancel()
		case <-operationCtx.Done():
		}
	}()
	return operationCtx, cancel, watchDone
}

func (s *Supervisor) writeInput(ctx context.Context, lease *InputLease, cursor output.Cursor, data []byte) error {
	if len(data) > maxInputBytes {
		return &InputTooLargeError{Size: len(data), Limit: maxInputBytes}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if lease == nil || lease.supervisor != s {
		return ErrInputClosed
	}
	rec := lease.rec
	s.mu.RLock()
	valid := s.records[rec.key] == rec && rec.input == lease && rec.tty
	if !valid {
		s.mu.RUnlock()
		return ErrInputClosed
	}
	if rec.terminal {
		s.mu.RUnlock()
		return InputStoppedError{}
	}
	if cursor != rec.cursor {
		current := rec.cursor
		s.mu.RUnlock()
		return &InputStaleError{Want: cursor, Current: current}
	}
	child := rec.child
	s.mu.RUnlock()
	inputChild, ok := child.(InputChild)
	if !ok {
		return ErrInputClosed
	}
	incarnationDone := lease.incarnationChannel()
	_, contextWriter := child.(ContextInputWriter)
	operation, err := acquireInputOperation(ctx, rec, lease, incarnationDone, !contextWriter)
	if err != nil {
		return err
	}
	operationCtx, cancel, watchDone := inputOperationContext(ctx, lease, incarnationDone)
	defer func() {
		cancel()
		<-watchDone
	}()

	type writeResult struct {
		n   int
		err error
	}
	result := make(chan writeResult, 1)
	go func() {
		var n int
		var callErr error
		if writer, ok := child.(ContextInputWriter); ok {
			n, callErr = writer.WriteContext(operationCtx, data)
		} else {
			n, callErr = inputChild.Write(data)
		}
		result <- writeResult{n: n, err: callErr}
		rec.finishInputOperation(operation)
	}()
	finish := func(outcome writeResult) error {
		if outcome.err == nil && outcome.n != len(data) {
			outcome.err = io.ErrShortWrite
		}
		if errors.Is(outcome.err, os.ErrProcessDone) || errors.Is(outcome.err, syscall.EIO) {
			if !inputLeaseClosed(lease) {
				return InputStoppedError{}
			}
			return ErrInputClosed
		}
		return outcome.err
	}
	select {
	case outcome := <-result:
		<-operation.done
		if inputLeaseClosed(lease) {
			return ErrInputClosed
		}
		if inputIncarnationStopped(lease, incarnationDone) {
			return InputStoppedError{}
		}
		return finish(outcome)
	case <-ctx.Done():
		if !operation.legacy {
			<-result
			<-operation.done
		}
		return ctx.Err()
	case <-lease.closed:
		if !operation.legacy {
			<-result
			<-operation.done
		}
		return ErrInputClosed
	case <-incarnationDone:
		if !operation.legacy {
			<-result
			<-operation.done
		}
		return InputStoppedError{}
	}
}

func inputLeaseClosed(lease *InputLease) bool {
	if lease == nil {
		return true
	}
	return inputChannelClosed(lease.closed)
}

func inputChannelClosed(channel <-chan struct{}) bool {
	if channel == nil {
		return true
	}
	select {
	case <-channel:
		return true
	default:
		return false
	}
}

func inputIncarnationStopped(lease *InputLease, incarnationDone <-chan struct{}) bool {
	if inputLeaseClosed(lease) {
		return false
	}
	return inputChannelClosed(incarnationDone)
}

func (s *Supervisor) resizeInput(ctx context.Context, lease *InputLease, cursor output.Cursor, columns, rows uint16) error {
	if columns == 0 || rows == 0 {
		return fmt.Errorf("%w: tty dimensions must be non-zero", ErrInvalidRequest)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if lease == nil || lease.supervisor != s {
		return ErrInputClosed
	}
	rec := lease.rec
	// The requested size is retained even when this incarnation has already
	// stopped. The next launch uses the owner's most recent size.
	s.mu.Lock()
	valid := s.records[rec.key] == rec && rec.input == lease && rec.tty
	if !valid {
		s.mu.Unlock()
		return ErrInputClosed
	}
	if cursor != rec.cursor {
		current := rec.cursor
		s.mu.Unlock()
		return &InputStaleError{Want: cursor, Current: current}
	}
	size := TTYSize{Columns: columns, Rows: rows}
	rec.ttySize = &size
	if rec.terminal {
		s.mu.Unlock()
		return InputStoppedError{}
	}
	child := rec.child
	s.mu.Unlock()
	if child == nil {
		return InputStoppedError{}
	}
	inputChild, ok := child.(InputChild)
	if !ok {
		return ErrInputClosed
	}
	incarnationDone := lease.incarnationChannel()
	_, contextResizer := child.(ContextInputResizer)
	operation, err := acquireInputOperation(ctx, rec, lease, incarnationDone, !contextResizer)
	if err != nil {
		return err
	}
	operationCtx, cancel, watchDone := inputOperationContext(ctx, lease, incarnationDone)
	defer func() {
		cancel()
		<-watchDone
	}()
	result := make(chan error, 1)
	go func() {
		var callErr error
		if resizer, ok := child.(ContextInputResizer); ok {
			callErr = resizer.ResizeContext(operationCtx, columns, rows)
		} else {
			callErr = inputChild.Resize(columns, rows)
		}
		result <- callErr
		rec.finishInputOperation(operation)
	}()
	finish := func(callErr error) error {
		if errors.Is(callErr, os.ErrProcessDone) || errors.Is(callErr, syscall.EIO) {
			if !inputLeaseClosed(lease) {
				return InputStoppedError{}
			}
			return ErrInputClosed
		}
		return callErr
	}
	select {
	case callErr := <-result:
		<-operation.done
		if inputLeaseClosed(lease) {
			return ErrInputClosed
		}
		if inputIncarnationStopped(lease, incarnationDone) {
			return InputStoppedError{}
		}
		return finish(callErr)
	case <-ctx.Done():
		if !operation.legacy {
			<-result
			<-operation.done
		}
		return ctx.Err()
	case <-lease.closed:
		if !operation.legacy {
			<-result
			<-operation.done
		}
		return ErrInputClosed
	case <-incarnationDone:
		if !operation.legacy {
			<-result
			<-operation.done
		}
		return InputStoppedError{}
	}
}

func (s *Supervisor) Subscribe(cwd, name string, opts output.ReadOptions) (*Follower, error) {
	rec, err := s.ensureSession(cwd, name)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.records[rec.key] != rec || rec.store == nil {
		root, processName := rec.root, rec.name
		s.mu.Unlock()
		return nil, &NotFoundError{Root: root, Name: processName}
	}
	sub := rec.store.Subscribe(opts)
	rec.followers++
	if !rec.start.IsZero() && !rec.terminal {
		sub.ReplayLatestExitSince(rec.start)
	}
	s.mu.Unlock()
	return &Follower{sub: sub, close: func() {
		s.mu.Lock()
		if s.records[rec.key] == rec && rec.followers > 0 {
			rec.followers--
		}
		s.mu.Unlock()
	}}, nil
}

// Follower is one live durable follow attachment. It wraps the output cursor
// so snapshots can count follow clients without including private Wait users.
type Follower struct {
	sub       *output.Subscription
	close     func()
	closeOnce sync.Once
}

func (f *Follower) Next(ctx context.Context) (output.Event, error) { return f.sub.Next(ctx) }
func (f *Follower) Cursor() output.Cursor                          { return f.sub.Cursor() }

// Close detaches the follower and updates the session count exactly once.
func (f *Follower) Close() {
	f.closeOnce.Do(func() {
		f.sub.Close()
		f.close()
	})
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
		if opts.After != nil || !errors.Is(err, ErrProcessNotFound) {
			return WaitResult{}, err
		}
		rec, err = s.ensureSession(cwd, name)
		if err != nil {
			return WaitResult{}, err
		}
	}

	s.mu.RLock()
	if s.records[rec.key] != rec || rec.store == nil {
		root, processName := rec.root, rec.name
		s.mu.RUnlock()
		return WaitResult{}, &NotFoundError{Root: root, Name: processName}
	}
	store := rec.store
	after := opts.After
	if after == nil {
		if rec.terminal {
			if next := store.NextCursor(); next != 0 {
				cursor := next - 1
				after = &cursor
			}
		} else if rec.launchBoundary {
			cursor := rec.cursor
			after = &cursor
		}
	}
	sub := store.Subscribe(output.ReadOptions{After: after, Match: opts.Match, Streams: output.BothStreams, MaxBytes: s.maxLineBytes})
	if !rec.terminal {
		sub.ReplayLatestExitSince(rec.start)
	}
	s.mu.RUnlock()
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
			if opts.Match != nil && len(event.Read.Entries) != 0 {
				return WaitResult{Outcome: WaitMatched, Cursor: sub.Cursor()}, nil
			}
			continue
		}
		if event.Exit == nil {
			continue
		}
		exitResult := process.Result{ExitCode: event.Exit.Code, ExitedAt: event.Exit.Time}
		s.mu.RLock()
		if rec.terminal {
			exitResult = rec.result
		}
		s.mu.RUnlock()
		return WaitResult{Outcome: WaitExited, Cursor: sub.Cursor(), Exit: &exitResult}, nil
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

// Remove stops and permanently discards one supervision session. Followers
// are closed cleanly; configuration outside runtime state is never touched.
func (s *Supervisor) Remove(ctx context.Context, cwd, name string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	rec, err := s.lookup(cwd, name)
	if err != nil {
		return err
	}
	s.mu.RLock()
	_, starting := s.starting[rec.key]
	s.mu.RUnlock()
	if starting {
		return fmt.Errorf("%w: %q is being started", ErrNameInUse, name)
	}
	rec.stopMu.Lock()
	err = s.stopRecord(ctx, rec)
	rec.stopMu.Unlock()
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.records[rec.key] != rec {
		s.mu.Unlock()
		return &NotFoundError{Root: rec.root, Name: rec.name}
	}
	delete(s.records, rec.key)
	s.removeCompletedLocked(rec)
	store := rec.store
	input := rec.input
	if input != nil {
		// Close while the record is still registry-visible so a writer that
		// validated just before removal cannot start after ownership is cleared.
		input.close()
	}
	rec.input = nil
	rec.store, rec.env, rec.argv, rec.child = nil, nil, nil, nil
	s.mu.Unlock()
	if input != nil {
		closeInputLease(input)
	}
	if store != nil {
		store.Close(nil)
	}
	return nil
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
	stores := make([]*output.Store, 0, len(s.records))
	leases := make([]*InputLease, 0, len(s.records))
	for _, rec := range s.records {
		if rec.store != nil {
			stores = append(stores, rec.store)
		}
		if rec.input != nil {
			leases = append(leases, rec.input)
			rec.input.close()
			rec.input = nil
		}
	}
	s.shutdownErr = shutdownErr
	close(s.shutdownDone)
	s.mu.Unlock()
	for _, lease := range leases {
		closeInputLease(lease)
	}
	for _, store := range stores {
		store.Close(ErrSupervisorClosed)
	}
	if callerErr := ctx.Err(); callerErr != nil {
		return errors.Join(shutdownErr, callerErr)
	}
	return shutdownErr
}

func (r *record) snapshotLocked() Process {
	model := Process{
		Name:         r.name,
		Source:       r.source,
		Root:         r.root,
		TTY:          r.tty,
		PID:          r.pid,
		PGID:         r.pgid,
		Cwd:          r.cwd,
		Argv:         append([]string(nil), r.argv...),
		Start:        r.start,
		LaunchCursor: r.cursor,
		State:        r.state,
		RestartCount: r.restartCount,
		Followers:    r.followers,
	}
	if r.store != nil {
		model.NextCursor = r.store.NextCursor()
	}
	if !r.terminal && r.source != "" {
		switch {
		case r.readyConfig == nil:
			model.Readiness = &Readiness{State: ReadinessRunningUnverified}
		default:
			ready, cursor, at := r.tracker.snapshot()
			if ready {
				model.Readiness = &Readiness{
					State: ReadinessReady, Cursor: &cursor, Time: at,
					Match: r.readyConfig.Match,
				}
			} else {
				model.Readiness = &Readiness{
					State: ReadinessStarting, Match: r.readyConfig.Match,
				}
			}
		}
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
