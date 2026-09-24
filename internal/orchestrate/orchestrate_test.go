package orchestrate

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProcessStopGraceDefinitionDrift(t *testing.T) {
	inherited := Definition{Name: "api", Source: "manifest", Argv: []string{"api"}}
	explicitZero := time.Duration(0)
	if changed := DefinitionChangedFields("/project", inherited, Process{Source: "manifest", Argv: []string{"api"}, StopGrace: explicitZero}); !reflect.DeepEqual(changed, []string{"stop_grace"}) {
		t.Fatalf("explicit zero to omitted changed fields = %v, want [stop_grace]", changed)
	}
	if changed := DefinitionChangedFields("/project", inherited, Process{Source: "manifest", Argv: []string{"api"}, StopGraceInherited: true}); len(changed) != 0 {
		t.Fatalf("inherited to omitted changed fields = %v, want none", changed)
	}
	explicitGrace := 2 * time.Second
	if changed := DefinitionChangedFields("/project", Definition{Name: "api", Source: "manifest", Argv: []string{"api"}, StopGrace: &explicitGrace}, Process{Source: "manifest", Argv: []string{"api"}, StopGrace: time.Second}); !reflect.DeepEqual(changed, []string{"stop_grace"}) {
		t.Fatalf("explicit grace mismatch changed fields = %v, want [stop_grace]", changed)
	}
}

func TestExecutableReadiness(t *testing.T) {
	root := t.TempDir()
	calls := 0
	initial := Process{Name: "api", Source: "manifest", Root: root, Cwd: root, PID: 9, LaunchCursor: 2, State: "running", Readiness: &Readiness{Method: "exec", Argv: []string{"health", "api"}, Interval: time.Second, State: ReadinessStarting}}
	current := initial
	result, err := WaitForReadiness(context.Background(), root, Definition{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"api"}, Ready: &ReadinessConfig{Method: "exec", Argv: []string{"health", "api"}}}, initial, "started", time.Second, ReadinessOperations{
		Get: func(context.Context, string, string) (Process, error) {
			calls++
			if calls > 1 {
				current.Readiness = &Readiness{Method: "exec", Argv: []string{"health", "api"}, State: ReadinessReady, Time: time.Now()}
			}
			return current, nil
		},
		Wait: func(context.Context, WaitRequest) (WaitResult, error) {
			t.Fatal("exec readiness must not issue output wait")
			return WaitResult{}, nil
		},
	})
	if err != nil || result.Outcome != "started" || result.Process == nil || result.Process.Readiness.State != ReadinessReady {
		t.Fatalf("result=%#v err=%v", result, err)
	}

	initial.Readiness = &Readiness{Method: "exec", Argv: []string{"health", "api"}, Interval: time.Second, State: ReadinessStarting}
	for _, test := range []struct {
		name       string
		state      string
		readyTime  time.Time
		launch     Process
		wantResult string
	}{
		{name: "ExecutableReadinessTerminalReadyBeforeDeadline", state: "exited", readyTime: time.Now(), wantResult: "started"},
		{name: "ExecutableReadinessReadyAfterDeadline", state: "exited", readyTime: time.Now().Add(time.Second), wantResult: "timed_out"},
		{name: "ExecutableReadinessSuccessorReadyDoesNotMatch", state: "running", readyTime: time.Now(), launch: Process{PID: 10, LaunchCursor: 3}, wantResult: "exited_before_ready"},
	} {
		t.Run(test.name, func(t *testing.T) {
			launch := initial
			current := initial
			current.State = test.state
			current.Readiness = &Readiness{Method: "exec", Argv: initial.Readiness.Argv, State: ReadinessReady, Time: test.readyTime}
			if test.launch.PID != 0 {
				current.PID, current.LaunchCursor = test.launch.PID, test.launch.LaunchCursor
			}
			result, err := WaitForReadiness(context.Background(), root, Definition{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"api"}, Ready: &ReadinessConfig{Method: "exec", Argv: initial.Readiness.Argv}}, launch, "started", 100*time.Millisecond, ReadinessOperations{
				Get: func(context.Context, string, string) (Process, error) { return current, nil },
			})
			if err != nil || result.Outcome != test.wantResult {
				t.Fatalf("result=%#v err=%v, want %q", result, err, test.wantResult)
			}
		})
	}

	t.Run("ExecutableReadinessCallerDeadlineCancels", func(t *testing.T) {
		waiting := initial
		waiting.Readiness = &Readiness{Method: "exec", Argv: []string{"health", "api"}, State: ReadinessStarting}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_, err := WaitForReadiness(ctx, root, Definition{Name: "api", Ready: &ReadinessConfig{Method: "exec", Argv: []string{"probe"}}}, waiting, "started", time.Second, ReadinessOperations{
			Get: func(context.Context, string, string) (Process, error) { return waiting, nil },
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err=%v, want caller deadline cancellation", err)
		}
	})

	t.Run("ExecutableReadinessTimeoutReturnsTimedOut", func(t *testing.T) {
		waiting := initial
		waiting.Readiness = &Readiness{Method: "exec", Argv: []string{"health", "api"}, State: ReadinessStarting}
		result, err := WaitForReadiness(context.Background(), root, Definition{Name: "api", Ready: &ReadinessConfig{Method: "exec", Argv: []string{"probe"}}}, waiting, "started", 5*time.Millisecond, ReadinessOperations{
			Get: func(context.Context, string, string) (Process, error) { return waiting, nil },
		})
		if err != nil || result.Outcome != "timed_out" {
			t.Fatalf("result=%#v err=%v, want timed_out", result, err)
		}
	})
}

func TestReadinessHTTPDrift(t *testing.T) {
	root := t.TempDir()
	definition := Definition{Name: "web", Source: "manifest", Cwd: root, Argv: []string{"web"}, Ready: &ReadinessConfig{Method: "http", Target: "http://127.0.0.1:1/readyz"}}
	process := Process{Name: "web", Source: "manifest", Cwd: root, Argv: []string{"web"}, State: "running", Readiness: &Readiness{Method: "http", Target: "http://127.0.0.1:2/readyz"}, StopGraceInherited: true}
	got := DefinitionDriftResult(root, definition, process)
	if !reflect.DeepEqual(got.ChangedFields, []string{"readiness_http"}) {
		t.Fatalf("HTTP drift = %#v", got.ChangedFields)
	}
}

