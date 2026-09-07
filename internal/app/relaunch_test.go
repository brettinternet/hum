package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"hum/internal/output"
	"hum/internal/process"
)

type relaunchTestChild struct {
	pid    int
	result process.Result
	done   chan struct{}
	once   sync.Once
	store  *output.Store
}

func (c *relaunchTestChild) PID() int              { return c.pid }
func (c *relaunchTestChild) PGID() int             { return c.pid }
func (c *relaunchTestChild) Done() <-chan struct{} { return c.done }
func (c *relaunchTestChild) Wait() process.Result  { <-c.done; return c.result }
func (c *relaunchTestChild) release() {
	c.once.Do(func() {
		if c.store != nil {
			c.store.NotifyExit(output.Exit{Code: c.result.ExitCode, Time: c.result.ExitedAt})
		}
		close(c.done)
	})
}
func (c *relaunchTestChild) Signal(os.Signal) error { c.release(); return os.ErrProcessDone }

type relaunchTestTimers struct {
	mu    sync.Mutex
	items map[time.Duration][]chan time.Time
	now   time.Time
}

func newRelaunchTestTimers(now time.Time) *relaunchTestTimers {
	return &relaunchTestTimers{items: make(map[time.Duration][]chan time.Time), now: now}
}
func (t *relaunchTestTimers) after(delay time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	t.mu.Lock()
	t.items[delay] = append(t.items[delay], ch)
	t.mu.Unlock()
	return ch
}
func (t *relaunchTestTimers) fire(delay time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	items := t.items[delay]
	if len(items) == 0 {
		return false
	}
	t.items[delay] = items[1:]
	items[0] <- t.now.Add(delay)
	return true
}
func (t *relaunchTestTimers) fireAll(delay time.Duration) int {
	t.mu.Lock()
	items := t.items[delay]
	t.items[delay] = nil
	t.mu.Unlock()
	for _, item := range items {
		item <- t.now.Add(delay)
	}
	return len(items)
}
func (t *relaunchTestTimers) wait(delay time.Duration) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		t.mu.Lock()
		n := len(t.items[delay])
		t.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func relaunchTestProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func waitForRelaunch(t *testing.T, s *Supervisor, root, name string, predicate func(Process) bool) Process {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		process, err := s.Get(root, name)
		if err == nil && predicate(process) {
			return process
		}
		time.Sleep(time.Millisecond)
	}
	process, err := s.Get(root, name)
	t.Fatalf("process = %#v, err=%v; predicate was not satisfied", process, err)
	return Process{}
}

func waitForRelaunchChildren(t *testing.T, harness *relaunchTestHarness, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if harness.childCount() >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("automatic relaunches = %d, want at least %d", harness.childCount(), count)
}

type relaunchTestLaunch struct {
	code     int
	spawnErr error
}

type relaunchTestHarness struct {
	s      *Supervisor
	root   string
	now    time.Time
	timers *relaunchTestTimers

	mu       sync.Mutex
	attempt  int
	children []*relaunchTestChild
	specs    []process.Spec
}

func (h *relaunchTestHarness) child(index int) *relaunchTestChild {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.children[index]
}

func (h *relaunchTestHarness) childCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.children)
}

func (h *relaunchTestHarness) spec(index int) process.Spec {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.specs[index]
}

