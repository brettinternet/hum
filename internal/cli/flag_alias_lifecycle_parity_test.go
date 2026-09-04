package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"hum/internal/daemon"
)

func TestFlagAliasParityLifecycleCommands(t *testing.T) {
	t.Run("run detach daemon request and JSON output", func(t *testing.T) {
		short := flagAliasLifecycleRun(t, "short", "-d", "--json")
		long := flagAliasLifecycleRun(t, "long", "--detach", "--json")
		if !reflect.DeepEqual(short, long) {
			t.Fatalf("short run = %#v, long run = %#v", short, long)
		}
	})

	t.Run("run JSON daemon request and output", func(t *testing.T) {
		short := flagAliasLifecycleRun(t, "short", "--detach", "-j")
		long := flagAliasLifecycleRun(t, "long", "--detach", "--json")
		if !reflect.DeepEqual(short, long) {
			t.Fatalf("short run = %#v, long run = %#v", short, long)
		}
	})

	t.Run("serve daemon mode", func(t *testing.T) {
		short := flagAliasLifecycleServe(t, "short", "-d")
		long := flagAliasLifecycleServe(t, "long", "--daemon")
		if !reflect.DeepEqual(short, long) {
			t.Fatalf("short serve = %#v, long serve = %#v", short, long)
		}
	})

	t.Run("init JSON output", func(t *testing.T) {
		short := flagAliasLifecycleInit(t, "short", "-j")
		long := flagAliasLifecycleInit(t, "long", "--json")
		if !reflect.DeepEqual(short, long) {
			t.Fatalf("short init = %#v, long init = %#v", short, long)
		}
	})

	t.Run("shutdown JSON request output and exit", func(t *testing.T) {
		short := flagAliasLifecycleShutdown(t, "short", "-j")
		long := flagAliasLifecycleShutdown(t, "long", "--json")
		if !reflect.DeepEqual(short, long) {
			t.Fatalf("short shutdown = %#v, long shutdown = %#v", short, long)
		}
	})
	t.Run("manifest start timeout and JSON", func(t *testing.T) {
		short := flagAliasLifecycleManifest(t, "short", "start", "-t", "-j")
		long := flagAliasLifecycleManifest(t, "long", "start", "--timeout", "--json")
		if !reflect.DeepEqual(short, long) {
			t.Fatalf("short start = %#v, long start = %#v", short, long)
		}
	})

	t.Run("manifest up timeout and JSON", func(t *testing.T) {
		short := flagAliasLifecycleManifest(t, "short", "up", "-t", "-j")
		long := flagAliasLifecycleManifest(t, "long", "up", "--timeout", "--json")
		if !reflect.DeepEqual(short, long) {
			t.Fatalf("short up = %#v, long up = %#v", short, long)
		}
	})

	t.Run("down JSON request output and exit", func(t *testing.T) {
		short := flagAliasLifecycleDown(t, "short", "-j")
		long := flagAliasLifecycleDown(t, "long", "--json")
		if !reflect.DeepEqual(short, long) {
			t.Fatalf("short down = %#v, long down = %#v", short, long)
		}
	})

	t.Run("stop JSON request output and exit", func(t *testing.T) {
		short := flagAliasLifecycleStop(t, "short", "-j")
		long := flagAliasLifecycleStop(t, "long", "--json")
		if !reflect.DeepEqual(short, long) {
			t.Fatalf("short stop = %#v, long stop = %#v", short, long)
		}
	})

	t.Run("restart JSON request output and exit", func(t *testing.T) {
		short := flagAliasLifecycleRestart(t, "short", "-j")
		long := flagAliasLifecycleRestart(t, "long", "--json")
		if !reflect.DeepEqual(short, long) {
			t.Fatalf("short restart = %#v, long restart = %#v", short, long)
		}
	})

	t.Run("lifecycle validation", func(t *testing.T) {
		pairs := []struct {
			short []string
			long  []string
		}{
			{[]string{"init", "-j", "extra"}, []string{"init", "--json", "extra"}},
			{[]string{"down", "-j", "extra"}, []string{"down", "--json", "extra"}},
			{[]string{"stop", "-j"}, []string{"stop", "--json"}},
			{[]string{"shutdown", "-j", "extra"}, []string{"shutdown", "--json", "extra"}},
		}
		for _, pair := range pairs {
			short := flagAliasParityRunRoot(t, pair.short)
			long := flagAliasParityRunRoot(t, pair.long)
			if short.err != long.err || short.exitCode != long.exitCode || short.stdout != long.stdout || short.stderr != long.stderr {
				t.Fatalf("short validation = %#v, long validation = %#v", short, long)
			}
			if short.err == "" || short.exitCode != 1 {
				t.Fatalf("validation unexpectedly succeeded: %#v", short)
			}
		}
	})
}

