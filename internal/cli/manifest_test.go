package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/orchestrate"
	"hum/internal/output"
	"hum/internal/project"
	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
)

func writeManifestCLITestFile(t *testing.T, root, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatalf("write hum.yaml: %v", err)
	}
}

func manifestCLILaunchResults(t *testing.T, output string) []manifestLaunchResult {
	t.Helper()
	var results []manifestLaunchResult
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var result manifestLaunchResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatalf("decode launch result %q: %v", line, err)
		}
		results = append(results, result)
	}
	return results
}

func manifestCLIExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	return -1
}

func manifestCLIRecoveryStubDaemon(t *testing.T, processes map[string]protocol.Process) (string, <-chan protocol.Operation, <-chan struct{}) {
	t.Helper()
	runtimeDir, err := os.MkdirTemp("/tmp", "h-")
	if err != nil {
		t.Fatalf("create recovery runtime directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	listener, err := net.Listen("unix", daemon.NewRuntimePaths(runtimeDir).Socket)
	if err != nil {
		t.Fatalf("listen for recovery stub daemon: %v", err)
	}
	operations := make(chan protocol.Operation, 16)
	done := make(chan struct{})
	var connMu sync.Mutex
	var conn net.Conn
	t.Cleanup(func() {
		_ = listener.Close()
		connMu.Lock()
		active := conn
		connMu.Unlock()
		if active != nil {
			_ = active.Close()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("recovery stub daemon did not finish")
		}
	})
	go func() {
		defer close(done)
		active, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		connMu.Lock()
		conn = active
		connMu.Unlock()
		defer active.Close()

		decoder := protocol.NewDecoder(active)
		encoder := protocol.NewEncoder(active)
		request, decodeErr := decoder.DecodeRequest()
		if decodeErr != nil || request.Op != protocol.OpHello {
			return
		}
		if encodeErr := encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}); encodeErr != nil {
			return
		}
		for {
			request, decodeErr = decoder.DecodeRequest()
			if decodeErr != nil {
				return
			}
			operations <- request.Op
			switch request.Op {
			case protocol.OpGet:
				if request.Get == nil {
					return
				}
				process, ok := processes[request.Get.Name]
				if !ok {
					_ = encoder.EncodeResponse(protocol.NewErrorResponse(protocol.OpGet, protocol.NewWireError(protocol.ErrorNotFound, "not found", nil)))
					continue
				}
				if encodeErr := encoder.EncodeResponse(protocol.NewGetResponse(process)); encodeErr != nil {
					return
				}
			case protocol.OpWait:
				if encodeErr := encoder.EncodeResponse(protocol.NewWaitResponse(protocol.WaitExited, 0, nil)); encodeErr != nil {
					return
				}
			case protocol.OpList:
				listed := make([]protocol.Process, 0, len(processes))
				for _, process := range processes {
					listed = append(listed, process)
				}
				if encodeErr := encoder.EncodeResponse(protocol.NewListResponse(listed)); encodeErr != nil {
					return
				}
			case protocol.OpStart:
				if encodeErr := encoder.EncodeResponse(protocol.NewErrorResponse(protocol.OpStart, protocol.NewWireError(protocol.ErrorInternal, "unexpected start request", nil))); encodeErr != nil {
					return
				}
			default:
				if encodeErr := encoder.EncodeResponse(protocol.NewErrorResponse(request.Op, protocol.NewWireError(protocol.ErrorInternal, "unexpected request", nil))); encodeErr != nil {
					return
				}
			}
		}
	}()
	return runtimeDir, operations, done
}

func TestUpAdapterParity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	definition := project.Definition{
		Name: "api", Source: "manifest", Cwd: ".", Argv: []string{"new"},
		Ready: &project.ReadyDefinition{Match: "new"}, After: []string{"db"},
	}
	process := app.Process{
		Name: "api", Source: "manifest", Root: root, Cwd: root, Argv: []string{"old"},
		State: app.StateRunning, PID: 41, LaunchCursor: 9, StopGraceInherited: true,
		Readiness: &app.Readiness{State: app.ReadinessStarting, Match: "old"},
	}
	sharedDrift := orchestrate.DefinitionDriftResult(root, cliOrchestrateDefinition(definition), cliOrchestrateProcess(process))
	drift := cliManifestLaunchResult(definition, sharedDrift)
	if drift.Name != "api" || drift.Outcome != "definition_drift" || !reflect.DeepEqual(drift.ChangedFields, []string{"argv", "readiness_match"}) || drift.Guidance != "hum restart api" || drift.PID == nil || *drift.PID != 41 {
		t.Fatalf("CLI adapter drift=%#v", drift)
	}
	sharedSkipped := orchestrate.Result{Name: "web", Outcome: "skipped", BlockedBy: []string{"api", "db"}, Guidance: ""}
	skipped := cliManifestLaunchResult(project.Definition{Name: "web", Source: "manifest", Argv: []string{"web"}}, sharedSkipped)
	if skipped.Name != "web" || skipped.Outcome != "skipped" || !reflect.DeepEqual(skipped.BlockedBy, []string{"api", "db"}) || skipped.Guidance != "" {
		t.Fatalf("CLI adapter skipped=%#v", skipped)
	}

	nextLaunch := time.Now().Add(time.Minute)
	exitCode := 23
	retained := manifestLaunchResult{
		Name: "web", Outcome: "skipped", Source: "manifest", Argv: []string{"web"}, State: string(app.StateExited),
		Restart: string(app.RestartOnFailure), Relaunches: 2, NextLaunchAt: &nextLaunch, ExitCode: &exitCode,
	}
	retainedRoundTrip := cliManifestLaunchResult(project.Definition{Name: "web"}, cliSharedLaunchResult(project.Definition{Name: "web"}, retained, nil))
	if retainedRoundTrip.Restart != retained.Restart || retainedRoundTrip.Relaunches != 2 || retainedRoundTrip.NextLaunchAt == nil || retainedRoundTrip.ExitCode == nil || *retainedRoundTrip.ExitCode != exitCode {
		t.Fatalf("CLI adapter retained skipped snapshot=%#v", retainedRoundTrip)
	}

	readiness := manifestLaunchResult{
		Name: "web", Outcome: "started", Source: "manifest", Argv: []string{"web"}, State: string(app.StateRunning), PID: func() *int { value := 41; return &value }(),
		Readiness: app.ReadinessStarting, ReadinessConfigured: true, ReadinessMethod: "exec",
		ReadinessArgv: []string{"probe", "--service", "web"}, ReadinessInterval: 125 * time.Millisecond,
		ReadinessDiagnostic: "status 1", ReadyCursor: func() *uint64 { value := uint64(17); return &value }(),
	}
	readinessRoundTrip := cliManifestLaunchResult(project.Definition{Name: "web"}, cliSharedLaunchResult(project.Definition{Name: "web"}, readiness, nil))
	if readinessRoundTrip.ReadinessMethod != readiness.ReadinessMethod || !reflect.DeepEqual(readinessRoundTrip.ReadinessArgv, readiness.ReadinessArgv) || readinessRoundTrip.ReadinessInterval != readiness.ReadinessInterval || readinessRoundTrip.ReadinessDiagnostic != readiness.ReadinessDiagnostic || readinessRoundTrip.ReadyCursor == nil || *readinessRoundTrip.ReadyCursor != 17 {
		t.Fatalf("CLI adapter readiness snapshot=%#v", readinessRoundTrip)
	}

	stale := app.Process{Name: "api", Source: "manifest", Argv: []string{"api"}, State: app.StateRunning, PID: 41, Restart: app.RestartNever}
	freshExit := manifestLaunchResult{Name: "api", Outcome: "exited_before_ready", Source: "manifest", Argv: []string{"api"}, State: string(app.StateExited), Restart: string(app.RestartNever), ExitCode: &exitCode}
	freshRoundTrip := cliManifestLaunchResult(definition, cliSharedLaunchResult(definition, freshExit, &stale))
	if freshRoundTrip.State != string(app.StateExited) || freshRoundTrip.PID != nil || freshRoundTrip.ExitCode == nil || *freshRoundTrip.ExitCode != exitCode {
		t.Fatalf("CLI adapter fresh readiness snapshot=%#v", freshRoundTrip)
	}
}

