package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"hum/internal/output"
	"hum/internal/process"
)

type ttyLeaseChild struct {
	pid    int
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	writes [][]byte
	sizes  [][2]uint16
	result process.Result
}

func (c *ttyLeaseChild) PID() int              { return c.pid }
func (c *ttyLeaseChild) PGID() int             { return c.pid }
func (c *ttyLeaseChild) Done() <-chan struct{} { return c.done }
func (c *ttyLeaseChild) Wait() process.Result  { <-c.done; return c.result }
func (c *ttyLeaseChild) Signal(os.Signal) error {
	c.once.Do(func() { close(c.done) })
	return nil
}
func (c *ttyLeaseChild) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, append([]byte(nil), data...))
	return len(data), nil
}
func (c *ttyLeaseChild) Resize(columns, rows uint16) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sizes = append(c.sizes, [2]uint16{columns, rows})
	return nil
}
func (c *ttyLeaseChild) exit() { c.once.Do(func() { close(c.done) }) }

type cancelableTTYLeaseChild struct {
	*ttyLeaseChild
	started   chan struct{}
	startOnce sync.Once
}

type exitOnResizeTTYLeaseChild struct {
	*ttyLeaseChild
}

func (c *exitOnResizeTTYLeaseChild) Resize(uint16, uint16) error {
	c.exit()
	return os.ErrProcessDone
}

type blockingLegacyTTYLeaseChild struct {
	*ttyLeaseChild
	started   chan struct{}
	unblock   chan struct{}
	writeDone chan struct{}
	startOnce sync.Once
}

func (c *blockingLegacyTTYLeaseChild) Write(data []byte) (int, error) {
	c.startOnce.Do(func() { close(c.started) })
	<-c.unblock
	defer close(c.writeDone)
	return c.ttyLeaseChild.Write(data)
}

func (c *cancelableTTYLeaseChild) WriteContext(ctx context.Context, data []byte) (int, error) {
	c.startOnce.Do(func() { close(c.started) })
	if bytes.Equal(data, []byte("old-payload")) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-c.done:
			return 0, os.ErrProcessDone
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, append([]byte(nil), data...))
	return len(data), nil
}

