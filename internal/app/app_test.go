package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"hum/internal/output"
	"hum/internal/process"
)

func testSupervisor(t *testing.T, opts Options) *Supervisor {
	t.Helper()
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})
	return s
}

func makeProject(t *testing.T, gitFile bool) string {
	t.Helper()
	root := t.TempDir()
	if gitFile {
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ../.git/worktree\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	} else if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func startShell(s *Supervisor, root, name, script string) (Process, error) {
	return s.Start(StartRequest{
		Name: name,
		Cwd:  root,
		Argv: []string{"/bin/sh", "-c", script},
		Env:  []string{"HUM_PRIVATE=secret", "PATH=/usr/bin:/bin"},
	})
}

type esrchChild struct {
	done chan struct{}
	once sync.Once
}

func (c *esrchChild) PID() int  { return 1234 }
func (c *esrchChild) PGID() int { return 1234 }
func (c *esrchChild) Done() <-chan struct{} {
	return c.done
}
func (c *esrchChild) Wait() process.Result {
	<-c.done
	return process.Result{ExitCode: -1, ExitedAt: time.Now()}
}
func (c *esrchChild) exit() {
	c.once.Do(func() { close(c.done) })
}
func (c *esrchChild) Signal(os.Signal) error {
	c.exit()
	return syscall.ESRCH
}

func waitExited(t *testing.T, s *Supervisor, cwd, name string) Process {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		model, err := s.Get(cwd, name)
		if err == nil && model.State == StateExited {
			return model
		}
		time.Sleep(time.Millisecond)
	}
	model, err := s.Get(cwd, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("process %q remained %s", name, model.State)
	return Process{}
}

func recordDone(t *testing.T, s *Supervisor, root, name string) <-chan struct{} {
	t.Helper()
	s.mu.RLock()
	rec := s.records[keyFor(root, name)]
	s.mu.RUnlock()
	if rec == nil {
		t.Fatalf("record %s/%s missing", root, name)
	}
	return rec.done
}

func waitForOutput(t *testing.T, subscription *output.Subscription, text string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		event, err := subscription.Next(ctx)
		if err != nil {
			t.Fatalf("wait for output %q: %v", text, err)
		}
		if event.Exit != nil {
			t.Fatalf("process exited before output %q", text)
		}
		for _, entry := range event.Read.Entries {
			if strings.Contains(entry.Text, text) {
				return
			}
		}
	}
}

type timedChild struct {
	pid    int
	done   chan struct{}
	once   sync.Once
	result process.Result
}

func (c *timedChild) PID() int  { return c.pid }
func (c *timedChild) PGID() int { return c.pid }
func (c *timedChild) Done() <-chan struct{} {
	return c.done
}
func (c *timedChild) Wait() process.Result {
	<-c.done
	return c.result
}
func (c *timedChild) release() {
	c.once.Do(func() { close(c.done) })
}
func (c *timedChild) Signal(os.Signal) error {
	c.release()
	return os.ErrProcessDone
}

type subscriptionChild struct {
	pid    int
	done   chan struct{}
	once   sync.Once
	result process.Result
	text   string
	store  *output.Store

	doneObserved   chan struct{}
	allowDoneCheck chan struct{}
	allowDoneOnce  sync.Once
	observeOnce    sync.Once
}

func newSubscriptionChild(pid, code int, at time.Time, text string) *subscriptionChild {
	return &subscriptionChild{
		pid:    pid,
		done:   make(chan struct{}),
		result: process.Result{ExitCode: code, ExitedAt: at},
		text:   text,
	}
}

func (c *subscriptionChild) PID() int  { return c.pid }
func (c *subscriptionChild) PGID() int { return c.pid }
func (c *subscriptionChild) Done() <-chan struct{} {
	if c.doneObserved != nil {
		c.observeOnce.Do(func() {
			close(c.doneObserved)
			<-c.allowDoneCheck
		})
	}
	return c.done
}
func (c *subscriptionChild) Wait() process.Result {
	<-c.done
	return c.result
}
func (c *subscriptionChild) release() {
	c.once.Do(func() {
		if c.store != nil {
			c.store.NotifyExit(output.Exit{
				Code: c.result.ExitCode,
				Time: c.result.ExitedAt,
			})
		}
		close(c.done)
	})
}
func (c *subscriptionChild) Signal(os.Signal) error {
	c.release()
	return os.ErrProcessDone
}
func (c *subscriptionChild) releaseDoneCheck() {
	if c.allowDoneCheck != nil {
		c.allowDoneOnce.Do(func() { close(c.allowDoneCheck) })
	}
}