type flagAliasLifecycleRunSnapshot struct {
	name       string
	source     string
	cwd        string
	argv       []string
	state      string
	jsonOutput bool
	exitCode   int
}

func flagAliasLifecycleRun(t *testing.T, variant string, optionArgs ...string) flagAliasLifecycleRunSnapshot {
	t.Helper()
	var snapshot flagAliasLifecycleRunSnapshot
	t.Run(variant, func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		marker := filepath.Join(t.TempDir(), "stream")
		argv := cliServeRunFixtureArgv("stream", marker)
		args := append([]string{"run", "alias-run"}, optionArgs...)
		args = append(args, "--")
		args = append(args, argv...)

		stdout, stderr, err := cliServeRunInvokeForTest(args...)
		snapshot.exitCode = cliServeRunExitCode(err)
		if snapshot.exitCode != 0 || stderr != "" {
			t.Fatalf("run %v = stdout %q stderr %q err %v", optionArgs, stdout, stderr, err)
		}
		name, pid, cursor, err := cliServeRunDecodeProcessSummary([]byte(stdout))
		if err != nil || name != "alias-run" || pid <= 0 || cursor < 0 {
			t.Fatalf("decode run result %q: name=%q pid=%d cursor=%d err=%v", stdout, name, pid, cursor, err)
		}
		snapshot.name = name
		snapshot.jsonOutput = true
		if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
			t.Fatalf("wait for run fixture: %v", err)
		}

		paths := daemon.NewRuntimePaths(runtimeDir)
		client, err := daemon.Dial(context.Background(), paths.Socket)
		if err != nil {
			t.Fatalf("dial daemon: %v", err)
		}
		items, err := client.List(context.Background(), daemon.ListRequest{Cwd: mustWorkingDirectory(t)})
		_ = client.Close()
		if err != nil {
			t.Fatalf("list daemon processes: %v", err)
		}
		for _, item := range items {
			if item.Name == "alias-run" {
				snapshot.source = fmt.Sprint(item.Source)
				snapshot.cwd = item.Cwd
				snapshot.argv = append([]string(nil), item.Argv...)
				snapshot.state = fmt.Sprint(item.State)
				break
			}
		}
		if snapshot.source == "" || !reflect.DeepEqual(snapshot.argv, argv) {
			t.Fatalf("daemon request snapshot = %#v, want argv %#v", snapshot, argv)
		}
		snapshot.argv[len(snapshot.argv)-1] = "<marker>"
		if err := cliServeRunStop(t, "alias-run"); err != nil {
			t.Fatalf("stop run fixture: %v", err)
		}
		if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
			t.Fatalf("wait for run fixture termination: %v", err)
		}
	})
	return snapshot
}

type flagAliasLifecycleServeSnapshot struct {
	stdoutEmpty bool
	listening   bool
	exitCode    int
}

func flagAliasLifecycleServe(t *testing.T, variant, flag string) flagAliasLifecycleServeSnapshot {
	t.Helper()
	var snapshot flagAliasLifecycleServeSnapshot
	t.Run(variant, func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		t.Cleanup(func() {
			_, _, _ = cliServeRunInvokeForTest("shutdown", "--stop-processes")
		})
		stdout, stderr, err := cliServeRunInvokeForTest("serve", flag)
		pid := cliServeRunPID(stderr)
		if pid > 0 {
			t.Cleanup(func() {
				_, _, _ = cliServeRunInvokeForTest("shutdown", "--stop-processes")
				if waitErr := flagAliasLifecycleWaitForProcessExit(pid); waitErr != nil {
					t.Errorf("serve PID %d did not exit during cleanup: %v", pid, waitErr)
				}
			})
		}
		snapshot = flagAliasLifecycleServeSnapshot{
			stdoutEmpty: stdout == "",
			listening:   pid > 0,
			exitCode:    cliServeRunExitCode(err),
		}
		if !snapshot.stdoutEmpty || !snapshot.listening || snapshot.exitCode != 0 {
			t.Fatalf("serve %s = stdout %q stderr %q err %v", flag, stdout, stderr, err)
		}
		paths := daemon.NewRuntimePaths(runtimeDir)
		if err := cliServeRunWaitForDaemon(paths.Socket); err != nil {
			t.Fatalf("wait for serve daemon: %v", err)
		}
		if _, _, err := cliServeRunInvokeForTest("shutdown", "--stop-processes"); err != nil {
			t.Fatalf("shutdown serve daemon: %v", err)
		}
		if err := flagAliasLifecycleWaitForProcessExit(pid); err != nil {
			t.Fatalf("wait for serve PID %d: %v", pid, err)
		}
	})
	return snapshot
}