func TestExecutableReadiness(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	argv := []string{"probe", "--service", "api"}
	definition := project.Definition{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"server"}, Ready: &project.ReadyDefinition{Exec: argv, Interval: 125 * time.Millisecond, Timeout: 2 * time.Second}}
	sharedDefinition := cliOrchestrateDefinition(definition)
	if sharedDefinition.Ready == nil || sharedDefinition.Ready.Method != "exec" || !reflect.DeepEqual(sharedDefinition.Ready.Argv, argv) || sharedDefinition.Ready.Interval != definition.Ready.Interval {
		t.Fatalf("CLI readiness definition=%#v, want executable argv and interval", sharedDefinition.Ready)
	}

	startRequest := orchestrate.StartRequest{}
	started := orchestrate.Ensure(context.Background(), root, sharedDefinition, []string{"PROBE=1"}, false, orchestrate.EnsureOperations{
		Get: func(context.Context, string, string) (orchestrate.Process, error) {
			return orchestrate.Process{}, protocol.NewWireError(protocol.ErrorNotFound, "not found", nil)
		},
		IsNotFound: func(error) bool { return true },
		Start: func(_ context.Context, request orchestrate.StartRequest) (orchestrate.Process, error) {
			startRequest = request
			return orchestrate.Process{Name: "api", Source: "manifest", Root: root, Cwd: root, Argv: []string{"server"}, State: "running", PID: 42, LaunchCursor: 7, Readiness: &orchestrate.Readiness{Method: "exec", Argv: argv, Interval: definition.Ready.Interval, State: orchestrate.ReadinessStarting}}, nil
		},
	})
	if started.Result.Outcome != "started" || startRequest.Ready == nil || startRequest.Ready.Method != "exec" || !reflect.DeepEqual(startRequest.Ready.Argv, argv) || startRequest.Ready.Interval != definition.Ready.Interval {
		t.Fatalf("CLI start result=%#v request=%#v, want executable readiness propagated", started.Result, startRequest)
	}

	current := cliOrchestrateProcess(app.Process{Name: "api", Source: "manifest", Root: root, Cwd: root, Argv: []string{"server"}, State: app.StateRunning, PID: 42, LaunchCursor: 7, StopGraceInherited: true, Readiness: &app.Readiness{Method: "exec", Argv: argv, Interval: definition.Ready.Interval, State: app.ReadinessStarting, Diagnostic: "status 1"}})
	waits := 0
	result, err := orchestrate.WaitForReadiness(context.Background(), root, sharedDefinition, current, "started", time.Second, orchestrate.ReadinessOperations{
		Get: func(context.Context, string, string) (orchestrate.Process, error) {
			current.Readiness.State = orchestrate.ReadinessReady
			return current, nil
		},
		Wait: func(context.Context, orchestrate.WaitRequest) (orchestrate.WaitResult, error) {
			waits++
			return orchestrate.WaitResult{}, nil
		},
	})
	if err != nil || waits != 0 || result.Outcome != "started" || result.Process == nil || result.Process.Readiness.State != orchestrate.ReadinessReady {
		t.Fatalf("CLI readiness result=%#v err=%v waits=%d, want ready without output wait", result, err, waits)
	}

	for path, outcome := range map[string]string{"start": "started", "up": "already_running", "restart": "restarted"} {
		launch := manifestLaunchResultFor(definition, cliAppProcess(*result.Process), outcome)
		encoded, err := json.Marshal(launch)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"readiness_method":"exec"`, `"readiness_argv":["probe","--service","api"]`, `"readiness_interval":125000000`, `"readiness_diagnostic":"status 1"`} {
			if !strings.Contains(string(encoded), want) {
				t.Fatalf("CLI %s result=%s, missing %s", path, encoded, want)
			}
		}
	}
	snapshot := cliAppProcess(current)
	for name, value := range map[string]string{
		"status": func() string { data, _ := json.Marshal(statusJSONFor(snapshot)); return string(data) }(),
		"list":   func() string { data, _ := json.Marshal(processJSON(snapshot)); return string(data) }(),
		"restart": func() string {
			data, _ := json.Marshal(restartOutputFromProcess(snapshot, definition, "restarted", ""))
			return string(data)
		}(),
	} {
		for _, want := range []string{`"readiness_method":"exec"`, `"readiness_argv":["probe","--service","api"]`, `"readiness_interval":125000000`, `"readiness_diagnostic":"status 1"`} {
			if !strings.Contains(value, want) {
				t.Fatalf("CLI %s result=%s, missing %s", name, value, want)
			}
		}
	}

	driftDefinition := sharedDefinition
	driftDefinition.Ready = &orchestrate.ReadinessConfig{Method: "exec", Argv: []string{"probe", "--service", "different"}, Interval: definition.Ready.Interval, Timeout: definition.Ready.Timeout}
	execDrift := orchestrate.DefinitionDriftResult(root, driftDefinition, current)
	if execDrift.Outcome != "definition_drift" || !reflect.DeepEqual(execDrift.ChangedFields, []string{"readiness_exec"}) {
		t.Fatalf("CLI executable readiness drift=%#v, want readiness_exec", execDrift)
	}
	matchDefinition := project.Definition{Name: "api", Source: "manifest", Cwd: root, Argv: []string{"server"}, Ready: &project.ReadyDefinition{Match: "ready"}}
	matchProcess := app.Process{Name: "api", Source: "manifest", Root: root, Cwd: root, Argv: []string{"server"}, State: app.StateRunning, StopGraceInherited: true, Readiness: &app.Readiness{Method: "match", Match: "ready", State: app.ReadinessStarting}}
	matchDrift := orchestrate.DefinitionDriftResult(root, cliOrchestrateDefinition(matchDefinition), cliOrchestrateProcess(matchProcess))
	if len(matchDrift.ChangedFields) != 0 {
		t.Fatalf("unchanged match readiness drift=%#v, want no changed fields", matchDrift)
	}
	matchToExec := orchestrate.DefinitionDriftResult(root, sharedDefinition, cliOrchestrateProcess(matchProcess))
	if !reflect.DeepEqual(matchToExec.ChangedFields, []string{"readiness_exec"}) {
		t.Fatalf("CLI match-to-exec readiness drift=%#v, want readiness_exec", matchToExec)
	}
	execToMatch := orchestrate.DefinitionDriftResult(root, cliOrchestrateDefinition(matchDefinition), current)
	if !reflect.DeepEqual(execToMatch.ChangedFields, []string{"readiness_exec"}) {
		t.Fatalf("CLI exec-to-match readiness drift=%#v, want readiness_exec", execToMatch)
	}
}

func TestUpEmptyManifest(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := t.TempDir()
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, "version: 1\nprocesses: {}\n")

	stdout, stderr, err := stopShutdownRun(t, "up")
	if err != nil {
		t.Fatalf("empty manifest up: %v", err)
	}
	if stdout != "No processes are declared in hum.yaml.\n" || stderr != "" {
		t.Fatalf("empty manifest output = stdout %q stderr %q", stdout, stderr)
	}
	assertDownRuntimeAbsent(t, runtimeDir)
}

func TestUpEmptyManifestJSON(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := t.TempDir()
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, "version: 1\nprocesses: {}\n")

	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err != nil {
		t.Fatalf("empty manifest JSON up: %v", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("empty manifest JSON output = stdout %q stderr %q, want both empty", stdout, stderr)
	}
	assertDownRuntimeAbsent(t, runtimeDir)
}

func TestUpNonEmptyManifestStartsDaemon(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := cliServeRunRuntimeDir(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  worker:
    argv: [/bin/sh, -c, "sleep 30"]
`)
	t.Cleanup(func() {
		_, _, _ = stopShutdownRun(t, "shutdown", "--stop-processes")
	})

	stdout, stderr, err := stopShutdownRun(t, "up", "--json", "--no-wait")
	if err != nil {
		t.Fatalf("non-empty manifest up: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 1 || results[0].Name != "worker" || results[0].Outcome != "running_unverified" {
		t.Fatalf("non-empty manifest results = %+v", results)
	}
	paths := daemon.NewRuntimePaths(runtimeDir)
	if _, err := os.Stat(paths.Socket); err != nil {
		t.Fatalf("daemon socket: %v", err)
	}
	client, err := daemon.Dial(context.Background(), paths.Socket)
	if err != nil {
		t.Fatalf("dial started daemon: %v", err)
	}
	defer client.Close()
	active, err := client.List(context.Background(), daemon.ListRequest{Cwd: root})
	if err != nil {
		t.Fatalf("list started process: %v", err)
	}
	if len(active) != 1 || active[0].Name != "worker" {
		t.Fatalf("active processes = %+v", active)
	}
}

func TestUpEmptyManifestValidatesInput(t *testing.T) {
	root := stopShutdownTestProject(t)
	writeManifestCLITestFile(t, root, "version: 1\nprocesses: {}\n")

	tests := []struct {
		name    string
		ctx     context.Context
		args    []string
		wantErr string
	}{
		{name: "arguments", ctx: context.Background(), args: []string{"up", "worker"}, wantErr: "up accepts no positional arguments"},
		{name: "malformed timeout", ctx: context.Background(), args: []string{"up", "--timeout", "invalid"}, wantErr: "timeout must be a valid duration"},
		{name: "non-positive timeout", ctx: context.Background(), args: []string{"up", "--timeout", "0s"}, wantErr: "timeout must be positive"},
		{name: "sub-millisecond timeout", ctx: context.Background(), args: []string{"up", "--timeout", "1us"}, wantErr: "timeout must be at least 1ms"},
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests = append(tests, struct {
		name    string
		ctx     context.Context
		args    []string
		wantErr string
	}{name: "canceled context", ctx: canceled, args: []string{"up"}, wantErr: context.Canceled.Error()})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeDir := t.TempDir()
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			var stdout, stderr bytes.Buffer
			err := cliServeRunInvoke(test.ctx, test.args, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("up error = %v, want containing %q", err, test.wantErr)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("validation output = stdout %q stderr %q, want empty", stdout.String(), stderr.String())
			}
			assertDownRuntimeAbsent(t, runtimeDir)
		})
	}
}