func TestReadinessHTTPPolicyDoesNotDrift(t *testing.T) {
	root := t.TempDir()
	definition := Definition{Name: "web", Source: "manifest", Cwd: root, Argv: []string{"web"}, Ready: &ReadinessConfig{Method: "http", Target: "http://127.0.0.1:1/ready", Interval: time.Second, Timeout: 30 * time.Second}}
	process := Process{Name: "web", Source: "manifest", Cwd: root, Argv: []string{"web"}, State: "running", StopGraceInherited: true, Readiness: &Readiness{Method: "http", Target: definition.Ready.Target, Interval: 5 * time.Second, State: ReadinessStarting}}
	if got := DefinitionDriftResult(root, definition, process); len(got.ChangedFields) != 0 {
		t.Fatalf("HTTP policy drift = %v", got.ChangedFields)
	}
}

func TestReadinessTCPDrift(t *testing.T) {
	root := t.TempDir()
	definition := Definition{Name: "db", Source: "manifest", Cwd: root, Argv: []string{"db"}, Ready: &ReadinessConfig{Method: "tcp", Target: "127.0.0.1:1"}}
	process := Process{Name: "db", Source: "manifest", Cwd: root, Argv: []string{"db"}, State: "running", Readiness: &Readiness{Method: "tcp", Target: "127.0.0.1:2"}, StopGraceInherited: true}
	got := DefinitionDriftResult(root, definition, process)
	if !reflect.DeepEqual(got.ChangedFields, []string{"readiness_tcp"}) {
		t.Fatalf("TCP drift = %#v", got.ChangedFields)
	}
}

func TestReadinessDriftAllMethodPairs(t *testing.T) {
	root := t.TempDir()
	methods := []string{"", "match", "exec", "http", "tcp"}
	makeConfig := func(method string) *ReadinessConfig {
		if method == "" {
			return nil
		}
		config := &ReadinessConfig{Method: method, Match: "ready", Argv: []string{"probe"}, Target: "http://127.0.0.1:1/"}
		if method == "tcp" {
			config.Target = "127.0.0.1:1"
		}
		return config
	}
	for _, oldMethod := range methods {
		for _, newMethod := range methods {
			t.Run(oldMethod+"-to-"+newMethod, func(t *testing.T) {
				definition := Definition{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"api"}, Ready: makeConfig(newMethod)}
				process := Process{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"api"}, State: "running", StopGraceInherited: true}
				if oldMethod != "" {
					config := makeConfig(oldMethod)
					process.Readiness = &Readiness{Method: oldMethod, Match: config.Match, Argv: config.Argv, Target: config.Target, State: ReadinessStarting}
				}
				got := DefinitionChangedFields(root, definition, process)
				want := []string{}
				if oldMethod != newMethod {
					if oldMethod != "" {
						want = append(want, "readiness_"+oldMethod)
					}
					if newMethod != "" {
						want = append(want, "readiness_"+newMethod)
					}
				}
				sort.Strings(want)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("old=%q new=%q got=%v want=%v", oldMethod, newMethod, got, want)
				}
			})
		}
	}
}

func TestExitReadinessWaitsForSuccessfulCompletion(t *testing.T) {
	root := t.TempDir()
	initial := Process{
		Name: "migrate", Source: "manifest", Root: root, Cwd: root, PID: 41, LaunchCursor: 3,
		State: "running", Readiness: &Readiness{Method: "exit", State: ReadinessStarting},
	}
	current := initial
	definition := Definition{Name: "migrate", Source: "manifest", Cwd: root, Argv: []string{"migrate"}, Ready: &ReadinessConfig{Method: "exit"}}
	calls := 0
	result, err := WaitForReadiness(context.Background(), root, definition, initial, "started", time.Second, ReadinessOperations{
		Get: func(context.Context, string, string) (Process, error) {
			calls++
			if calls == 2 {
				at := time.Now()
				current.State = "exited"
				current.Exit = &Exit{Code: 0, Time: at}
				current.Readiness = &Readiness{Method: "exit", State: ReadinessReady, Time: at}
			}
			return current, nil
		},
		Wait: func(context.Context, WaitRequest) (WaitResult, error) {
			t.Fatal("exit readiness must not use output wait")
			return WaitResult{}, nil
		},
	})
	if err != nil || result.Outcome != "completed" || result.Process == nil || result.Process.State != "exited" || !ResultSatisfiesGate(result) {
		t.Fatalf("completed exit readiness result = %#v, err = %v", result, err)
	}

	t.Run("timeout", func(t *testing.T) {
		waiting := initial
		result, err := WaitForReadiness(context.Background(), root, definition, waiting, "started", 5*time.Millisecond, ReadinessOperations{
			Get: func(context.Context, string, string) (Process, error) { return waiting, nil },
		})
		if err != nil || result.Outcome != "timed_out" {
			t.Fatalf("exit readiness timeout = %#v, err = %v", result, err)
		}
	})
}

