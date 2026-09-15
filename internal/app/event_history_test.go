package app

import (
	"errors"
	"regexp"
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