func TestManifestLaunchResultPreservesEmptyReadinessMatcher(t *testing.T) {
	definition := project.Definition{Name: "worker", Source: "manifest", Cwd: "/project", Argv: []string{"worker"}, Ready: &project.ReadyDefinition{Match: ""}}
	process := app.Process{
		Name: "worker", Source: "manifest", Root: "/project", Cwd: "/project", Argv: []string{"worker"},
		State: app.StateExited, StopGraceInherited: true, Readiness: &app.Readiness{State: app.ReadinessStarting, Match: ""},
	}
	result := manifestLaunchResultFor(definition, process, "recovery_pending")
	if !result.ReadinessConfigured || result.ReadinessMatch != "" {
		t.Fatalf("empty readiness matcher result = %#v, want configured empty matcher", result)
	}
	encoded, err := json.Marshal(manifestResultJSON(result))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"readiness_match":""`)) {
		t.Fatalf("empty readiness matcher JSON = %s, want explicit empty readiness_match", encoded)
	}
}

func TestUpPreservesEmptyReadinessMatcherInNDJSON(t *testing.T) {
	root := stopShutdownTestProject(t)
	next := time.Date(2026, time.September, 6, 5, 0, 1, 0, time.UTC)
	nextCursor := protocol.Cursor(1)
	processes := map[string]protocol.Process{
		"worker": {
			Name: "worker", Source: "manifest", Root: root, Cwd: root, Argv: []string{"worker"},
			State: "exited", Readiness: &protocol.Readiness{State: protocol.ReadinessStarting},
			NextCursor: &nextCursor, Restart: protocol.RestartOnFailure, StopGraceInherited: true, Relaunches: 2, NextLaunchAt: &next,
		},
	}
	runtimeDir, _, done := manifestCLIRecoveryStubDaemon(t, processes)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  worker:
    argv: [worker]
    ready: {match: ""}
    restart: on-failure
`)

	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if manifestCLIExitCode(err) != 3 || stderr != "" {
		t.Fatalf("empty readiness recovery up: code %d err=%v stdout=%q stderr=%q", manifestCLIExitCode(err), err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"readiness_match":""`) {
		t.Fatalf("empty readiness recovery NDJSON = %q, want explicit empty readiness_match", stdout)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recovery stub daemon did not observe CLI connection close")
	}
}

func TestUpPreservesCrashRecovery(t *testing.T) {
	root := stopShutdownTestProject(t)
	next := time.Date(2026, time.September, 6, 5, 0, 1, 0, time.UTC)
	pendingNextCursor := protocol.Cursor(21)
	exhaustedNextCursor := protocol.Cursor(29)
	processes := map[string]protocol.Process{
		"pending": {
			Name: "pending", Source: "manifest", Root: root, Cwd: root, Argv: []string{"pending"},
			State: "exited", Readiness: &protocol.Readiness{State: protocol.ReadinessStarting, Match: "ready"}, LaunchCursor: 11, NextCursor: &pendingNextCursor, Restart: protocol.RestartOnFailure, StopGraceInherited: true, Relaunches: 2, NextLaunchAt: &next,
		},
		"exhausted": {
			Name: "exhausted", Source: "manifest", Root: root, Cwd: root, Argv: []string{"exhausted"},
			State: "exited", Readiness: &protocol.Readiness{State: protocol.ReadinessStarting, Match: "ready"}, LaunchCursor: 19, NextCursor: &exhaustedNextCursor, Restart: protocol.RestartOnFailure, StopGraceInherited: true, Relaunches: 5,
		},
	}
	runtimeDir, operations, done := manifestCLIRecoveryStubDaemon(t, processes)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  pending:
    argv: [pending]
    ready:
      match: ready
      timeout: 1s
    restart: on-failure
  exhausted:
    argv: [exhausted]
    ready:
      match: ready
      timeout: 1s
    restart: on-failure
`)

	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err == nil || manifestCLIExitCode(err) != 3 || stderr != "" {
		t.Fatalf("up = code %d err=%v stdout=%q stderr=%q, want exit code 3 without stderr", manifestCLIExitCode(err), err, stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 2 {
		t.Fatalf("up returned %d results, want two: %s", len(results), stdout)
	}
	if results[0].Name != "exhausted" || results[1].Name != "pending" {
		t.Fatalf("up order = %#v, want lexical order", results)
	}
	for _, result := range results {
		if result.Source != "manifest" || result.State != "exited" || result.Restart != protocol.RestartOnFailure || result.Readiness != "" || result.ReadinessMatch != "ready" {
			t.Fatalf("recovery result = %#v, want exited manifest on-failure without readiness", result)
		}
		if result.LaunchCursor == nil {
			t.Fatalf("recovery result omitted launch cursor: %#v", result)
		}
		switch result.Name {
		case "pending":
			if result.Outcome != "recovery_pending" || *result.LaunchCursor != 11 || result.Relaunches != 2 || result.NextLaunchAt == nil || !result.NextLaunchAt.Equal(next) {
				t.Fatalf("pending recovery result = %#v", result)
			}
		case "exhausted":
			if result.Outcome != "recovery_exhausted" || *result.LaunchCursor != 19 || result.Relaunches != 5 || result.NextLaunchAt != nil {
				t.Fatalf("exhausted recovery result = %#v", result)
			}
		default:
			t.Fatalf("unexpected recovery result = %#v", result)
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recovery stub daemon did not observe the CLI connection close")
	}
	var got []protocol.Operation
	for {
		select {
		case operation := <-operations:
			got = append(got, operation)
		default:
			if len(got) != 3 || got[0] != protocol.OpGet || got[1] != protocol.OpGet || got[2] != protocol.OpList {
				t.Fatalf("CLI up daemon operations = %v, want two get requests and one list", got)
			}
			return
		}
	}
}

type manifestProgressCapture struct {
	mu   sync.Mutex
	data []byte
}

func (c *manifestProgressCapture) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = append(c.data, data...)
	return len(data), nil
}

func (c *manifestProgressCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.data)
}

func (c *manifestProgressCapture) waitFor(text string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if strings.Contains(c.String(), text) {
			return true
		}
		if remaining := time.Until(deadline); remaining <= 0 {
			return false
		} else if remaining < 10*time.Millisecond {
			time.Sleep(remaining)
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

type manifestTTYProgressCapture struct {
	manifestProgressCapture
	fd uintptr
}

func (c *manifestTTYProgressCapture) Fd() uintptr { return c.fd }

type manifestBlockingProgressCapture struct {
	manifestProgressCapture
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *manifestBlockingProgressCapture) Write(data []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return c.manifestProgressCapture.Write(data)
}

func manifestProgressLines(output string) []string {
	trimmed := strings.TrimSuffix(output, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func TestManifestStart(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "printf ready; sleep 30"]
    ready:
      match: ready
      timeout: 2s
  worker:
    argv: [/bin/sh, -c, "sleep 30"]
`)

	stdout, stderr, err := stopShutdownRun(t, "start", "--json", "web", "worker")
	if err != nil {
		t.Fatalf("start manifest processes: %v (stderr: %s)", err, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 2 {
		t.Fatalf("start returned %d results, want 2: %s", len(results), stdout)
	}
	if results[0].Name != "web" || results[0].Outcome != "started" || results[0].Readiness != "ready" {
		t.Fatalf("web start result = %+v", results[0])
	}
	if results[1].Name != "worker" || results[1].Outcome != "running_unverified" {
		t.Fatalf("worker start result = %+v", results[1])
	}
	if results[0].Source != "manifest" || !reflect.DeepEqual(results[0].Argv, []string{"/bin/sh", "-c", "printf ready; sleep 30"}) {
		t.Fatalf("web launch identity = %+v", results[0])
	}

	stdout, stderr, err = stopShutdownRun(t, "start", "--json", "web", "worker")
	if err != nil {
		t.Fatalf("idempotent manifest start: %v (stderr: %s)", err, stderr)
	}
	results = manifestCLILaunchResults(t, stdout)
	if len(results) != 2 || results[0].Outcome != "already_running" || results[1].Outcome != "already_running" {
		t.Fatalf("duplicate start results = %+v", results)
	}
}

func TestManifestReadinessSurvivesExitAfterMatch(t *testing.T) {
	t.Parallel()
	serverConn, clientConn := net.Pipe()
	client := daemon.NewClient(clientConn)
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverConn.Close()
	})

	serverDone := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		decoder := protocol.NewDecoder(serverConn)
		encoder := protocol.NewEncoder(serverConn)
		request, err := decoder.DecodeRequest()
		if err != nil {
			serverDone <- err
			return
		}
		if request.Op != protocol.OpHello || request.Hello == nil || request.Hello.Version != protocol.Version {
			serverDone <- fmt.Errorf("hello request = %#v", request)
			return
		}
		if err := encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}); err != nil {
			serverDone <- err
			return
		}

		next := protocol.Cursor(1)
		process := protocol.Process{
			Name: "web", Source: "manifest", Root: "/tmp/project", PID: 123,
			Cwd: "/tmp/project", Argv: []string{"fixture"}, LaunchCursor: 0,
			NextCursor: &next, State: string(app.StateRunning), StopGraceInherited: true,
		}
		gets := 0
		for {
			request, err = decoder.DecodeRequest()
			if err != nil {
				if errors.Is(err, io.EOF) {
					serverDone <- nil
				} else {
					serverDone <- err
				}
				return
			}
			switch request.Op {
			case protocol.OpGet:
				gets++
				process.State = string(app.StateExited)
				process.Readiness = nil
				if err := encoder.EncodeResponse(protocol.NewGetResponse(process)); err != nil {
					serverDone <- err
					return
				}
				if gets > 1 {
					serverDone <- nil
					return
				}
			case protocol.OpWait:
				if request.Wait == nil {
					serverDone <- errors.New("wait request omitted payload")
					return
				}
				if request.Wait.After != nil {
					serverDone <- fmt.Errorf("wait after cursor = %d, want nil", *request.Wait.After)
					return
				}
				if err := encoder.EncodeResponse(protocol.NewWaitResponse(protocol.WaitMatched, 0, nil)); err != nil {
					serverDone <- err
					return
				}
			default:
				serverDone <- fmt.Errorf("unexpected request %q", request.Op)
				return
			}
		}
	}()

	if err := client.Hello(context.Background()); err != nil {
		t.Fatalf("hello: %v", err)
	}
	definition := project.Definition{
		Name: "web", Source: "manifest", Argv: []string{"fixture"},
		Ready: &project.ReadyDefinition{Match: "ready"},
	}
	process := app.Process{
		Name: "web", Source: "manifest", Root: "/tmp/project",
		PID: 123, Cwd: "/tmp/project", Argv: []string{"fixture"},
		State: app.StateRunning, LaunchCursor: 0,
		Readiness: &app.Readiness{State: app.ReadinessStarting, Match: "ready"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := cliReadinessResult(client, ctx, "/tmp/project", definition, process, "started", time.Second)
	if err != nil {
		t.Fatalf("manifest readiness: %v", err)
	}
	if result.Outcome != "started" || result.Readiness != app.ReadinessReady || result.ReadyCursor == nil || *result.ReadyCursor != 0 {
		t.Fatalf("manifest readiness result = %#v, want started and ready at cursor 0", result)
	}

	_ = client.Close()
	if err := <-serverDone; err != nil {
		t.Fatalf("fake daemon: %v", err)
	}
}

// TestManifestReadinessRefreshesExitedSnapshot proves that an
// exited_before_ready result carries the daemon's current process snapshot
// (state, pid, exit code) rather than the running snapshot recorded before
// Wait observed the exit.
func TestManifestReadinessRefreshesExitedSnapshot(t *testing.T) {
	t.Parallel()
	serverConn, clientConn := net.Pipe()
	client := daemon.NewClient(clientConn)
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverConn.Close()
	})

	serverDone := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		decoder := protocol.NewDecoder(serverConn)
		encoder := protocol.NewEncoder(serverConn)
		request, err := decoder.DecodeRequest()
		if err != nil {
			serverDone <- err
			return
		}
		if request.Op != protocol.OpHello || request.Hello == nil || request.Hello.Version != protocol.Version {
			serverDone <- fmt.Errorf("hello request = %#v", request)
			return
		}
		if err := encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}); err != nil {
			serverDone <- err
			return
		}

		next := protocol.Cursor(1)
		running := protocol.Process{
			Name: "web", Source: "manifest", Root: "/tmp/project", PID: 123,
			Cwd: "/tmp/project", Argv: []string{"fixture"}, LaunchCursor: 0,
			NextCursor: &next, State: string(app.StateRunning), StopGraceInherited: true,
			Readiness: &protocol.Readiness{State: protocol.ReadinessStarting, Match: "ready"},
		}
		exited := protocol.Process{
			Name: "web", Source: "manifest", Root: "/tmp/project", PID: 0,
			Cwd: "/tmp/project", Argv: []string{"fixture"}, LaunchCursor: 0,
			NextCursor: &next, State: string(app.StateExited), StopGraceInherited: true, ExitCode: 7,
		}
		gets := 0
		for {
			request, err = decoder.DecodeRequest()
			if err != nil {
				if errors.Is(err, io.EOF) {
					serverDone <- nil
				} else {
					serverDone <- err
				}
				return
			}
			switch request.Op {
			case protocol.OpGet:
				gets++
				process := running
				if gets > 1 {
					process = exited
				}
				if err := encoder.EncodeResponse(protocol.NewGetResponse(process)); err != nil {
					serverDone <- err
					return
				}
				if gets > 1 {
					serverDone <- nil
					return
				}
			case protocol.OpWait:
				if err := encoder.EncodeResponse(protocol.NewWaitResponse(protocol.WaitExited, 0, nil)); err != nil {
					serverDone <- err
					return
				}
			default:
				serverDone <- fmt.Errorf("unexpected request %q", request.Op)
				return
			}
		}
	}()

	if err := client.Hello(context.Background()); err != nil {
		t.Fatalf("hello: %v", err)
	}
	definition := project.Definition{
		Name: "web", Source: "manifest", Argv: []string{"fixture"},
		Ready: &project.ReadyDefinition{Match: "ready"},
	}
	process := app.Process{
		Name: "web", Source: "manifest", Root: "/tmp/project",
		PID: 123, Cwd: "/tmp/project", Argv: []string{"fixture"},
		State: app.StateRunning, LaunchCursor: 0,
		Readiness: &app.Readiness{State: app.ReadinessStarting, Match: "ready"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := cliReadinessResult(client, ctx, "/tmp/project", definition, process, "started", time.Second)
	if err != nil {
		t.Fatalf("manifest readiness: %v", err)
	}
	if result.Outcome != "exited_before_ready" {
		t.Fatalf("outcome = %q, want exited_before_ready", result.Outcome)
	}
	if result.State != string(app.StateExited) {
		t.Fatalf("state = %q, want exited (stale snapshot leaked through)", result.State)
	}
	if result.PID != nil {
		t.Fatalf("pid = %v, want nil for an exited process", result.PID)
	}
	if result.ExitCode == nil || *result.ExitCode != 7 {
		t.Fatalf("exit code = %v, want 7", result.ExitCode)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("fake daemon: %v", err)
	}
}

func TestManifestStartTimeout(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "sleep 30"]
    ready:
      match: never-seen
`)

	stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--timeout", "10ms", "web")
	if err == nil || manifestCLIExitCode(err) != 2 {
		t.Fatalf("readiness timeout = %v (code %d, stderr: %s), want exit code 2", err, manifestCLIExitCode(err), stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 1 || results[0].Outcome != "timed_out" {
		t.Fatalf("timeout result = %+v", results)
	}
}

func TestManifestStartConcurrent(t *testing.T) {
	root := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "sleep 30"]
`)

	const callers = 8
	var wg sync.WaitGroup
	outputs := make([]string, callers)
	errs := make([]error, callers)
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func(index int) {
			defer wg.Done()
			var stdout, stderr bytes.Buffer
			errs[index] = NewRootCommand("test", "test", &stdout, &stderr).Run(context.Background(), []string{"hum", "start", "--json", "--no-wait", "web"})
			outputs[index] = stdout.String() + stderr.String()
		}(i)
	}
	wg.Wait()

	launched := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent start %d failed: %v (%s)", i, err, outputs[i])
		}
		results := manifestCLILaunchResults(t, outputs[i])
		if len(results) != 1 {
			t.Fatalf("concurrent start %d returned %d results: %s", i, len(results), outputs[i])
		}
		if results[0].Outcome == "running_unverified" {
			launched++
		} else if results[0].Outcome != "already_running" {
			t.Fatalf("concurrent start %d result = %+v", i, results[0])
		}
	}
	if launched != 1 {
		t.Fatalf("concurrent starts launched %d children, want exactly 1", launched)
	}
	active := stopShutdownListActive(t, server, root)
	if len(active) != 1 || active[0].Name != "web" {
		t.Fatalf("active processes after concurrent start = %+v", active)
	}
}

func TestManifestUp(t *testing.T) {
	root := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  zeta:
    argv: [/bin/sh, -c, "sleep 30"]
  alpha:
    argv: [/bin/sh, -c, "printf ready; sleep 30"]
    ready:
      match: ready
      timeout: 2s
  beta:
    argv: [/definitely/not/a/real/hum-command]
`)

	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err == nil || manifestCLIExitCode(err) != 1 {
		t.Fatalf("up error precedence = %v (code %d, stderr: %s), want exit code 1", err, manifestCLIExitCode(err), stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 3 {
		t.Fatalf("up returned %d results, want 3: %s", len(results), stdout)
	}
	if got := []string{results[0].Name, results[1].Name, results[2].Name}; !reflect.DeepEqual(got, []string{"alpha", "beta", "zeta"}) {
		t.Fatalf("up order = %v, want lexical order", got)
	}
	if results[0].Outcome != "started" || results[1].Outcome != "error" || results[2].Outcome != "running_unverified" {
		t.Fatalf("up outcomes = %+v", results)
	}
	if results[1].Error == "" {
		t.Fatalf("failed beta result has no error: %+v", results[1])
	}
	active := stopShutdownListActive(t, server, root)
	activeNames := make([]string, 0, len(active))
	for _, process := range active {
		activeNames = append(activeNames, process.Name)
	}
	if !reflect.DeepEqual(activeNames, []string{"alpha", "zeta"}) && !reflect.DeepEqual(activeNames, []string{"zeta", "alpha"}) {
		t.Fatalf("active processes after partial up = %v", activeNames)
	}
}

func TestUpOrdersByAfter(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  db:
    argv: [/bin/sh, -c, "printf db-ready; sleep 30"]
    ready: {match: db-ready}
  api:
    argv: [/bin/sh, -c, "printf api-ready; sleep 30"]
    after: [db]
    ready: {match: api-ready}
  web:
    argv: [/bin/sh, -c, "printf web-ready; sleep 30"]
    after: [api]
    ready: {match: web-ready}
  root:
    argv: [/bin/sh, -c, "printf root-ready; sleep 30"]
    ready: {match: root-ready}
`)
	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err != nil {
		t.Fatalf("ordered up: %v (stdout=%s stderr=%s)", err, stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if got := []string{results[0].Name, results[1].Name, results[2].Name, results[3].Name}; !reflect.DeepEqual(got, []string{"api", "db", "root", "web"}) {
		t.Fatalf("ordered up names = %v", got)
	}
	for _, result := range results {
		if result.Outcome != "started" || result.Readiness != app.ReadinessReady {
			t.Fatalf("ordered up result = %+v", result)
		}
	}
	stdout, stderr, err = stopShutdownRun(t, "up", "--json")
	if err != nil {
		t.Fatalf("idempotent ordered up: %v (stdout=%s stderr=%s)", err, stdout, stderr)
	}
	results = manifestCLILaunchResults(t, stdout)
	for _, result := range results {
		if result.Outcome != "already_running" || result.Readiness != app.ReadinessReady {
			t.Fatalf("idempotent ordered up result = %+v", result)
		}
	}
}

func TestUpReportsBlockedExistingState(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  db:
    argv: [/bin/sh, -c, "exit 4"]
    ready: {match: db-ready}
  api:
    argv: [/bin/sh, -c, "sleep 30"]
    after: [db]
    ready: {match: api-ready}
  web:
    argv: [/bin/sh, -c, "sleep 30"]
    after: [api]
    ready: {match: web-ready}
`)

	startedOut, startedErr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "api")
	if err != nil || startedErr != "" {
		t.Fatalf("seed running api: %v (stdout=%s stderr=%s)", err, startedOut, startedErr)
	}
	started := manifestCLILaunchResults(t, startedOut)[0]

	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err == nil || manifestCLIExitCode(err) != 3 || stderr != "" {
		t.Fatalf("up with running blocked record: %v (stdout=%s stderr=%s)", err, stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if results[0].Name != "api" || results[0].Outcome != "skipped" || results[0].ExistingState != "running" || results[0].PID == nil || started.PID == nil || *results[0].PID != *started.PID {
		t.Fatalf("running blocked api = %+v, seeded %+v", results[0], started)
	}
	if !reflect.DeepEqual(results[0].BlockedBy, []string{"db"}) || results[2].Name != "web" || results[2].Outcome != "skipped" || results[2].ExistingState != "" || results[2].PID != nil {
		t.Fatalf("running blocked results = %+v", results)
	}

	if _, stopErr, stopRunErr := stopShutdownRun(t, "stop", "api"); stopRunErr != nil || stopErr != "" {
		t.Fatalf("stop seeded api: %v (stderr=%s)", stopRunErr, stopErr)
	}
	stdout, stderr, err = stopShutdownRun(t, "up", "--json")
	if err == nil || manifestCLIExitCode(err) != 3 || stderr != "" {
		t.Fatalf("up with exited blocked record: %v (stdout=%s stderr=%s)", err, stdout, stderr)
	}
	results = manifestCLILaunchResults(t, stdout)
	if results[0].Outcome != "skipped" || results[0].ExistingState != "stopped" || results[0].LaunchCursor == nil || started.LaunchCursor == nil || *results[0].LaunchCursor != *started.LaunchCursor {
		t.Fatalf("exited blocked api = %+v, seeded %+v", results[0], started)
	}
	if results[2].Outcome != "skipped" || results[2].ExistingState != "" {
		t.Fatalf("absent blocked web = %+v", results[2])
	}

	human, humanErr, humanRunErr := stopShutdownRun(t, "up")
	if humanRunErr == nil || manifestCLIExitCode(humanRunErr) != 3 {
		t.Fatalf("human blocked up: %v (stdout=%s stderr=%s)", humanRunErr, human, humanErr)
	}
	for _, phrase := range []string{"NAME", "RESULT", "STATE", "PID", "api", "skipped", "web"} {
		if !strings.Contains(human, phrase) {
			t.Fatalf("human summary table missing %q: %s", phrase, human)
		}
	}
	for _, phrase := range []string{"hum up: db: started; waiting for readiness", "hum up: db: exited before readiness; inspect retained logs: hum logs db", "hum up: api: skipped (blocked by db); existing process stopped", "hum up: web: skipped (blocked by api); not launched"} {
		if !strings.Contains(humanErr, phrase) {
			t.Fatalf("human progress missing %q: %s", phrase, humanErr)
		}
	}
}

func TestUpHumanProgress(t *testing.T) {
	if runCLIIsolatedTest(t) {
		return
	}
	t.Run("deterministic transition barriers", func(t *testing.T) {
		definitions := []project.Definition{
			{Name: "alpha", Argv: []string{"alpha"}, Ready: &project.ReadyDefinition{Match: "ready"}},
			{Name: "blocked", Argv: []string{"blocked"}, After: []string{"failure"}, Ready: &project.ReadyDefinition{Match: "ready"}},
			{Name: "failure", Argv: []string{"failure"}},
			{Name: "plain", Argv: []string{"plain"}},
			{Name: "zeta", Argv: []string{"zeta"}, Ready: &project.ReadyDefinition{Match: "ready"}},
		}
		manifest := manifestState{defs: definitions, byName: make(map[string]project.Definition)}
		starts := make(map[string]chan struct{})
		readiness := make(map[string]chan manifestLaunchResult)
		for _, definition := range definitions {
			manifest.byName[definition.Name] = definition
			starts[definition.Name] = make(chan struct{})
			if definition.Ready != nil {
				readiness[definition.Name] = make(chan manifestLaunchResult)
			}
		}
		ops := manifestUpScheduleOps{
			start: func(ctx context.Context, definition project.Definition) (manifestLaunchResult, app.Process, time.Time) {
				select {
				case <-ctx.Done():
					return manifestLaunchError(definition, ctx.Err()), app.Process{}, time.Now()
				case <-starts[definition.Name]:
				}
				if definition.Name == "failure" {
					return manifestLaunchError(definition, errors.New("boom")), app.Process{}, time.Now()
				}
				result := manifestLaunchResult{Name: definition.Name, Argv: definition.Argv, Outcome: "started", Readiness: app.ReadinessRunningUnverified}
				if definition.Ready != nil {
					result.Readiness = app.ReadinessStarting
				}
				return result, app.Process{Name: definition.Name, State: app.StateRunning}, time.Now()
			},
			readiness: func(ctx context.Context, definition project.Definition, _ app.Process, _ string, _ time.Duration) (manifestLaunchResult, error) {
				select {
				case <-ctx.Done():
					return manifestLaunchResult{}, ctx.Err()
				case result := <-readiness[definition.Name]:
					return result, nil
				}
			},
			skipped: func(_ context.Context, definition project.Definition, blocked []string) manifestLaunchResult {
				return manifestLaunchResult{Name: definition.Name, Argv: definition.Argv, Outcome: "skipped", BlockedBy: blocked}
			},
		}
		var progress manifestProgressCapture
		renderer := newManifestProgressRenderer(&progress, len(definitions))
		done := make(chan []manifestLaunchResult, 1)
		go func() {
			results, err := manifestUpScheduleWithOps(context.Background(), &urfavecli.Command{}, manifest, []string{"alpha", "blocked", "failure", "plain", "zeta"}, time.Second, renderer, ops)
			if err != nil {
				t.Errorf("schedule: %v", err)
			}
			done <- results
		}()

		close(starts["zeta"])
		if !progress.waitFor("hum up: zeta: started; waiting for readiness\n", time.Second) {
			t.Fatalf("zeta launch progress = %q", progress.String())
		}
		readiness["zeta"] <- manifestLaunchResult{Name: "zeta", Outcome: "started", Readiness: app.ReadinessReady}
		if !progress.waitFor("hum up: zeta: ready\n", time.Second) {
			t.Fatalf("zeta ready progress = %q", progress.String())
		}
		close(starts["alpha"])
		if !progress.waitFor("hum up: alpha: started; waiting for readiness\n", time.Second) {
			t.Fatalf("alpha launch progress = %q", progress.String())
		}
		readiness["alpha"] <- manifestLaunchResult{Name: "alpha", Outcome: "timed_out", Readiness: app.ReadinessStarting}
		close(starts["failure"])
		close(starts["plain"])
		results := <-done
		if len(results) != len(definitions) {
			t.Fatalf("results = %#v", results)
		}
		want := []string{
			"hum up: zeta: started; waiting for readiness",
			"hum up: zeta: ready",
			"hum up: alpha: started; waiting for readiness",
			"hum up: alpha: readiness timed out; inspect retained logs: hum logs alpha",
			"hum up: failure: error: boom",
			"hum up: blocked: skipped (blocked by failure); not launched",
			"hum up: plain: started; readiness unverified",
		}
		lines := manifestProgressLines(progress.String())
		if !reflect.DeepEqual(lines[:3], want[:3]) {
			t.Fatalf("temporal progress prefix = %q, want %q", lines[:3], want[:3])
		}
		for _, expected := range want {
			if strings.Count(progress.String(), expected+"\n") != 1 {
				t.Errorf("progress count for %q != 1: %q", expected, progress.String())
			}
		}
		for _, line := range lines {
			if strings.Count(line, "hum up: ") != 1 {
				t.Errorf("interleaved progress line %q", line)
			}
		}
	})

	t.Run("blocked stderr does not consume readiness timeout", func(t *testing.T) {
		root := stopShutdownTestProject(t)
		_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		writeManifestCLITestFile(t, root, `version: 1
processes:
  ready:
    argv: [/bin/sh, -c, "sleep 0.05; printf ready-now; sleep 30"]
    ready: {match: ready-now, timeout: 1s}
`)
		t.Cleanup(func() {
			_, _, _ = stopShutdownRun(t, "stop", "ready")
			_, _, _ = stopShutdownRun(t, "shutdown", "--stop-processes")
		})

		stderr := &manifestBlockingProgressCapture{started: make(chan struct{}), release: make(chan struct{})}
		var stdout manifestProgressCapture
		done := make(chan error, 1)
		go func() {
			done <- cliServeRunInvoke(context.Background(), []string{"up"}, &stdout, stderr)
		}()
		select {
		case <-stderr.started:
		case <-time.After(time.Second):
			t.Fatal("progress writer was not called")
		}
		time.Sleep(1500 * time.Millisecond)
		close(stderr.release)
		if err := <-done; err != nil {
			t.Fatalf("up with blocked progress writer: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
		}
		if got := stderr.String(); !strings.Contains(got, "hum up: ready: ready\n") || strings.Contains(got, "timed out") {
			t.Fatalf("blocked progress changed readiness result: %q", got)
		}
	})

	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	fastGate := filepath.Join(root, "fast-ready-gate")
	fastScript := fmt.Sprintf("while [ ! -f %s ]; do sleep 0.01; done; printf fast-child-output; printf fast-ready; sleep 30", strconv.Quote(fastGate))
	writeManifestCLITestFile(t, root, fmt.Sprintf(`version: 1
processes:
  slow:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: never-seen, timeout: 2s}
  fast:
    argv: [/bin/sh, -c, %s]
    ready: {match: fast-ready, timeout: 8s}
  plain:
    argv: [/bin/sh, -c, "printf plain-child; sleep 30"]
  error:
    argv: [/definitely/not/a/real/hum-command]
  blocked-root:
    argv: [/bin/sh, -c, %s]
    ready: {match: blocked-ready, timeout: 2s}
  blocked:
    argv: [/bin/sh, -c, "printf should-not-launch; sleep 30"]
    after: [blocked-root]
    ready: {match: blocked-ready, timeout: 2s}
`, strconv.Quote(fastScript), strconv.Quote("sleep 0.05; exit 4")))
	t.Cleanup(func() {
		for _, name := range []string{"blocked", "blocked-root", "error", "fast", "plain", "slow"} {
			_, _, _ = stopShutdownRun(t, "stop", name)
		}
		_, _, _ = stopShutdownRun(t, "shutdown", "--stop-processes")
	})

	var stdout, stderr manifestProgressCapture
	done := make(chan error, 1)
	go func() {
		done <- cliServeRunInvoke(context.Background(), []string{"up"}, &stdout, &stderr)
	}()
	if !stderr.waitFor("hum up: fast: started; waiting for readiness\n", 5*time.Second) {
		t.Fatalf("fast launch progress did not arrive while up waited: %q", stderr.String())
	}
	if err := os.WriteFile(fastGate, []byte("release\n"), 0o600); err != nil {
		t.Fatalf("release fast readiness gate: %v", err)
	}
	if !stderr.waitFor("hum up: fast: ready\n", 8*time.Second) {
		t.Fatalf("fast readiness progress did not arrive: %q", stderr.String())
	}
	if err := <-done; manifestCLIExitCode(err) != 1 {
		t.Fatalf("progress up exit = %v (code %d), want launch-error exit 1; stdout=%q stderr=%q", err, manifestCLIExitCode(err), stdout.String(), stderr.String())
	}

	lines := manifestProgressLines(stderr.String())
	counts := make(map[string]int)
	for _, line := range lines {
		if !strings.HasPrefix(line, "hum up: ") {
			t.Fatalf("progress line %q lacks hum up prefix", line)
		}
		body := strings.TrimPrefix(line, "hum up: ")
		nameEnd := strings.Index(body, ": ")
		if nameEnd < 0 {
			t.Fatalf("malformed progress line %q", line)
		}
		name := body[:nameEnd]
		counts[name]++
		if strings.Contains(line, "fast-child-output") || strings.Contains(line, "plain-child") || strings.Contains(line, "should-not-launch") {
			t.Fatalf("progress copied child output: %q", line)
		}
		if strings.ContainsRune(line, '\x1b') {
			t.Fatalf("progress contains terminal control: %q", line)
		}
	}
	for _, name := range []string{"blocked", "blocked-root", "error", "fast", "plain", "slow"} {
		if counts[name] == 0 || counts[name] > 2 {
			t.Fatalf("progress line count for %s = %d, lines=%q", name, counts[name], lines)
		}
	}
	wantLines := []string{
		"hum up: blocked: skipped (blocked by blocked-root); not launched",
		"hum up: blocked-root: started; waiting for readiness",
		"hum up: blocked-root: exited before readiness; inspect retained logs: hum logs blocked-root",
		"hum up: fast: started; waiting for readiness",
		"hum up: fast: ready",
		"hum up: plain: started; readiness unverified",
		"hum up: slow: started; waiting for readiness",
		"hum up: slow: readiness timed out; inspect retained logs: hum logs slow",
	}
	for _, want := range wantLines {
		found := false
		for _, line := range lines {
			if line == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing exact progress line %q in %q", want, lines)
		}
	}
	fastReadyPosition := -1
	slowTimeoutPosition := -1
	for index, line := range lines {
		if line == "hum up: fast: ready" {
			fastReadyPosition = index
		}
		if line == "hum up: slow: readiness timed out; inspect retained logs: hum logs slow" {
			slowTimeoutPosition = index
		}
	}
	if fastReadyPosition < 0 || slowTimeoutPosition < 0 || fastReadyPosition >= slowTimeoutPosition {
		t.Fatalf("terminal progress order = %q, want fast ready before slow timeout", lines)
	}

	finalLines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	wantNames := []string{"blocked", "blocked-root", "error", "fast", "plain", "slow"}
	if len(finalLines) != len(wantNames)+1 {
		t.Fatalf("final human result lines = %q, want header plus %d lexical rows", stdout.String(), len(wantNames))
	}
	if fields := strings.Fields(finalLines[0]); !reflect.DeepEqual(fields, []string{"NAME", "RESULT", "STATE", "PID"}) {
		t.Fatalf("final human result header = %q", finalLines[0])
	}
	for index, line := range finalLines[1:] {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			t.Fatalf("final human result row %d is empty: %q", index, stdout.String())
		}
		if fields[0] != wantNames[index] {
			t.Fatalf("final human result order = %q, want %q at %d", stdout.String(), wantNames[index], index)
		}
	}
}

func TestUpProgressOutputModes(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  zeta:
    argv: [/bin/sh, -c, "sleep 30"]
  alpha:
    argv: [/bin/sh, -c, "printf alpha-ready; sleep 30"]
    ready: {match: alpha-ready, timeout: 2s}
`)
	t.Cleanup(func() {
		for _, name := range []string{"alpha", "zeta"} {
			_, _, _ = stopShutdownRun(t, "stop", name)
		}
		_, _, _ = stopShutdownRun(t, "shutdown", "--stop-processes")
	})

	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("up --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 2 || results[0].Name != "alpha" || results[1].Name != "zeta" {
		t.Fatalf("up --json results = %#v, want lexical declarations", results)
	}
	if results[0].Readiness != app.ReadinessReady || results[1].Readiness != app.ReadinessRunningUnverified {
		t.Fatalf("up --json readiness = %#v", results)
	}
	for _, name := range []string{"alpha", "zeta"} {
		if _, _, stopErr := stopShutdownRun(t, "stop", name); stopErr != nil {
			t.Fatalf("stop %s after json up: %v", name, stopErr)
		}
	}

	stdout, stderr, err = stopShutdownRun(t, "up", "--no-wait")
	if err != nil || stderr != "" {
		t.Fatalf("up --no-wait: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if len(stopShutdownNonEmptyLines(stdout)) != 3 {
		t.Fatalf("up --no-wait stdout = %q, want a header and two final rows", stdout)
	}
	for _, name := range []string{"alpha", "zeta"} {
		if _, _, stopErr := stopShutdownRun(t, "stop", name); stopErr != nil {
			t.Fatalf("stop %s after no-wait up: %v", name, stopErr)
		}
	}

	stdout, stderr, err = stopShutdownRun(t, "start", "--timeout", "1s", "alpha")
	if err != nil || stderr != "" || !strings.Contains(stdout, "alpha") {
		t.Fatalf("human start: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	stdout, stderr, err = stopShutdownRun(t, "start", "--json", "--timeout", "1s", "alpha")
	if err != nil || stderr != "" {
		t.Fatalf("json start: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	var jsonStart manifestLaunchResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &jsonStart); err != nil {
		t.Fatalf("decode json start = %q: %v", stdout, err)
	}
	if jsonStart.Name != "alpha" || jsonStart.Outcome != "already_running" {
		t.Fatalf("json start result = %#v, want already_running alpha", jsonStart)
	}

	precedenceCases := []struct {
		name     string
		results  []manifestLaunchResult
		wantCode int
	}{
		{name: "launch error wins", results: []manifestLaunchResult{{Outcome: "timed_out"}, {Outcome: "exited_before_ready"}, {Outcome: "error"}}, wantCode: 1},
		{name: "early exit wins timeout", results: []manifestLaunchResult{{Outcome: "timed_out"}, {Outcome: "exited_before_ready"}}, wantCode: 3},
		{name: "timeout", results: []manifestLaunchResult{{Outcome: "timed_out"}}, wantCode: 2},
		{name: "success", results: []manifestLaunchResult{{Outcome: "started"}}, wantCode: 0},
	}
	for _, test := range precedenceCases {
		if got := manifestCLIExitCode(aggregateManifestExit(test.results)); got != test.wantCode {
			t.Errorf("%s aggregate exit = %d, want %d", test.name, got, test.wantCode)
		}
	}

	pid, launchCursor, readyCursor := 42, uint64(7), uint64(9)
	var final bytes.Buffer
	if err := renderManifestLaunchHuman(&final, manifestLaunchResult{
		Name: "alpha", Argv: []string{"/bin/sh", "-c", "printf ready"}, Source: "manifest", Outcome: "started",
		PID: &pid, LaunchCursor: &launchCursor, Readiness: app.ReadinessReady, ReadyCursor: &readyCursor,
	}); err != nil {
		t.Fatalf("render final human summary: %v", err)
	}
	if want := "started alpha (manifest: /bin/sh -c 'printf ready') pid=42 launch_cursor=7 readiness=ready ready_cursor=9\n"; final.String() != want {
		t.Fatalf("final human summary = %q, want %q", final.String(), want)
	}
}

func TestSignalExitDocs(t *testing.T) {
	contents, err := os.ReadFile("../../docs/design.md")
	if err != nil {
		t.Fatal(err)
	}
	docs := strings.ToLower(string(contents))
	for _, phrase := range []string{"exit_status: -1", "signal", `{"name":"sigterm","number":15}`, "non-signal exits omit", "operator-stopped", "list", "status", "up", "wait", "mcp"} {
		if !strings.Contains(docs, phrase) {
			t.Errorf("signal exit docs missing %q", phrase)
		}
	}
}

func TestUpAttachedOutput(t *testing.T) {
	root := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	gate := filepath.Join(root, "ready.release")
	writeManifestCLITestFile(t, root, fmt.Sprintf(`version: 1
processes:
  app:
    argv: [/bin/sh, -c, %s]
    ready: {match: app-ready, timeout: 3s}
`, strconv.Quote(fmt.Sprintf("printf 'before-ready\\n'; while [ ! -f %s ]; do sleep 0.02; done; printf 'app-ready\\n'; sleep 0.05; printf 'after-ready\\n'; sleep 30", strconv.Quote(gate)))))
	t.Cleanup(func() {
		_, _, _ = stopShutdownRun(t, "stop", "app")
	})

	tty := renderTestTTY(t)
	var stdout manifestTTYProgressCapture
	stdout.fd = tty.Fd()
	var stderr manifestProgressCapture
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- NewRootCommand("test", "test", &stdout, &stderr).Run(ctx, []string{"hum", "up"})
	}()
	if !stdout.waitFor(processLogPrefix(colorPolicy{enabled: true}, "app")+" before-ready\n", 3*time.Second) {
		cancel()
		t.Fatalf("attached up did not stream startup output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if err := os.WriteFile(gate, []byte("ready\n"), 0o600); err != nil {
		cancel()
		t.Fatal(err)
	}
	if !stderr.waitFor("hum up: startup complete; following logs", 3*time.Second) {
		cancel()
		t.Fatalf("attached up did not enter follow mode: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !stdout.waitFor(processLogPrefix(colorPolicy{enabled: true}, "app")+" after-ready\n", 3*time.Second) {
		cancel()
		t.Fatalf("attached up did not continue after readiness: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("detached attached up: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("attached up did not detach after cancellation")
	}
	processes := stopShutdownListActive(t, server, root)
	if len(processes) != 1 || processes[0].Name != "app" || !app.IsActiveState(processes[0].State) {
		t.Fatalf("processes after detach = %#v, want app still active", processes)
	}

	var detachedStdout manifestTTYProgressCapture
	detachedStdout.fd = tty.Fd()
	var detachedStderr manifestProgressCapture
	if err := NewRootCommand("test", "test", &detachedStdout, &detachedStderr).Run(context.Background(), []string{"hum", "up", "--detach"}); err != nil {
		t.Fatalf("up --detach: %v; stdout=%q stderr=%q", err, detachedStdout.String(), detachedStderr.String())
	}
	if strings.Contains(detachedStdout.String(), "[app]") || strings.Contains(detachedStderr.String(), "following logs") {
		t.Fatalf("up --detach followed output: stdout=%q stderr=%q", detachedStdout.String(), detachedStderr.String())
	}
}

func TestUpAttachedStartupInterrupt(t *testing.T) {
	root := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  app:
    argv: [/bin/sh, -c, "printf 'before-ready\\n'; sleep 30"]
    ready: {match: never-ready, timeout: 30s}
`)
	t.Cleanup(func() {
		_, _, _ = stopShutdownRun(t, "stop", "app")
	})

	registered := make(chan chan<- os.Signal, 1)
	previousNotify := notifyUpFollowSignals
	notifyUpFollowSignals = func(signals chan<- os.Signal) { registered <- signals }
	t.Cleanup(func() { notifyUpFollowSignals = previousNotify })

	tty := renderTestTTY(t)
	var stdout manifestTTYProgressCapture
	stdout.fd = tty.Fd()
	var stderr manifestProgressCapture
	done := make(chan error, 1)
	go func() {
		done <- NewRootCommand("test", "test", &stdout, &stderr).Run(context.Background(), []string{"hum", "up"})
	}()
	if !stdout.waitFor(processLogPrefix(colorPolicy{enabled: true}, "app")+" before-ready\n", 3*time.Second) {
		t.Fatalf("attached up did not stream before interrupt: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	var signals chan<- os.Signal
	select {
	case signals = <-registered:
	case <-time.After(3 * time.Second):
		t.Fatal("attached up did not register its interrupt handler")
	}
	signals <- os.Interrupt
	select {
	case err := <-done:
		if manifestCLIExitCode(err) != 130 {
			t.Fatalf("interrupted attached up exit = %v, want 130", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("interrupted attached up did not return")
	}
	if !strings.Contains(stderr.String(), "startup interrupted; launched processes remain supervised") {
		t.Fatalf("interrupted attached up stderr = %q", stderr.String())
	}
	processes := stopShutdownListActive(t, server, root)
	if len(processes) != 1 || processes[0].Name != "app" || !app.IsActiveState(processes[0].State) {
		t.Fatalf("processes after startup interrupt = %#v, want app still active", processes)
	}
}

func TestUpAttachedStartupFailure(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  broken:
    argv: [/bin/sh, -c, "printf 'failure-detail\\n'; exit 7"]
    ready: {match: never-ready, timeout: 3s}
  dependent:
    argv: [/bin/sh, -c, "sleep 30"]
    after: [broken]
`)

	tty := renderTestTTY(t)
	var stdout manifestTTYProgressCapture
	stdout.fd = tty.Fd()
	var stderr manifestProgressCapture
	err := NewRootCommand("test", "test", &stdout, &stderr).Run(context.Background(), []string{"hum", "up"})
	if manifestCLIExitCode(err) != 3 {
		t.Fatalf("attached failing up exit = %v, want 3; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), processLogPrefix(colorPolicy{enabled: true}, "broken")+" failure-detail\n") {
		t.Fatalf("attached failing up omitted diagnostics: stdout=%q", stdout.String())
	}
	if strings.Contains(stderr.String(), "following logs") {
		t.Fatalf("attached failing up continued following: stderr=%q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "dependent: skipped (blocked by broken); not launched") {
		t.Fatalf("attached failing up changed skipped state: stderr=%q", stderr.String())
	}
}

func TestUpProgressDocs(t *testing.T) {
	design, err := os.ReadFile("../../docs/design.md")
	if err != nil {
		t.Fatalf("read design docs: %v", err)
	}
	var help, helpErr bytes.Buffer
	root := NewRootCommand("test", "test", &help, &helpErr)
	if err := root.Run(context.Background(), []string{"hum", "up", "--help"}); err != nil {
		t.Fatalf("up help: %v", err)
	}
	if helpErr.Len() != 0 {
		t.Fatalf("up help stderr = %q", helpErr.String())
	}
	all := strings.ToLower(help.String() + "\n" + string(design))
	for _, phrase := range []string{"interactive terminal", "--detach", "ctrl+c detaches", "non-terminal", "stderr", "stdout", "--json", "--no-wait", "at most two lines", "temporal", "transition", "child output", "timeout", "early-exit", "hum logs name"} {
		if !strings.Contains(all, phrase) {
			t.Errorf("progress docs missing %q", phrase)
		}
	}
}

func TestUpProgressBlockedExistingState(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  queue:
    argv: [/bin/sh, -c, "exit 5"]
    ready: {match: queue-ready}
  db:
    argv: [/bin/sh, -c, "exit 4"]
    ready: {match: db-ready}
  api:
    argv: [/bin/sh, -c, "sleep 30"]
    after: [queue, db]
    ready: {match: api-ready}
  web:
    argv: [/bin/sh, -c, "sleep 30"]
    after: [api]
    ready: {match: web-ready}
`)
	t.Cleanup(func() {
		for _, name := range []string{"api", "db", "queue", "web"} {
			_, _, _ = stopShutdownRun(t, "stop", name)
		}
		_, _, _ = stopShutdownRun(t, "shutdown", "--stop-processes")
	})
	seed, seedErr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "api")
	if err != nil || seedErr != "" {
		t.Fatalf("seed running api: err=%v stdout=%q stderr=%q", err, seed, seedErr)
	}

	_, stderr, err := stopShutdownRun(t, "up")
	if manifestCLIExitCode(err) != 3 {
		t.Fatalf("running blocked up exit = %v, want 3; stderr=%q", err, stderr)
	}
	for _, want := range []string{
		"hum up: api: skipped (blocked by db, queue); existing process running",
		"hum up: web: skipped (blocked by api); not launched",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("running blocked progress missing %q: %s", want, stderr)
		}
	}
	if _, _, err := stopShutdownRun(t, "stop", "api"); err != nil {
		t.Fatalf("stop seeded api: %v", err)
	}
	_, stderr, err = stopShutdownRun(t, "up")
	if manifestCLIExitCode(err) != 3 {
		t.Fatalf("exited blocked up exit = %v, want 3; stderr=%q", err, stderr)
	}
	for _, want := range []string{
		"hum up: api: skipped (blocked by db, queue); existing process stopped",
		"hum up: web: skipped (blocked by api); not launched",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("exited blocked progress missing %q: %s", want, stderr)
		}
	}
	for _, name := range []string{"api", "web"} {
		count := strings.Count(stderr, "hum up: "+name+":")
		if count != 1 {
			t.Errorf("progress line count for %s = %d, stderr=%q", name, count, stderr)
		}
	}
}

func TestManifestList(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "echo hello world"]
  api:
    argv: [go, run, ./api, --port=8080]
`)

	stdout, stderr, err := stopShutdownRun(t, "list", "--json")
	if err != nil {
		t.Fatalf("manifest list without daemon: %v (stderr: %s)", err, stderr)
	}
	var result listJSON
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode manifest list: %v (%s)", err, stdout)
	}
	if len(result.Processes) != 2 {
		t.Fatalf("manifest list returned %d processes, want 2", len(result.Processes))
	}
	if result.Processes[0].Name != "api" || result.Processes[0].Source != "manifest" || result.Processes[0].Root != root {
		t.Fatalf("first manifest list process = %+v", result.Processes[0])
	}
	if !reflect.DeepEqual(result.Processes[0].Argv, []string{"go", "run", "./api", "--port=8080"}) {
		t.Fatalf("api argv = %v", result.Processes[0].Argv)
	}
	if result.Processes[0].State != "stopped" || result.Processes[1].State != "stopped" {
		t.Fatalf("manifest list states = %q, %q", result.Processes[0].State, result.Processes[1].State)
	}
	if _, err := os.Stat(runtimeDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only manifest list touched runtime directory: %v", err)
	}

	stdout, stderr, err = stopShutdownRun(t, "list", "--full")
	if err != nil {
		t.Fatalf("full human manifest list: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "source=manifest") || !strings.Contains(stdout, "argv=go run ./api --port=8080") {
		t.Fatalf("human manifest list omitted source/argv: %s", stdout)
	}
}

func TestDeclaredNameCollision(t *testing.T) {
	root := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	stdout, stderr, err := stopShutdownRun(t, "run", "web", "--detach", "--", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("start ad-hoc process: %v (stdout: %s, stderr: %s)", err, stdout, stderr)
	}
	writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "sleep 30"]
`)

	stdout, stderr, err = stopShutdownRun(t, "start", "--json", "web")
	if err == nil || manifestCLIExitCode(err) != 1 {
		t.Fatalf("declared/ad-hoc collision = %v (code %d, stdout: %s, stderr: %s)", err, manifestCLIExitCode(err), stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 1 || results[0].Outcome != "error" || !strings.Contains(results[0].Error, "ad-hoc") {
		t.Fatalf("collision result = %+v", results)
	}
	active := stopShutdownListActive(t, server, root)
	if len(active) != 1 || active[0].Source != "ad_hoc" {
		t.Fatalf("collision changed active process = %+v", active)
	}

	_, stderr, err = stopShutdownRun(t, "run", "web", "--detach", "--", "/bin/sh", "-c", "sleep 30")
	if err == nil || !strings.Contains(err.Error(), "declared") {
		t.Fatalf("raw run for declared name error = %v (stderr: %s)", err, stderr)
	}
}

func TestManifestRestart(t *testing.T) {
	root := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	marker := filepath.Join(root, "restart-marker")
	command := fmt.Sprintf(`printf "$HUM_MANIFEST_TEST_VALUE" > %s; sleep 30`, marker)
	writeManifestCLITestFile(t, root, fmt.Sprintf(`version: 1
processes:
  web:
    argv: [/bin/sh, -c, %q]
`, command))

	t.Setenv("HUM_MANIFEST_TEST_VALUE", "before")
	stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "web")
	if err != nil {
		t.Fatalf("start before restart: %v (stderr: %s)", err, stderr)
	}
	started := manifestCLILaunchResults(t, stdout)
	if len(started) != 1 || started[0].LaunchCursor == nil {
		t.Fatalf("start result = %+v", started)
	}
	if err := waitForManifestMarker(marker, "before"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HUM_MANIFEST_TEST_VALUE", "after")
	stdout, stderr, err = stopShutdownRun(t, "restart", "--json", "web")
	if err != nil {
		t.Fatalf("restart manifest process: %v (stderr: %s)", err, stderr)
	}
	var restarted restartResult
	if err := json.Unmarshal([]byte(stdout), &restarted); err != nil {
		t.Fatalf("decode restart result: %v (%s)", err, stdout)
	}
	if restarted.Source != "manifest" || !reflect.DeepEqual(restarted.Argv, []string{"/bin/sh", "-c", command}) {
		t.Fatalf("restart identity = %+v", restarted)
	}
	store, err := server.Supervisor().Output(root, "web")
	if err != nil {
		t.Fatalf("restart output store: %v", err)
	}
	read, err := store.Read(output.ReadOptions{})
	if err != nil {
		t.Fatalf("read restart output: %v", err)
	}
	var (
		markerCursor output.Cursor
		foundMarker  bool
	)
	for _, entry := range read.Entries {
		if entry.Stream == output.System && entry.Text == "web restarted\n" {
			markerCursor = entry.Cursor
			foundMarker = true
			break
		}
	}
	if !foundMarker {
		t.Fatalf("restart marker missing from %#v", read.Entries)
	}
	if got := output.Cursor(restarted.LaunchCursor); got != markerCursor {
		t.Fatalf("restart launch cursor = %d, marker cursor = %d", got, markerCursor)
	}
	current, err := server.Supervisor().Get(root, "web")
	if err != nil {
		t.Fatalf("get restarted process: %v", err)
	}
	if current.LaunchCursor != markerCursor || current.NextCursor <= markerCursor {
		t.Fatalf("restarted process cursors = launch %d, next %d; marker %d", current.LaunchCursor, current.NextCursor, markerCursor)
	}
	if err := waitForManifestMarker(marker, "after"); err != nil {
		t.Fatal(err)
	}
}

func TestReadinessFields(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  waiting:
    argv: [/bin/sh, -c, "sleep 30"]
    ready:
      match: never-seen
      timeout: 1s
  plain:
    argv: [/bin/sh, -c, "sleep 30"]
`)

	if _, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "waiting", "plain"); err != nil {
		t.Fatalf("start readiness fixture: %v (stderr: %s)", err, stderr)
	}
	if _, stderr, err := stopShutdownRun(t, "run", "adhoc", "--detach", "--", "/bin/sh", "-c", "sleep 30"); err != nil {
		t.Fatalf("start ad-hoc readiness fixture: %v (stderr: %s)", err, stderr)
	}

	stdout, stderr, err := stopShutdownRun(t, "status", "--json", "waiting")
	if err != nil {
		t.Fatalf("status waiting: %v (stderr: %s)", err, stderr)
	}
	var waiting statusJSON
	if err := json.Unmarshal([]byte(stdout), &waiting); err != nil {
		t.Fatalf("decode waiting status: %v", err)
	}
	if waiting.Source != "manifest" || waiting.Readiness != "starting" {
		t.Fatalf("waiting status = %+v", waiting)
	}

	stdout, stderr, err = stopShutdownRun(t, "status", "--json", "plain")
	if err != nil {
		t.Fatalf("status plain: %v (stderr: %s)", err, stderr)
	}
	var plain statusJSON
	if err := json.Unmarshal([]byte(stdout), &plain); err != nil {
		t.Fatalf("decode plain status: %v", err)
	}
	if plain.Source != "manifest" || plain.Readiness != "running_unverified" {
		t.Fatalf("plain status = %+v", plain)
	}

	stdout, stderr, err = stopShutdownRun(t, "status", "--json", "adhoc")
	if err != nil {
		t.Fatalf("status ad-hoc: %v (stderr: %s)", err, stderr)
	}
	var adhoc map[string]any
	if err := json.Unmarshal([]byte(stdout), &adhoc); err != nil {
		t.Fatalf("decode ad-hoc status: %v", err)
	}
	if _, ok := adhoc["readiness"]; ok {
		t.Fatalf("ad-hoc status unexpectedly contains readiness: %s", stdout)
	}
}

// TestManifestProgressDriftDetail proves the definition_drift progress
// detail names the sorted changed fields and restart guidance instead of
// the bare outcome ("hum up: db: definition_drift"). render.go's
// manifestProgressInitialLine and manifestProgressTerminalLine must call
// this helper from a "definition_drift" case (see report) for the fix to
// reach the rendered "hum up: db: ..." stderr line.
func TestManifestProgressDriftDetail(t *testing.T) {
	result := manifestLaunchResult{
		Name:          "db",
		Outcome:       "definition_drift",
		ChangedFields: []string{"argv", "cwd"},
		Guidance:      "hum restart db",
	}
	want := "definition_drift (argv, cwd); run hum restart db"
	if got := manifestProgressDriftDetail(result); got != want {
		t.Fatalf("manifestProgressDriftDetail = %q, want %q", got, want)
	}
}

func TestUpReportsManifestRuntimeDrift(t *testing.T) {
	cases := []struct {
		name      string
		edit      string
		wantField string
	}{
		{name: "argv", edit: `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "sleep 31"]
    cwd: .
    restart: never
`, wantField: "argv"},
		{name: "cwd", edit: `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "sleep 30"]
    cwd: sub
    restart: never
`, wantField: "cwd"},
		{name: "readiness", edit: `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "sleep 30"]
    cwd: .
    ready: {match: ready}
    restart: never
`, wantField: "readiness_match"},
		{name: "tty", edit: `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "sleep 30"]
    cwd: .
    tty: true
    restart: never
`, wantField: "tty"},
		{name: "restart", edit: `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "sleep 30"]
    cwd: .
    restart: on-failure
