package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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

type delayedExitChild struct {
	pid       int
	done      chan struct{}
	published chan struct{}
	store     *output.Store
	result    process.Result
	publish   sync.Once
	doneOnce  sync.Once
}

func newDelayedExitChild(pid, code int, at time.Time) *delayedExitChild {
	return &delayedExitChild{
		pid:       pid,
		done:      make(chan struct{}),
		published: make(chan struct{}),
		result:    process.Result{ExitCode: code, ExitedAt: at},
	}
}

func (c *delayedExitChild) PID() int  { return c.pid }
func (c *delayedExitChild) PGID() int { return c.pid }
func (c *delayedExitChild) Done() <-chan struct{} {
	return c.done
}
func (c *delayedExitChild) Wait() process.Result {
	<-c.done
	return c.result
}
func (c *delayedExitChild) Signal(os.Signal) error {
	c.publish.Do(func() {
		c.store.NotifyExit(output.Exit{Code: c.result.ExitCode, Time: c.result.ExitedAt})
		close(c.published)
	})
	return os.ErrProcessDone
}
func (c *delayedExitChild) releaseDone() {
	c.doneOnce.Do(func() { close(c.done) })
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

	if _, err := s.Get(root, "first"); err != nil {
		t.Fatalf("followed first session was evicted: %v", err)
	}
	assertSubscriptionOutput(t, sub, "first-output\n")
	assertNoSubscriptionEvent(t, sub)
	sub.Close()
	if _, err := s.Get(root, "first"); err != nil {
		t.Fatalf("bounded retained first session disappeared: %v", err)
	}
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

type waitCallResult struct {
	result WaitResult
	err    error
}

func awaitWaitResult(t *testing.T, result <-chan waitCallResult) waitCallResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(time.Second):
		t.Fatal("wait did not return")
		return waitCallResult{}
	}
}