func subscriptionStarter(children map[string]*subscriptionChild) func(process.Spec) (Child, error) {
	return func(spec process.Spec) (Child, error) {
		name := spec.Argv[len(spec.Argv)-1]
		child := children[name]
		if child == nil {
			return nil, fmt.Errorf("unknown test child %q", name)
		}
		child.store = spec.Output
		if child.text != "" {
			if _, err := spec.Output.Append(output.Stdout, child.result.ExitedAt, child.text); err != nil {
				return nil, err
			}
		}
		return child, nil
	}
}

type subscriptionResult struct {
	sub *output.Subscription
	err error
}

func waitSubscriptionSignal(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s", what)
	}
}

func nextSubscriptionEvent(t *testing.T, sub *output.Subscription) output.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatalf("next subscription event: %v", err)
	}
	return event
}

func assertSubscriptionOutput(t *testing.T, sub *output.Subscription, text string) {
	t.Helper()
	event := nextSubscriptionEvent(t, sub)
	if event.Read == nil || event.Exit != nil || len(event.Read.Entries) != 1 || event.Read.Entries[0].Text != text {
		t.Fatalf("subscription output event = %#v, want one %q read", event, text)
	}
}

func assertSubscriptionExit(t *testing.T, sub *output.Subscription, code int) {
	t.Helper()
	event := nextSubscriptionEvent(t, sub)
	if event.Exit == nil || event.Read != nil || event.Exit.Code != code {
		t.Fatalf("subscription exit event = %#v, want code %d", event, code)
	}
}

func assertNoSubscriptionEvent(t *testing.T, sub *output.Subscription) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected subscription event = %#v, err %v; want bounded timeout", event, err)
	}
}