`, wantField: "restart"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := stopShutdownTestProject(t)
			_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			if test.name == "cwd" {
				if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "sleep 30"]
    cwd: .
    restart: never
`)
			if stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "web"); err != nil {
				t.Fatalf("initial start: %v stdout=%q stderr=%q", err, stdout, stderr)
			}
			stdout, stderr, err := stopShutdownRun(t, "up", "--json", "--no-wait")
			if err != nil || stderr != "" {
				t.Fatalf("matching up: %v stdout=%q stderr=%q", err, stdout, stderr)
			}
			matching := manifestCLILaunchResults(t, stdout)
			if len(matching) != 1 || matching[0].Outcome != "already_running" {
				t.Fatalf("matching up result = %#v", matching)
			}
			pid := matching[0].PID
			writeManifestCLITestFile(t, root, test.edit)
			for _, command := range []string{"up", "start"} {
				args := []string{command, "--json", "--no-wait"}
				if command == "start" {
					args = append(args, "web")
				}
				stdout, stderr, err = stopShutdownRun(t, args...)
				if err == nil || manifestCLIExitCode(err) != 1 || stderr != "" {
					t.Fatalf("%s drift: code %d err=%v stdout=%q stderr=%q", command, manifestCLIExitCode(err), err, stdout, stderr)
				}
				results := manifestCLILaunchResults(t, stdout)
				if len(results) != 1 || results[0].Outcome != "definition_drift" || !reflect.DeepEqual(results[0].ChangedFields, []string{test.wantField}) || results[0].Guidance != "hum restart web" {
					t.Fatalf("%s drift result = %#v", command, results)
				}
				if pid != nil && (results[0].PID == nil || *results[0].PID != *pid) {
					t.Fatalf("%s changed process PID: before=%v after=%v", command, *pid, results[0].PID)
				}
			}
			if stdout, stderr, err := stopShutdownRun(t, "stop", "web"); err != nil || stderr != "" {
				t.Fatalf("cleanup stop: %v stdout=%q stderr=%q", err, stdout, stderr)
			}
		})
	}
}

