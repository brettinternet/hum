package orchestrate

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestOrchestrateUp(t *testing.T) {
	root := t.TempDir()
	t.Run("readiness success timeout early exit", func(t *testing.T) {
		for _, test := range []struct {
			name       string
			waitResult WaitResult
			mutate     func(*Process)
			outcome    string
		}{
			{name: "success", waitResult: WaitResult{Outcome: WaitMatched, Cursor: 9}, mutate: func(process *Process) {
				process.Readiness = &Readiness{State: ReadinessReady, Match: "ready", Cursor: uint64Pointer(9)}
			}, outcome: "started"},
			{name: "timeout", waitResult: WaitResult{Outcome: WaitTimedOut}, mutate: func(_ *Process) {}, outcome: "timed_out"},
			{name: "early exit", waitResult: WaitResult{Outcome: WaitExited}, mutate: func(process *Process) {
				process.State = "exited"
			}, outcome: "exited_before_ready"},
		} {
			t.Run(test.name, func(t *testing.T) {
				current := Process{Name: "api", Source: "manifest", Cwd: root, PID: 41, State: "running", LaunchCursor: 3, Readiness: &Readiness{State: ReadinessStarting, Match: "ready"}}
				definition := Definition{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"api"}, Ready: &ReadinessConfig{Match: "ready"}}
				var mu sync.Mutex
				waits := 0
				timeout := time.Second
				if test.name == "timeout" {
					timeout = 10 * time.Millisecond
				}
				result, err := WaitForReadiness(context.Background(), root, definition, current, "started", timeout, ReadinessOperations{
					Get: func(context.Context, string, string) (Process, error) {
						mu.Lock()
						defer mu.Unlock()
						return NormalizeProcess(current), nil
					},
					Wait: func(_ context.Context, request WaitRequest) (WaitResult, error) {
						if test.name == "timeout" {
							time.Sleep(request.Timeout)
						}
						mu.Lock()
						defer mu.Unlock()
						waits++
						test.mutate(&current)
						return test.waitResult, nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				if result.Outcome != test.outcome {
					t.Fatalf("outcome=%q, want %q", result.Outcome, test.outcome)
				}
				if waits != 1 {
					t.Fatalf("wait calls=%d, want one", waits)
				}
				if test.name == "success" && (result.Process == nil || result.Process.Readiness == nil || result.Process.Readiness.State != ReadinessReady) {
					t.Fatalf("success result=%#v", result)
				}
				if test.name == "early exit" && (result.Process == nil || result.Process.State != "exited") {
					t.Fatalf("early result=%#v", result)
				}
			})
		}
	})

	t.Run("DAG ordering concurrent roots and classifications", func(t *testing.T) {
		ready := func(name string) Definition {
			return Definition{Name: name, Source: "manifest", Cwd: root, Argv: []string{name}, Ready: &ReadinessConfig{Match: "ready"}}
		}
		definitions := []Definition{
			{Name: "dependent", Source: "manifest", Cwd: root, Argv: []string{"dependent"}, After: []string{"root-a"}, Ready: &ReadinessConfig{Match: "ready"}},
			ready("root-a"), ready("root-b"),
			{Name: "failure", Source: "manifest", Cwd: root, Argv: []string{"failure"}},
			{Name: "timeout", Source: "manifest", Cwd: root, Argv: []string{"timeout"}, Ready: &ReadinessConfig{Match: "ready"}},
			{Name: "blocked", Source: "manifest", Cwd: root, Argv: []string{"blocked"}, After: []string{"timeout", "failure"}, Ready: &ReadinessConfig{Match: "ready"}},
		}
		bothRoots := make(chan struct{})
		var rootsMu sync.Mutex
		rootStarts := make([]string, 0, 2)
		started := make([]string, 0, len(definitions))
		var orderMu sync.Mutex
		results, err := OrchestrateUp(context.Background(), UpOptions{Definitions: definitions}, UpOperations{
			Start: func(_ context.Context, definition Definition) (StartResult, error) {
				if definition.Name == "failure" {
					return StartResult{Result: ErrorResult(definition, errors.New("boom"))}, nil
				}
				orderMu.Lock()
				started = append(started, definition.Name)
				pid := len(started)
				orderMu.Unlock()
				process := Process{Name: definition.Name, Source: definition.Source, Cwd: root, PID: pid, State: "running", LaunchCursor: 1}
				if definition.Ready != nil {
					process.Readiness = &Readiness{State: ReadinessStarting, Match: definition.Ready.Match}
				}
				if definition.Name == "root-a" || definition.Name == "root-b" {
					rootsMu.Lock()
					rootStarts = append(rootStarts, definition.Name)
					if len(rootStarts) == 2 {
						close(bothRoots)
					}
					rootsMu.Unlock()
				}
				return StartResult{Result: ResultForProcess(definition, process, "started"), Process: process, ObservedAt: time.Now()}, nil
			},
			Readiness: func(_ context.Context, definition Definition, process Process, outcome string, _ time.Duration) (Result, error) {
				if definition.Name == "root-a" {
					<-bothRoots
				}
				if definition.Name == "timeout" {
					return ResultForProcess(definition, process, "timed_out"), nil
				}
				process.Readiness = &Readiness{State: ReadinessReady, Match: "ready", Cursor: uint64Pointer(4)}
				return ResultForProcess(definition, process, outcome), nil
			},
			Skipped: func(_ context.Context, definition Definition, blocked []string) Result {
				return Result{Name: definition.Name, Outcome: "skipped", BlockedBy: append([]string(nil), blocked...)}
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(rootStarts, []string{"root-a", "root-b"}) && !reflect.DeepEqual(rootStarts, []string{"root-b", "root-a"}) {
			t.Fatalf("root starts=%v, want both independent roots", rootStarts)
		}
		if indexOf(started, "dependent") < indexOf(started, "root-a") {
			t.Fatalf("dependent launched before root-a: %v", started)
		}
		byName := make(map[string]Result, len(results))
		names := make([]string, 0, len(results))
		for _, result := range results {
			byName[result.Name] = result
			names = append(names, result.Name)
		}
		if !sort.StringsAreSorted(names) {
			t.Fatalf("result order=%v, want lexical", names)
		}
		if byName["dependent"].Outcome != "started" || byName["dependent"].Process == nil || byName["dependent"].Process.Readiness.State != ReadinessReady {
			t.Fatalf("dependent=%#v", byName["dependent"])
		}
		if byName["blocked"].Outcome != "skipped" || !reflect.DeepEqual(byName["blocked"].BlockedBy, []string{"failure", "timeout"}) {
			t.Fatalf("blocked=%#v, want sorted direct blockers", byName["blocked"])
		}
		if byName["timeout"].Outcome != "timed_out" || byName["failure"].Outcome != "error" {
			t.Fatalf("terminal classifications: timeout=%#v failure=%#v", byName["timeout"], byName["failure"])
		}
	})

	t.Run("pre-launch follower is not an existing process", func(t *testing.T) {
		definition := Definition{Name: "dependent", Source: "manifest", Argv: []string{"dependent"}}
		result := SkippedResult(context.Background(), root, definition, []string{"root"}, func(context.Context, string, string) (Process, error) {
			return Process{Name: "dependent", Root: root, Cwd: root, State: "exited", NextCursor: uint64Pointer(0)}, nil
		})
		if result.Process != nil || result.ExistingState != "" {
			t.Fatalf("pre-launch follower result = %#v, want no existing process", result)
		}
	})

	t.Run("definition drift removed definitions and recovery", func(t *testing.T) {
		definition := Definition{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"new"}, Ready: &ReadinessConfig{Match: "new"}}
		drifted := Process{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"old"}, State: "running", PID: 7, LaunchCursor: 8, Readiness: &Readiness{State: ReadinessStarting, Match: "old"}}
		drift := DefinitionDriftResult(root, definition, drifted)
		if drift.Outcome != "definition_drift" || !reflect.DeepEqual(drift.ChangedFields, []string{"argv", "readiness_match"}) || drift.Guidance != "hum restart api" {
			t.Fatalf("drift=%#v", drift)
		}
		next := time.Now().Add(time.Minute)
		pending := Process{Name: "pending", Source: "manifest", State: "exited", Restart: "on-failure", Relaunches: 2, NextLaunchAt: &next}
		exhausted := Process{Name: "exhausted", Source: "manifest", State: "exited", Restart: "on-failure", Relaunches: AutomaticRelaunchLimit}
		if outcome, ok := RecoveryOutcome(Definition{Name: "pending", Source: "manifest"}, pending); !ok || outcome != "recovery_pending" {
			t.Fatalf("pending recovery=%q,%v", outcome, ok)
		}
		if outcome, ok := RecoveryOutcome(Definition{Name: "exhausted", Source: "manifest"}, exhausted); !ok || outcome != "recovery_exhausted" {
			t.Fatalf("exhausted recovery=%q,%v", outcome, ok)
		}
		removed := RemovedDefinitionResults(root, []Definition{{Name: "current", Source: "manifest"}}, []Process{
			{Name: "current", Source: "manifest", State: "running"},
			{Name: "pending", Source: "manifest", State: "exited", Restart: "on-failure", Relaunches: 2, NextLaunchAt: &next},
			{Name: "exhausted", Source: "manifest", State: "exited", Restart: "on-failure", Relaunches: AutomaticRelaunchLimit},
			{Name: "stopped", Source: "manifest", State: "exited"},
			{Name: "ad-hoc", Source: "ad_hoc", State: "running"},
		})
		if len(removed) != 2 || removed[0].Name != "exhausted" || removed[1].Name != "pending" || removed[0].Outcome != "removed_definition" || removed[1].Outcome != "removed_definition" {
			t.Fatalf("removed=%#v", removed)
		}
	})

	t.Run("surviving descendants retain the launch slot", func(t *testing.T) {
		definition := Definition{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"api"}}
		current := Process{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"api"}, State: "descendants", PGID: 41}
		starts := 0
		result := Ensure(context.Background(), root, definition, nil, false, EnsureOperations{
			Get: func(context.Context, string, string) (Process, error) { return current, nil },
			Start: func(context.Context, StartRequest) (Process, error) {
				starts++
				return Process{}, errors.New("unexpected start")
			},
		})
		if starts != 0 || !result.Already || result.Result.Outcome != "already_running" || result.Process.State != "descendants" {
			t.Fatalf("descendants ensure = %#v, starts=%d", result, starts)
		}
	})
}

func indexOf(values []string, value string) int {
	for index, candidate := range values {
		if candidate == value {
			return index
		}
	}
	return len(values)
}