type flagAliasLifecycleInitSnapshot struct {
	pathBase    string
	outcome     string
	nextCommand string
	candidates  []initCandidateJSON
	exitCode    int
	stderr      string
}

func flagAliasLifecycleInit(t *testing.T, variant, flag string) flagAliasLifecycleInitSnapshot {
	t.Helper()
	var snapshot flagAliasLifecycleInitSnapshot
	t.Run(variant, func(t *testing.T) {
		root := stopShutdownTestProject(t)
		initTestRuntime(t)
		writeDiscoveredBin(t, root)
		stdout, stderr, err := stopShutdownRun(t, "init", flag)
		var result initJSON
		if decodeErr := json.Unmarshal([]byte(stdout), &result); decodeErr != nil {
			t.Fatalf("decode init output %q: %v", stdout, decodeErr)
		}
		snapshot = flagAliasLifecycleInitSnapshot{
			pathBase:    filepath.Base(result.Path),
			outcome:     string(result.Outcome),
			nextCommand: result.NextCommand,
			candidates:  result.Candidates,
			exitCode:    initCLIExitCode(err),
			stderr:      stderr,
		}
		if snapshot.exitCode != 0 || snapshot.stderr != "" {
			t.Fatalf("init %s = %#v", flag, snapshot)
		}
	})
	return snapshot
}

type flagAliasLifecycleShutdownSnapshot struct {
	status   string
	exitCode int
	stderr   string
}

