package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRestartCLIJSONAndHumanResults(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	firstAlpha := stopShutdownStartProcess(t, server, projectRoot, "alpha", []string{"/bin/sh", "-c", "sleep 30"})
	firstBeta := stopShutdownStartProcess(t, server, projectRoot, "beta", []string{"/bin/sh", "-c", "sleep 30"})
	stdout, stderr, err := stopShutdownRun(t, "restart", "--json", "alpha", "beta")
	if err != nil || stderr != "" {
		t.Fatalf("restart --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	lines := stopShutdownNonEmptyLines(stdout)
	if len(lines) != 2 {
		t.Fatalf("restart result count = %d, want 2: %q", len(lines), stdout)
	}
	for index, line := range lines {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatalf("decode result %d: %v", index, err)
		}
		for _, field := range []string{"name", "outcome", "readiness", "pid", "launch_cursor"} {
			if raw[field] == nil {
				t.Fatalf("restart result %d omits %q: %#v", index, field, raw)
			}
		}
		var result restartOutputResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatal(err)
		}
		wantName := []string{"alpha", "beta"}[index]
		oldPID := []int{firstAlpha.PID, firstBeta.PID}[index]
		if result.Name != wantName || result.PID == oldPID || result.Restarts != 1 || result.Outcome != "running_unverified" || result.Readiness != "running_unverified" {
			t.Fatalf("restart result %d = %#v, old PID %d", index, result, oldPID)
		}
	}

	stdout, stderr, err = stopShutdownRun(t, "restart", "alpha")
	if err != nil || stderr != "" {
		t.Fatalf("restart human: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "alpha running_unverified pid=") || !strings.Contains(stdout, "restarts=2 launch_cursor=") || !strings.Contains(stdout, "readiness=running_unverified") {
		t.Fatalf("restart human output = %q", stdout)
	}
}

func TestRestartCLIMissingMiddleStopsProcessing(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	first := stopShutdownStartProcess(t, server, projectRoot, "first", []string{"/bin/sh", "-c", "sleep 30"})
	trailing := stopShutdownStartProcess(t, server, projectRoot, "trailing", []string{"/bin/sh", "-c", "sleep 30"})
	stdout, stderr, err := stopShutdownRun(t, "restart", "--json", "first", "missing", "trailing")
	if err == nil || manifestCLIExitCode(err) != 1 {
		t.Fatalf("restart with missing middle name: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}

	lines := stopShutdownNonEmptyLines(stdout)
	if len(lines) != 2 {
		t.Fatalf("restart result count = %d, want first success and request error: %q", len(lines), stdout)
	}
	var result restartOutputResult
	if err := json.Unmarshal([]byte(lines[0]), &result); err != nil {
		t.Fatalf("decode first restart result: %v (%q)", err, lines[0])
	}
	if result.Name != "first" || result.PID == first.PID || result.Restarts != 1 {
		t.Fatalf("first restart result = %#v, original PID %d", result, first.PID)
	}
	var failure restartOutputResult
	if err := json.Unmarshal([]byte(lines[1]), &failure); err != nil {
		t.Fatalf("decode request error result: %v (%q)", err, lines[1])
	}
	if failure.Name != "missing" || failure.Outcome != "error" || failure.Message == "" {
		t.Fatalf("request error result = %#v", failure)
	}

	var firstPID, trailingPID int
	var firstRestarts, trailingRestarts int
	for _, process := range stopShutdownListActive(t, server, projectRoot) {
		switch process.Name {
		case "first":
			firstPID = process.PID
			firstRestarts = process.RestartCount
		case "trailing":
			trailingPID = process.PID
			trailingRestarts = process.RestartCount
		}
	}
	if firstPID == 0 || firstPID == first.PID || firstRestarts != 1 {
		t.Fatalf("first process after restart = pid %d restarts %d, want new PID and one restart", firstPID, firstRestarts)
	}
	if trailingPID != trailing.PID || trailingRestarts != 0 {
		t.Fatalf("trailing process after missing middle name = pid %d restarts %d, want original PID %d and zero restarts", trailingPID, trailingRestarts, trailing.PID)
	}
}

func TestRestartCLIInvalidMissingAndUnavailable(t *testing.T) {
	t.Run("invalid and missing", func(t *testing.T) {
		_ = stopShutdownTestProject(t)
		_, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		if _, _, err := stopShutdownRun(t, "restart", "bad name"); err == nil {
			t.Fatal("invalid restart succeeded")
		}
		if _, _, err := stopShutdownRun(t, "restart", "missing"); err == nil {
			t.Fatal("missing restart succeeded")
		}

	})

	t.Run("unavailable daemon", func(t *testing.T) {
		runtimeDir := filepath.Join(t.TempDir(), "runtime")
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		stdout, stderr, err := stopShutdownRun(t, "restart", "api")
		if err == nil || stdout != "" || stderr != "" || err.Error() != logsUnavailableMessage {
			t.Fatalf("unavailable restart: err=%v stdout=%q stderr=%q", err, stdout, stderr)
		}
		if _, statErr := os.Stat(runtimeDir); !os.IsNotExist(statErr) {
			t.Fatalf("unavailable restart created runtime state: %v", statErr)
		}
	})
}

func TestRestartWaitsForReadiness(t *testing.T) {
	tests := []struct {
		name       string
		manifest   string
		restartArg []string
		wantOut    string
		wantReady  string
		wantCode   int
	}{
		{
			name: "ready",
			manifest: `version: 1
processes:
  ready:
    argv: [/bin/sh, -c, "printf ready; sleep 30"]
    ready: {match: ready}
`,
			restartArg: []string{"--timeout", "1s", "ready"},
			wantOut:    "restarted",
			wantReady:  "ready",
		},
		{
			name: "running_unverified",
			manifest: `version: 1
processes:
  unverified:
    argv: [/bin/sh, -c, "sleep 30"]
`,
			restartArg: []string{"unverified"},
			wantOut:    "running_unverified",
			wantReady:  "running_unverified",
		},
		{
			name: "exited_before_ready",
			manifest: `version: 1
processes:
  exited:
    argv: [/bin/sh, -c, "sleep 0.1; exit 7"]
    ready: {match: never-seen}
`,
			restartArg: []string{"--timeout", "1s", "exited"},
			wantOut:    "exited_before_ready",
			wantReady:  "",
			wantCode:   3,
		},
		{
			name: "timed_out",
			manifest: `version: 1
processes:
  slow:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: never-seen}
`,
			restartArg: []string{"--timeout", "15ms", "slow"},
			wantOut:    "timed_out",
			wantReady:  "starting",
			wantCode:   2,
		},
		{
			name: "no_wait",
			manifest: `version: 1
processes:
  nowait:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: never-seen}
`,
			restartArg: []string{"--no-wait", "nowait"},
			wantOut:    "restarted",
			wantReady:  "starting",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := stopShutdownTestProject(t)
			_, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			writeManifestCLITestFile(t, root, test.manifest)
			name := test.restartArg[len(test.restartArg)-1]
			if stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", name); err != nil || stderr != "" {
				t.Fatalf("seed %s: err=%v stdout=%q stderr=%q", name, err, stdout, stderr)
			}
			startedAt := time.Now()
			stdout, stderr, err := stopShutdownRun(t, append([]string{"restart", "--json"}, test.restartArg...)...)
			if got := manifestCLIExitCode(err); got != test.wantCode {
				t.Fatalf("restart code = %d, want %d (err=%v stdout=%q stderr=%q)", got, test.wantCode, err, stdout, stderr)
			}
			if stderr != "" {
				t.Fatalf("restart stderr = %q", stderr)
			}
			lines := stopShutdownNonEmptyLines(stdout)
			if len(lines) != 1 {
				t.Fatalf("restart lines = %d, want one: %q", len(lines), stdout)
			}
			var result restartOutputResult
			if decodeErr := json.Unmarshal([]byte(lines[0]), &result); decodeErr != nil {
				t.Fatalf("decode restart result %q: %v", lines[0], decodeErr)
			}
			if result.Name != name || result.Outcome != test.wantOut || result.Readiness != test.wantReady || result.PID == 0 && test.wantOut != "exited_before_ready" {
				t.Fatalf("restart result = %#v, want name=%s outcome=%s readiness=%s", result, name, test.wantOut, test.wantReady)
			}
			if test.name == "no_wait" && time.Since(startedAt) > 500*time.Millisecond {
				t.Fatalf("--no-wait took too long: %s", time.Since(startedAt))
			}
		})
	}

	t.Run("timeout starts independently for each launch", func(t *testing.T) {
		root := stopShutdownTestProject(t)
		_, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		writeManifestCLITestFile(t, root, `version: 1
processes:
  first:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: never-seen}
  second:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: never-seen}
`)
		for _, name := range []string{"first", "second"} {
			if stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", name); err != nil || stderr != "" {
				t.Fatalf("seed %s: err=%v stdout=%q stderr=%q", name, err, stdout, stderr)
			}
		}
		startedAt := time.Now()
		stdout, stderr, err := stopShutdownRun(t, "restart", "--json", "--timeout", "20ms", "first", "second")
		if manifestCLIExitCode(err) != 2 || stderr != "" {
			t.Fatalf("independent timeout: code=%d err=%v stdout=%q stderr=%q", manifestCLIExitCode(err), err, stdout, stderr)
		}
		lines := stopShutdownNonEmptyLines(stdout)
		if len(lines) != 2 || time.Since(startedAt) < 30*time.Millisecond {
			t.Fatalf("independent timeout lines=%d elapsed=%s stdout=%q", len(lines), time.Since(startedAt), stdout)
		}
		for index, line := range lines {
			var result restartOutputResult
			if err := json.Unmarshal([]byte(line), &result); err != nil {
				t.Fatal(err)
			}
			if result.Name != []string{"first", "second"}[index] || result.Outcome != "timed_out" {
				t.Fatalf("independent result %d = %#v", index, result)
			}
		}
	})

	t.Run("retained matcher after declaration removal", func(t *testing.T) {
		root := stopShutdownTestProject(t)
		_, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		writeManifestCLITestFile(t, root, `version: 1
processes:
  removed:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: ""}
`)
		if stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "removed"); err != nil || stderr != "" {
			t.Fatalf("seed removed: err=%v stdout=%q stderr=%q", err, stdout, stderr)
		}
		writeManifestCLITestFile(t, root, "version: 1\nprocesses: {}\n")
		startedAt := time.Now()
		stdout, stderr, err := stopShutdownRun(t, "restart", "--json", "--timeout", "20ms", "removed")
		if manifestCLIExitCode(err) != 2 || stderr != "" {
			t.Fatalf("retained matcher restart: code=%d err=%v stdout=%q stderr=%q", manifestCLIExitCode(err), err, stdout, stderr)
		}
		var result restartOutputResult
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); err != nil {
			t.Fatalf("decode retained matcher result: %v (%q)", err, stdout)
		}
		if result.Outcome != "timed_out" || result.Readiness != "starting" || time.Since(startedAt) < 15*time.Millisecond {
			t.Fatalf("retained matcher result=%#v elapsed=%s", result, time.Since(startedAt))
		}
	})
}