func TestUpRejectsDriftedReadinessGate(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  db:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: old}
  api:
    argv: [/bin/sh, -c, "touch api-launched; sleep 30"]
    ready: {match: api-ready}
    after: [db]
`)
	if stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "db"); err != nil {
		t.Fatalf("initial db start: %v stdout=%q stderr=%q", err, stdout, stderr)
	}
	writeManifestCLITestFile(t, root, `version: 1
processes:
  db:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: new}
  api:
    argv: [/bin/sh, -c, "touch api-launched; sleep 30"]
    ready: {match: api-ready}
    after: [db]
`)
	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err == nil || manifestCLIExitCode(err) != 1 || stderr != "" {
		t.Fatalf("drifted gate up: code %d err=%v stdout=%q stderr=%q", manifestCLIExitCode(err), err, stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 2 || results[0].Name != "api" || results[0].Outcome != "skipped" || !reflect.DeepEqual(results[0].BlockedBy, []string{"db"}) || results[1].Name != "db" || results[1].Outcome != "definition_drift" {
		t.Fatalf("drifted gate results = %#v", results)
	}
	if _, statErr := os.Stat(filepath.Join(root, "api-launched")); !os.IsNotExist(statErr) {
		t.Fatalf("drifted gate launched dependent: %v", statErr)
	}
	if stdout, stderr, stopErr := stopShutdownRun(t, "stop", "db"); stopErr != nil || stderr != "" {
		t.Fatalf("cleanup stop: %v stdout=%q stderr=%q", stopErr, stdout, stderr)
	}
}

func TestUpReportsRemovedManifestSessions(t *testing.T) {
	root := stopShutdownTestProject(t)
	next := time.Date(2026, time.September, 6, 5, 0, 1, 0, time.UTC)
	otherRoot := filepath.Join(root, "other")
	if err := os.Mkdir(otherRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	currentNextCursor := protocol.Cursor(10)
	processes := map[string]protocol.Process{
		"current":    {Name: "current", Source: "manifest", Root: root, Cwd: root, Argv: []string{"current"}, State: "running", PID: 10, LaunchCursor: 1, NextCursor: &currentNextCursor, StopGraceInherited: true},
		"running":    {Name: "running", Source: "manifest", Root: root, Cwd: root, Argv: []string{"running"}, State: "running", PID: 11, LaunchCursor: 2, StopGraceInherited: true},
		"pending":    {Name: "pending", Source: "manifest", Root: root, Cwd: root, Argv: []string{"pending"}, State: "exited", LaunchCursor: 3, Restart: protocol.RestartOnFailure, StopGraceInherited: true, Relaunches: 2, NextLaunchAt: &next},
		"exhausted":  {Name: "exhausted", Source: "manifest", Root: root, Cwd: root, Argv: []string{"exhausted"}, State: "exited", LaunchCursor: 4, Restart: protocol.RestartOnFailure, StopGraceInherited: true, Relaunches: 5},
		"stopped":    {Name: "stopped", Source: "manifest", Root: root, Cwd: root, Argv: []string{"stopped"}, State: "exited", LaunchCursor: 5},
		"ad_hoc":     {Name: "ad_hoc", Source: "ad_hoc", Root: root, Cwd: root, Argv: []string{"ad_hoc"}, State: "running", PID: 12, LaunchCursor: 6},
		"discovered": {Name: "discovered", Source: "package_json", Root: root, Cwd: root, Argv: []string{"discovered"}, State: "running", PID: 13, LaunchCursor: 7},
		"other":      {Name: "other", Source: "manifest", Root: otherRoot, Cwd: otherRoot, Argv: []string{"other"}, State: "running", PID: 14, LaunchCursor: 8},
	}
	runtimeDir, _, done := manifestCLIRecoveryStubDaemon(t, processes)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  current:
    argv: [current]
`)
	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("removed up: %v stdout=%q stderr=%q", err, stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 4 {
		t.Fatalf("removed up results = %#v", results)
	}
	for index, name := range []string{"current", "exhausted", "pending", "running"} {
		if results[index].Name != name {
			t.Fatalf("removed up order = %#v", results)
		}
	}
	if results[0].Outcome != "already_running" {
		t.Fatalf("current result = %#v", results[0])
	}
	for _, result := range results[1:] {
		if result.Outcome != "removed_definition" || !strings.Contains(result.Guidance, "hum stop "+result.Name) || !strings.Contains(result.Guidance, "hum remove "+result.Name) {
			t.Fatalf("removed result = %#v", result)
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("removed stub daemon did not observe CLI connection close")
	}
}

func TestUpReportsRemovedManifestSessionsWithoutManifest(t *testing.T) {
	root := stopShutdownTestProject(t)
	processes := map[string]protocol.Process{
		"removed": {
			Name: "removed", Source: "manifest", Root: root, Cwd: root, Argv: []string{"removed"},
			State: "running", PID: 11, LaunchCursor: 2, StopGraceInherited: true,
		},
	}
	runtimeDir, _, done := manifestCLIRecoveryStubDaemon(t, processes)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("removed up without manifest: %v stdout=%q stderr=%q", err, stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 1 || results[0].Name != "removed" || results[0].Outcome != "removed_definition" || !strings.Contains(results[0].Guidance, "hum stop removed") || !strings.Contains(results[0].Guidance, "hum remove removed") {
		t.Fatalf("removed up without manifest results = %#v", results)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("removed stub daemon did not observe CLI connection close")
	}
}

func TestManifestStartRetainsRecordedReadinessAfterManifestEdit(t *testing.T) {
	tests := []struct {
		name   string
		edited string
	}{
		{
			name: "remove ready",
			edited: `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "echo new; sleep 0.3; echo old; sleep 30"]
`,
		},
		{
			name: "change ready",
			edited: `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "echo new; sleep 0.3; echo old; sleep 30"]
    ready:
      match: new
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := stopShutdownTestProject(t)
			_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "echo new; sleep 0.3; echo old; sleep 30"]
    ready:
      match: old
`)

			stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "web")
			if err != nil {
				t.Fatalf("initial start: %v (stderr: %s)", err, stderr)
			}
			started := manifestCLILaunchResults(t, stdout)
			if len(started) != 1 || started[0].Readiness != app.ReadinessStarting {
				t.Fatalf("initial start result = %+v, want starting readiness", started)
			}

			writeManifestCLITestFile(t, root, test.edited)
			stdout, stderr, err = stopShutdownRun(t, "start", "--json", "--timeout", "1s", "web")
			if err == nil || manifestCLIExitCode(err) != 1 {
				t.Fatalf("start after manifest edit: code %d err=%v stderr=%q, want definition drift", manifestCLIExitCode(err), err, stderr)
			}
			results := manifestCLILaunchResults(t, stdout)
			if len(results) != 1 {
				t.Fatalf("start after manifest edit returned %d results: %s", len(results), stdout)
			}
			if results[0].Outcome != "definition_drift" || !reflect.DeepEqual(results[0].ChangedFields, []string{"readiness_match"}) {
				t.Fatalf("start after %s result = %+v, want definition_drift/readiness_match", test.name, results[0])
			}
			if results[0].Guidance != "hum restart web" || results[0].ReadinessMatch != "old" {
				t.Fatalf("start after %s guidance/readiness = %+v", test.name, results[0])
			}
			if results[0].Source != "manifest" || !reflect.DeepEqual(results[0].Argv, []string{"/bin/sh", "-c", "echo new; sleep 0.3; echo old; sleep 30"}) {
				t.Fatalf("start after %s identity = %+v", test.name, results[0])
			}
		})
	}
}