func TestWaitBufferedDefaultAndExplicitCursorZero(t *testing.T) {
	root := makeProject(t, false)
	child := newSubscriptionChild(5001, 0, time.Unix(11, 0), "buffered-ready\n")
	s := testSupervisor(t, Options{
		Now: func() time.Time { return time.Unix(10, 0) },
		OutputLimits: output.Limits{
			RetainedBytes:      1024,
			DefaultReadEntries: 16,
			DefaultReadBytes:   1024,
		},
		StartProcess: subscriptionStarter(map[string]*subscriptionChild{"buffered": child}),
	})
	if _, err := s.Start(StartRequest{Name: "buffered", Cwd: root, Argv: []string{"/bin/fake", "buffered"}}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := s.Wait(ctx, root, "buffered", WaitOptions{Match: regexp.MustCompile(`buffered-ready`)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != WaitMatched || got.Cursor != 0 || got.Exit != nil {
		t.Fatalf("default launch wait = %#v, want matched cursor 0 without exit", got)
	}

	zero := output.Cursor(0)
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer timeoutCancel()
	got, err = s.Wait(timeoutCtx, root, "buffered", WaitOptions{
		After: &zero,
		Match: regexp.MustCompile(`buffered-ready`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != WaitTimedOut || got.Cursor != 0 {
		t.Fatalf("explicit cursor-zero wait = %#v, want timed_out cursor 0", got)
	}
}

func TestWaitBufferedAndNewMatchCoexistWithFollower(t *testing.T) {
	root := makeProject(t, false)
	child := newSubscriptionChild(5002, 0, time.Unix(21, 0), "before\n")
	s := testSupervisor(t, Options{
		Now: func() time.Time { return time.Unix(20, 0) },
		OutputLimits: output.Limits{
			RetainedBytes:      1024,
			DefaultReadEntries: 16,
			DefaultReadBytes:   1024,
		},
		StartProcess: subscriptionStarter(map[string]*subscriptionChild{"new-match": child}),
	})
	if _, err := s.Start(StartRequest{Name: "new-match", Cwd: root, Argv: []string{"/bin/fake", "new-match"}}); err != nil {
		t.Fatal(err)
	}
	store := child.store
	if store == nil {
		t.Fatal("wait test child did not receive output store")
	}
	follower := store.Subscribe(output.ReadOptions{})
	defer follower.Close()

	result := make(chan waitCallResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		waitResult, waitErr := s.Wait(ctx, root, "new-match", WaitOptions{Match: regexp.MustCompile(`ready`)})
		result <- waitCallResult{result: waitResult, err: waitErr}
	}()

	if _, err := store.Append(output.Stdout, time.Unix(22, 0), "ready\n"); err != nil {
		t.Fatal(err)
	}
	got := awaitWaitResult(t, result)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.result.Outcome != WaitMatched || got.result.Cursor != 1 {
		t.Fatalf("new matching output wait = %#v, want matched cursor 1", got.result)
	}

	followerEvent := nextSubscriptionEvent(t, follower)
	if followerEvent.Read == nil || len(followerEvent.Read.Entries) != 2 ||
		followerEvent.Read.Entries[0].Cursor != 0 || followerEvent.Read.Entries[1].Cursor != 1 {
		t.Fatalf("coexisting follower event = %#v, want both buffered and appended entries", followerEvent)
	}
}

func TestWaitExitWakeupWithAndWithoutMatch(t *testing.T) {
	root := makeProject(t, false)
	tests := []struct {
		name  string
		match *regexp.Regexp
		code  int
		at    time.Time
	}{
		{name: "exit-match", match: regexp.MustCompile(`never`), code: 3, at: time.Unix(31, 0)},
		{name: "exit-any", code: 0, at: time.Unix(32, 0)},
	}
	children := make(map[string]*subscriptionChild, len(tests))
	for _, test := range tests {
		children[test.name] = newSubscriptionChild(5100+test.code, test.code, test.at, "noise\n")
	}
	s := testSupervisor(t, Options{
		Now: func() time.Time { return time.Unix(30, 0) },
		OutputLimits: output.Limits{
			RetainedBytes:      1024,
			DefaultReadEntries: 16,
			DefaultReadBytes:   1024,
		},
		StartProcess: subscriptionStarter(children),
	})
	for _, test := range tests {
		if _, err := s.Start(StartRequest{Name: test.name, Cwd: root, Argv: []string{"/bin/fake", test.name}}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		result := make(chan waitCallResult, 1)
		go func(test struct {
			name  string
			match *regexp.Regexp
			code  int
			at    time.Time
		}) {
			waitResult, waitErr := s.Wait(ctx, root, test.name, WaitOptions{Match: test.match})
			result <- waitCallResult{result: waitResult, err: waitErr}
		}(test)
		deadline := time.Now().Add(time.Second)
		for children[test.name].store.SubscriberCount() == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		children[test.name].release()
		got := awaitWaitResult(t, result)
		cancel()
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.result.Outcome != WaitExited || got.result.Cursor != 0 || got.result.Exit == nil ||
			got.result.Exit.ExitCode != test.code || !got.result.Exit.ExitedAt.Equal(test.at) {
			t.Fatalf("exit wait %q = %#v, want exited code %d at %v", test.name, got.result, test.code, test.at)
		}
	}
}

func TestWaitTimeoutCursorCancellationAndConcurrentWaiters(t *testing.T) {
	root := makeProject(t, false)
	children := map[string]*subscriptionChild{
		"timeout": newSubscriptionChild(5201, 0, time.Unix(41, 0), ""),
		"cancel":  newSubscriptionChild(5202, 0, time.Unix(42, 0), ""),
		"many":    newSubscriptionChild(5203, 0, time.Unix(43, 0), "prefix\n"),
	}
	s := testSupervisor(t, Options{
		Now: func() time.Time { return time.Unix(40, 0) },
		OutputLimits: output.Limits{
			RetainedBytes:      1024,
			DefaultReadEntries: 16,
			DefaultReadBytes:   1024,
		},
		StartProcess: subscriptionStarter(children),
	})
	for name := range children {
		if _, err := s.Start(StartRequest{Name: name, Cwd: root, Argv: []string{"/bin/fake", name}}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := children["timeout"].store.Append(output.Stdout, time.Unix(44, 0), "ignored-one\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := children["timeout"].store.Append(output.Stdout, time.Unix(45, 0), "ignored-two\n"); err != nil {
		t.Fatal(err)
	}
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	timeoutResult := make(chan waitCallResult, 1)
	go func() {
		waitResult, waitErr := s.Wait(timeoutCtx, root, "timeout", WaitOptions{Match: regexp.MustCompile(`never`)})
		timeoutResult <- waitCallResult{result: waitResult, err: waitErr}
	}()
	got := awaitWaitResult(t, timeoutResult)
	timeoutCancel()
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.result.Outcome != WaitTimedOut || got.result.Cursor != 1 {
		t.Fatalf("timeout wait = %#v, want timed_out cursor 1", got.result)
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancelResult := make(chan waitCallResult, 1)
	go func() {
		waitResult, waitErr := s.Wait(cancelCtx, root, "cancel", WaitOptions{Match: regexp.MustCompile(`never`)})
		cancelResult <- waitCallResult{result: waitResult, err: waitErr}
	}()
	cancel()
	canceled := awaitWaitResult(t, cancelResult)
	if !errors.Is(canceled.err, context.Canceled) {
		t.Fatalf("canceled wait error = %v, want context.Canceled", canceled.err)
	}
	if _, err := children["cancel"].store.Append(output.Stdout, time.Unix(46, 0), "after-cancel\n"); err != nil {
		t.Fatal(err)
	}
	postCancelCtx, postCancel := context.WithTimeout(context.Background(), time.Second)
	defer postCancel()
	postCancelResult, err := s.Wait(postCancelCtx, root, "cancel", WaitOptions{Match: regexp.MustCompile(`after-cancel`)})
	if err != nil {
		t.Fatal(err)
	}
	if postCancelResult.Outcome != WaitMatched || postCancelResult.Cursor != 0 {
		t.Fatalf("wait after canceled wait = %#v, want matched cursor 0", postCancelResult)
	}

	const waiters = 2
	results := make(chan waitCallResult, waiters)
	ctx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	for range waiters {
		go func() {
			waitResult, waitErr := s.Wait(ctx, root, "many", WaitOptions{Match: regexp.MustCompile(`ready`)})
			results <- waitCallResult{result: waitResult, err: waitErr}
		}()
	}
	if _, err := children["many"].store.Append(output.Stdout, time.Unix(46, 0), "ready\n"); err != nil {
		t.Fatal(err)
	}
	var cursors []output.Cursor
	for range waiters {
		waited := awaitWaitResult(t, results)
		if waited.err != nil {
			t.Fatal(waited.err)
		}
		if waited.result.Outcome != WaitMatched {
			t.Fatalf("concurrent wait result = %#v, want matched", waited.result)
		}
		cursors = append(cursors, waited.result.Cursor)
	}
	if len(cursors) != waiters || cursors[0] != 1 || cursors[1] != 1 {
		t.Fatalf("concurrent wait cursors = %#v, want two monotonic cursor 1 results", cursors)
	}
}

func TestWaitMatchesLineWithinSupervisorMaxLineBytes(t *testing.T) {
	root := makeProject(t, false)
	largeLine := strings.Repeat("x", output.DefaultReadBytes) + "needle\n"
	child := newSubscriptionChild(5301, 0, time.Unix(51, 0), largeLine)
	s := testSupervisor(t, Options{
		MaxLineBytes: len(largeLine),
		OutputLimits: output.Limits{
			RetainedBytes:      len(largeLine) * 2,
			DefaultReadEntries: 16,
			DefaultReadBytes:   output.DefaultReadBytes,
		},
		StartProcess: subscriptionStarter(map[string]*subscriptionChild{"large-match": child}),
	})
	if _, err := s.Start(StartRequest{Name: "large-match", Cwd: root, Argv: []string{"/bin/fake", "large-match"}}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := s.Wait(ctx, root, "large-match", WaitOptions{Match: regexp.MustCompile(`needle`)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != WaitMatched || got.Cursor != 0 || got.Exit != nil {
		t.Fatalf("large-line match wait = %#v, want matched cursor 0 without exit", got)
	}
}

func TestWaitReachesExitAfterLineWithinSupervisorMaxLineBytes(t *testing.T) {
	root := makeProject(t, false)
	largeLine := strings.Repeat("x", output.DefaultReadBytes) + "\n"
	child := newSubscriptionChild(5302, 7, time.Unix(52, 0), largeLine)
	s := testSupervisor(t, Options{
		Now:          func() time.Time { return time.Unix(51, 0) },
		MaxLineBytes: len(largeLine),
		OutputLimits: output.Limits{
			RetainedBytes:      len(largeLine) * 2,
			DefaultReadEntries: 16,
			DefaultReadBytes:   output.DefaultReadBytes,
		},
		StartProcess: subscriptionStarter(map[string]*subscriptionChild{"large-exit": child}),
	})
	if _, err := s.Start(StartRequest{Name: "large-exit", Cwd: root, Argv: []string{"/bin/fake", "large-exit"}}); err != nil {
		t.Fatal(err)
	}
	child.release()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := s.Wait(ctx, root, "large-exit", WaitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != WaitExited || got.Cursor != 0 || got.Exit == nil || got.Exit.ExitCode != 7 ||
		!got.Exit.ExitedAt.Equal(child.result.ExitedAt) {
		t.Fatalf("large-line exit wait = %#v, want exited code 7 at %v", got, child.result.ExitedAt)
	}
}

func TestRestartPreservesLaunchAndOutputState(t *testing.T) {
	root := makeProject(t, false)
	children := []*timedChild{
		{pid: 4101, done: make(chan struct{}), result: process.Result{ExitCode: 0, ExitedAt: time.Now()}},
		{pid: 4102, done: make(chan struct{}), result: process.Result{ExitCode: 0, ExitedAt: time.Now().Add(time.Second)}},
		{pid: 4103, done: make(chan struct{}), result: process.Result{ExitCode: 0, ExitedAt: time.Now().Add(2 * time.Second)}},
	}
	var (
		mu      sync.Mutex
		specs   []process.Spec
		calls   int
		entered = make(chan struct{})
		release = make(chan struct{})
	)
	s := testSupervisor(t, Options{StartProcess: func(spec process.Spec) (Child, error) {
		mu.Lock()
		index := calls
		calls++
		specs = append(specs, process.Spec{
			Dir: spec.Dir, Argv: append([]string(nil), spec.Argv...),
			Env: append([]string(nil), spec.Env...), Output: spec.Output,
			MaxLineBytes: spec.MaxLineBytes, Now: spec.Now,
		})
		mu.Unlock()
		if index == 1 {
			close(entered)
			<-release
		}
		return children[index], nil
	}})
	request := StartRequest{
		Name: "api", Cwd: root,
		Argv: []string{"/bin/sh", "-c", "printf launch"},
		Env:  []string{"TOKEN=secret", "PATH=/usr/bin:/bin"},
	}
	first, err := s.Start(request)
	if err != nil {
		t.Fatal(err)
	}
	store, err := s.Output(root, "api")
	if err != nil {
		t.Fatal(err)
	}
	oldCursor, err := store.Append(output.Stdout, time.Now(), "old-only\n")
	if err != nil {
		t.Fatal(err)
	}

	type restartResult struct {
		process Process
		err     error
	}
	resultCh := make(chan restartResult, 1)
	go func() {
		restarted, restartErr := s.Restart(context.Background(), root, "api")
		resultCh <- restartResult{process: restarted, err: restartErr}
	}()
	<-entered
	if !s.Restarting(root, "api") {
		t.Fatal("restart state was not visible while relaunch was blocked")
	}
	if _, err := s.Start(request); !errors.Is(err, ErrNameInUse) {
		t.Fatalf("start during restart error = %v, want ErrNameInUse", err)
	}
	close(release)
	result := <-resultCh
	if result.err != nil {
		t.Fatal(result.err)
	}
	second := result.process
	if second.PID == first.PID || second.RestartCount != 1 {
		t.Fatalf("restarted process = %#v, want new PID and restart count 1", second)
	}
	if second.LaunchCursor <= oldCursor {
		t.Fatalf("launch cursor = %d, want after old cursor %d", second.LaunchCursor, oldCursor)
	}
	sameStore, err := s.Output(root, "api")
	if err != nil {
		t.Fatal(err)
	}
	if sameStore != store {
		t.Fatal("restart replaced the output store")
	}
	read, err := store.Read(output.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	foundMarker := false
	for _, entry := range read.Entries {
		if entry.Cursor == second.LaunchCursor && entry.Stream == output.System && strings.Contains(entry.Text, "restarted") {
			foundMarker = true
		}
	}
	if !foundMarker {
		t.Fatalf("restart marker missing from %#v", read.Entries)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	waited, err := s.Wait(ctx, root, "api", WaitOptions{Match: regexp.MustCompile("old-only")})
	if err != nil {
		t.Fatal(err)
	}
	if waited.Outcome != WaitTimedOut {
		t.Fatalf("wait outcome = %q, want %q", waited.Outcome, WaitTimedOut)
	}

	mu.Lock()
	if len(specs) != 2 {
		t.Fatalf("start specs = %d, want 2", len(specs))
	}
	if specs[0].Dir != specs[1].Dir || !reflect.DeepEqual(specs[0].Argv, specs[1].Argv) || !reflect.DeepEqual(specs[0].Env, specs[1].Env) || specs[0].Output != specs[1].Output {
		t.Fatalf("restart changed launch spec: first=%#v second=%#v", specs[0], specs[1])
	}
	mu.Unlock()

	children[1].release()
	waitExited(t, s, root, "api")
	third, err := s.Restart(context.Background(), root, "api")
	if err != nil {
		t.Fatal(err)
	}
	if third.PID != 4103 || third.RestartCount != 2 {
		t.Fatalf("restart of exited process = %#v", third)
	}
}

func TestRestartRejectsInvalidAndMissingNames(t *testing.T) {
	root := makeProject(t, false)
	s := testSupervisor(t, Options{})
	if _, err := s.Restart(context.Background(), root, "not valid"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("invalid-name error = %v", err)
	}
	if _, err := s.Restart(context.Background(), root, "missing"); err == nil {
		t.Fatal("missing process restart succeeded")
	}
}

func TestRestartOrdinaryStartReplacementUsesInitialWaitCursor(t *testing.T) {
	root := makeProject(t, false)
	first := newSubscriptionChild(5401, 0, time.Unix(61, 0), "")
	replacement := newSubscriptionChild(5402, 0, time.Unix(62, 0), "replacement-ready\n")
	s := testSupervisor(t, Options{
		StartProcess: subscriptionStarter(map[string]*subscriptionChild{
			"ordinary-first":  first,
			"ordinary-second": replacement,
		}),
	})
	request := StartRequest{Name: "ordinary", Cwd: root, Argv: []string{"/bin/fake", "ordinary-first"}}
	if _, err := s.Start(request); err != nil {
		t.Fatal(err)
	}
	first.release()
	firstExit := waitExited(t, s, root, "ordinary")
	if firstExit.Exit == nil {
		t.Fatal("ordinary replacement test did not retain first exit")
	}
	request.Argv = []string{"/bin/fake", "ordinary-second"}
	replaced, err := s.Start(request)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.RestartCount != 1 || replaced.LaunchCursor != 0 {
		t.Fatalf("ordinary replacement metadata = %#v, want restart count 1 and launch boundary cursor 0", replaced)
	}
	if !s.FollowContinuesAfter(root, "ordinary", firstExit.ExitedAt) {
		t.Fatal("ordinary Start replacement did not continue the durable follower")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := s.Wait(ctx, root, "ordinary", WaitOptions{Match: regexp.MustCompile(`replacement-ready`)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != WaitMatched || got.Cursor != 1 || got.Exit != nil {
		t.Fatalf("ordinary replacement wait = %#v, want matched cursor 1 without exit", got)
	}
}

func TestRestartFailureClearsContinuationAndRepublishesExit(t *testing.T) {
	root := makeProject(t, false)
	exitAt := time.Unix(71, 0)
	first := newSubscriptionChild(5501, 23, exitAt, "")
	replacement := newSubscriptionChild(5502, 0, time.Unix(72, 0), "")
	entered := make(chan struct{})
	release := make(chan struct{})
	var (
		mu    sync.Mutex
		calls int
	)
	s := testSupervisor(t, Options{
		StartProcess: func(spec process.Spec) (Child, error) {
			mu.Lock()
			index := calls
			calls++
			mu.Unlock()
			switch index {
			case 0:
				first.store = spec.Output
				return first, nil
			case 1:
				close(entered)
				<-release
				return nil, errors.New("restart launch failed")
			case 2:
				replacement.store = spec.Output
				return replacement, nil
			default:
				return nil, errors.New("unexpected launch")
			}
		},
	})
	if _, err := s.Start(StartRequest{Name: "failed", Cwd: root, Argv: []string{"/bin/fake", "failed"}}); err != nil {
		t.Fatal(err)
	}
	store, err := s.Output(root, "failed")
	if err != nil {
		t.Fatal(err)
	}
	follower := store.Subscribe(output.ReadOptions{})
	defer follower.Close()

	type restartResult struct {
		err error
	}
	restartResultCh := make(chan restartResult, 1)
	go func() {
		_, restartErr := s.Restart(context.Background(), root, "failed")
		restartResultCh <- restartResult{err: restartErr}
	}()
	<-entered
	firstEvent := nextSubscriptionEvent(t, follower)
	if firstEvent.Exit == nil || firstEvent.Exit.Code != 23 || !firstEvent.Exit.Time.Equal(exitAt) {
		t.Fatalf("first restart exit event = %#v, want code 23 at %v", firstEvent, exitAt)
	}
	if !s.FollowContinuesAfter(root, "failed", exitAt) {
		t.Fatal("failed restart stopped continuing before relaunch failed")
	}

	close(release)
	result := <-restartResultCh
	if result.err == nil {
		t.Fatal("failed restart succeeded")
	}
	if s.FollowContinuesAfter(root, "failed", exitAt) {
		t.Fatal("failed restart left continuation enabled")
	}

	var republished *output.Exit
	for republished == nil {
		event := nextSubscriptionEvent(t, follower)
		if event.Exit != nil {
			republished = event.Exit
		}
	}
	if republished.Code != 23 || !republished.Time.Equal(exitAt) {
		t.Fatalf("republished restart exit = %#v, want code 23 at %v", republished, exitAt)
	}

	if _, err := s.Start(StartRequest{Name: "failed", Cwd: root, Argv: []string{"/bin/fake", "recovered"}}); err != nil {
		t.Fatalf("start after failed restart = %v, want cleared name reservation", err)
	}
}

func TestRestartCanceledAfterPublishedExitRepublishesAfterReconcile(t *testing.T) {
	root := makeProject(t, false)
	exitAt := time.Unix(81, 0)
	child := newDelayedExitChild(5601, 29, exitAt)
	s := testSupervisor(t, Options{
		StartProcess: func(spec process.Spec) (Child, error) {
			child.store = spec.Output
			return child, nil
		},
	})
	if _, err := s.Start(StartRequest{Name: "pending", Cwd: root, Argv: []string{"/bin/fake", "pending"}}); err != nil {
		t.Fatal(err)
	}
	store, err := s.Output(root, "pending")
	if err != nil {
		t.Fatal(err)
	}
	follower := store.Subscribe(output.ReadOptions{})
	defer follower.Close()

	ctx, cancel := context.WithCancel(context.Background())
	restartResultCh := make(chan error, 1)
	go func() {
		_, restartErr := s.Restart(ctx, root, "pending")
		restartResultCh <- restartErr
	}()
	waitSubscriptionSignal(t, child.published, "restart child to publish its exit")
	firstEvent := nextSubscriptionEvent(t, follower)
	if firstEvent.Exit == nil || firstEvent.Exit.Code != 29 || !firstEvent.Exit.Time.Equal(exitAt) {
		t.Fatalf("first canceled-restart exit event = %#v, want code 29 at %v", firstEvent, exitAt)
	}
	if !s.FollowContinuesAfter(root, "pending", exitAt) {
		t.Fatal("canceled restart stopped continuing before cancellation")
	}

	cancel()
	if err := <-restartResultCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled restart error = %v, want context.Canceled", err)
	}
	if s.FollowContinuesAfter(root, "pending", exitAt) {
		t.Fatal("canceled restart left continuation enabled")
	}

	child.releaseDone()
	republished := nextSubscriptionEvent(t, follower)
	if republished.Exit == nil || republished.Read != nil ||
		republished.Exit.Code != 29 || !republished.Exit.Time.Equal(exitAt) {
		t.Fatalf("reconciled canceled-restart exit event = %#v, want code 29 at %v", republished, exitAt)
	}
}

func TestRestartEvictionSkipsReservedRecordsAndReevictsAfterFailure(t *testing.T) {
	root := makeProject(t, false)
	a := newSubscriptionChild(5701, 0, time.Unix(91, 0), "")
	b := newSubscriptionChild(5702, 0, time.Unix(92, 0), "")
	bReplacement := newSubscriptionChild(5703, 0, time.Unix(93, 0), "")
	bStartEntered := make(chan struct{})
	allowBStart := make(chan struct{})
	aRestartEntered := make(chan struct{})
	allowARestart := make(chan struct{})
	var (
		mu     sync.Mutex
		aCalls int
	)
	s := testSupervisor(t, Options{
		CompletedLimit: 1,
		StartProcess: func(spec process.Spec) (Child, error) {
			name := spec.Argv[len(spec.Argv)-1]
			switch name {
			case "a":
				mu.Lock()
				aCalls++
				call := aCalls
				mu.Unlock()
				if call == 1 {
					a.store = spec.Output
					return a, nil
				}
				close(aRestartEntered)
				<-allowARestart
				return nil, errors.New("a relaunch failed")
			case "b":
				b.store = spec.Output
				return b, nil
			case "b-replacement":
				close(bStartEntered)
				<-allowBStart
				bReplacement.store = spec.Output
				return bReplacement, nil
			default:
				return nil, fmt.Errorf("unexpected launch %q", name)
			}
		},
	})
	if _, err := s.Start(StartRequest{Name: "a", Cwd: root, Argv: []string{"/bin/fake", "a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(StartRequest{Name: "b", Cwd: root, Argv: []string{"/bin/fake", "b"}}); err != nil {
		t.Fatal(err)
	}
	bDone := recordDone(t, s, root, "b")
	b.release()
	waitSubscriptionSignal(t, bDone, "B terminal reconciliation")

	bStartResultCh := make(chan error, 1)
	go func() {
		_, startErr := s.Start(StartRequest{Name: "b", Cwd: root, Argv: []string{"/bin/fake", "b-replacement"}})
		bStartResultCh <- startErr
	}()
	waitSubscriptionSignal(t, bStartEntered, "replacement start reservation")

	aRestartResultCh := make(chan error, 1)
	go func() {
		_, restartErr := s.Restart(context.Background(), root, "a")
		aRestartResultCh <- restartErr
	}()
	waitSubscriptionSignal(t, aRestartEntered, "restart relaunch reservation")

	s.mu.RLock()
	complete := len(s.complete)
	s.mu.RUnlock()
	if complete != 2 {
		t.Fatalf("completed records while names are reserved = %d, want temporary overflow of 2", complete)
	}
	if _, err := s.Get(root, "a"); err != nil {
		t.Fatalf("reserved restarting record was evicted: %v", err)
	}
	if got, err := s.Get(root, "b"); err != nil || got.State != StateExited {
		t.Fatalf("reserved starting record = %#v, err %v; want retained exited record", got, err)
	}

	close(allowARestart)
	if err := <-aRestartResultCh; err == nil {
		t.Fatal("failed restart succeeded")
	}
	if _, err := s.Get(root, "a"); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("failed restart record lookup = %v, want bounded eviction after reservation release", err)
	}
	if _, err := s.Get(root, "b"); err != nil {
		t.Fatalf("other reserved record was evicted after failed restart: %v", err)
	}

	close(allowBStart)
	if err := <-bStartResultCh; err != nil {
		t.Fatalf("replacement start = %v", err)
	}
}

func TestRestartFailedStartReleasesReservationAndReevicts(t *testing.T) {
	root := makeProject(t, false)
	a := newSubscriptionChild(5801, 0, time.Unix(102, 0), "")
	aReplacement := newSubscriptionChild(5802, 0, time.Unix(103, 0), "")
	b := newSubscriptionChild(5803, 0, time.Unix(101, 0), "")
	bStartEntered := make(chan struct{})
	allowBStart := make(chan struct{})
	aRestartEntered := make(chan struct{})
	allowARestart := make(chan struct{})
	var (
		mu     sync.Mutex
		aCalls int
	)
	s := testSupervisor(t, Options{
		CompletedLimit: 1,
		StartProcess: func(spec process.Spec) (Child, error) {
			name := spec.Argv[len(spec.Argv)-1]
			switch name {
			case "a":
				mu.Lock()
				aCalls++
				call := aCalls
				mu.Unlock()
				if call == 1 {
					a.store = spec.Output
					return a, nil
				}
				close(aRestartEntered)
				<-allowARestart
				aReplacement.store = spec.Output
				return aReplacement, nil
			case "b":
				b.store = spec.Output
				return b, nil
			case "b-replacement":
				close(bStartEntered)
				<-allowBStart
				return nil, errors.New("ordinary start failed")
			default:
				return nil, fmt.Errorf("unexpected launch %q", name)
			}
		},
	})
	if _, err := s.Start(StartRequest{Name: "a", Cwd: root, Argv: []string{"/bin/fake", "a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(StartRequest{Name: "b", Cwd: root, Argv: []string{"/bin/fake", "b"}}); err != nil {
		t.Fatal(err)
	}
	bDone := recordDone(t, s, root, "b")
	b.release()
	waitSubscriptionSignal(t, bDone, "B terminal reconciliation")

	bStartResultCh := make(chan error, 1)
	go func() {
		_, startErr := s.Start(StartRequest{Name: "b", Cwd: root, Argv: []string{"/bin/fake", "b-replacement"}})
		bStartResultCh <- startErr
	}()
	waitSubscriptionSignal(t, bStartEntered, "ordinary start reservation")

	aRestartResultCh := make(chan error, 1)
	go func() {
		_, restartErr := s.Restart(context.Background(), root, "a")
		aRestartResultCh <- restartErr
	}()
	waitSubscriptionSignal(t, aRestartEntered, "restart reservation")

	s.mu.RLock()
	complete := len(s.complete)
	s.mu.RUnlock()
	if complete != 2 {
		t.Fatalf("completed records while reservations overlap = %d, want temporary overflow of 2", complete)
	}
	if !s.Restarting(root, "a") {
		t.Fatal("restart reservation ended before ordinary start failure")
	}

	close(allowBStart)
	if err := <-bStartResultCh; err == nil {
		t.Fatal("failed ordinary start succeeded")
	}
	if _, err := s.Get(root, "b"); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("failed ordinary start record lookup = %v, want immediate eviction", err)
	}
	if _, err := s.Get(root, "a"); err != nil {
		t.Fatalf("restart-reserved record was evicted after ordinary start failure: %v", err)
	}
	if !s.Restarting(root, "a") {
		t.Fatal("restart reservation ended after ordinary start failure")
	}

	close(allowARestart)
	if err := <-aRestartResultCh; err != nil {
		t.Fatalf("restart after ordinary start failure = %v", err)
	}
}

func waitForReadinessState(t *testing.T, s *Supervisor, cwd, name, want string) Process {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		model, err := s.Get(cwd, name)
		if err == nil && model.Readiness != nil && model.Readiness.State == want {
			return model
		}
		time.Sleep(time.Millisecond)
	}
	model, err := s.Get(cwd, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("process %q readiness = %#v, want state %q", name, model.Readiness, want)
	return Process{}
}

func TestManifestReadinessRetention(t *testing.T) {
	root := makeProject(t, false)
	child := newSubscriptionChild(6001, 0, time.Unix(201, 0), "")
	s := testSupervisor(t, Options{
		OutputLimits: output.Limits{
			RetainedBytes:      8,
			DefaultReadEntries: 8,
			DefaultReadBytes:   64,
		},
		StartProcess: func(spec process.Spec) (Child, error) {
			child.store = spec.Output
			return child, nil
		},
	})
	started, err := s.Start(StartRequest{
		Name: "web", Source: "manifest:hum.yaml", Cwd: root,
		Argv:  []string{"/bin/fake", "web"},
		Ready: &ReadinessConfig{Match: `^ready$`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.Readiness == nil || started.Readiness.State != ReadinessStarting {
		t.Fatalf("initial readiness = %#v, want starting", started.Readiness)
	}
	store, err := s.Output(root, "web")
	if err != nil {
		t.Fatal(err)
	}
	matchingCursor, err := store.Append(output.Stdout, time.Unix(202, 0), "ready")
	if err != nil {
		t.Fatal(err)
	}
	ready := waitForReadinessState(t, s, root, "web", ReadinessReady)
	if ready.Source != "manifest:hum.yaml" || ready.Readiness == nil ||
		ready.Readiness.Cursor == nil || *ready.Readiness.Cursor != matchingCursor {
		t.Fatalf("ready snapshot = %#v, want source and cursor %d", ready, matchingCursor)
	}
	if _, err := store.Append(output.Stdout, time.Unix(203, 0), "filler"); err != nil {
		t.Fatal(err)
	}
	read, err := store.Read(output.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if read.Oldest == nil || *read.Oldest <= matchingCursor {
		t.Fatalf("retained output oldest = %#v, want match cursor %d evicted", read.Oldest, matchingCursor)
	}
	retained, err := s.Get(root, "web")
	if err != nil {
		t.Fatal(err)
	}
	if retained.Readiness == nil || retained.Readiness.State != ReadinessReady ||
		retained.Readiness.Cursor == nil || *retained.Readiness.Cursor != matchingCursor {
		t.Fatalf("readiness after eviction = %#v, want ready cursor %d", retained.Readiness, matchingCursor)
	}
}

func TestManifestRestartReadinessReset(t *testing.T) {
	root := makeProject(t, false)
	first := newSubscriptionChild(6101, 0, time.Unix(211, 0), "")
	second := newSubscriptionChild(6102, 0, time.Unix(212, 0), "")
	var (
		mu    sync.Mutex
		calls int
	)
	s := testSupervisor(t, Options{
		StartProcess: func(spec process.Spec) (Child, error) {
			mu.Lock()
			index := calls
			calls++
			mu.Unlock()
			switch index {
			case 0:
				first.store = spec.Output
				return first, nil
			case 1:
				second.store = spec.Output
				return second, nil
			default:
				return nil, fmt.Errorf("unexpected launch %d", index)
			}
		},
	})
	if _, err := s.Start(StartRequest{
		Name: "api", Source: "manifest:hum.yaml", Cwd: root,
		Argv:  []string{"/bin/fake", "api"},
		Env:   []string{"TOKEN=secret"},
		Ready: &ReadinessConfig{Match: `ready`},
	}); err != nil {
		t.Fatal(err)
	}
	store, err := s.Output(root, "api")
	if err != nil {
		t.Fatal(err)
	}
	oldCursor, err := store.Append(output.Stdout, time.Unix(213, 0), "ready old")
	if err != nil {
		t.Fatal(err)
	}
	ready := waitForReadinessState(t, s, root, "api", ReadinessReady)
	if ready.Readiness == nil || ready.Readiness.Cursor == nil || *ready.Readiness.Cursor != oldCursor {
		t.Fatalf("first readiness = %#v, want cursor %d", ready.Readiness, oldCursor)
	}
	restarted, err := s.Restart(context.Background(), root, "api")
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Readiness == nil || restarted.Readiness.State != ReadinessStarting {
		t.Fatalf("restarted readiness = %#v, want starting", restarted.Readiness)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	oldWait, err := s.Wait(ctx, root, "api", WaitOptions{Match: regexp.MustCompile(`ready`)})
	if err != nil {
		t.Fatal(err)
	}
	if oldWait.Outcome != WaitTimedOut {
		t.Fatalf("old readiness wait = %#v, want timed_out", oldWait)
	}
	newCursor, err := store.Append(output.Stdout, time.Unix(214, 0), "ready new")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	newWait, err := s.Wait(ctx, root, "api", WaitOptions{Match: regexp.MustCompile(`ready`)})
	if err != nil {
		t.Fatal(err)
	}
	if newWait.Outcome != WaitMatched || newWait.Cursor != newCursor {
		t.Fatalf("new readiness wait = %#v, want matched cursor %d", newWait, newCursor)
	}
}

func TestReadinessField(t *testing.T) {
	root := makeProject(t, false)
	early := newSubscriptionChild(6201, 0, time.Unix(221, 0), "ready early\n")
	starting := newSubscriptionChild(6202, 0, time.Unix(222, 0), "")
	unverified := newSubscriptionChild(6203, 0, time.Unix(223, 0), "")
	adHoc := newSubscriptionChild(6204, 0, time.Unix(224, 0), "")
	exited := newSubscriptionChild(6205, 7, time.Unix(225, 0), "")
	children := map[string]*subscriptionChild{
		"early": early, "starting": starting, "unverified": unverified,
		"ad-hoc": adHoc, "exited": exited,
	}
	s := testSupervisor(t, Options{StartProcess: subscriptionStarter(children)})

	earlyProcess, err := s.Start(StartRequest{
		Name: "early", Source: "manifest:hum.yaml", Cwd: root,
		Argv:  []string{"/bin/fake", "early"},
		Ready: &ReadinessConfig{Match: `ready`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if earlyProcess.Source != "manifest:hum.yaml" || earlyProcess.Readiness == nil ||
		earlyProcess.Readiness.State != ReadinessReady || earlyProcess.Readiness.Cursor == nil {
		t.Fatalf("early process = %#v, want source and ready readiness", earlyProcess)
	}
	startingProcess, err := s.Start(StartRequest{
		Name: "starting", Source: "manifest:hum.yaml", Cwd: root,
		Argv:  []string{"/bin/fake", "starting"},
		Ready: &ReadinessConfig{Match: `ready`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if startingProcess.Readiness == nil || startingProcess.Readiness.State != ReadinessStarting {
		t.Fatalf("starting process readiness = %#v, want starting", startingProcess.Readiness)
	}
	unverifiedProcess, err := s.Start(StartRequest{
		Name: "unverified", Source: "discovery", Cwd: root,
		Argv: []string{"/bin/fake", "unverified"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if unverifiedProcess.Readiness == nil || unverifiedProcess.Readiness.State != ReadinessRunningUnverified {
		t.Fatalf("unverified process readiness = %#v, want running_unverified", unverifiedProcess.Readiness)
	}
	adHocProcess, err := s.Start(StartRequest{
		Name: "ad-hoc", Cwd: root, Argv: []string{"/bin/fake", "ad-hoc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if adHocProcess.Readiness != nil || adHocProcess.Source != "" {
		t.Fatalf("ad-hoc process = %#v, want no source or readiness", adHocProcess)
	}
	if _, err := s.Start(StartRequest{
		Name: "exited", Source: "manifest:hum.yaml", Cwd: root,
		Argv:  []string{"/bin/fake", "exited"},
		Ready: &ReadinessConfig{Match: `ready`},
	}); err != nil {
		t.Fatal(err)
	}
	exited.release()
	terminal := waitExited(t, s, root, "exited")
	if terminal.Readiness != nil {
		t.Fatalf("exited readiness = %#v, want omitted", terminal.Readiness)
	}
}

func TestStartReservationCoversConcurrentAndFailedLaunch(t *testing.T) {
	root := makeProject(t, false)
	entered := make(chan struct{})
	release := make(chan struct{})
	child := newSubscriptionChild(6301, 0, time.Unix(231, 0), "")
	var (
		mu    sync.Mutex
		calls int
	)
	s := testSupervisor(t, Options{
		StartProcess: func(spec process.Spec) (Child, error) {
			mu.Lock()
			index := calls
			calls++
			mu.Unlock()
			if index == 0 {
				close(entered)
				<-release
				return nil, errors.New("launch failed")
			}
			child.store = spec.Output
			return child, nil
		},
	})

	firstResult := make(chan error, 1)
	go func() {
		_, err := s.Start(StartRequest{Name: "reserved", Cwd: root, Argv: []string{"/bin/fake", "reserved"}})
		firstResult <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first launch did not reach starter")
	}
	if _, err := s.Start(StartRequest{Name: "reserved", Cwd: root, Argv: []string{"/bin/fake", "reserved"}}); !errors.Is(err, ErrNameInUse) {
		t.Fatalf("concurrent start error = %v, want ErrNameInUse", err)
	}
	close(release)
	if err := <-firstResult; err == nil {
		t.Fatal("failed launch unexpectedly succeeded")
	}
	if _, err := s.Start(StartRequest{Name: "reserved", Cwd: root, Argv: []string{"/bin/fake", "reserved"}}); err != nil {
		t.Fatalf("start after failed launch = %v", err)
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 2 {
		t.Fatalf("starter calls = %d, want one failed and one successful launch", gotCalls)
	}
}

func TestManifestRootKeepsNestedGitCwdIdentity(t *testing.T) {
	root := makeProject(t, false)
	nested := filepath.Join(root, "tool")
	if err := os.MkdirAll(filepath.Join(nested, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	first := newSubscriptionChild(6401, 0, time.Unix(241, 0), "")
	second := newSubscriptionChild(6402, 0, time.Unix(242, 0), "")
	var (
		mu    sync.Mutex
		calls int
	)
	s := testSupervisor(t, Options{
		StartProcess: func(spec process.Spec) (Child, error) {
			mu.Lock()
			index := calls
			calls++
			mu.Unlock()
			if index == 0 {
				first.store = spec.Output
				return first, nil
			}
			second.store = spec.Output
			return second, nil
		},
	})
	started, err := s.Start(StartRequest{
		Name: "nested", Root: root + string(filepath.Separator) + ".", Cwd: nested,
		Source: "manifest:hum.yaml", Argv: []string{"/bin/fake", "nested"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.Root != filepath.Clean(root) || started.Cwd != filepath.Clean(nested) {
		t.Fatalf("started identity = %#v, want root %q and cwd %q", started, filepath.Clean(root), filepath.Clean(nested))
	}
	items, err := s.List(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Root != filepath.Clean(root) {
		t.Fatalf("outer-root list = %#v, want nested process under %q", items, filepath.Clean(root))
	}
	if _, err := s.Get(root, "nested"); err != nil {
		t.Fatalf("outer-root get = %v", err)
	}
	if _, err := s.Get(nested, "nested"); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("nested-root get error = %v, want ErrProcessNotFound", err)
	}

	restarted, err := s.Restart(context.Background(), nested, "nested", RestartOptions{
		Update: true, Root: root + string(filepath.Separator) + ".", Cwd: nested,
		Source: "manifest:hum.yaml", Argv: []string{"/bin/fake", "nested"},
	})
	if err != nil {
		t.Fatalf("nested-cwd explicit-root restart = %v", err)
	}
	if restarted.Root != filepath.Clean(root) || restarted.Cwd != filepath.Clean(nested) {
		t.Fatalf("restarted identity = %#v, want root %q and cwd %q", restarted, filepath.Clean(root), filepath.Clean(nested))
	}
}

func TestManifestReadinessCapturesSynchronousEvictedStart(t *testing.T) {
	root := makeProject(t, false)
	child := newSubscriptionChild(6501, 0, time.Unix(251, 0), "")
	s := testSupervisor(t, Options{
		OutputLimits: output.Limits{
			RetainedBytes: 8, DefaultReadEntries: 8, DefaultReadBytes: 64,
		},
		StartProcess: func(spec process.Spec) (Child, error) {
			child.store = spec.Output
			if _, err := spec.Output.Append(output.Stdout, time.Unix(252, 0), "ready"); err != nil {
				return nil, err
			}
			if _, err := spec.Output.Append(output.Stdout, time.Unix(253, 0), "filler"); err != nil {
				return nil, err
			}
			return child, nil
		},
	})
	started, err := s.Start(StartRequest{
		Name: "sync-start", Source: "manifest:hum.yaml", Cwd: root,
		Argv: []string{"/bin/fake", "sync-start"}, Ready: &ReadinessConfig{Match: `^ready$`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.Readiness == nil || started.Readiness.State != ReadinessReady ||
		started.Readiness.Cursor == nil || *started.Readiness.Cursor != 0 {
		t.Fatalf("synchronous start readiness = %#v, want ready cursor 0", started.Readiness)
	}
	store, err := s.Output(root, "sync-start")
	if err != nil {
		t.Fatal(err)
	}
	retained, err := store.Read(output.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if retained.Oldest == nil || *retained.Oldest <= 0 {
		t.Fatalf("synchronous start retained output = %#v, want cursor 0 evicted", retained)
	}
	got, err := s.Get(root, "sync-start")
	if err != nil {
		t.Fatal(err)
	}
	if got.Readiness == nil || got.Readiness.State != ReadinessReady ||
		got.Readiness.Cursor == nil || *got.Readiness.Cursor != 0 {
		t.Fatalf("synchronous start readiness after eviction = %#v", got.Readiness)
	}
}

func TestManifestRestartReadinessCapturesSynchronousEvictedStart(t *testing.T) {
	root := makeProject(t, false)
	first := newSubscriptionChild(6601, 0, time.Unix(261, 0), "")
	second := newSubscriptionChild(6602, 0, time.Unix(262, 0), "")
	var (
		mu    sync.Mutex
		calls int
	)
	s := testSupervisor(t, Options{
		OutputLimits: output.Limits{
			RetainedBytes: 8, DefaultReadEntries: 8, DefaultReadBytes: 64,
		},
		StartProcess: func(spec process.Spec) (Child, error) {
			mu.Lock()
			index := calls
			calls++
			mu.Unlock()
			if index == 0 {
				first.store = spec.Output
				return first, nil
			}
			second.store = spec.Output
			if _, err := spec.Output.Append(output.Stdout, time.Unix(263, 0), "ready"); err != nil {
				return nil, err
			}
			if _, err := spec.Output.Append(output.Stdout, time.Unix(264, 0), "filler"); err != nil {
				return nil, err
			}
			return second, nil
		},
	})
	if _, err := s.Start(StartRequest{
		Name: "sync-restart", Source: "manifest:hum.yaml", Cwd: root,
		Argv: []string{"/bin/fake", "sync-restart"}, Ready: &ReadinessConfig{Match: `^ready$`},
	}); err != nil {
		t.Fatal(err)
	}
	store, err := s.Output(root, "sync-restart")
	if err != nil {
		t.Fatal(err)
	}
	oldCursor, err := store.Append(output.Stdout, time.Unix(265, 0), "ready")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(output.Stdout, time.Unix(266, 0), "filler"); err != nil {
		t.Fatal(err)
	}
	waitForReadinessState(t, s, root, "sync-restart", ReadinessReady)

	restarted, err := s.Restart(context.Background(), root, "sync-restart")
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Readiness == nil || restarted.Readiness.State != ReadinessReady ||
		restarted.Readiness.Cursor == nil || *restarted.Readiness.Cursor <= oldCursor {
		t.Fatalf("synchronous restart readiness = %#v, want a new ready cursor after %d", restarted.Readiness, oldCursor)
	}
	latest, err := store.Read(output.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if latest.Oldest == nil || restarted.Readiness.Cursor == nil || *latest.Oldest <= *restarted.Readiness.Cursor {
		t.Fatalf("synchronous restart retained output = %#v, want new match cursor evicted", latest)
	}
	got, err := s.Get(root, "sync-restart")
	if err != nil {
		t.Fatal(err)
	}
	if got.Readiness == nil || got.Readiness.State != ReadinessReady ||
		got.Readiness.Cursor == nil || *got.Readiness.Cursor != *restarted.Readiness.Cursor {
		t.Fatalf("synchronous restart readiness after eviction = %#v", got.Readiness)
	}
}

func TestSessionFollowSurvivesStopAndStartStopped(t *testing.T) {
	root := makeProject(t, false)
	s := testSupervisor(t, Options{StopGrace: 20 * time.Millisecond})
	sub, err := s.Subscribe(root, "session", output.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if _, err := startShell(s, root, "session", "printf 'first\\n'"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	seenFirst, exits := false, 0
	for exits == 0 {
		event, err := sub.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if event.Read != nil {
			for _, entry := range event.Read.Entries {
				seenFirst = seenFirst || entry.Text == "first\n"
			}
		}
		if event.Exit != nil {
			exits++
		}
	}
	if !seenFirst {
		t.Fatal("follower missed first incarnation output")
	}
	if _, err := startShell(s, root, "session", "printf 'second\\n'"); err != nil {
		t.Fatal(err)
	}
	seenSecond := false
	for exits < 2 {
		event, err := sub.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if event.Read != nil {
			for _, entry := range event.Read.Entries {
				seenSecond = seenSecond || entry.Text == "second\n"
			}
		}
		if event.Exit != nil {
			exits++
		}
	}
	if !seenSecond {
		t.Fatal("follower missed successor incarnation output")
	}
}

func TestRemoveSessionClosesFollowerAndDiscardsLaunch(t *testing.T) {
	root := makeProject(t, false)
	s := testSupervisor(t, Options{})
	sub, err := s.Subscribe(root, "remove", output.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(context.Background(), root, "remove"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := sub.Next(ctx); !errors.Is(err, output.ErrStoreClosed) {
		t.Fatalf("removed follower error = %v, want ErrStoreClosed", err)
	}
	if _, err := s.Get(root, "remove"); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("removed lookup = %v, want not found", err)
	}
}

func TestWaitPreLaunchStartsAtNextIncarnation(t *testing.T) {
	root := makeProject(t, false)
	s := testSupervisor(t, Options{})
	result := make(chan WaitResult, 1)
	errCh := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		got, err := s.Wait(ctx, root, "future", WaitOptions{Match: regexp.MustCompile("ready")})
		result <- got
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if _, err := startShell(s, root, "future", "printf 'ready\\n'; sleep .05"); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if got := <-result; got.Outcome != WaitMatched {
		t.Fatalf("pre-launch wait = %#v, want matched", got)
	}
}