func TestProjectScopedNames(t *testing.T) {
	rootA := makeProject(t, false)
	rootB := makeProject(t, true)
	nested := filepath.Join(rootA, "nested", "leaf")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	gotRoot, err := DiscoverProjectRoot(nested)
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot != rootA {
		t.Fatalf("nearest git directory root = %q, want %q", gotRoot, rootA)
	}
	gotRoot, err = DiscoverProjectRoot(filepath.Join(rootB, "child"))
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot != rootB {
		t.Fatalf("git worktree file root = %q, want %q", gotRoot, rootB)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(rootA, alias); err != nil {
		t.Fatal(err)
	}
	gotRoot, err = DiscoverProjectRoot(alias)
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot != filepath.Clean(alias) {
		t.Fatalf("symlinked project root = %q, want cleaned lexical path %q", gotRoot, filepath.Clean(alias))
	}

	fallback := t.TempDir()
	fallbackCwd := filepath.Join(fallback, "nested")
	if err := os.Mkdir(fallbackCwd, 0o700); err != nil {
		t.Fatal(err)
	}
	gotRoot, err = DiscoverProjectRoot(filepath.Join(fallbackCwd, "."))
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot != fallbackCwd {
		t.Fatalf("cwd fallback root = %q, want %q", gotRoot, fallbackCwd)
	}

	s := testSupervisor(t, Options{StopGrace: 20 * time.Millisecond})
	first, err := startShell(s, rootA, "api", "sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	_, err = startShell(s, nested, "api", "sleep 30")
	if !errors.Is(err, ErrNameInUse) {
		t.Fatalf("same-root duplicate error = %v, want ErrNameInUse", err)
	}
	if _, err = startShell(s, rootB, "api", "sleep 30"); err != nil {
		t.Fatalf("same name in isolated root: %v", err)
	}
	if first.Root != rootA || first.PID <= 0 || first.PGID != first.PID {
		t.Fatalf("first process identity = %#v", first)
	}
	_, err = startShell(s, rootA, "api", "true")
	if err == nil {
		t.Fatal("duplicate running name unexpectedly succeeded")
	}
	var duplicate *DuplicateError
	if !errors.As(err, &duplicate) {
		t.Fatalf("duplicate error = %v, want DuplicateError", err)
	}
	if duplicate.PID != first.PID || !strings.Contains(err.Error(), strconv.Itoa(first.PID)) {
		t.Fatalf("duplicate error = %v, want PID %d", err, first.PID)
	}
	for _, unsafe := range []string{"", ".api", "-api", "api/name", "api name", strings.Repeat("a", 65)} {
		if _, err := startShell(s, rootA, unsafe, "true"); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("name %q error = %v, want ErrInvalidName", unsafe, err)
		}
	}
}

func TestStopProcessTree(t *testing.T) {
	root := makeProject(t, false)
	s := testSupervisor(t, Options{StopGrace: 25 * time.Millisecond})
	model, err := startShell(s, root, "tree", "trap '' TERM; sleep 30 & wait")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := s.Stop(context.Background(), root, "tree"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bounded stop took %s", elapsed)
	}
	stopped, err := s.Get(root, "tree")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != StateExited || stopped.Exit == nil {
		t.Fatalf("stopped model = %#v", stopped)
	}
	if stopped.ExitCode != -1 {
		t.Fatalf("stopped exit code = %d, want signal status -1 (started as %d)", stopped.ExitCode, model.PID)
	}
}

func TestStopTreatsESRCHAsExited(t *testing.T) {
	root := makeProject(t, false)
	child := &esrchChild{done: make(chan struct{})}
	s := testSupervisor(t, Options{
		StartProcess: func(process.Spec) (Child, error) {
			return child, nil
		},
	})
	if _, err := s.Start(StartRequest{
		Name: "gone",
		Cwd:  root,
		Argv: []string{"/bin/true"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(context.Background(), root, "gone"); err != nil {
		t.Fatalf("stop after ESRCH = %v, want nil", err)
	}
	model, err := s.Get(root, "gone")
	if err != nil {
		t.Fatal(err)
	}
	if model.State != StateExited {
		t.Fatalf("ESRCH model state = %s, want exited", model.State)
	}
}

func TestShutdownProcessTrees(t *testing.T) {
	root := makeProject(t, false)
	s := testSupervisor(t, Options{StopGrace: 25 * time.Millisecond})
	for _, name := range []string{"one", "two", "three"} {
		if _, err := startShell(s, root, name, "trap '' TERM; sleep 30 & wait"); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := s.List(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("shutdown list length = %d, want 3", len(items))
	}
	for _, item := range items {
		if item.State != StateExited {
			t.Errorf("%s state = %s, want exited", item.Name, item.State)
		}
	}
}

func TestShutdownCompletesAfterCanceledContext(t *testing.T) {
	root := makeProject(t, false)
	s := testSupervisor(t, Options{StopGrace: 10 * time.Millisecond})
	if _, err := startShell(s, root, "stubborn", "trap '' TERM; sleep 30 & wait"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled shutdown error = %v, want context.Canceled after cleanup", err)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("subsequent shutdown after canceled cleanup = %v, want nil", err)
	}
	items, err := s.List(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "stubborn" || items[0].State != StateExited {
		t.Fatalf("canceled shutdown records = %#v, want one exited record", items)
	}
}

func TestStatusRunningSnapshotMetadataAndCursor(t *testing.T) {
	root := makeProject(t, false)
	startedAt := time.Unix(10, 20)
	child := &timedChild{
		pid:  4101,
		done: make(chan struct{}),
		result: process.Result{
			ExitCode: 0,
			ExitedAt: time.Unix(11, 0),
		},
	}
	s := testSupervisor(t, Options{
		Now: func() time.Time { return startedAt },
		StartProcess: func(spec process.Spec) (Child, error) {
			_, err := spec.Output.Append(output.Stdout, startedAt, "ready\n")
			return child, err
		},
	})

	got, err := s.Start(StartRequest{
		Name: "running",
		Cwd:  root,
		Argv: []string{"/bin/test", "--ready"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "running" || got.Root != root || got.PID != 4101 || got.PGID != 4101 || got.Cwd != root {
		t.Fatalf("running identity = %#v, want name/root/pid/pgid/cwd %q/%q/4101/4101/%q", got, "running", root, root)
	}
	if len(got.Argv) != 2 || got.Argv[0] != "/bin/test" || got.Argv[1] != "--ready" {
		t.Fatalf("running argv = %#v, want [/bin/test --ready]", got.Argv)
	}
	if !got.Start.Equal(startedAt) {
		t.Fatalf("running start = %v, want %v", got.Start, startedAt)
	}
	if got.LaunchCursor != 0 || got.NextCursor != 1 {
		t.Fatalf("running cursors = launch %d, next %d; want 0, 1", got.LaunchCursor, got.NextCursor)
	}
	if got.State != StateRunning || got.Exit != nil || got.ExitCode != 0 || !got.ExitedAt.IsZero() || got.RestartCount != 0 {
		t.Fatalf("running lifecycle metadata = %#v", got)
	}
}

func TestStatusOutputAdvancesCursorWithoutMutatingSnapshotState(t *testing.T) {
	root := makeProject(t, false)
	child := &timedChild{pid: 4102, done: make(chan struct{}), result: process.Result{ExitCode: 0}}
	s := testSupervisor(t, Options{
		StartProcess: func(process.Spec) (Child, error) {
			return child, nil
		},
	})
	before, err := s.Start(StartRequest{
		Name: "output",
		Cwd:  root,
		Argv: []string{"/bin/test", "output"},
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := s.Output(root, "output")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(output.Stdout, time.Unix(1, 0), "one\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(output.Stderr, time.Unix(2, 0), "two\n"); err != nil {
		t.Fatal(err)
	}
	after, err := s.Get(root, "output")
	if err != nil {
		t.Fatal(err)
	}
	if before.NextCursor != 0 || after.NextCursor != 2 {
		t.Fatalf("status cursors before/after output = %d/%d; want 0/2", before.NextCursor, after.NextCursor)
	}
	if after.Name != before.Name || after.Root != before.Root || after.PID != before.PID ||
		after.PGID != before.PGID || after.Cwd != before.Cwd || !after.Start.Equal(before.Start) ||
		after.State != StateRunning || after.Exit != nil || after.RestartCount != before.RestartCount {
		t.Fatalf("status metadata changed after output = before %#v, after %#v", before, after)
	}
}

func TestStatusExitedSnapshotIncludesTerminalResult(t *testing.T) {
	root := makeProject(t, false)
	exitAt := time.Unix(21, 0)
	child := &timedChild{
		pid:  4103,
		done: make(chan struct{}),
		result: process.Result{
			ExitCode: 7,
			ExitedAt: exitAt,
		},
	}
	s := testSupervisor(t, Options{
		StartProcess: func(process.Spec) (Child, error) {
			return child, nil
		},
	})
	if _, err := s.Start(StartRequest{Name: "exited", Cwd: root, Argv: []string{"/bin/test", "exited"}}); err != nil {
		t.Fatal(err)
	}
	done := recordDone(t, s, root, "exited")
	child.release()
	waitSubscriptionSignal(t, done, "exited process")

	got, err := s.Get(root, "exited")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateExited || got.Exit == nil || got.Exit.ExitCode != 7 || got.ExitCode != 7 ||
		!got.Exit.ExitedAt.Equal(exitAt) || !got.ExitedAt.Equal(exitAt) || got.NextCursor != 0 {
		t.Fatalf("exited status = %#v, want terminal code 7 at %v", got, exitAt)
	}
}

func TestStatusSignaledSnapshotIncludesTerminalResult(t *testing.T) {
	root := makeProject(t, false)
	exitAt := time.Unix(22, 0)
	child := &timedChild{
		pid:  4104,
		done: make(chan struct{}),
		result: process.Result{
			ExitCode: -1,
			ExitedAt: exitAt,
		},
	}
	s := testSupervisor(t, Options{
		StartProcess: func(process.Spec) (Child, error) {
			return child, nil
		},
	})
	if _, err := s.Start(StartRequest{Name: "signaled", Cwd: root, Argv: []string{"/bin/test", "signaled"}}); err != nil {
		t.Fatal(err)
	}
	done := recordDone(t, s, root, "signaled")
	child.release()
	waitSubscriptionSignal(t, done, "signaled process")

	got, err := s.Get(root, "signaled")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateExited || got.Exit == nil || got.Exit.ExitCode != -1 || got.ExitCode != -1 ||
		!got.Exit.ExitedAt.Equal(exitAt) || !got.ExitedAt.Equal(exitAt) {
		t.Fatalf("signaled status = %#v, want terminal code -1 at %v", got, exitAt)
	}
}

func TestStatusInvalidNameErrorIsTyped(t *testing.T) {
	root := makeProject(t, false)
	s := testSupervisor(t, Options{})
	_, err := s.Get(root, "bad/name")
	var invalid *InvalidNameError
	if !errors.As(err, &invalid) || !errors.Is(err, ErrInvalidName) {
		t.Fatalf("invalid status name error = %v, want typed InvalidNameError", err)
	}
}

func TestStatusMissingProcessErrorIsTyped(t *testing.T) {
	root := makeProject(t, false)
	s := testSupervisor(t, Options{})
	_, err := s.Get(root, "missing")
	var notFound *NotFoundError
	if !errors.As(err, &notFound) || !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("missing status process error = %v, want typed NotFoundError", err)
	}
	if notFound.Root != root || notFound.Name != "missing" {
		t.Fatalf("missing status process details = %#v, want root/name %q/%q", notFound, root, "missing")
	}
}

func TestStatusReturnedArgvIsImmutable(t *testing.T) {
	root := makeProject(t, false)
	child := &timedChild{pid: 4105, done: make(chan struct{}), result: process.Result{ExitCode: 0}}
	s := testSupervisor(t, Options{
		StartProcess: func(process.Spec) (Child, error) {
			return child, nil
		},
	})
	argv := []string{"/bin/test", "--mode", "watch"}
	model, err := s.Start(StartRequest{Name: "argv", Cwd: root, Argv: argv})
	if err != nil {
		t.Fatal(err)
	}
	argv[1] = "request-mutated"
	if model.Argv[1] != "--mode" {
		t.Fatalf("start status argv changed through request mutation = %#v", model.Argv)
	}
	model.Argv[1] = "model-mutated"

	got, err := s.Get(root, "argv")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Argv) != 3 || got.Argv[0] != "/bin/test" || got.Argv[1] != "--mode" || got.Argv[2] != "watch" {
		t.Fatalf("status argv after returned-slice mutation = %#v, want original argv", got.Argv)
	}
	got.Argv[2] = "get-mutated"
	again, err := s.Get(root, "argv")
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Argv) != 3 || again.Argv[0] != "/bin/test" || again.Argv[1] != "--mode" || again.Argv[2] != "watch" {
		t.Fatalf("status argv after second returned-slice mutation = %#v, want original argv", again.Argv)
	}
}

func TestReadModelsExcludeEnvironment(t *testing.T) {
	root := makeProject(t, false)
	s := testSupervisor(t, Options{StopGrace: 10 * time.Millisecond})
	request := StartRequest{
		Name: "secret",
		Cwd:  root,
		Argv: []string{"/bin/sh", "-c", "sleep 30"},
		Env:  []string{"TOKEN=do-not-return"},
	}
	model, err := s.Start(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Env[0] = "TOKEN=mutated"
	request.Argv[2] = "true"
	if model.Argv[2] != "sleep 30" {
		t.Fatalf("start model argv changed through request mutation: %#v", model.Argv)
	}
	model.Argv[2] = "changed externally"
	got, err := s.Get(root, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if got.Argv[2] != "sleep 30" {
		t.Fatalf("stored argv changed through read-model mutation: %#v", got.Argv)
	}
	typ := reflect.TypeOf(Process{})
	for _, field := range []string{"Env", "Environment", "Output", "Store"} {
		if _, ok := typ.FieldByName(field); ok {
			t.Fatalf("read model exposes private field %s", field)
		}
	}
	if _, err := s.Output(root, "secret"); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyEnvironmentPassedToChild(t *testing.T) {
	root := makeProject(t, false)
	type observation struct {
		nilEnv bool
		length int
	}
	var observed []observation
	s := testSupervisor(t, Options{
		StartProcess: func(spec process.Spec) (Child, error) {
			observed = append(observed, observation{nilEnv: spec.Env == nil, length: len(spec.Env)})
			child := &esrchChild{done: make(chan struct{})}
			child.exit()
			return child, nil
		},
	})
	for _, item := range []struct {
		name string
		env  []string
	}{
		{name: "nil-env", env: nil},
		{name: "empty-env", env: []string{}},
	} {
		if _, err := s.Start(StartRequest{
			Name: item.name,
			Cwd:  root,
			Argv: []string{"/bin/true"},
			Env:  item.env,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(observed) != 2 {
		t.Fatalf("child launch observations = %d, want 2", len(observed))
	}
	for i, got := range observed {
		if got.nilEnv || got.length != 0 {
			t.Errorf("child launch %d env = %#v, want non-nil empty environment", i, got)
		}
	}
}

func TestCompletedRecordRetention(t *testing.T) {
	root := makeProject(t, false)
	otherRoot := makeProject(t, false)
	s := testSupervisor(t, Options{
		CompletedLimit: 2,
		StopGrace:      10 * time.Millisecond,
		OutputLimits: output.Limits{
			RetainedBytes:      1024,
			DefaultReadEntries: 16,
			DefaultReadBytes:   1024,
		},
	})
	active, err := startShell(s, root, "active", "sleep 30")
	if err != nil {
		t.Fatal(err)
	}

	var oldest *record
	var oldestStore *output.Store
	for i, text := range []string{"first", "second", "third"} {
		name := fmt.Sprintf("done%d", i)
		launchRoot := root
		if i == 1 {
			launchRoot = otherRoot
		}
		if _, err := startShell(s, launchRoot, name, "printf '"+text+"'"); err != nil {
			t.Fatal(err)
		}
		waitExited(t, s, launchRoot, name)
		if i != 0 {
			continue
		}
		key := keyFor(launchRoot, name)
		s.mu.RLock()
		oldest = s.records[key]
		if oldest == nil || len(oldest.env) == 0 || oldest.store == nil {
			s.mu.RUnlock()
			t.Fatalf("completed record before eviction = %#v, want environment and output retained", oldest)
		}
		s.mu.RUnlock()
		oldestStore, err = s.Output(launchRoot, name)
		if err != nil {
			t.Fatalf("completed output before eviction: %v", err)
		}
		result, err := oldestStore.Read(output.ReadOptions{})
		if err != nil || len(result.Entries) == 0 {
			t.Fatalf("completed output before eviction = %#v, err %v", result, err)
		}
	}
	if _, err := s.Get(root, "done0"); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("oldest completed lookup error = %v, want ErrProcessNotFound", err)
	}
	if _, err := s.Output(root, "done0"); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("evicted output lookup error = %v, want ErrProcessNotFound", err)
	}
	for _, item := range []struct {
		root string
		name string
	}{
		{root: otherRoot, name: "done1"},
		{root: root, name: "done2"},
	} {
		model, err := s.Get(item.root, item.name)
		if err != nil || model.State != StateExited {
			t.Fatalf("retained %s = %#v, err %v", item.name, model, err)
		}
	}
	if got, err := s.Get(root, "active"); err != nil || got.State != StateRunning {
		t.Fatalf("active record after completed eviction = %#v, err %v", got, err)
	}
	if activeModel, err := s.Get(root, "active"); err != nil || activeModel.PID != active.PID {
		t.Fatalf("active identity after eviction = %#v, err %v", activeModel, err)
	}
	s.mu.RLock()
	evicted := s.records[keyFor(root, "done0")]
	clearedEnv := oldest != nil && oldest.env == nil
	clearedStore := oldest != nil && oldest.store == nil
	s.mu.RUnlock()
	if evicted != nil {
		t.Fatalf("evicted record still registered: %#v", evicted)
	}
	if !clearedEnv || !clearedStore {
		t.Fatalf("evicted record references: env-cleared=%t store-cleared=%t", clearedEnv, clearedStore)
	}
}

func TestCompletedRetentionUsesExitTime(t *testing.T) {
	roots := []string{
		makeProject(t, false),
		makeProject(t, false),
		makeProject(t, false),
	}
	children := map[string]*timedChild{
		"older": {
			pid:    2001,
			done:   make(chan struct{}),
			result: process.Result{ExitCode: 0, ExitedAt: time.Unix(1, 0)},
		},
		"newer": {
			pid:    2002,
			done:   make(chan struct{}),
			result: process.Result{ExitCode: 0, ExitedAt: time.Unix(3, 0)},
		},
		"kept": {
			pid:    2003,
			done:   make(chan struct{}),
			result: process.Result{ExitCode: 0, ExitedAt: time.Unix(2, 0)},
		},
	}
	s := testSupervisor(t, Options{
		CompletedLimit: 2,
		StartProcess: func(spec process.Spec) (Child, error) {
			name := spec.Argv[1]
			child := children[name]
			if child == nil {
				return nil, fmt.Errorf("unknown test child %q", name)
			}
			return child, nil
		},
	})
	launches := []struct {
		root string
		name string
	}{
		{root: roots[0], name: "older"},
		{root: roots[1], name: "newer"},
		{root: roots[2], name: "kept"},
	}
	done := make(map[string]<-chan struct{}, len(launches))
	for _, launch := range launches {
		if _, err := s.Start(StartRequest{
			Name: launch.name,
			Cwd:  launch.root,
			Argv: []string{"/bin/true", launch.name},
			Env:  []string{"TOKEN=retained"},
		}); err != nil {
			t.Fatal(err)
		}
		done[launch.name] = recordDone(t, s, launch.root, launch.name)
	}
	children["newer"].release()
	<-done["newer"]
	children["kept"].release()
	<-done["kept"]
	children["older"].release()
	<-done["older"]

	s.mu.RLock()
	_, oldPresent := s.records[keyFor(roots[0], "older")]
	s.mu.RUnlock()
	if oldPresent {
		t.Fatal("completed record with oldest exit time was retained")
	}
	for _, launch := range launches[1:] {
		model, err := s.Get(launch.root, launch.name)
		if err != nil || model.State != StateExited {
			t.Fatalf("retained %s = %#v, err %v", launch.name, model, err)
		}
	}
}

func TestSubscribeAfterProcessExit(t *testing.T) {
	root := makeProject(t, false)
	child := newSubscriptionChild(3101, 7, time.Unix(1, 0), "late-output\n")
	s := testSupervisor(t, Options{
		CompletedLimit: 2,
		Now:            func() time.Time { return time.Unix(0, 0) },
		OutputLimits: output.Limits{
			RetainedBytes:      1024,
			DefaultReadEntries: 16,
			DefaultReadBytes:   1024,
		},
		StartProcess: subscriptionStarter(map[string]*subscriptionChild{"late": child}),
	})
	if _, err := s.Start(StartRequest{
		Name: "late",
		Cwd:  root,
		Argv: []string{"/bin/fake", "late"},
	}); err != nil {
		t.Fatal(err)
	}
	done := recordDone(t, s, root, "late")
	child.release()
	waitSubscriptionSignal(t, done, "late process exit")

	sub, err := s.Subscribe(root, "late", output.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertSubscriptionOutput(t, sub, "late-output\n")
	assertSubscriptionExit(t, sub, 7)
	assertNoSubscriptionEvent(t, sub)
}

func TestSubscribeDoesNotWaitForChildDone(t *testing.T) {
	root := makeProject(t, false)
	child := newSubscriptionChild(3102, 13, time.Unix(2, 0), "raced-output\n")
	child.doneObserved = make(chan struct{})
	child.allowDoneCheck = make(chan struct{})
	defer child.releaseDoneCheck()
	s := testSupervisor(t, Options{
		CompletedLimit: 2,
		Now:            func() time.Time { return time.Unix(1, 0) },
		OutputLimits: output.Limits{
			RetainedBytes:      1024,
			DefaultReadEntries: 16,
			DefaultReadBytes:   1024,
		},
		StartProcess: subscriptionStarter(map[string]*subscriptionChild{"raced": child}),
	})
	if _, err := s.Start(StartRequest{
		Name: "raced",
		Cwd:  root,
		Argv: []string{"/bin/fake", "raced"},
	}); err != nil {
		t.Fatal(err)
	}
	done := recordDone(t, s, root, "raced")

	result := make(chan subscriptionResult, 1)
	go func() {
		sub, err := s.Subscribe(root, "raced", output.ReadOptions{})
		result <- subscriptionResult{sub: sub, err: err}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var got subscriptionResult
	select {
	case got = <-result:
	case <-ctx.Done():
		t.Fatal("Subscribe waited for child Done")
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.sub == nil {
		t.Fatal("Subscribe returned a nil subscription")
	}
	select {
	case <-child.done:
		t.Fatal("child exited before the NotifyExit-before-Done check")
	default:
	}
	select {
	case <-child.doneObserved:
		t.Fatal("Subscribe inspected child Done")
	default:
	}

	child.release()
	waitSubscriptionSignal(t, done, "raced process exit")
	assertSubscriptionOutput(t, got.sub, "raced-output\n")
	assertSubscriptionExit(t, got.sub, 13)
	assertNoSubscriptionEvent(t, got.sub)
}

func TestSubscribeRunningProcessSkipsHistoricalExitAndDoesNotDuplicateCurrent(t *testing.T) {
	root := makeProject(t, false)
	child := newSubscriptionChild(3103, 17, time.Unix(6, 0), "running-output\n")
	s := testSupervisor(t, Options{
		CompletedLimit: 2,
		Now:            func() time.Time { return time.Unix(5, 0) },
		OutputLimits: output.Limits{
			RetainedBytes:      1024,
			DefaultReadEntries: 16,
			DefaultReadBytes:   1024,
		},
		StartProcess: subscriptionStarter(map[string]*subscriptionChild{"running": child}),
	})
	if _, err := s.Start(StartRequest{
		Name: "running",
		Cwd:  root,
		Argv: []string{"/bin/fake", "running"},
	}); err != nil {
		t.Fatal(err)
	}
	done := recordDone(t, s, root, "running")
	if child.store == nil {
		t.Fatal("running child did not receive an output store")
	}
	child.store.NotifyExit(output.Exit{Code: 99, Time: time.Unix(4, 0)})

	sub, err := s.Subscribe(root, "running", output.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertSubscriptionOutput(t, sub, "running-output\n")

	child.release()
	waitSubscriptionSignal(t, done, "running process exit")
	assertSubscriptionExit(t, sub, 17)
	assertNoSubscriptionEvent(t, sub)
}

func TestSubscribeSurvivesCompletedLimitEviction(t *testing.T) {
	root := makeProject(t, false)
	first := newSubscriptionChild(3104, 21, time.Unix(5, 0), "first-output\n")
	second := newSubscriptionChild(3105, 22, time.Unix(6, 0), "second-output\n")
	s := testSupervisor(t, Options{
		CompletedLimit: 1,
		Now:            func() time.Time { return time.Unix(0, 0) },
		OutputLimits: output.Limits{
			RetainedBytes:      1024,
			DefaultReadEntries: 16,
			DefaultReadBytes:   1024,
		},
		StartProcess: subscriptionStarter(map[string]*subscriptionChild{
			"first":  first,
			"second": second,
		}),
	})
	if _, err := s.Start(StartRequest{
		Name: "first",
		Cwd:  root,
		Argv: []string{"/bin/fake", "first"},
	}); err != nil {
		t.Fatal(err)
	}
	firstDone := recordDone(t, s, root, "first")
	first.release()
	waitSubscriptionSignal(t, firstDone, "first process exit")

	sub, err := s.Subscribe(root, "first", output.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(StartRequest{
		Name: "second",
		Cwd:  root,
		Argv: []string{"/bin/fake", "second"},
	}); err != nil {
		t.Fatal(err)
	}
	secondDone := recordDone(t, s, root, "second")
	second.release()
	waitSubscriptionSignal(t, secondDone, "second process exit and eviction")

	if _, err := s.Get(root, "first"); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("evicted first lookup error = %v, want ErrProcessNotFound", err)
	}
	if _, err := s.Output(root, "first"); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("evicted first output lookup error = %v, want ErrProcessNotFound", err)
	}
	assertSubscriptionOutput(t, sub, "first-output\n")
	assertSubscriptionExit(t, sub, 21)
	assertNoSubscriptionEvent(t, sub)
}

func TestSignalForwardsInterrupt(t *testing.T) {
	root := makeProject(t, false)
	s := testSupervisor(t, Options{StopGrace: 20 * time.Millisecond})
	if _, err := startShell(s, root, "interrupt", "trap 'exit 17' INT; printf 'interrupt-ready\\n'; while :; do :; done"); err != nil {
		t.Fatal(err)
	}
	done := recordDone(t, s, root, "interrupt")

	store, err := s.Output(root, "interrupt")
	if err != nil {
		t.Fatal(err)
	}
	subscription := store.Subscribe(output.ReadOptions{})
	waitForOutput(t, subscription, "interrupt-ready\n")
	if err := s.Signal(root, "interrupt", syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("process remained running after SIGINT")
	}
	model, err := s.Get(root, "interrupt")
	if err != nil {
		t.Fatal(err)
	}
	if model.ExitCode != 17 {
		t.Fatalf("SIGINT exit code = %d, want 17", model.ExitCode)
	}
}