func TestUpDriftDocs(t *testing.T) {
	paths := []string{"../../docs/design.md", "../../docs/coding-agents.md", "../skill/SKILL.md", "../../plugins/hum/skills/hum/SKILL.md"}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := strings.ToLower(string(contents))
		for _, phrase := range []string{"definition_drift", "removed_definition", "changed_fields", "hum restart", "hum stop", "hum remove", "argv", "readiness matcher", "restart policy"} {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s missing drift guidance %q", path, phrase)
			}
		}
	}
	var stdout, stderr bytes.Buffer
	if err := NewRootCommand("test", "test", &stdout, &stderr).Run(context.Background(), []string{"hum", "up", "--help"}); err != nil {
		t.Fatal(err)
	}
	help := strings.ToLower(stdout.String())
	for _, phrase := range []string{"definition drift", "exit 1", "docs/design.md"} {
		if !strings.Contains(help, phrase) {
			t.Errorf("CLI up help missing concise drift guidance %q", phrase)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("CLI up help stderr = %q", stderr.String())
	}
}

func waitForManifestMarker(filename, want string) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(filename)
		if err == nil && string(contents) == want {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	contents, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read restart marker %q: %w", filename, err)
	}
	return fmt.Errorf("restart marker = %q, want %q", string(contents), want)
}