func flagAliasLifecycleShutdown(t *testing.T, variant, flag string) flagAliasLifecycleShutdownSnapshot {
	t.Helper()
	var snapshot flagAliasLifecycleShutdownSnapshot
	t.Run(variant, func(t *testing.T) {
		_, runtimeDir := stopShutdownTestServer(t, 100*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		stdout, stderr, err := stopShutdownRun(t, "shutdown", flag)
		var result shutdownResult
		if decodeErr := json.Unmarshal([]byte(stdout), &result); decodeErr != nil {
			t.Fatalf("decode shutdown output %q: %v", stdout, decodeErr)
		}
		snapshot = flagAliasLifecycleShutdownSnapshot{
			status:   result.Status,
			exitCode: waitCLIExitCode(err),
			stderr:   stderr,
		}
		if snapshot.status != "stopped" || snapshot.exitCode != 0 || snapshot.stderr != "" {
			t.Fatalf("shutdown %s = %#v", flag, snapshot)
		}
	})
	return snapshot
}

type flagAliasLifecycleActionSnapshot struct {
	name     string
	outcome  string
	source   string
	argv     []string
	statuses []flagAliasLifecycleNameStatus
	restarts int
	exitCode int
	stderr   string
}

type flagAliasLifecycleNameStatus struct {
	name   string
	status string
}

func flagAliasLifecycleManifest(t *testing.T, variant, command, timeoutFlag, jsonFlag string) flagAliasLifecycleActionSnapshot {
	t.Helper()
	var snapshot flagAliasLifecycleActionSnapshot
	t.Run(variant, func(t *testing.T) {
		root := stopShutdownTestProject(t)
		_, runtimeDir := stopShutdownTestServer(t, 100*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		argv := []string{"/bin/sh", "-c", "sleep 30"}
		writeManifestCLITestFile(t, root, "version: 1\nprocesses:\n  web:\n    argv: [/bin/sh, -c, \"sleep 30\"]\n    ready:\n      match: never-seen\n")
		args := []string{command, timeoutFlag, "10ms", jsonFlag}
		if command == "start" {
			args = append(args, "web")
		}
		startedAt := time.Now()
		stdout, stderr, err := stopShutdownRun(t, args...)
		if elapsed := time.Since(startedAt); elapsed > 2*time.Second {
			t.Fatalf("%s %s ignored 10ms timeout; elapsed %s", command, timeoutFlag, elapsed)
		}
		results := manifestCLILaunchResults(t, stdout)
		if len(results) != 1 {
			t.Fatalf("%s %v returned %d results: %q", command, args, len(results), stdout)
		}
		snapshot = flagAliasLifecycleActionSnapshot{
			name:     results[0].Name,
			outcome:  results[0].Outcome,
			source:   results[0].Source,
			argv:     append([]string(nil), results[0].Argv...),
			exitCode: manifestCLIExitCode(err),
			stderr:   stderr,
		}
		if snapshot.name != "web" || snapshot.outcome != "timed_out" || snapshot.source != "manifest" || !reflect.DeepEqual(snapshot.argv, argv) || snapshot.exitCode != 2 || snapshot.stderr != "" {
			t.Fatalf("%s %v = %#v", command, args, snapshot)
		}
	})
	return snapshot
}

func flagAliasLifecycleDown(t *testing.T, variant, flag string) flagAliasLifecycleActionSnapshot {
	t.Helper()
	var snapshot flagAliasLifecycleActionSnapshot
	t.Run(variant, func(t *testing.T) {
		root := stopShutdownTestProject(t)
		_, runtimeDir := stopShutdownTestServer(t, 100*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		writeManifestCLITestFile(t, root, "version: 1\nprocesses:\n  web:\n    argv: [/bin/sh, -c, \"sleep 30\"]\n")
		if stdout, stderr, err := stopShutdownRun(t, "start", "--json", "web"); err != nil {
			t.Fatalf("start down fixture: %v (stdout=%q stderr=%q)", err, stdout, stderr)
		}
		stdout, stderr, err := stopShutdownRun(t, "down", flag)
		results := stopShutdownDecodeResults(t, stdout)
		snapshot.statuses = flagAliasLifecycleStatuses(results)
		snapshot.exitCode = waitCLIExitCode(err)
		snapshot.stderr = stderr
		if !reflect.DeepEqual(snapshot.statuses, []flagAliasLifecycleNameStatus{{name: "web", status: "stopped"}}) || snapshot.exitCode != 0 || snapshot.stderr != "" {
			t.Fatalf("down %s = %#v", flag, snapshot)
		}
	})
	return snapshot
}

func flagAliasLifecycleStop(t *testing.T, variant, flag string) flagAliasLifecycleActionSnapshot {
	t.Helper()
	var snapshot flagAliasLifecycleActionSnapshot
	t.Run(variant, func(t *testing.T) {
		root := stopShutdownTestProject(t)
		server, runtimeDir := stopShutdownTestServer(t, 100*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		stopShutdownStartProcess(t, server, root, "api", []string{"/bin/sh", "-c", "sleep 30"})
		stdout, stderr, err := stopShutdownRun(t, "stop", "api", flag)
		results := stopShutdownDecodeResults(t, stdout)
		snapshot.statuses = flagAliasLifecycleStatuses(results)
		snapshot.exitCode = waitCLIExitCode(err)
		snapshot.stderr = stderr
		if !reflect.DeepEqual(snapshot.statuses, []flagAliasLifecycleNameStatus{{name: "api", status: "stopped"}}) || snapshot.exitCode != 0 || snapshot.stderr != "" || len(stopShutdownListActive(t, server, root)) != 0 {
			t.Fatalf("stop %s = %#v", flag, snapshot)
		}
	})
	return snapshot
}

func flagAliasLifecycleRestart(t *testing.T, variant, flag string) flagAliasLifecycleActionSnapshot {
	t.Helper()
	var snapshot flagAliasLifecycleActionSnapshot
	t.Run(variant, func(t *testing.T) {
		root := stopShutdownTestProject(t)
		server, runtimeDir := stopShutdownTestServer(t, 100*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		argv := []string{"/bin/sh", "-c", "sleep 30"}
		stopShutdownStartProcess(t, server, root, "api", argv)
		stdout, stderr, err := stopShutdownRun(t, "restart", "api", flag)
		var result restartResult
		if decodeErr := json.Unmarshal([]byte(stdout), &result); decodeErr != nil {
			t.Fatalf("decode restart output %q: %v", stdout, decodeErr)
		}
		snapshot = flagAliasLifecycleActionSnapshot{
			name:     result.Name,
			source:   result.Source,
			argv:     append([]string(nil), result.Argv...),
			restarts: result.Restarts,
			exitCode: waitCLIExitCode(err),
			stderr:   stderr,
		}
		if snapshot.name != "api" || snapshot.restarts != 1 || snapshot.exitCode != 0 || snapshot.stderr != "" {
			t.Fatalf("restart %s = %#v", flag, snapshot)
		}
	})
	return snapshot
}

func flagAliasLifecycleStatuses(results []stopShutdownJSONResult) []flagAliasLifecycleNameStatus {
	statuses := make([]flagAliasLifecycleNameStatus, len(results))
	for index, result := range results {
		statuses[index] = flagAliasLifecycleNameStatus{name: result.Name, status: result.Status}
	}
	return statuses
}

func flagAliasLifecycleWaitForProcessExit(pid int) error {
	return cliServeRunWaitForCondition(func() bool {
		err := syscall.Kill(pid, 0)
		return errors.Is(err, syscall.ESRCH)
	})
}
