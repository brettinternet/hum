package app

import (
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
