//go:build !windows

package app

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"hum/internal/output"
	"hum/internal/process"
)

func TestEventHistoryLifecycleKinds(t *testing.T) {
	root := makeProject(t, false)
	child := newSubscriptionChild(7001, 7, time.Now().UTC(), "ready\n")
	s := testSupervisor(t, Options{
		OutputLimits: output.Limits{RetainedBytes: 1024, DefaultReadEntries: 16, DefaultReadBytes: 1024},
		StartProcess: subscriptionStarter(map[string]*subscriptionChild{"api": child}),
	})
	var mu sync.Mutex
	var events []LifecycleEvent
	s.SetLifecycleHook(func(event LifecycleEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})
	if _, err := s.Start(StartRequest{Name: "api", Root: root, Cwd: root, Source: "manifest", Argv: []string{"fake", "api"}, Ready: &ReadinessConfig{Method: "match", Match: "ready", Timeout: time.Second}}); err != nil {
		t.Fatal(err)
	}
	child.release()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		seenExit := false
		for _, event := range events {
			seenExit = seenExit || event.Event == "exit"
		}
		mu.Unlock()
		if seenExit {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	want := map[string]bool{"launch": false, "ready": false, "exit": false}
	for _, event := range events {
		if _, ok := want[event.Event]; ok {
			want[event.Event] = true
		}
		if event.Event == "exit" && (event.ExitCode == nil || *event.ExitCode != 7) {
			t.Fatalf("exit event = %#v", event)
		}
	}
	for event, seen := range want {
		if !seen {
			t.Errorf("missing %s event in %#v", event, events)
		}
	}
}

func TestLifecycleLogCursor(t *testing.T) {
	root := makeProject(t, false)
	first := newSubscriptionChild(7201, 3, time.Now().UTC(), "one\n")
	second := newSubscriptionChild(7202, 3, time.Now().UTC(), "two\n")
	empty := newSubscriptionChild(7203, 0, time.Now().UTC(), "")
	children := []*subscriptionChild{first, second, empty}
	var started int
	s := testSupervisor(t, Options{StartProcess: func(spec process.Spec) (Child, error) {
		child := children[started]
		started++
		return subscriptionStarter(map[string]*subscriptionChild{spec.Argv[len(spec.Argv)-1]: child})(spec)
	}})
	events := make(chan LifecycleEvent, 16)
	s.SetLifecycleHook(func(event LifecycleEvent) { events <- event })
	await := func(kind string) LifecycleEvent {
		t.Helper()
		for {
			select {
			case event := <-events:
				if event.Event == kind {
					return event
				}
			case <-time.After(time.Second):
				t.Fatalf("timed out waiting for %s", kind)
			}
		}
	}
	start := func(name string) {
		t.Helper()
		if _, err := s.Start(StartRequest{Name: name, Root: root, Cwd: root, Argv: []string{"fake", name}}); err != nil {
			t.Fatal(err)
		}
	}
	start("api")
	if event := await("launch"); event.LogCursor != nil {
		t.Fatalf("first launch cursor = %v, want nil", event.LogCursor)
	}
	first.release()
	if event := await("exit"); event.LogCursor == nil || *event.LogCursor != 0 {
		t.Fatalf("first exit cursor = %v, want 0", event.LogCursor)
	}
	start("api")
	launch := await("launch")
	if launch.LogCursor == nil || *launch.LogCursor != 1 {
		t.Fatalf("second launch cursor = %v, want marker 1", launch.LogCursor)
	}
	retained, err := second.store.Read(output.ReadOptions{})
	if err != nil || len(retained.Entries) != 3 || retained.Entries[1].Cursor != *launch.LogCursor || retained.Entries[1].Text != "api launched\n" {
		t.Fatalf("launch marker: page=%+v err=%v", retained, err)
	}
	page, err := second.store.Read(output.ReadOptions{After: launch.LogCursor})
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Text != "two\n" || page.Entries[0].Cursor != 2 {
		t.Fatalf("after launch: page=%+v err=%v", page, err)
	}
	second.release()
	if event := await("exit"); event.LogCursor == nil || *event.LogCursor != 2 {
		t.Fatalf("second exit cursor = %v, want 2", event.LogCursor)
	}
	start("empty")
	if event := await("launch"); event.LogCursor != nil {
		t.Fatalf("empty launch cursor = %v", event.LogCursor)
	}
	empty.release()
	if event := await("exit"); event.LogCursor != nil {
		t.Fatalf("empty exit cursor = %v, want nil", event.LogCursor)
	}

	t.Run("exit cursor excludes concurrent next launch", func(t *testing.T) {
		root := makeProject(t, false)
		first := newSubscriptionChild(7301, 0, time.Now().UTC(), "first\n")
		second := newSubscriptionChild(7302, 0, time.Now().UTC(), "second\n")
		var launches int
		s := testSupervisor(t, Options{StartProcess: func(spec process.Spec) (Child, error) {
			child := first
			if launches != 0 {
				child = second
			}
			launches++
			return subscriptionStarter(map[string]*subscriptionChild{"api": child})(spec)
		}})
		inReady := make(chan struct{})
		continueExit := make(chan struct{})
		var blockReady sync.Once
		exits := make(chan LifecycleEvent, 2)
		s.SetLifecycleHook(func(event LifecycleEvent) {
			if event.Event == "ready" && event.Detail == "method=exit" {
				blockReady.Do(func() {
					close(inReady)
					<-continueExit
				})
			}
			if event.Event == "exit" {
				exits <- event
			}
		})
		request := StartRequest{Name: "api", Root: root, Cwd: root, Argv: []string{"fake", "api"}, Ready: &ReadinessConfig{Method: "exit"}}
		if _, err := s.Start(request); err != nil {
			t.Fatal(err)
		}
		first.release()
		select {
		case <-inReady:
		case <-time.After(time.Second):
			t.Fatal("first exit did not reach ready hook")
		}
		if _, err := s.Start(request); err != nil {
			close(continueExit)
			t.Fatal(err)
		}
		close(continueExit)
		select {
		case event := <-exits:
			if event.LogCursor == nil || *event.LogCursor != 0 {
				t.Fatalf("old exit cursor = %v, want first output cursor 0", event.LogCursor)
			}
		case <-time.After(time.Second):
			t.Fatal("first exit event missing")
		}
		second.release()
	})
}

func TestExitReadinessSupervisorCompletion(t *testing.T) {
	for _, test := range []struct {
		name               string
		code               int
		stop               bool
		wantState          State
		wantReady          string
		wantStartupFailure bool
	}{
		{name: "successful exit", wantState: StateExited, wantReady: ReadinessReady},
		{name: "nonzero exit", code: 3, wantState: StateExited, wantReady: ReadinessStarting, wantStartupFailure: true},
		{name: "operator stop", stop: true, wantState: StateStopped, wantReady: ReadinessStarting},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := makeProject(t, false)
			child := newSubscriptionChild(7100, test.code, time.Now().UTC(), "setup output\n")
			s := testSupervisor(t, Options{StartProcess: subscriptionStarter(map[string]*subscriptionChild{"migrate": child})})
			var mu sync.Mutex
			var events []LifecycleEvent
			s.SetLifecycleHook(func(event LifecycleEvent) {
				mu.Lock()
				events = append(events, event)
				mu.Unlock()
			})
			started, err := s.Start(StartRequest{
				Name: "migrate", Root: root, Cwd: root, Source: "manifest", Argv: []string{"fake", "migrate"},
				Ready: &ReadinessConfig{Method: "exit", Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}
			if started.Readiness == nil || started.Readiness.Method != "exit" || started.Readiness.State != ReadinessStarting {
				t.Fatalf("initial readiness = %#v", started.Readiness)
			}
			if test.stop {
				if err := s.Stop(context.Background(), root, "migrate"); err != nil {
					t.Fatal(err)
				}
			} else {
				child.release()
			}
			deadline := time.Now().Add(time.Second)
			var current Process
			for time.Now().Before(deadline) {
				current, err = s.Get(root, "migrate")
				if err == nil && current.State == test.wantState {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if err != nil || current.State != test.wantState {
				t.Fatalf("terminal process = %+v, err=%v, want state %s", current, err, test.wantState)
			}
			if current.Readiness == nil || current.Readiness.Method != "exit" || current.Readiness.State != test.wantReady {
				t.Fatalf("terminal readiness = %#v, want %s", current.Readiness, test.wantReady)
			}
			if test.wantReady == ReadinessReady && (current.Exit == nil || current.Exit.ExitCode != 0 || !current.Readiness.Time.Equal(current.Exit.ExitedAt)) {
				t.Fatalf("successful completion status = process %#v", current)
			}

			eventDeadline := time.Now().Add(time.Second)
			for time.Now().Before(eventDeadline) {
				mu.Lock()
				seenExit := false
				for _, event := range events {
					seenExit = seenExit || event.Event == "exit"
				}
				mu.Unlock()
				if seenExit {
					break
				}
				time.Sleep(time.Millisecond)
			}
			mu.Lock()
			defer mu.Unlock()
			readyIndex, exitIndex, startupFailure := -1, -1, false
			for index, event := range events {
				switch event.Event {
				case "ready":
					if event.Detail == "method=exit" {
						readyIndex = index
					}
				case "exit":
					exitIndex = index
				case "startup_failure":
					startupFailure = true
				}
			}
			if (readyIndex >= 0) != (test.wantReady == ReadinessReady) || readyIndex >= exitIndex && readyIndex >= 0 {
				t.Fatalf("ready event index %d, exit event index %d, events=%#v", readyIndex, exitIndex, events)
			}
			if startupFailure != test.wantStartupFailure {
				t.Fatalf("startup failure event = %v, want %v (events=%#v)", startupFailure, test.wantStartupFailure, events)
			}
		})
	}
}

func TestExitReadinessRestartDuringExitPersistence(t *testing.T) {
	root := makeProject(t, false)
	first := newSubscriptionChild(7110, 0, time.Now().UTC(), "")
	second := newSubscriptionChild(7111, 0, time.Now().UTC().Add(time.Second), "")
	persisting := make(chan struct{})
	allowPersist := make(chan struct{})
	var calls int
	s := testSupervisor(t, Options{
		StartProcess: func(spec process.Spec) (Child, error) {
			calls++
			if calls == 1 {
				first.store = spec.Output
				return first, nil
			}
			second.store = spec.Output
			return second, nil
		},
		PersistExit: func(Process) error {
			if calls == 1 {
				close(persisting)
				<-allowPersist
			}
			return nil
		},
	})
	if _, err := s.Start(StartRequest{Name: "migrate", Root: root, Cwd: root, Source: "manifest", Argv: []string{"fake"}, Ready: &ReadinessConfig{Method: "exit"}}); err != nil {
		t.Fatal(err)
	}
	first.release()
	<-persisting
	restarted := make(chan error, 1)
	go func() {
		_, err := s.Restart(context.Background(), root, "migrate")
		restarted <- err
	}()
	// Wait for stopRecord to mark the persisting exit as controlled.
	rec, err := s.lookupScoped(ScopeProject, root, "migrate", "")
	if err != nil {
		close(allowPersist)
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	controlled := false
	for time.Now().Before(deadline) {
		s.mu.RLock()
		controlled = rec.controlIntent && rec.persisting
		s.mu.RUnlock()
		if controlled {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !controlled {
		close(allowPersist)
		t.Fatal("restart did not mark the persisting exit as controlled")
	}
	close(allowPersist)
	if err := <-restarted; err != nil {
		t.Fatal(err)
	}
	second.release()
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, err := s.Get(root, "migrate")
		if err == nil && current.State == StateExited {
			if current.Readiness == nil || current.Readiness.State != ReadinessReady {
				t.Fatalf("replacement exit readiness = %#v, want ready", current.Readiness)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("replacement did not exit")
}

func TestEventHistoryAutomaticRelaunchKinds(t *testing.T) {
	launches := make([]relaunchTestLaunch, maxAutomaticRelaunches+1)
	launches[0].code = 1
	for index := 1; index < len(launches); index++ {
		launches[index].spawnErr = errors.New("secret-bearing relaunch failure")
	}
	harness := newRelaunchTestHarness(t, launches, 20)
	var mu sync.Mutex
	seen := make(map[string][]LifecycleEvent)
	harness.s.SetLifecycleHook(func(event LifecycleEvent) {
		mu.Lock()
		seen[event.Event] = append(seen[event.Event], event)
		mu.Unlock()
	})
	if _, err := harness.s.Start(StartRequest{Name: "api", Source: "manifest", Root: harness.root, Cwd: harness.root, Argv: []string{"fake"}, Restart: RestartOnFailure}); err != nil {
		t.Fatal(err)
	}
	harness.child(0).release()
	for attempt, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second} {
		waitForRelaunch(t, harness.s, harness.root, "api", func(process Process) bool { return process.NextLaunchAt != nil && process.Relaunches == attempt })
		harness.timers.wait(delay)
		if !harness.timers.fire(delay) {
			t.Fatalf("missing relaunch timer %d", attempt+1)
		}
	}
	waitForRelaunch(t, harness.s, harness.root, "api", func(process Process) bool {
		return process.State == StateExited && process.Relaunches == maxAutomaticRelaunches && process.NextLaunchAt == nil
	})
	mu.Lock()
	defer mu.Unlock()
	for _, kind := range []string{"exit", "relaunch_scheduled", "relaunch_attempt", "relaunch_failure", "relaunch_exhausted"} {
		if len(seen[kind]) == 0 {
			t.Errorf("missing %s event in %#v", kind, seen)
		}
	}
	for _, event := range seen["relaunch_failure"] {
		if strings.Contains(event.Detail, "secret-bearing") {
			t.Fatalf("relaunch event retained raw error: %#v", event)
		}
	}
}

func TestEventHistoryStartupFailure(t *testing.T) {
	root := makeProject(t, false)
	s := testSupervisor(t, Options{StartProcess: func(process.Spec) (Child, error) {
		return nil, errors.New("secret-bearing launch failure")
	}})
	var got LifecycleEvent
	s.SetLifecycleHook(func(event LifecycleEvent) { got = event })
	_, err := s.Start(StartRequest{Name: "api", Root: root, Cwd: root, Argv: []string{"fake", "api"}})
	if err == nil {
		t.Fatal("start unexpectedly succeeded")
	}
	if got.Event != "startup_failure" || got.Detail != "process launch failed" || regexp.MustCompile("secret-bearing").MatchString(got.Detail) {
		t.Fatalf("startup failure event = %#v", got)
	}
}