func TestTTYSessionInput(t *testing.T) {
	root := makeProject(t, false)
	children := []*ttyLeaseChild{}
	s, err := New(Options{StartProcess: func(spec process.Spec) (Child, error) {
		child := &ttyLeaseChild{pid: 8000 + len(children), done: make(chan struct{}), result: process.Result{ExitCode: 0, ExitedAt: time.Now()}}
		children = append(children, child)
		return child, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	// A missing unresolved record and a known non-TTY record never reserve input.
	if _, err := s.AcquireInput(root, "missing", true); err == nil {
		t.Fatal("unresolved session acquired a TTY lease")
	}
	nonTTY, err := s.Start(StartRequest{Name: "pipe", Cwd: root, Argv: []string{"pipe"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireInput(root, nonTTY.Name, true); err == nil {
		t.Fatal("non-TTY session acquired a lease")
	}
	if err := s.Stop(context.Background(), root, nonTTY.Name); err != nil {
		t.Fatal(err)
	}

	started, err := s.Start(StartRequest{Name: "dev", Cwd: root, Argv: []string{"dev"}, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	if !started.TTY {
		t.Fatal("TTY snapshot is false")
	}
	lease, err := s.AcquireInput(root, "dev", true)
	if err != nil {
		t.Fatalf("acquire owner: %v", err)
	}
	defer lease.Release()
	initial, err := lease.Next(context.Background())
	if err != nil || initial.State != InputRunning || initial.LaunchCursor != started.LaunchCursor {
		t.Fatalf("initial input event = %+v, err %v", initial, err)
	}
	if _, err := s.AcquireInput(root, "dev", true); err == nil || !errorsIs(err, ErrInputConflict) {
		t.Fatalf("second owner error = %v", err)
	}
	payload := []byte{0, 0xff, 'x'}
	if err := lease.Write(started.LaunchCursor, payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := lease.Resize(started.LaunchCursor, 120, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if got := children[1].writes[0]; !bytes.Equal(got, payload) {
		t.Fatalf("child write = %v, want %v", got, payload)
	}

	children[1].exit()
	stoppedCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		event, nextErr := lease.Next(stoppedCtx)
		if nextErr != nil {
			t.Fatalf("stopped event: %v", nextErr)
		}
		if event.State == InputStopped {
			break
		}
	}
	if err := lease.Write(started.LaunchCursor, []byte("discard")); !errorsIs(err, ErrInputClosed) {
		t.Fatalf("stopped write = %v", err)
	}

	restarted, err := s.Restart(context.Background(), root, "dev")
	if err != nil {
		t.Fatalf("restart tty session: %v", err)
	}
	if !restarted.TTY || restarted.LaunchCursor == started.LaunchCursor {
		t.Fatalf("restart snapshot = %+v", restarted)
	}
	for {
		event, nextErr := lease.Next(stoppedCtx)
		if nextErr != nil {
			t.Fatalf("successor event: %v", nextErr)
		}
		if event.State == InputRunning && event.LaunchCursor == restarted.LaunchCursor {
			break
		}
	}
	if err := lease.Write(started.LaunchCursor, []byte("stale")); !errorsIs(err, ErrInputStale) {
		t.Fatalf("stale write = %v", err)
	}
	if err := lease.Write(restarted.LaunchCursor, []byte("next")); err != nil {
		t.Fatalf("successor write: %v", err)
	}

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := lease.Next(context.Background()); errorsIs(err, ErrInputClosed) {
			break
		}
	}
}

func TestTTYAcquireRetainsLeaseWhenInitialResizeSeesExit(t *testing.T) {
	root := makeProject(t, false)
	child := &exitOnResizeTTYLeaseChild{ttyLeaseChild: &ttyLeaseChild{pid: 9050, done: make(chan struct{}), result: process.Result{ExitCode: 0, ExitedAt: time.Now()}}}
	s, err := New(Options{StartProcess: func(process.Spec) (Child, error) { return child, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())
	started, err := s.Start(StartRequest{Name: "dev", Cwd: root, Argv: []string{"dev"}, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.AcquireInput(root, "dev", true, TTYSize{Columns: 80, Rows: 24})
	if err != nil {
		t.Fatalf("acquire after exit during resize: %v", err)
	}
	defer lease.Release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		event, nextErr := lease.Next(ctx)
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		if event.State == InputStopped {
			break
		}
	}
	if _, err := s.AcquireInput(root, "dev", true); !errors.Is(err, ErrInputConflict) {
		t.Fatalf("lease after resize exit = %v", err)
	}
	_ = started
}

func TestTTYInputCancellationDoesNotReachSuccessorOwner(t *testing.T) {
	root := makeProject(t, false)
	child := &cancelableTTYLeaseChild{ttyLeaseChild: &ttyLeaseChild{pid: 9100, done: make(chan struct{}), result: process.Result{ExitCode: 0, ExitedAt: time.Now()}}, started: make(chan struct{})}
	s, err := New(Options{StartProcess: func(process.Spec) (Child, error) { return child, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())
	started, err := s.Start(StartRequest{Name: "dev", Cwd: root, Argv: []string{"dev"}, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.AcquireInput(root, "dev", true)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if _, err := lease.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	writeDone := make(chan error, 1)
	go func() { writeDone <- lease.Write(ctx, started.LaunchCursor, []byte("old-payload")) }()
	select {
	case <-child.started:
	case <-time.After(time.Second):
		t.Fatal("blocked input write did not start")
	}
	cancel()
	select {
	case err := <-writeDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled input write = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled input write remained blocked")
	}
	lease.Release()

	next, err := s.AcquireInput(root, "dev", true)
	if err != nil {
		t.Fatalf("acquire successor owner: %v", err)
	}
	defer next.Release()
	if err := next.Write(started.LaunchCursor, []byte("new-payload")); err != nil {
		t.Fatalf("successor write: %v", err)
	}
	child.mu.Lock()
	defer child.mu.Unlock()
	if len(child.writes) != 1 || !bytes.Equal(child.writes[0], []byte("new-payload")) {
		t.Fatalf("writes after cancellation = %q, want only successor payload", child.writes)
	}
}

func TestTTYLegacyWriteCancellationDrainsBeforeSuccessor(t *testing.T) {
	root := makeProject(t, false)
	legacy := &blockingLegacyTTYLeaseChild{
		ttyLeaseChild: &ttyLeaseChild{pid: 9150, done: make(chan struct{}), result: process.Result{ExitCode: 0, ExitedAt: time.Now()}},
		started:       make(chan struct{}), unblock: make(chan struct{}), writeDone: make(chan struct{}),
	}
	children := []*ttyLeaseChild{}
	s, err := New(Options{StartProcess: func(process.Spec) (Child, error) {
		if len(children) == 0 {
			children = append(children, legacy.ttyLeaseChild)
			return legacy, nil
		}
		child := &ttyLeaseChild{pid: 9151, done: make(chan struct{}), result: process.Result{ExitCode: 0, ExitedAt: time.Now()}}
		children = append(children, child)
		return child, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())
	started, err := s.Start(StartRequest{Name: "dev", Cwd: root, Argv: []string{"dev"}, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.AcquireInput(root, "dev", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	writeDone := make(chan error, 1)
	go func() { writeDone <- lease.Write(ctx, started.LaunchCursor, []byte("old-payload")) }()
	select {
	case <-legacy.started:
	case <-time.After(time.Second):
		t.Fatal("legacy input write did not start")
	}
	cancel()
	select {
	case err := <-writeDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled legacy write = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled legacy write remained blocked")
	}
	lease.Release()
	if _, err := s.AcquireInput(root, "dev", true); !errors.Is(err, ErrInputConflict) {
		t.Fatalf("successor acquired before legacy drain: %v", err)
	}
	close(legacy.unblock)
	select {
	case <-legacy.writeDone:
	case <-time.After(time.Second):
		t.Fatal("legacy write did not drain")
	}
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.RLock()
		input := s.records[keyFor(root, "dev")].input
		s.mu.RUnlock()
		if input == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("legacy lease remained registered after drain")
		}
		time.Sleep(time.Millisecond)
	}
	restarted, err := s.Restart(context.Background(), root, "dev")
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.AcquireInput(root, "dev", true)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Release()
	if err := next.Write(restarted.LaunchCursor, []byte("new-payload")); err != nil {
		t.Fatal(err)
	}
	children[1].mu.Lock()
	defer children[1].mu.Unlock()
	if len(children[1].writes) != 1 || !bytes.Equal(children[1].writes[0], []byte("new-payload")) {
		t.Fatalf("successor writes = %q", children[1].writes)
	}
}

func TestTTYStoppedResizeRetainsSuccessorSize(t *testing.T) {
	root := makeProject(t, false)
	specs := make([]process.Spec, 0, 2)
	children := make([]*ttyLeaseChild, 0, 2)
	s, err := New(Options{StartProcess: func(spec process.Spec) (Child, error) {
		specs = append(specs, spec)
		base := &ttyLeaseChild{pid: 9250 + len(children), done: make(chan struct{}), result: process.Result{ExitCode: 0, ExitedAt: time.Now()}}
		children = append(children, base)
		if len(children) == 1 {
			return &exitOnResizeTTYLeaseChild{ttyLeaseChild: base}, nil
		}
		return base, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())
	started, err := s.Start(StartRequest{Name: "dev", Cwd: root, Argv: []string{"dev"}, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.AcquireInput(root, "dev", true, TTYSize{Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		event, nextErr := lease.Next(ctx)
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		if event.State == InputStopped {
			break
		}
	}
	if err := lease.Resize(started.LaunchCursor, 120, 40); !errors.Is(err, ErrInputStopped) {
		t.Fatalf("stopped resize = %v", err)
	}
	if _, err := s.Restart(context.Background(), root, "dev"); err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 || specs[1].TTYSize == nil || specs[1].TTYSize.Columns != 120 || specs[1].TTYSize.Rows != 40 {
		t.Fatalf("successor tty size = %#v", specs)
	}
}

func TestTTYLeaseRetainsAllQueuedEvents(t *testing.T) {
	lease := &InputLease{
		events: make(chan InputEvent, 16), eventNotify: make(chan struct{}, 1),
		closed: make(chan struct{}), incarnationDone: make(chan struct{}), incarnationClosed: true,
	}
	go lease.dispatchEvents()
	const count = 100
	for i := 0; i < count; i++ {
		lease.emit(InputEvent{State: InputRunning, LaunchCursor: output.Cursor(i)})
	}
	for i := 0; i < count; i++ {
		select {
		case event := <-lease.events:
			if event.LaunchCursor != output.Cursor(i) {
				t.Fatalf("queued event %d = %d", i, event.LaunchCursor)
			}
		case <-time.After(time.Second):
			t.Fatalf("queued event %d was lost", i)
		}
	}
	lease.close()
	select {
	case _, ok := <-lease.events:
		if ok {
			t.Fatal("event channel remained open after lease close")
		}
	case <-time.After(time.Second):
		t.Fatal("event channel did not close")
	}
}

func TestTTYLeaseClosedBeforeNonTTYReplacement(t *testing.T) {
	root := makeProject(t, false)
	children := []*ttyLeaseChild{}
	s, err := New(Options{StartProcess: func(process.Spec) (Child, error) {
		child := &ttyLeaseChild{pid: 9200 + len(children), done: make(chan struct{}), result: process.Result{ExitCode: 0, ExitedAt: time.Now()}}
		children = append(children, child)
		return child, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())
	started, err := s.Start(StartRequest{Name: "dev", Cwd: root, Argv: []string{"dev"}, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.AcquireInput(root, "dev", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(context.Background(), root, "dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(StartRequest{Name: "dev", Cwd: root, Argv: []string{"dev-non-tty"}, TTY: false}); err != nil {
		t.Fatalf("non-TTY replacement: %v", err)
	}
	if _, err := lease.Next(context.Background()); !errors.Is(err, ErrInputClosed) {
		t.Fatalf("lease after non-TTY replacement = %v", err)
	}
	if snapshot, err := s.Get(root, "dev"); err != nil || snapshot.TTY {
		t.Fatalf("replacement snapshot = %+v, err %v", snapshot, err)
	}
	if _, err := s.AcquireInput(root, "dev", true); !errors.Is(err, ErrInputNotTTY) {
		t.Fatalf("replacement input acquire = %v", err)
	}
	_ = started
}

func TestPrepareTTYPreservesRetainedLaunchSpec(t *testing.T) {
	root := makeProject(t, false)
	nested := filepath.Join(root, "tools")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatal(err)
	}
	var dirs []string
	var envs [][]string
	children := []*ttyLeaseChild{}
	s, err := New(Options{StartProcess: func(spec process.Spec) (Child, error) {
		dirs = append(dirs, spec.Dir)
		envs = append(envs, append([]string(nil), spec.Env...))
		child := &ttyLeaseChild{pid: 9300 + len(children), done: make(chan struct{}), result: process.Result{ExitCode: 0, ExitedAt: time.Now()}}
		children = append(children, child)
		return child, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())
	request := StartRequest{Name: "dev", Cwd: nested, Argv: []string{"dev"}, Env: []string{"TOKEN=required"}, TTY: true}
	if _, err := s.Start(request); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(context.Background(), root, "dev"); err != nil {
		t.Fatal(err)
	}
	if err := s.PrepareTTY(StartRequest{Name: "dev", Root: root, Cwd: root, Argv: []string{"dev"}, Source: "ad_hoc", TTY: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(StartRequest{Name: "dev", Root: root, Cwd: root}); err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 2 || dirs[1] != nested {
		t.Fatalf("retained cwd = %q, want %q", dirs, nested)
	}
	if len(envs) != 2 || len(envs[1]) != 1 || envs[1][0] != "TOKEN=required" {
		t.Fatalf("retained environment = %#v", envs[1])
	}
}

func errorsIs(err, target error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, target)
}