func TestRestartMixedOutcomes(t *testing.T) {
	t.Run("readiness failures continue and preserve order", func(t *testing.T) {
		root := stopShutdownTestProject(t)
		_, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		writeManifestCLITestFile(t, root, `version: 1
processes:
  early:
    argv: [/bin/sh, -c, "sleep 0.1; exit 7"]
    ready: {match: never-seen}
  timeout:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: never-seen}
  ready:
    argv: [/bin/sh, -c, "printf ready; sleep 30"]
    ready: {match: ready}
`)
		for _, name := range []string{"early", "timeout", "ready"} {
			if stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", name); err != nil || stderr != "" {
				t.Fatalf("seed %s: err=%v stdout=%q stderr=%q", name, err, stdout, stderr)
			}
		}
		stdout, stderr, err := stopShutdownRun(t, "restart", "--json", "--timeout", "500ms", "early", "timeout", "ready")
		if manifestCLIExitCode(err) != 3 || stderr != "" {
			t.Fatalf("mixed readiness exit: code=%d err=%v stdout=%q stderr=%q", manifestCLIExitCode(err), err, stdout, stderr)
		}
		lines := stopShutdownNonEmptyLines(stdout)
		if len(lines) != 3 {
			t.Fatalf("mixed readiness results = %d, want 3: %q", len(lines), stdout)
		}
		want := []struct{ name, outcome string }{{"early", "exited_before_ready"}, {"timeout", "timed_out"}, {"ready", "restarted"}}
		for index, line := range lines {
			var result restartOutputResult
			if err := json.Unmarshal([]byte(line), &result); err != nil {
				t.Fatal(err)
			}
			if result.Name != want[index].name || result.Outcome != want[index].outcome {
				t.Fatalf("mixed readiness result %d = %#v, want %#v", index, result, want[index])
			}
		}
	})

	t.Run("request error stops later names and wins precedence", func(t *testing.T) {
		root := stopShutdownTestProject(t)
		server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		writeManifestCLITestFile(t, root, `version: 1
processes:
  early:
    argv: [/bin/sh, -c, "sleep 0.1; exit 7"]
    ready: {match: never-seen}
  later:
    argv: [/bin/sh, -c, "sleep 30"]
`)
		for _, name := range []string{"early", "later"} {
			if stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", name); err != nil || stderr != "" {
				t.Fatalf("seed %s: err=%v stdout=%q stderr=%q", name, err, stdout, stderr)
			}
		}
		before := stopShutdownListActive(t, server, root)
		var laterPID int
		for _, process := range before {
			if process.Name == "later" {
				laterPID = process.PID
			}
		}
		stdout, stderr, err := stopShutdownRun(t, "restart", "--json", "--timeout", "500ms", "early", "missing", "later")
		if manifestCLIExitCode(err) != 1 || stderr != "" {
			t.Fatalf("request precedence: code=%d err=%v stdout=%q stderr=%q", manifestCLIExitCode(err), err, stdout, stderr)
		}
		lines := stopShutdownNonEmptyLines(stdout)
		if len(lines) != 2 {
			t.Fatalf("request-stop results = %d, want 2: %q", len(lines), stdout)
		}
		var early, failure restartOutputResult
		if err := json.Unmarshal([]byte(lines[0]), &early); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(lines[1]), &failure); err != nil {
			t.Fatal(err)
		}
		if early.Name != "early" || early.Outcome != "exited_before_ready" || failure.Name != "missing" || failure.Outcome != "error" || failure.Message == "" {
			t.Fatalf("request-stop results = %#v, %#v", early, failure)
		}
		for _, process := range stopShutdownListActive(t, server, root) {
			if process.Name == "later" && process.PID != laterPID {
				t.Fatalf("later process changed after request error: before=%d after=%d", laterPID, process.PID)
			}
		}
	})

	t.Run("timeout and success precedence", func(t *testing.T) {
		root := stopShutdownTestProject(t)
		_, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		writeManifestCLITestFile(t, root, `version: 1
processes:
  timeout:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: never-seen}
  success:
    argv: [/bin/sh, -c, "printf ready; sleep 30"]
    ready: {match: ready}
`)
		for _, name := range []string{"timeout", "success"} {
			if stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", name); err != nil || stderr != "" {
				t.Fatalf("seed %s: err=%v stdout=%q stderr=%q", name, err, stdout, stderr)
			}
		}
		stdout, stderr, err := stopShutdownRun(t, "restart", "--json", "--timeout", "500ms", "timeout", "success")
		if manifestCLIExitCode(err) != 2 || stderr != "" {
			t.Fatalf("timeout precedence: code=%d err=%v stdout=%q stderr=%q", manifestCLIExitCode(err), err, stdout, stderr)
		}
		if len(stopShutdownNonEmptyLines(stdout)) != 2 {
			t.Fatalf("timeout precedence output = %q", stdout)
		}
	})
}