func newRelaunchTestHarness(t *testing.T, launches []relaunchTestLaunch, completedLimit int) *relaunchTestHarness {
	t.Helper()
	root := relaunchTestProject(t)
	now := time.Unix(100, 0).UTC()
	timers := newRelaunchTestTimers(now)
	harness := &relaunchTestHarness{root: root, now: now, timers: timers}
	s, err := New(Options{
		CompletedLimit: completedLimit,
		Now:            nowFunc(now),
		After:          timers.after,
		StartProcess: func(spec process.Spec) (Child, error) {
			harness.mu.Lock()
			harness.specs = append(harness.specs, spec)
			index := harness.attempt
			harness.attempt++
			harness.mu.Unlock()
			if index < len(launches) && launches[index].spawnErr != nil {
				return nil, launches[index].spawnErr
			}
			code := 1
			if index < len(launches) {
				code = launches[index].code
			}
			child := &relaunchTestChild{
				pid:    7000 + harness.childCount(),
				result: process.Result{ExitCode: code, ExitedAt: now},
				done:   make(chan struct{}),
				store:  spec.Output,
			}
			harness.mu.Lock()
			harness.children = append(harness.children, child)
			harness.mu.Unlock()
			if spec.Started != nil {
				if err := spec.Started(); err != nil {
					return nil, err
				}
			}
			return child, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.s = s
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	return harness
}

func readRelaunchOutput(t *testing.T, s *Supervisor, root, name string) []output.Entry {
	t.Helper()
	store, err := s.Output(root, name)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.Read(output.ReadOptions{MaxEntries: 100, MaxBytes: 32 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	return result.Entries
}

func TestRelaunchOnFailure(t *testing.T) {
	wantDelays := [...]time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}

	t.Run("bounded retries and exhaustion", func(t *testing.T) {
		launches := make([]relaunchTestLaunch, 6)
		for i := range launches {
			launches[i].code = 1
		}
		harness := newRelaunchTestHarness(t, launches, 20)
		s := harness.s
		started, err := s.Start(StartRequest{Name: "api", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake", "api"}, Restart: RestartOnFailure})
		if err != nil {
			t.Fatal(err)
		}
		if started.Restart != RestartOnFailure || started.Relaunches != 0 || started.NextLaunchAt != nil {
			t.Fatalf("start snapshot = %#v", started)
		}
		for attempt := 0; attempt < 6; attempt++ {
			harness.child(attempt).release()
			wantRelaunches := attempt
			if attempt == 5 {
				final := waitForRelaunch(t, s, harness.root, "api", func(process Process) bool {
					return process.State == StateExited && process.Relaunches == 5 && process.NextLaunchAt == nil
				})
				if final.Restart != RestartOnFailure || final.ExitCode != 1 {
					t.Fatalf("exhausted snapshot = %#v", final)
				}
				continue
			}
			pending := waitForRelaunch(t, s, harness.root, "api", func(process Process) bool {
				return process.NextLaunchAt != nil && process.Relaunches == wantRelaunches
			})
			if pending.NextLaunchAt == nil {
				t.Fatalf("attempt %d did not expose next launch", attempt+1)
			}
			delay := wantDelays[attempt]
			harness.timers.wait(delay)
			if !harness.timers.fire(delay) {
				t.Fatalf("missing timer for attempt %d", attempt+1)
			}
			waitForRelaunchChildren(t, harness, attempt+2)
			current := waitForRelaunch(t, s, harness.root, "api", func(process Process) bool {
				return process.State == StateRunning && process.Relaunches == attempt+1
			})
			if current.RestartCount != attempt+1 || current.NextLaunchAt != nil {
				t.Fatalf("attempt %d snapshot = %#v", attempt+1, current)
			}
		}
		entries := readRelaunchOutput(t, s, harness.root, "api")
		relaunchBoundaries, gaveUp := 0, 0
		for _, entry := range entries {
			if entry.Stream != output.System {
				continue
			}
			relaunchBoundaries += strings.Count(entry.Text, "relaunching in ")
			gaveUp += strings.Count(entry.Text, "gave up after 5 relaunch attempts")
		}
		if relaunchBoundaries != 5 || gaveUp != 1 {
			t.Fatalf("retry boundaries=%d gave-up=%d entries=%#v", relaunchBoundaries, gaveUp, entries)
		}
	})

	t.Run("spawn failures consume attempts", func(t *testing.T) {
		harness := newRelaunchTestHarness(t, []relaunchTestLaunch{
			{code: 1}, {spawnErr: errors.New("launcher unavailable")}, {code: 1},
		}, 20)
		if _, err := harness.s.Start(StartRequest{Name: "api", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}, Restart: RestartOnFailure}); err != nil {
			t.Fatal(err)
		}
		harness.child(0).release()
		waitForRelaunch(t, harness.s, harness.root, "api", func(process Process) bool { return process.NextLaunchAt != nil && process.Relaunches == 0 })
		harness.timers.wait(time.Second)
		if !harness.timers.fire(time.Second) {
			t.Fatal("missing failed-spawn timer")
		}
		pending := waitForRelaunch(t, harness.s, harness.root, "api", func(process Process) bool { return process.NextLaunchAt != nil && process.Relaunches == 1 })
		if pending.NextLaunchAt == nil {
			t.Fatal("spawn failure did not schedule next attempt")
		}
		harness.timers.wait(2 * time.Second)
		if !harness.timers.fire(2 * time.Second) {
			t.Fatal("missing post-spawn-failure timer")
		}
		waitForRelaunchChildren(t, harness, 2)
		current := waitForRelaunch(t, harness.s, harness.root, "api", func(process Process) bool { return process.State == StateRunning && process.Relaunches == 2 })
		if current.RestartCount != 1 {
			t.Fatalf("post-spawn-failure snapshot = %#v", current)
		}
		var spawnFailure bool
		for _, entry := range readRelaunchOutput(t, harness.s, harness.root, "api") {
			spawnFailure = spawnFailure || strings.Contains(entry.Text, "relaunch failed: launcher unavailable")
		}
		if !spawnFailure {
			t.Fatal("spawn failure was not retained in output")
		}
		harness.child(1).release()

		exhausting := make([]relaunchTestLaunch, 6)
		exhausting[0].code = 1
		for index := 1; index < len(exhausting); index++ {
			exhausting[index].spawnErr = errors.New("spawn failed")
		}
		exhausted := newRelaunchTestHarness(t, exhausting, 20)
		if _, err := exhausted.s.Start(StartRequest{Name: "exhausted", Source: "manifest", Root: exhausted.root, Cwd: exhausted.root, Argv: []string{"fake"}, Restart: RestartOnFailure}); err != nil {
			t.Fatal(err)
		}
		exhausted.child(0).release()
		for attempt := 1; attempt <= maxAutomaticRelaunches; attempt++ {
			delay := wantDelays[attempt-1]
			exhausted.timers.wait(delay)
			if !exhausted.timers.fire(delay) {
				t.Fatalf("missing exhaustion timer for attempt %d", attempt)
			}
			if attempt < maxAutomaticRelaunches {
				waitForRelaunch(t, exhausted.s, exhausted.root, "exhausted", func(process Process) bool {
					return process.NextLaunchAt != nil && process.Relaunches == attempt
				})
			} else {
				waitForRelaunch(t, exhausted.s, exhausted.root, "exhausted", func(process Process) bool {
					return process.State == StateExited && process.NextLaunchAt == nil && process.Relaunches == maxAutomaticRelaunches
				})
			}
		}
		gaveUp := 0
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) && gaveUp == 0 {
			for _, entry := range readRelaunchOutput(t, exhausted.s, exhausted.root, "exhausted") {
				gaveUp += strings.Count(entry.Text, "gave up after 5 relaunch attempts")
			}
			if gaveUp == 0 {
				time.Sleep(time.Millisecond)
			}
		}
		if gaveUp != 1 {
			t.Fatalf("spawn-failure exhaustion gave-up boundaries = %d", gaveUp)
		}
	})

	t.Run("stability resets and pending controls cancel", func(t *testing.T) {
		harness := newRelaunchTestHarness(t, []relaunchTestLaunch{{code: 1}, {code: 1}, {code: 1}}, 20)
		s := harness.s
		if _, err := s.Start(StartRequest{Name: "api", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}, Restart: RestartOnFailure}); err != nil {
			t.Fatal(err)
		}
		harness.child(0).release()
		waitForRelaunch(t, s, harness.root, "api", func(process Process) bool { return process.NextLaunchAt != nil })
		harness.timers.wait(time.Second)
		if !harness.timers.fire(time.Second) {
			t.Fatal("missing stability test relaunch timer")
		}
		waitForRelaunchChildren(t, harness, 2)
		current := waitForRelaunch(t, s, harness.root, "api", func(process Process) bool { return process.State == StateRunning && process.Relaunches == 1 })
		if current.NextLaunchAt != nil {
			t.Fatalf("running relaunch has pending timer: %#v", current)
		}
		if harness.timers.fireAll(relaunchStabilityWindow) == 0 {
			t.Fatal("missing stability timer")
		}
		waitForRelaunch(t, s, harness.root, "api", func(process Process) bool { return process.State == StateRunning && process.Relaunches == 0 })
		harness.child(1).release()
		waitForRelaunch(t, s, harness.root, "api", func(process Process) bool { return process.NextLaunchAt != nil && process.Relaunches == 0 })
		if err := s.Stop(context.Background(), harness.root, "api"); err != nil {
			t.Fatal(err)
		}
		stopped := waitForRelaunch(t, s, harness.root, "api", func(process Process) bool {
			return process.State == StateStopped && process.NextLaunchAt == nil && process.Relaunches == 0
		})
		harness.timers.wait(time.Second)
		if stopped.Restart != RestartOnFailure || harness.timers.fire(time.Second) == false {
			t.Fatalf("stop did not cancel pending relaunch: %#v", stopped)
		}
		if harness.childCount() != 2 {
			t.Fatalf("cancelled timer launched child: %d", harness.childCount())
		}
	})

	t.Run("retains effective spec and explicit start wins", func(t *testing.T) {
		harness := newRelaunchTestHarness(t, []relaunchTestLaunch{{code: 1}, {code: 1}}, 20)
		s := harness.s
		ready := &ReadinessConfig{Match: "ready", Timeout: 2 * time.Second}
		env := []string{"TOKEN=secret"}
		if _, err := s.Start(StartRequest{Name: "api", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake", "--old"}, Env: env, Ready: ready, TTY: true, Restart: RestartOnFailure}); err != nil {
			t.Fatal(err)
		}
		harness.child(0).release()
		waitForRelaunch(t, s, harness.root, "api", func(process Process) bool { return process.NextLaunchAt != nil })
		manual, err := s.Start(StartRequest{Name: "api", Root: harness.root, Cwd: harness.root})
		if err != nil {
			t.Fatal(err)
		}
		if manual.Relaunches != 0 || manual.NextLaunchAt != nil || manual.Restart != RestartOnFailure {
			t.Fatalf("manual override snapshot = %#v", manual)
		}
		harness.timers.wait(time.Second)
		if !harness.timers.fire(time.Second) {
			t.Fatal("missing cancelled timer")
		}
		time.Sleep(10 * time.Millisecond)
		if harness.childCount() != 2 {
			t.Fatalf("explicit start and stale timer created %d children", harness.childCount())
		}
		got, original := harness.spec(1), harness.spec(0)
		if !reflect.DeepEqual(got.Argv, original.Argv) || got.Dir != original.Dir || !reflect.DeepEqual(got.Env, original.Env) || got.TTY != original.TTY {
			t.Fatalf("retained launch spec = %#v, want argv=%#v dir=%q env=%#v tty=%t", got, original.Argv, original.Dir, original.Env, original.TTY)
		}
		harness.child(1).release()
	})

	t.Run("never, zero, and foreign signal", func(t *testing.T) {
		for _, test := range []struct {
			name   string
			code   int
			policy RestartPolicy
			want   bool
		}{
			{name: "never", code: 1, policy: RestartNever, want: false},
			{name: "zero", code: 0, policy: RestartOnFailure, want: false},
			{name: "signal", code: -1, policy: RestartOnFailure, want: true},
		} {
			t.Run(test.name, func(t *testing.T) {
				harness := newRelaunchTestHarness(t, []relaunchTestLaunch{{code: test.code}, {code: 0}}, 20)
				if _, err := harness.s.Start(StartRequest{Name: test.name, Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}, Restart: test.policy}); err != nil {
					t.Fatal(err)
				}
				harness.child(0).release()
				process := waitForRelaunch(t, harness.s, harness.root, test.name, func(process Process) bool {
					return process.State == StateExited && (process.NextLaunchAt != nil) == test.want
				})
				if (process.NextLaunchAt != nil) != test.want || process.Relaunches != 0 {
					t.Fatalf("snapshot = %#v, want pending=%t and zero relaunches", process, test.want)
				}
			})
		}
	})

	t.Run("pending records resist eviction", func(t *testing.T) {
		harness := newRelaunchTestHarness(t, []relaunchTestLaunch{{code: 1}, {code: 0}}, 1)
		s := harness.s
		if _, err := s.Start(StartRequest{Name: "pending", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}, Restart: RestartOnFailure}); err != nil {
			t.Fatal(err)
		}
		harness.child(0).release()
		waitForRelaunch(t, s, harness.root, "pending", func(process Process) bool { return process.NextLaunchAt != nil })
		if _, err := s.Start(StartRequest{Name: "other", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}}); err != nil {
			t.Fatal(err)
		}
		harness.child(1).release()
		if _, err := s.Get(harness.root, "pending"); err != nil {
			t.Fatalf("pending record was evicted: %v", err)
		}
		items, err := s.List(harness.root, false)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range items {
			found = found || item.Name == "pending" && item.NextLaunchAt != nil
		}
		if !found {
			t.Fatalf("pending record missing from active list: %#v", items)
		}
	})

	t.Run("pending restart remove and shutdown invalidate stale timers", func(t *testing.T) {
		t.Run("restart", func(t *testing.T) {
			harness := newRelaunchTestHarness(t, []relaunchTestLaunch{{code: 1}, {code: 0}}, 20)
			if _, err := harness.s.Start(StartRequest{Name: "api", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}, Restart: RestartOnFailure}); err != nil {
				t.Fatal(err)
			}
			harness.child(0).release()
			waitForRelaunch(t, harness.s, harness.root, "api", func(process Process) bool { return process.NextLaunchAt != nil })
			if _, err := harness.s.Restart(context.Background(), harness.root, "api"); err != nil {
				t.Fatal(err)
			}
			harness.timers.wait(time.Second)
			if !harness.timers.fire(time.Second) {
				t.Fatal("missing cancelled restart timer")
			}
			time.Sleep(10 * time.Millisecond)
			if harness.childCount() != 2 {
				t.Fatalf("restart plus stale timer created %d children", harness.childCount())
			}
			harness.child(1).release()
		})

		t.Run("remove", func(t *testing.T) {
			harness := newRelaunchTestHarness(t, []relaunchTestLaunch{{code: 1}}, 20)
			if _, err := harness.s.Start(StartRequest{Name: "api", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}, Restart: RestartOnFailure}); err != nil {
				t.Fatal(err)
			}
			harness.child(0).release()
			waitForRelaunch(t, harness.s, harness.root, "api", func(process Process) bool { return process.NextLaunchAt != nil })
			if err := harness.s.Remove(context.Background(), harness.root, "api"); err != nil {
				t.Fatal(err)
			}
			harness.timers.wait(time.Second)
			if !harness.timers.fire(time.Second) {
				t.Fatal("missing cancelled remove timer")
			}
			time.Sleep(10 * time.Millisecond)
			if harness.childCount() != 1 {
				t.Fatalf("remove plus stale timer created %d children", harness.childCount())
			}
			if _, err := harness.s.Get(harness.root, "api"); !errors.Is(err, ErrProcessNotFound) {
				t.Fatalf("Get after remove error = %v", err)
			}
		})

		t.Run("shutdown", func(t *testing.T) {
			harness := newRelaunchTestHarness(t, []relaunchTestLaunch{{code: 1}}, 20)
			if _, err := harness.s.Start(StartRequest{Name: "api", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}, Restart: RestartOnFailure}); err != nil {
				t.Fatal(err)
			}
			harness.child(0).release()
			waitForRelaunch(t, harness.s, harness.root, "api", func(process Process) bool { return process.NextLaunchAt != nil })
			if err := harness.s.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if fired := harness.timers.fireAll(time.Second); fired != 1 {
				t.Fatalf("shutdown pending timers fired = %d, want 1", fired)
			}
			time.Sleep(10 * time.Millisecond)
			if harness.childCount() != 1 {
				t.Fatalf("shutdown plus stale timer created %d children", harness.childCount())
			}
		})
	})

	t.Run("timer-claimed launch remains visible to operator controls", func(t *testing.T) {
		harness := newRelaunchTestHarness(t, []relaunchTestLaunch{{code: 1}, {code: 0}}, 20)
		if _, err := harness.s.Start(StartRequest{Name: "api", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}, Restart: RestartOnFailure}); err != nil {
			t.Fatal(err)
		}
		harness.child(0).release()
		waitForRelaunch(t, harness.s, harness.root, "api", func(process Process) bool { return process.NextLaunchAt != nil })

		originalStarter := harness.s.startProcess
		entered := make(chan struct{})
		proceed := make(chan struct{})
		harness.s.startProcess = func(spec process.Spec) (Child, error) {
			close(entered)
			<-proceed
			return originalStarter(spec)
		}
		harness.timers.wait(time.Second)
		if !harness.timers.fire(time.Second) {
			t.Fatal("missing timer-claim timer")
		}
		<-entered
		items, err := harness.s.List(harness.root, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Name != "api" || items[0].Relaunches != 0 {
			t.Fatalf("active list during incomplete automatic launch = %#v", items)
		}
		close(proceed)
		waitForRelaunchChildren(t, harness, 2)
		if err := harness.s.Stop(context.Background(), harness.root, "api"); err != nil {
			t.Fatal(err)
		}
		stopped := waitForRelaunch(t, harness.s, harness.root, "api", func(process Process) bool { return process.State == StateStopped })
		if stopped.NextLaunchAt != nil || stopped.Relaunches != 0 {
			t.Fatalf("operator stop after timer claim = %#v", stopped)
		}
	})

	t.Run("explicit start serializes with operator controls", func(t *testing.T) {
		for _, control := range []string{"stop", "restart"} {
			t.Run(control, func(t *testing.T) {
				harness := newRelaunchTestHarness(t, []relaunchTestLaunch{{code: 1}, {code: 0}, {code: 0}}, 20)
				if _, err := harness.s.Start(StartRequest{Name: "api", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}, Restart: RestartOnFailure}); err != nil {
					t.Fatal(err)
				}
				harness.child(0).release()
				waitForRelaunch(t, harness.s, harness.root, "api", func(process Process) bool { return process.NextLaunchAt != nil })

				originalStarter := harness.s.startProcess
				entered := make(chan struct{})
				proceed := make(chan struct{})
				harness.s.startProcess = func(spec process.Spec) (Child, error) {
					harness.s.startProcess = originalStarter
					close(entered)
					<-proceed
					return originalStarter(spec)
				}
				startResult := make(chan error, 1)
				go func() {
					_, err := harness.s.Start(StartRequest{Name: "api", Root: harness.root, Cwd: harness.root})
					startResult <- err
				}()
				<-entered
				controlResult := make(chan error, 1)
				go func() {
					if control == "stop" {
						controlResult <- harness.s.Stop(context.Background(), harness.root, "api")
						return
					}
					_, err := harness.s.Restart(context.Background(), harness.root, "api")
					controlResult <- err
				}()
				select {
				case err := <-controlResult:
					t.Fatalf("%s returned before explicit launch published: %v", control, err)
				case <-time.After(10 * time.Millisecond):
				}
				close(proceed)
				if err := <-startResult; err != nil {
					t.Fatalf("explicit Start error = %v", err)
				}
				if err := <-controlResult; err != nil {
					t.Fatalf("%s error = %v", control, err)
				}
				wantChildren := 2
				wantState := StateStopped
				if control == "restart" {
					wantChildren = 3
					wantState = StateRunning
				}
				waitForRelaunchChildren(t, harness, wantChildren)
				current := waitForRelaunch(t, harness.s, harness.root, "api", func(process Process) bool {
					return process.State == wantState
				})
				if current.NextLaunchAt != nil || current.Relaunches != 0 {
					t.Fatalf("post-%s snapshot = %#v", control, current)
				}
				harness.timers.wait(time.Second)
				if !harness.timers.fire(time.Second) {
					t.Fatal("missing cancelled explicit-start timer")
				}
				time.Sleep(10 * time.Millisecond)
				if harness.childCount() != wantChildren {
					t.Fatalf("explicit Start and %s created %d children, want %d", control, harness.childCount(), wantChildren)
				}
				if control == "restart" {
					harness.child(2).release()
				}
			})
		}
	})

	t.Run("invalid policy", func(t *testing.T) {
		harness := newRelaunchTestHarness(t, nil, 20)
		_, err := harness.s.Start(StartRequest{Name: "api", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}, Restart: RestartPolicy("always")})
		if err == nil || !strings.Contains(err.Error(), "restart must be never or on-failure") {
			t.Fatalf("invalid policy error = %v", err)
		}
	})
}