func TestExitReadinessOrchestrateUp(t *testing.T) {
	root := t.TempDir()
	missing := errors.New("not found")
	definitions := []Definition{
		{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"api"}, After: []string{"migrate"}, Ready: &ReadinessConfig{Match: "ready"}},
		{Name: "migrate", Source: "manifest", Cwd: root, Argv: []string{"migrate"}, Ready: &ReadinessConfig{Method: "exit"}},
	}

	t.Run("exit zero releases dependents even when no-wait is set", func(t *testing.T) {
		var started []string
		results, err := OrchestrateUp(context.Background(), UpOptions{Root: root, Definitions: definitions, NoWait: true}, UpOperations{
			Start: func(_ context.Context, definition Definition) (StartResult, error) {
				started = append(started, definition.Name)
				process := Process{Name: definition.Name, Source: definition.Source, Root: root, Cwd: root, Argv: definition.Argv, PID: len(started), State: "running", LaunchCursor: uint64(len(started)), StopGraceInherited: true}
				if definition.Ready.Method == "exit" {
					process.Readiness = &Readiness{Method: "exit", State: ReadinessStarting}
				} else {
					process.Readiness = &Readiness{Method: "match", Match: "ready", State: ReadinessStarting}
				}
				return StartResult{Result: ResultForProcess(definition, process, "started"), Process: process}, nil
			},
			Readiness: func(ctx context.Context, definition Definition, process Process, outcome string, timeout time.Duration) (Result, error) {
				if definition.Name == "migrate" {
					at := time.Now()
					process.State = "exited"
					process.Exit = &Exit{Code: 0, Time: at}
					process.Readiness = &Readiness{Method: "exit", State: ReadinessReady, Time: at}
					current := process
					return WaitForReadiness(ctx, root, definition, process, outcome, timeout, ReadinessOperations{
						Get: func(context.Context, string, string) (Process, error) { return current, nil },
					})
				}
				process.Readiness.State = ReadinessReady
				return ResultForProcess(definition, process, "started"), nil
			},
			IsNotFound: func(err error) bool { return errors.Is(err, missing) },
			Get:        func(context.Context, string, string) (Process, error) { return Process{}, missing },
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(started, []string{"migrate", "api"}) || len(results) != 2 || results[0].Outcome != "started" || results[1].Outcome != "completed" || !ResultSatisfiesGate(results[1]) {
			t.Fatalf("started=%v, results=%#v", started, results)
		}
	})

	for _, test := range []struct {
		name  string
		state string
		exit  *Exit
	}{
		{name: "nonzero", state: "exited", exit: &Exit{Code: 3}},
		{name: "signal", state: "exited", exit: &Exit{Code: -1, Signal: &SignalInfo{Name: "SIGTERM", Number: 15}}},
		{name: "operator stop", state: "stopped"},
	} {
		t.Run(test.name+" skips dependents", func(t *testing.T) {
			var started []string
			results, err := OrchestrateUp(context.Background(), UpOptions{Root: root, Definitions: definitions, NoWait: true}, UpOperations{
				Start: func(_ context.Context, definition Definition) (StartResult, error) {
					started = append(started, definition.Name)
					process := Process{Name: definition.Name, Source: definition.Source, Root: root, Cwd: root, Argv: definition.Argv, PID: 41, State: "running", LaunchCursor: 3, StopGraceInherited: true, Readiness: &Readiness{Method: "exit", State: ReadinessStarting}}
					return StartResult{Result: ResultForProcess(definition, process, "started"), Process: process}, nil
				},
				Readiness: func(ctx context.Context, definition Definition, process Process, outcome string, timeout time.Duration) (Result, error) {
					process.State = test.state
					process.Exit = test.exit
					current := process
					return WaitForReadiness(ctx, root, definition, process, outcome, timeout, ReadinessOperations{
						Get: func(context.Context, string, string) (Process, error) { return current, nil },
					})
				},
				Skipped: func(_ context.Context, definition Definition, blocked []string) Result {
					return Result{Name: definition.Name, Outcome: "skipped", BlockedBy: blocked}
				},
				IsNotFound: func(err error) bool { return errors.Is(err, missing) },
				Get:        func(context.Context, string, string) (Process, error) { return Process{}, missing },
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(started, []string{"migrate"}) || len(results) != 2 || results[0].Outcome != "skipped" || !reflect.DeepEqual(results[0].BlockedBy, []string{"migrate"}) || results[1].Outcome != "exited_before_ready" || ResultSatisfiesGate(results[1]) {
				t.Fatalf("started=%v, results=%#v", started, results)
			}
		})
	}
}

func TestExitReadinessRetainedCompletionConvergence(t *testing.T) {
	root := t.TempDir()
	missing := errors.New("not found")
	definitions := []Definition{
		{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"api"}, After: []string{"migrate"}, Ready: &ReadinessConfig{Match: "ready"}},
		{Name: "migrate", Source: "manifest", Cwd: root, Argv: []string{"migrate"}, Ready: &ReadinessConfig{Method: "exit"}},
	}
	completedAt := time.Now()
	completed := Process{
		Name: "migrate", Source: "manifest", Root: root, Cwd: root, Argv: []string{"migrate"}, State: "exited",
		Exit: &Exit{Code: 0, Time: completedAt}, Readiness: &Readiness{Method: "exit", State: ReadinessReady, Time: completedAt}, StopGraceInherited: true,
	}
	readyAPI := Process{
		Name: "api", Source: "manifest", Root: root, Cwd: root, Argv: []string{"api"}, PID: 42, LaunchCursor: 4, State: "running",
		Readiness: &Readiness{Method: "match", Match: "ready", State: ReadinessReady}, StopGraceInherited: true,
	}

	t.Run("running and ready dependents use retained completion", func(t *testing.T) {
		processes := map[string]Process{"migrate": completed, "api": readyAPI}
		starts := 0
		results, err := runExitReadinessUp(t, root, definitions, processes, missing, &starts)
		if err != nil {
			t.Fatal(err)
		}
		if starts != 0 || len(results) != 2 || results[0].Outcome != "already_running" || results[1].Outcome != "completed" {
			t.Fatalf("actual launches=%d, results=%#v", starts, results)
		}
	})

	t.Run("missing dependent reruns one-shot first", func(t *testing.T) {
		processes := map[string]Process{"migrate": completed}
		var launches []string
		results, err := runExitReadinessUpOrdered(t, root, definitions, processes, missing, &launches)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(launches, []string{"migrate", "api"}) || len(results) != 2 || results[1].Outcome != "completed" {
			t.Fatalf("launches=%v, results=%#v", launches, results)
		}
	})
}

func TestExitReadinessDriftedCompletion(t *testing.T) {
	root := t.TempDir()
	missing := errors.New("not found")
	definition := Definition{Name: "migrate", Source: "manifest", Cwd: root, Argv: []string{"new-migrate"}, Ready: &ReadinessConfig{Method: "exit"}}
	at := time.Now()
	current := Process{
		Name: "migrate", Source: "manifest", Root: root, Cwd: root, Argv: []string{"old-migrate"}, State: "exited",
		Exit: &Exit{Code: 0, Time: at}, Readiness: &Readiness{Method: "exit", State: ReadinessReady, Time: at}, StopGraceInherited: true,
	}
	starts := 0
	results, err := OrchestrateUp(context.Background(), UpOptions{Root: root, Definitions: []Definition{definition}}, UpOperations{
		Get:        func(context.Context, string, string) (Process, error) { return current, nil },
		IsNotFound: func(err error) bool { return errors.Is(err, missing) },
		Start: func(ctx context.Context, definition Definition) (StartResult, error) {
			ensured := Ensure(ctx, root, definition, nil, true, EnsureOperations{
				Get: func(context.Context, string, string) (Process, error) { return current, nil },
				Start: func(context.Context, StartRequest) (Process, error) {
					starts++
					return Process{}, errors.New("drifted completion relaunched")
				},
			})
			return StartResult{Result: ensured.Result, Process: ensured.Process, Already: ensured.Already}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if starts != 0 || len(results) != 1 || results[0].Outcome != "definition_drift" || !reflect.DeepEqual(results[0].ChangedFields, []string{"argv"}) || results[0].Process.Readiness.Method != "exit" {
		t.Fatalf("drifted completion = %#v, starts=%d", results, starts)
	}
	if !ProcessSupportsDrift(current) {
		t.Fatal("successful exit-ready record must retain definition identity for drift reporting")
	}
}

func TestExitReadinessDriftAllMethodPairs(t *testing.T) {
	root := t.TempDir()
	methods := []string{"", "match", "exec", "http", "tcp", "exit"}
	makeConfig := func(method string) *ReadinessConfig {
		if method == "" {
			return nil
		}
		if method == "exit" {
			return &ReadinessConfig{Method: method}
		}
		config := &ReadinessConfig{Method: method, Match: "ready", Argv: []string{"probe"}, Target: "http://127.0.0.1:1/"}
		if method == "tcp" {
			config.Target = "127.0.0.1:1"
		}
		return config
	}
	for _, oldMethod := range methods {
		for _, newMethod := range methods {
			t.Run(oldMethod+"-to-"+newMethod, func(t *testing.T) {
				definition := Definition{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"api"}, Ready: makeConfig(newMethod)}
				process := Process{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"api"}, State: "running", StopGraceInherited: true}
				if oldMethod != "" {
					config := makeConfig(oldMethod)
					process.Readiness = &Readiness{Method: oldMethod, Match: config.Match, Argv: config.Argv, Target: config.Target, State: ReadinessStarting}
				}
				got := DefinitionChangedFields(root, definition, process)
				want := []string{}
				if oldMethod != newMethod {
					if oldMethod != "" {
						want = append(want, "readiness_"+oldMethod)
					}
					if newMethod != "" {
						want = append(want, "readiness_"+newMethod)
					}
				}
				sort.Strings(want)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("old=%q new=%q got=%v want=%v", oldMethod, newMethod, got, want)
				}
			})
		}
	}
}

func runExitReadinessUp(t *testing.T, root string, definitions []Definition, processes map[string]Process, missing error, launches *int) ([]Result, error) {
	t.Helper()
	return runExitReadinessUpOrdered(t, root, definitions, processes, missing, nil, launches)
}

func runExitReadinessUpOrdered(t *testing.T, root string, definitions []Definition, processes map[string]Process, missing error, launchOrder *[]string, launchCount ...*int) ([]Result, error) {
	t.Helper()
	var mu sync.Mutex
	ops := UpOperations{
		IsNotFound: func(err error) bool { return errors.Is(err, missing) },
		Get: func(_ context.Context, name, _ string) (Process, error) {
			mu.Lock()
			defer mu.Unlock()
			process, ok := processes[name]
			if !ok {
				return Process{}, missing
			}
			return process, nil
		},
		Start: func(ctx context.Context, definition Definition) (StartResult, error) {
			ensured := Ensure(ctx, root, definition, nil, true, EnsureOperations{
				IsNotFound: func(err error) bool { return errors.Is(err, missing) },
				Get: func(_ context.Context, name, _ string) (Process, error) {
					mu.Lock()
					defer mu.Unlock()
					process, ok := processes[name]
					if !ok {
						return Process{}, missing
					}
					return process, nil
				},
				Start: func(_ context.Context, request StartRequest) (Process, error) {
					mu.Lock()
					defer mu.Unlock()
					if launchOrder != nil {
						*launchOrder = append(*launchOrder, request.Name)
					}
					for _, count := range launchCount {
						(*count)++
					}
					process := Process{Name: request.Name, Source: request.Source, Root: request.Root, Cwd: request.Cwd, Argv: request.Argv, PID: len(processes) + 1, State: "running", LaunchCursor: 1, StopGraceInherited: true}
					if request.Ready != nil {
						process.Readiness = &Readiness{Method: request.Ready.Method, Match: request.Ready.Match, Argv: request.Ready.Argv, Target: request.Ready.Target, State: ReadinessStarting}
					}
					processes[request.Name] = process
					return process, nil
				},
			})
			return StartResult{Result: ensured.Result, Process: ensured.Process, Already: ensured.Already, ObservedAt: ensured.ObservedAt}, nil
		},
		Readiness: func(_ context.Context, definition Definition, process Process, outcome string, _ time.Duration) (Result, error) {
			if readinessMethod(definition.Ready) == "exit" {
				at := time.Now()
				process.State = "exited"
				process.Exit = &Exit{Code: 0, Time: at}
				process.Readiness = &Readiness{Method: "exit", State: ReadinessReady, Time: at}
			} else {
				process.Readiness.State = ReadinessReady
			}
			mu.Lock()
			processes[definition.Name] = process
			mu.Unlock()
			if readinessMethod(definition.Ready) == "exit" {
				outcome = "completed"
			}
			return ResultForProcess(definition, process, outcome), nil
		},
	}
	return OrchestrateUp(context.Background(), UpOptions{Root: root, Definitions: definitions}, ops)
}

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
				current := Process{Name: "api", Source: "manifest", Cwd: root, PID: 41, State: "running", LaunchCursor: 3, Readiness: &Readiness{State: ReadinessStarting, Match: "ready"}, StopGraceInherited: true}
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
		chain := ready("chain")
		chain.After = []string{"dependent"}
		definitions := []Definition{
			{Name: "dependent", Source: "manifest", Cwd: root, Argv: []string{"dependent"}, After: []string{"root-a"}, Ready: &ReadinessConfig{Match: "ready"}},
			chain, ready("root-a"), ready("root-b"),
			{Name: "failure", Source: "manifest", Cwd: root, Argv: []string{"failure"}},
			{Name: "timeout", Source: "manifest", Cwd: root, Argv: []string{"timeout"}, Ready: &ReadinessConfig{Match: "ready"}},
			{Name: "blocked", Source: "manifest", Cwd: root, Argv: []string{"blocked"}, After: []string{"timeout", "failure"}, Ready: &ReadinessConfig{Match: "ready"}},
		}
		if !DefinitionsHaveAfter(definitions) || DefinitionsHaveAfter([]Definition{{Name: "independent"}}) {
			t.Fatal("after dependency detection did not match definitions")
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
				process := Process{Name: definition.Name, Source: definition.Source, Cwd: root, PID: pid, State: "running", LaunchCursor: 1, StopGraceInherited: true}
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
		if indexOf(started, "dependent") < indexOf(started, "root-a") || indexOf(started, "chain") < indexOf(started, "dependent") {
			t.Fatalf("dependent launch order violates its after chain: %v", started)
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
		if byName["chain"].Outcome != "started" || byName["chain"].Process == nil || byName["chain"].Process.Readiness.State != ReadinessReady {
			t.Fatalf("chain=%#v", byName["chain"])
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
		drifted := Process{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"old"}, State: "running", PID: 7, LaunchCursor: 8, Readiness: &Readiness{State: ReadinessStarting, Match: "old"}, StopGraceInherited: true}
		drift := DefinitionDriftResult(root, definition, drifted)
		if drift.Outcome != "definition_drift" || !reflect.DeepEqual(drift.ChangedFields, []string{"argv", "readiness_match"}) || drift.Guidance != "hum restart api" {
			t.Fatalf("drift=%#v", drift)
		}
		next := time.Now().Add(time.Minute)
		pending := Process{Name: "pending", Source: "manifest", State: "exited", Restart: "on-failure", Relaunches: 2, NextLaunchAt: &next, StopGraceInherited: true}
		exhausted := Process{Name: "exhausted", Source: "manifest", State: "exited", Restart: "on-failure", Relaunches: AutomaticRelaunchLimit, StopGraceInherited: true}
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
		current := Process{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"api"}, State: "descendants", PGID: 41, StopGraceInherited: true}
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

func TestOrchestrateUpRetainsRulesForAdapters(t *testing.T) {
	root := t.TempDir()
	missing := errors.New("not found")

	t.Run("matched prerequisite that exited still releases dependent", func(t *testing.T) {
		definitions := []Definition{
			{Name: "api", Source: "manifest", Argv: []string{"api"}, After: []string{"db"}, Ready: &ReadinessConfig{Match: "ready"}},
			{Name: "db", Source: "manifest", Argv: []string{"db"}, Ready: &ReadinessConfig{Match: "ready"}},
		}
		var started []string
		results, err := OrchestrateUp(context.Background(), UpOptions{Root: root, Definitions: definitions}, UpOperations{
			Start: func(_ context.Context, definition Definition) (StartResult, error) {
				started = append(started, definition.Name)
				process := Process{Name: definition.Name, State: "running", Readiness: &Readiness{State: ReadinessStarting, Match: "ready"}}
				return StartResult{Result: ResultForProcess(definition, process, "started"), Process: process}, nil
			},
			Readiness: func(_ context.Context, definition Definition, process Process, outcome string, _ time.Duration) (Result, error) {
				process.Readiness.State = ReadinessReady
				if definition.Name == "db" {
					process.State = "exited"
				}
				return ResultForProcess(definition, process, outcome), nil
			},
		})
		if err != nil || !reflect.DeepEqual(started, []string{"db", "api"}) || len(results) != 2 || results[1].Outcome != "started" || results[1].Process == nil || results[1].Process.State != "exited" {
			t.Fatalf("matched then exited: started=%v results=%#v err=%v", started, results, err)
		}
	})

	t.Run("blocked dependents retain existing state", func(t *testing.T) {
		next := time.Now().Add(time.Minute)
		for _, state := range []string{"running", "stopped", "exited"} {
			t.Run(state, func(t *testing.T) {
				definitions := []Definition{
					{Name: "web", Source: "manifest:hum.yaml", Argv: []string{"web"}, After: []string{"api"}},
					{Name: "api", Source: "manifest:hum.yaml", Argv: []string{"api"}, After: []string{"db"}},
					{Name: "db", Source: "manifest:hum.yaml", Argv: []string{"db"}},
				}
				api := Process{Name: "api", Source: "manifest:hum.yaml", Root: root, Cwd: root, Argv: []string{"api"}, State: state, PID: 41, LaunchCursor: 12, Restart: "on-failure", Relaunches: 2, NextLaunchAt: &next}
				var started []string
				results, err := OrchestrateUp(context.Background(), UpOptions{Root: root, Definitions: definitions}, UpOperations{
					Start: func(_ context.Context, definition Definition) (StartResult, error) {
						started = append(started, definition.Name)
						return StartResult{Result: ErrorResult(definition, errors.New("dependency failed"))}, nil
					},
					Get: func(_ context.Context, name, _ string) (Process, error) {
						if name == "api" {
							return api, nil
						}
						return Process{}, missing
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(started, []string{"db"}) {
					t.Fatalf("started=%v, want only db", started)
				}
				if len(results) != 3 || results[0].Name != "api" || results[0].Outcome != "skipped" || results[0].ExistingState != state || results[0].Process == nil || results[0].Process.PID != 41 || results[0].Process.LaunchCursor != 12 || !reflect.DeepEqual(results[0].BlockedBy, []string{"db"}) {
					t.Fatalf("blocked api=%#v", results)
				}
				if results[2].Name != "web" || results[2].Outcome != "skipped" || results[2].ExistingState != "" || results[2].Process != nil || !reflect.DeepEqual(results[2].BlockedBy, []string{"api"}) {
					t.Fatalf("blocked web=%#v", results[2])
				}
			})
		}
	})

	t.Run("drifted prerequisite gates dependents", func(t *testing.T) {
		next := time.Now().Add(time.Minute)
		for _, test := range []struct {
			name    string
			process Process
		}{
			{name: "running", process: Process{State: "running", PID: 9}},
			{name: "recovery pending", process: Process{State: "exited", Restart: "on-failure", Relaunches: 2, NextLaunchAt: &next}},
			{name: "recovery exhausted", process: Process{State: "exited", Restart: "on-failure", Relaunches: AutomaticRelaunchLimit}},
		} {
			t.Run(test.name, func(t *testing.T) {
				current := test.process
				current.Name, current.Source, current.Root, current.Cwd = "db", "manifest:hum.yaml", root, root
				current.Argv = []string{"db"}
				current.Restart = "on-failure"
				current.StopGraceInherited = true
				current.Readiness = &Readiness{State: ReadinessStarting, Match: "old"}
				definitions := []Definition{
					{Name: "api", Source: "manifest:hum.yaml", Argv: []string{"api"}, After: []string{"db"}},
					{Name: "db", Source: "manifest:hum.yaml", Cwd: root, Argv: []string{"db"}, Ready: &ReadinessConfig{Match: "new"}, Restart: "on-failure"},
				}
				var launches, waits int
				results, err := OrchestrateUp(context.Background(), UpOptions{Root: root, Definitions: definitions}, UpOperations{
					Start: func(ctx context.Context, definition Definition) (StartResult, error) {
						if definition.Name == "api" {
							launches++
							return StartResult{}, errors.New("blocked dependent launched")
						}
						ensured := Ensure(ctx, root, definition, nil, true, EnsureOperations{
							Get: func(context.Context, string, string) (Process, error) { return current, nil },
							Start: func(context.Context, StartRequest) (Process, error) {
								launches++
								return Process{}, errors.New("drifted process launched")
							},
						})
						return StartResult{Result: ensured.Result, Process: ensured.Process, Already: ensured.Already}, nil
					},
					Get: func(_ context.Context, name, _ string) (Process, error) {
						if name == "db" {
							return current, nil
						}
						return Process{}, missing
					},
					Readiness: func(context.Context, Definition, Process, string, time.Duration) (Result, error) {
						waits++
						return Result{}, nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				if len(results) != 2 || results[0].Outcome != "skipped" || !reflect.DeepEqual(results[0].BlockedBy, []string{"db"}) || results[1].Outcome != "definition_drift" || !reflect.DeepEqual(results[1].ChangedFields, []string{"readiness_match"}) || results[1].Guidance != "hum restart db" || results[1].Process == nil || results[1].Process.Readiness.Match != "old" {
					t.Fatalf("drifted gate results=%#v", results)
				}
				if launches != 0 || waits != 0 {
					t.Fatalf("drifted launches=%d waits=%d, want neither", launches, waits)
				}
			})
		}
	})

	t.Run("crash recovery is classified without launch or wait", func(t *testing.T) {
		next := time.Now().Add(time.Minute)
		definitions := []Definition{
			{Name: "pending", Source: "manifest:hum.yaml", Argv: []string{"pending"}, Ready: &ReadinessConfig{Match: "ready"}, Restart: "on-failure"},
			{Name: "exhausted", Source: "manifest:hum.yaml", Argv: []string{"exhausted"}, Ready: &ReadinessConfig{Match: "ready"}, Restart: "on-failure"},
		}
		processes := map[string]Process{
			"pending":   {Name: "pending", Source: "manifest:hum.yaml", Root: root, State: "exited", Argv: []string{"pending"}, Readiness: &Readiness{State: ReadinessStarting, Match: "ready"}, Restart: "on-failure", Relaunches: 2, NextLaunchAt: &next, StopGraceInherited: true},
			"exhausted": {Name: "exhausted", Source: "manifest:hum.yaml", Root: root, State: "exited", Argv: []string{"exhausted"}, Readiness: &Readiness{State: ReadinessStarting, Match: "ready"}, Restart: "on-failure", Relaunches: AutomaticRelaunchLimit, StopGraceInherited: true},
		}
		var launches, waits int
		results, err := OrchestrateUp(context.Background(), UpOptions{Root: root, Definitions: definitions}, UpOperations{
			Start: func(ctx context.Context, definition Definition) (StartResult, error) {
				ensured := Ensure(ctx, root, definition, nil, true, EnsureOperations{
					Get: func(_ context.Context, name, _ string) (Process, error) { return processes[name], nil },
					Start: func(context.Context, StartRequest) (Process, error) {
						launches++
						return Process{}, errors.New("recovery process relaunched")
					},
				})
				return StartResult{Result: ensured.Result, Process: ensured.Process, Already: ensured.Already}, nil
			},
			Readiness: func(context.Context, Definition, Process, string, time.Duration) (Result, error) {
				waits++
				return Result{}, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		byName := make(map[string]Result, len(results))
		for _, result := range results {
			byName[result.Name] = result
		}
		if byName["pending"].Outcome != "recovery_pending" || byName["pending"].Process == nil || byName["pending"].Process.NextLaunchAt == nil || byName["exhausted"].Outcome != "recovery_exhausted" || byName["exhausted"].Process == nil || byName["exhausted"].Process.Relaunches != AutomaticRelaunchLimit {
			t.Fatalf("recovery results=%#v", results)
		}
		if launches != 0 || waits != 0 {
			t.Fatalf("recovery launches=%d waits=%d, want neither", launches, waits)
		}
	})

	t.Run("removed definitions are appended and sorted", func(t *testing.T) {
		next := time.Now().Add(time.Minute)
		otherRoot := t.TempDir()
		definitions := []Definition{{Name: "current", Source: "manifest:hum.yaml", Cwd: root, Argv: []string{"current"}}}
		processes := []Process{
			{Name: "current", Source: "manifest:hum.yaml", Root: root, State: "running", Argv: []string{"current"}},
			{Name: "pending", Source: "manifest:hum.yaml", Root: root, State: "exited", Restart: "on-failure", Relaunches: 2, NextLaunchAt: &next},
			{Name: "exhausted", Source: "manifest:hum.yaml", Root: root, State: "exited", Restart: "on-failure", Relaunches: AutomaticRelaunchLimit},
			{Name: "running", Source: "manifest:hum.yaml", Root: root, State: "running", PID: 12},
			{Name: "stopped", Source: "manifest:hum.yaml", Root: root, State: "exited"},
			{Name: "ad-hoc", Source: "ad_hoc", Root: root, State: "running", PID: 13},
			{Name: "discovered", Source: "package_json", Root: root, State: "running", PID: 14},
			{Name: "other-root", Source: "manifest:hum.yaml", Root: otherRoot, State: "running", PID: 15},
		}
		results, err := OrchestrateUp(context.Background(), UpOptions{Root: root, Definitions: definitions}, UpOperations{
			Start: func(_ context.Context, definition Definition) (StartResult, error) {
				process := processes[0]
				return StartResult{Result: ResultForProcess(definition, process, "already_running"), Process: process, Already: true}, nil
			},
			List: func(context.Context) ([]Process, error) { return processes, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 4 || !reflect.DeepEqual([]string{results[0].Name, results[1].Name, results[2].Name, results[3].Name}, []string{"current", "exhausted", "pending", "running"}) {
			t.Fatalf("up results=%#v", results)
		}
		for _, result := range results[1:] {
			if result.Outcome != "removed_definition" || result.Process == nil || !strings.Contains(result.Guidance, "hum stop "+result.Name) || !strings.Contains(result.Guidance, "hum remove "+result.Name) {
				t.Fatalf("removed result=%#v", result)
			}
		}
	})
}

func TestSelectWithPrerequisites(t *testing.T) {
	nameOf := func(definitions []Definition) []string {
		names := make([]string, len(definitions))
		for index, definition := range definitions {
			names[index] = definition.Name
		}
		return names
	}

	t.Run("chain preserves declaration order", func(t *testing.T) {
		definitions := []Definition{
			{Name: "db"},
			{Name: "api", After: []string{"db"}},
			{Name: "web", After: []string{"api"}},
			{Name: "worker"},
		}
		selected, err := SelectWithPrerequisites(definitions, []string{"web"})
		if err != nil || !reflect.DeepEqual(nameOf(selected), []string{"db", "api", "web"}) {
			t.Fatalf("selected=%v err=%v, want [db api web]", nameOf(selected), err)
		}
	})

	t.Run("diamond deduplicates shared prerequisite", func(t *testing.T) {
		definitions := []Definition{
			{Name: "base"},
			{Name: "left", After: []string{"base"}},
			{Name: "right", After: []string{"base"}},
			{Name: "top", After: []string{"left", "right"}},
			{Name: "unused"},
		}
		selected, err := SelectWithPrerequisites(definitions, []string{"top"})
		if err != nil || !reflect.DeepEqual(nameOf(selected), []string{"base", "left", "right", "top"}) {
			t.Fatalf("selected=%v err=%v, want [base left right top]", nameOf(selected), err)
		}
	})

	t.Run("multiple names share prerequisites", func(t *testing.T) {
		definitions := []Definition{
			{Name: "base"},
			{Name: "one", After: []string{"base"}},
			{Name: "two", After: []string{"base"}},
			{Name: "unused"},
		}
		selected, err := SelectWithPrerequisites(definitions, []string{"two", "one"})
		if err != nil || !reflect.DeepEqual(nameOf(selected), []string{"base", "one", "two"}) {
			t.Fatalf("selected=%v err=%v, want [base one two]", nameOf(selected), err)
		}
	})

	t.Run("definition without after selects itself", func(t *testing.T) {
		definitions := []Definition{{Name: "db"}, {Name: "api", After: []string{"db"}}}
		selected, err := SelectWithPrerequisites(definitions, []string{"db"})
		if err != nil || !reflect.DeepEqual(nameOf(selected), []string{"db"}) {
			t.Fatalf("selected=%v err=%v, want [db]", nameOf(selected), err)
		}
	})

	t.Run("all unknown names are reported together", func(t *testing.T) {
		_, err := SelectWithPrerequisites([]Definition{{Name: "db"}}, []string{"missing-z", "missing-a", "missing-z"})
		if err == nil || !strings.Contains(err.Error(), "missing-a") || !strings.Contains(err.Error(), "missing-z") {
			t.Fatalf("unknown-name error=%v, want both unknown names", err)
		}
	})

	t.Run("scheduler rejects an omitted prerequisite", func(t *testing.T) {
		_, err := OrchestrateUp(context.Background(), UpOptions{
			Definitions: []Definition{{Name: "db"}, {Name: "api", After: []string{"db"}}},
			Names:       []string{"api"},
		}, UpOperations{})
		if err == nil || !strings.Contains(err.Error(), `"api"`) || !strings.Contains(err.Error(), `"db"`) {
			t.Fatalf("missing-prerequisite error=%v, want api and db", err)
		}
	})
}

func TestEnsureStartsFromMissingProcess(t *testing.T) {
	root := t.TempDir()
	grace := time.Second
	definition := Definition{
		Name: "api", Source: "manifest:hum.yaml", Cwd: "service", Argv: []string{"server"},
		Ready:   &ReadinessConfig{Method: "exec", Argv: []string{"probe", "api"}, Interval: 25 * time.Millisecond},
		Restart: "on-failure", StopGrace: &grace, TTY: true,
	}
	missing := errors.New("not found")
	var request StartRequest
	result := Ensure(context.Background(), root, definition, []string{"TOKEN=secret"}, false, EnsureOperations{
		Get:        func(context.Context, string, string) (Process, error) { return Process{}, missing },
		IsNotFound: func(err error) bool { return errors.Is(err, missing) },
		Start: func(_ context.Context, got StartRequest) (Process, error) {
			request = got
			return Process{Name: got.Name, Source: got.Source, Root: got.Root, Cwd: got.Cwd, Argv: got.Argv, State: "running", PID: 42, Readiness: &Readiness{Method: "exec", Argv: got.Ready.Argv, Interval: got.Ready.Interval, State: ReadinessStarting}}, nil
		},
	})
	if result.Result.Outcome != "started" || result.Process.PID != 42 || result.Already {
		t.Fatalf("ensure result=%#v", result)
	}
	if request.Name != "api" || request.Root != root || request.Cwd != "service" || !reflect.DeepEqual(request.Argv, definition.Argv) || !reflect.DeepEqual(request.Env, []string{"TOKEN=secret"}) || request.Ready == nil || request.Ready.Method != "exec" || !reflect.DeepEqual(request.Ready.Argv, []string{"probe", "api"}) || request.Restart != "on-failure" || request.StopGrace == nil || *request.StopGrace != grace || !request.TTY {
		t.Fatalf("start request=%#v", request)
	}
	definition.Argv[0] = "changed"
	definition.Ready.Argv[0] = "changed"
	grace = 2 * time.Second
	if request.Argv[0] != "server" || request.Ready.Argv[0] != "probe" || *request.StopGrace != time.Second {
		t.Fatalf("start request aliased definition: request=%#v definition=%#v", request, definition)
	}
}

func TestEnsureClassifiesExistingAndErrors(t *testing.T) {
	root := t.TempDir()
	definition := Definition{Name: "api", Source: "manifest:hum.yaml", Cwd: root, Argv: []string{"api"}, StopGrace: nil}
	running := Process{Name: "api", Source: "manifest:hum.yaml", Root: root, Cwd: root, Argv: []string{"api"}, State: "running", PID: 41, StopGraceInherited: true}

	t.Run("matching active process is already running", func(t *testing.T) {
		starts := 0
		result := Ensure(context.Background(), root, definition, nil, false, EnsureOperations{
			Get: func(context.Context, string, string) (Process, error) { return running, nil },
			Start: func(context.Context, StartRequest) (Process, error) {
				starts++
				return Process{}, errors.New("unexpected start")
			},
		})
		if starts != 0 || !result.Already || result.Result.Outcome != "already_running" || result.Process.PID != 41 {
			t.Fatalf("ensure result=%#v starts=%d", result, starts)
		}
	})

	t.Run("active name conflict and tty mismatch", func(t *testing.T) {
		adHoc := running
		adHoc.Source = "ad_hoc"
		nameConflict := Ensure(context.Background(), root, definition, nil, false, EnsureOperations{
			Get: func(context.Context, string, string) (Process, error) { return adHoc, nil },
		})
		if ErrorKindOf(nameConflict.Result.Error) != ErrorNameInUse {
			t.Fatalf("name conflict=%#v", nameConflict)
		}
		ttyDefinition := definition
		ttyDefinition.TTY = true
		ttyMismatch := Ensure(context.Background(), root, ttyDefinition, nil, false, EnsureOperations{
			Get: func(context.Context, string, string) (Process, error) { return running, nil },
		})
		if ttyMismatch.Result.Outcome != "definition_drift" || !reflect.DeepEqual(ttyMismatch.Result.ChangedFields, []string{"tty"}) {
			t.Fatalf("tty mismatch=%#v", ttyMismatch)
		}
	})

	t.Run("get and start failures are retained", func(t *testing.T) {
		getFailure := errors.New("get failed")
		got := Ensure(context.Background(), root, definition, nil, false, EnsureOperations{
			Get:        func(context.Context, string, string) (Process, error) { return Process{}, getFailure },
			IsNotFound: func(error) bool { return false },
		})
		if !errors.Is(got.Result.Error, getFailure) {
			t.Fatalf("get failure=%#v", got)
		}
		startFailure := errors.New("start failed")
		got = Ensure(context.Background(), root, definition, nil, false, EnsureOperations{
			Get:        func(context.Context, string, string) (Process, error) { return Process{}, errors.New("missing") },
			IsNotFound: func(error) bool { return true },
			Start:      func(context.Context, StartRequest) (Process, error) { return Process{}, startFailure },
		})
		if !errors.Is(got.Result.Error, startFailure) {
			t.Fatalf("start failure=%#v", got)
		}
	})

	t.Run("name-in-use race converges on the active process", func(t *testing.T) {
		calls := 0
		result := Ensure(context.Background(), root, definition, nil, false, EnsureOperations{
			Get: func(context.Context, string, string) (Process, error) {
				calls++
				if calls == 1 {
					return Process{}, errors.New("missing")
				}
				return running, nil
			},
			IsNotFound:  func(error) bool { return true },
			IsNameInUse: func(err error) bool { return ErrorKindOf(err) == ErrorNameInUse },
			Start: func(context.Context, StartRequest) (Process, error) {
				return Process{}, &Error{Kind: ErrorNameInUse, Name: "api"}
			},
		})
		if calls != 2 || !result.Already || result.Result.Outcome != "already_running" || result.Process.PID != 41 {
			t.Fatalf("racing ensure=%#v gets=%d", result, calls)
		}
	})
}

func TestEnsureReportsDriftedDefinitionFields(t *testing.T) {
	root := t.TempDir()
	definition := Definition{Name: "web", Source: "manifest:hum.yaml", Cwd: root, Argv: []string{"server"}}
	process := Process{Name: "web", Source: "manifest:hum.yaml", Root: root, Cwd: root, Argv: []string{"server"}, State: "running", StopGraceInherited: true}
	for _, test := range []struct {
		name  string
		field string
		edit  func(*Definition)
	}{
		{name: "argv", field: "argv", edit: func(definition *Definition) { definition.Argv = []string{"changed"} }},
		{name: "cwd", field: "cwd", edit: func(definition *Definition) { definition.Cwd = filepath.Join(root, "sub") }},
		{name: "readiness", field: "readiness_match", edit: func(definition *Definition) { definition.Ready = &ReadinessConfig{Match: "ready"} }},
		{name: "tty", field: "tty", edit: func(definition *Definition) { definition.TTY = true }},
		{name: "restart", field: "restart", edit: func(definition *Definition) { definition.Restart = "on-failure" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := definition
			test.edit(&changed)
			starts := 0
			result := Ensure(context.Background(), root, changed, nil, false, EnsureOperations{
				Get: func(context.Context, string, string) (Process, error) { return process, nil },
				Start: func(context.Context, StartRequest) (Process, error) {
					starts++
					return Process{}, errors.New("drifted definition relaunched")
				},
			})
			if starts != 0 || result.Result.Outcome != "definition_drift" || !reflect.DeepEqual(result.Result.ChangedFields, []string{test.field}) || !result.Already {
				t.Fatalf("drift result=%#v starts=%d, want [%s] without launch", result, starts, test.field)
			}
		})
	}
}

func TestReadinessTimeoutAndLaunchOutcome(t *testing.T) {
	definition := Definition{Name: "api", Ready: &ReadinessConfig{Match: "ready", Timeout: 3 * time.Second}}
	for _, test := range []struct {
		name       string
		override   time.Duration
		definition Definition
		want       time.Duration
		wantErr    bool
	}{
		{name: "default", definition: Definition{Name: "api"}, want: DefaultReadinessTimeout},
		{name: "definition", definition: definition, want: 3 * time.Second},
		{name: "override", definition: definition, override: 2 * time.Second, want: 2 * time.Second},
		{name: "invalid", definition: definition, override: -time.Second, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ReadinessTimeout(test.override, test.definition)
			if test.wantErr {
				if err == nil {
					t.Fatal("invalid timeout accepted")
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("ReadinessTimeout=%s,%v want %s", got, err, test.want)
			}
		})
	}
	for _, test := range []struct {
		name       string
		already    bool
		definition Definition
		want       string
	}{
		{name: "already", already: true, definition: definition, want: "already_running"},
		{name: "no readiness", definition: Definition{Name: "plain"}, want: ReadinessRunningUnverified},
		{name: "readiness", definition: definition, want: "started"},
	} {
		t.Run("outcome/"+test.name, func(t *testing.T) {
			if got := LaunchOutcome(test.already, test.definition); got != test.want {
				t.Fatalf("LaunchOutcome=%q, want %q", got, test.want)
			}
		})
	}
}

func indexOf(values []string, value string) int {
	for index, candidate := range values {
		if candidate == value {
			return index
		}
	}
	return len(values)
}