func nowFunc(now time.Time) func() time.Time { return func() time.Time { return now } }

func TestExhaustedRecordResistsEviction(t *testing.T) {
	launches := make([]relaunchTestLaunch, 7)
	launches[0].code = 1
	for index := 1; index <= maxAutomaticRelaunches; index++ {
		launches[index].spawnErr = errors.New("spawn failed")
	}
	harness := newRelaunchTestHarness(t, launches, 1)
	s := harness.s
	if _, err := s.Start(StartRequest{Name: "exhausted", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}, Restart: RestartOnFailure}); err != nil {
		t.Fatal(err)
	}
	harness.child(0).release()
	for attempt := 1; attempt <= maxAutomaticRelaunches; attempt++ {
		delay := relaunchDelay(attempt)
		harness.timers.wait(delay)
		if !harness.timers.fire(delay) {
			t.Fatalf("missing relaunch timer for attempt %d", attempt)
		}
		if attempt < maxAutomaticRelaunches {
			waitForRelaunch(t, s, harness.root, "exhausted", func(process Process) bool {
				return process.NextLaunchAt != nil && process.Relaunches == attempt
			})
		}
	}
	waitForRelaunch(t, s, harness.root, "exhausted", func(process Process) bool {
		return process.State == StateExited && process.NextLaunchAt == nil && process.Relaunches == maxAutomaticRelaunches
	})

	// Churn one more completed record past the limit of one.
	if _, err := s.Start(StartRequest{Name: "filler", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}}); err != nil {
		t.Fatal(err)
	}
	harness.child(1).release()
	// With a completed limit of one, the filler itself is the only evictable
	// record and disappears as soon as it completes.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if process, err := s.Get(harness.root, "filler"); err != nil || process.State == StateExited {
			break
		}
		time.Sleep(time.Millisecond)
	}

	process, err := s.Get(harness.root, "exhausted")
	if err != nil {
		t.Fatalf("exhausted record was evicted: %v", err)
	}
	if process.Relaunches != maxAutomaticRelaunches || process.NextLaunchAt != nil {
		t.Fatalf("exhausted snapshot = %#v, want retained exhaustion state", process)
	}
	items, err := s.List(harness.root, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		found = found || item.Name == "exhausted"
	}
	if !found {
		t.Fatalf("exhausted record missing from active list: %#v", items)
	}
}
