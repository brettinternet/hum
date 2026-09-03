//go:build darwin || linux

package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hum/internal/testutil"
)

const zeroConfigWorkflowTimeout = 15 * time.Second

type zeroConfigLaunchResult struct {
	Name         string          `json:"name"`
	Outcome      string          `json:"outcome"`
	Source       string          `json:"source"`
	Argv         []string        `json:"argv"`
	PID          *int            `json:"pid,omitempty"`
	LaunchCursor *uint64         `json:"launch_cursor,omitempty"`
	Readiness    string          `json:"readiness,omitempty"`
	ReadyCursor  *uint64         `json:"ready_cursor,omitempty"`
	Error        json.RawMessage `json:"error,omitempty"`
}

type zeroConfigProcess struct {
	Name         string   `json:"name"`
	Source       string   `json:"source"`
	Root         string   `json:"root"`
	PID          int      `json:"pid"`
	Cwd          string   `json:"cwd"`
	Argv         []string `json:"argv"`
	LaunchCursor uint64   `json:"launch_cursor"`
	State        string   `json:"state"`
	Readiness    string   `json:"readiness,omitempty"`
	RestartCount int      `json:"restart_count,omitempty"`
}

type zeroConfigListResponse struct {
	Processes []zeroConfigProcess `json:"processes"`
}

type zeroConfigRestartResult struct {
	Name         string   `json:"name"`
	Source       string   `json:"source"`
	Argv         []string `json:"argv"`
	PID          int      `json:"pid"`
	Restarts     int      `json:"restarts"`
	LaunchCursor uint64   `json:"launch_cursor"`
	Readiness    string   `json:"readiness,omitempty"`
}

type zeroConfigCase struct {
	name   string
	source string
	argv   []string
	setup  func(t *testing.T, root, shimDir, fixture, marker, launchLog, bodyMarker string)
}

func TestZeroConfigDiscovery(t *testing.T) {
	fixture := testutil.BuildFixture(t)
	hum := testutil.BuildHum(t)

	cases := []zeroConfigCase{
		{
			name:   "task-runner",
			source: "task",
			argv:   []string{"task", "dev"},
			setup:  setupZeroConfigTask,
		},
		{
			name:   "package-json",
			source: "package_json",
			argv:   []string{"npm", "run", "dev"},
			setup:  setupZeroConfigPackage,
		},
		{
			name:   "mix-phoenix",
			source: "mix",
			argv:   []string{"mix", "phx.server"},
			setup:  setupZeroConfigMix,
		},
		{
			name:   "bin-dev",
			source: "bin_dev",
			argv:   []string{"./bin/dev"},
			setup:  setupZeroConfigBinDev,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			runZeroConfigCase(t, hum, fixture, testCase)
		})
	}
}

func runZeroConfigCase(t *testing.T, hum, fixture string, testCase zeroConfigCase) {
	t.Helper()

	projectRoot := zeroConfigCanonicalTempDir(t)
	shimDir := t.TempDir()
	runtimeDir := testutil.RuntimeDir(t)
	marker := filepath.Join(projectRoot, "managed")
	launchLog := filepath.Join(projectRoot, "launches.log")
	bodyMarker := filepath.Join(projectRoot, "body-executed")
	testCase.setup(t, projectRoot, shimDir, fixture, marker, launchLog, bodyMarker)

	env := testutil.RuntimeEnv(
		runtimeDir,
		"PATH="+shimDir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin",
		"HUM_OUTPUT_BYTES=65536",
		"HUM_STOP_GRACE=1s",
	)
	t.Cleanup(func() {
		// Stop first so cleanup remains safe if a failed assertion left a child
		// alive; shutdown then owns daemon teardown and process-group cleanup.
		_ = testutil.Run(t, hum, projectRoot, env, "stop", "dev")
		_ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes")
	})

	// Project-aware list is an inspection path. It must resolve the one
	// candidate without executing its body or starting a daemon.
	initialHuman := testutil.Run(t, hum, projectRoot, env, "list")
	zeroConfigAssertSuccess(t, initialHuman, "initial project-aware list")
	zeroConfigAssertHumanMetadata(t, initialHuman.Stdout, "stopped", testCase.source, testCase.argv, false)
	zeroConfigAssertNoCandidateExecution(t, launchLog, bodyMarker)
	zeroConfigAssertNoDaemon(t, runtimeDir)

	initialJSON := testutil.Run(t, hum, projectRoot, env, "list", "--json")
	zeroConfigAssertSuccess(t, initialJSON, "initial JSON project-aware list")
	initialProcesses := zeroConfigDecodeList(t, initialJSON.Stdout)
	if len(initialProcesses) != 1 {
		t.Fatalf("initial list returned %d processes, want one: %s", len(initialProcesses), initialJSON.Stdout)
	}
	zeroConfigAssertProcess(t, initialProcesses[0], projectRoot, "stopped", testCase.source, testCase.argv, false)
	zeroConfigAssertNoCandidateExecution(t, launchLog, bodyMarker)
	zeroConfigAssertNoDaemon(t, runtimeDir)

	// Deliberately omit -- and the underlying command. The inferred argv is
	// supplied by the project resolver, not by this caller.
	run := testutil.Run(t, hum, projectRoot, env, "run", "--detach", "dev")
	zeroConfigAssertSuccess(t, run, "run dev without argv")
	zeroConfigAssertHumanMetadata(t, run.Stdout, "started", testCase.source, testCase.argv, true)
	testutil.WaitForFile(t, marker+".started", zeroConfigWorkflowTimeout)
	zeroConfigWaitForLaunches(t, launchLog, 1)

	runningBefore := zeroConfigDecodeSingleProcess(t, testutil.Run(t, hum, projectRoot, env, "list", "--json"), "running list")
	zeroConfigAssertProcess(t, runningBefore, projectRoot, "running", testCase.source, testCase.argv, true)
	if runningBefore.PID <= 0 || !testutil.ProcessAlive(runningBefore.PID) {
		t.Fatalf("running process = %#v, want a live PID", runningBefore)
	}

	// Start and up both reuse the same inferred definition and are idempotent
	// once the process is running; neither may create another child.
	start := testutil.Run(t, hum, projectRoot, env, "start", "dev", "--json")
	zeroConfigAssertSuccess(t, start, "idempotent start dev")
	startResult := zeroConfigDecodeLaunch(t, start.Stdout, "start dev")
	zeroConfigAssertLaunch(t, startResult, "already_running", testCase.source, testCase.argv, true)

	up := testutil.Run(t, hum, projectRoot, env, "up", "--json")
	zeroConfigAssertSuccess(t, up, "idempotent up")
	upResult := zeroConfigDecodeLaunch(t, up.Stdout, "up")
	zeroConfigAssertLaunch(t, upResult, "already_running", testCase.source, testCase.argv, true)
	zeroConfigWaitForLaunches(t, launchLog, 1)
	if upResult.PID == nil || *upResult.PID != runningBefore.PID {
		t.Fatalf("idempotent up PID = %v, want %d", upResult.PID, runningBefore.PID)
	}

	listed := zeroConfigDecodeSingleProcess(t, testutil.Run(t, hum, projectRoot, env, "list", "--json"), "post-idempotence list")
	zeroConfigAssertProcess(t, listed, projectRoot, "running", testCase.source, testCase.argv, true)
	if listed.PID != runningBefore.PID || listed.LaunchCursor != runningBefore.LaunchCursor {
		t.Fatalf("idempotent commands replaced process: before=%#v after=%#v", runningBefore, listed)
	}
	postHuman := testutil.Run(t, hum, projectRoot, env, "list")
	zeroConfigAssertSuccess(t, postHuman, "post-idempotence human list")
	zeroConfigAssertHumanMetadata(t, postHuman.Stdout, "running", testCase.source, testCase.argv, true)

	// Restart is the one intentional second launch. It must use the daemon's
	// recorded inferred argv rather than requiring the caller to repeat it.
	restart := testutil.Run(t, hum, projectRoot, env, "restart", "dev", "--json")
	zeroConfigAssertSuccess(t, restart, "restart dev")
	var restartResult zeroConfigRestartResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(restart.Stdout)), &restartResult); err != nil {
		t.Fatalf("decode restart dev JSON: %v; output=%q", err, restart.Stdout)
	}
	if restartResult.Name != "dev" || restartResult.Source != testCase.source || !reflect.DeepEqual(restartResult.Argv, testCase.argv) {
		t.Fatalf("restart identity = %#v, want source=%q argv=%#v", restartResult, testCase.source, testCase.argv)
	}
	if restartResult.PID <= 0 || restartResult.PID == runningBefore.PID {
		t.Fatalf("restart PID = %d, want a new live PID (old %d)", restartResult.PID, runningBefore.PID)
	}
	if restartResult.Restarts < 1 {
		t.Fatalf("restart count = %d, want at least one restart", restartResult.Restarts)
	}
	if restartResult.Readiness != "running_unverified" {
		t.Fatalf("restart readiness = %q, want running_unverified", restartResult.Readiness)
	}
	zeroConfigWaitForLaunches(t, launchLog, 2)

	finalProcess := zeroConfigDecodeSingleProcess(t, testutil.Run(t, hum, projectRoot, env, "list", "--json"), "final list")
	zeroConfigAssertProcess(t, finalProcess, projectRoot, "running", testCase.source, testCase.argv, true)
	if finalProcess.PID != restartResult.PID {
		t.Fatalf("final list PID = %d, want restarted PID %d", finalProcess.PID, restartResult.PID)
	}
	if finalProcess.RestartCount < 1 {
		t.Fatalf("final list restart_count = %d, want at least one restart", finalProcess.RestartCount)
	}
	finalHuman := testutil.Run(t, hum, projectRoot, env, "list")
	zeroConfigAssertSuccess(t, finalHuman, "final human list")
	zeroConfigAssertHumanMetadata(t, finalHuman.Stdout, "running", testCase.source, testCase.argv, true)
}

func zeroConfigCanonicalTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("canonicalize project directory %q: %v", dir, err)
	}
	return filepath.Clean(canonical)
}

func setupZeroConfigTask(t *testing.T, root, shimDir, fixture, marker, launchLog, bodyMarker string) {
	t.Helper()
	writeZeroConfigFile(t, filepath.Join(root, "Taskfile.yml"), fmt.Sprintf(`version: '3'
tasks:
  dev:
    cmds:
      - touch %s
`, zeroConfigShellQuote(bodyMarker)))
	writeZeroConfigExecutable(t, filepath.Join(shimDir, "task"), fmt.Sprintf(`if [ "$#" -eq 4 ] && [ "$1" = "--dir" ] && [ "$3" = "--list-all" ] && [ "$4" = "--json" ]; then
  printf '%%s\n' '{"tasks":[{"name":"dev"}]}'
  exit 0
fi
if [ "$#" -eq 1 ] && [ "$1" = "dev" ]; then
  printf 'argv=%%s\n' "$*" >> %s
  exec %s stream %s
fi
printf 'unexpected task argv: %%s\n' "$*" >&2
exit 64
`, zeroConfigShellQuote(launchLog), zeroConfigShellQuote(fixture), zeroConfigShellQuote(marker)))
}

func setupZeroConfigPackage(t *testing.T, root, shimDir, fixture, marker, launchLog, bodyMarker string) {
	t.Helper()
	packageJSON, err := json.Marshal(map[string]any{
		"scripts": map[string]string{
			"dev": "touch " + bodyMarker,
		},
	})
	if err != nil {
		t.Fatalf("encode package.json: %v", err)
	}
	writeZeroConfigFile(t, filepath.Join(root, "package.json"), string(packageJSON)+"\n")
	writeZeroConfigExecutable(t, filepath.Join(shimDir, "npm"), fmt.Sprintf(`if [ "$#" -eq 2 ] && [ "$1" = "run" ] && [ "$2" = "dev" ]; then
  printf 'argv=%%s\n' "$*" >> %s
  exec %s stream %s
fi
printf 'unexpected npm argv: %%s\n' "$*" >&2
exit 64
`, zeroConfigShellQuote(launchLog), zeroConfigShellQuote(fixture), zeroConfigShellQuote(marker)))
}

func setupZeroConfigMix(t *testing.T, root, shimDir, fixture, marker, launchLog, bodyMarker string) {
	t.Helper()
	writeZeroConfigFile(t, filepath.Join(root, "mix.exs"), fmt.Sprintf(`File.write!(%s, "mix body executed")

defmodule ZeroConfig.MixProject do
  use Mix.Project

  def project do
    [app: :zero_config, version: "0.1.0"]
  end
end
`, zeroConfigElixirQuote(bodyMarker)))
	writeZeroConfigExecutable(t, filepath.Join(shimDir, "mix"), fmt.Sprintf(`if [ "$#" -eq 2 ] && [ "$1" = "help" ] && [ "$2" = "--names" ]; then
  printf 'mix phx.server\n'
  exit 0
fi
if [ "$#" -eq 1 ] && [ "$1" = "phx.server" ]; then
  printf 'argv=%%s\n' "$*" >> %s
  exec %s stream %s
fi
printf 'unexpected mix argv: %%s\n' "$*" >&2
exit 64
`, zeroConfigShellQuote(launchLog), zeroConfigShellQuote(fixture), zeroConfigShellQuote(marker)))
}

func setupZeroConfigBinDev(t *testing.T, root, shimDir, fixture, marker, launchLog, bodyMarker string) {
	t.Helper()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("create bin directory: %v", err)
	}
	writeZeroConfigExecutable(t, filepath.Join(binDir, "dev"), fmt.Sprintf(`printf 'argv=%%s\n' "$*" >> %s
exec %s stream %s
`, zeroConfigShellQuote(launchLog), zeroConfigShellQuote(fixture), zeroConfigShellQuote(marker)))
}

func writeZeroConfigFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeZeroConfigExecutable(t *testing.T, path, body string) {
	t.Helper()
	contents := "#!/bin/sh\nset -eu\n" + body
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func zeroConfigShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func zeroConfigElixirQuote(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}

func zeroConfigAssertSuccess(t *testing.T, result testutil.Result, operation string) {
	t.Helper()
	if result.Code != 0 || result.Err != nil || result.Stderr != "" {
		t.Fatalf("%s failed: code=%d err=%v stdout=%q stderr=%q", operation, result.Code, result.Err, result.Stdout, result.Stderr)
	}
}

func zeroConfigAssertNoCandidateExecution(t *testing.T, launchLog, bodyMarker string) {
	t.Helper()
	if lines := zeroConfigReadLaunches(t, launchLog); len(lines) != 0 {
		t.Fatalf("candidate execution during inspection: launch log=%#v", lines)
	}
	if _, err := os.Stat(bodyMarker); err == nil {
		t.Fatalf("candidate body executed during inspection: marker=%q", bodyMarker)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspect candidate body marker %q: %v", bodyMarker, err)
	}
}

func zeroConfigAssertNoDaemon(t *testing.T, runtimeDir string) {
	t.Helper()
	for _, name := range []string{"hum.sock", "hum.pid", "hum.ready"} {
		path := filepath.Join(runtimeDir, name)
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("inspection started daemon artifact %q", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inspect daemon artifact %q: %v", path, err)
		}
	}
}

func zeroConfigAssertHumanMetadata(t *testing.T, output, state, source string, argv []string, running bool) {
	t.Helper()
	if !strings.Contains(output, "dev") {
		t.Fatalf("human output %q omitted process name", output)
	}
	if !strings.Contains(output, state) {
		t.Fatalf("human output %q omitted state %q", output, state)
	}
	if !strings.Contains(output, source) {
		t.Fatalf("human output %q omitted source %q", output, source)
	}
	for _, arg := range argv {
		if !strings.Contains(output, arg) {
			t.Fatalf("human output %q omitted argv element %q", output, arg)
		}
	}
	if running && !strings.Contains(output, "running_unverified") {
		t.Fatalf("human output %q omitted running_unverified readiness", output)
	}
}

func zeroConfigAssertProcess(t *testing.T, process zeroConfigProcess, root, state, source string, argv []string, running bool) {
	t.Helper()
	if process.Name != "dev" || process.Source != source || process.Root != root || process.Cwd != root {
		t.Fatalf("process identity = %#v, want name=dev source=%q root/cwd=%q", process, source, root)
	}
	if !reflect.DeepEqual(process.Argv, argv) {
		t.Fatalf("process argv = %#v, want %#v", process.Argv, argv)
	}
	if process.State != state {
		t.Fatalf("process state = %q, want %q", process.State, state)
	}
	if running {
		if process.PID <= 0 {
			t.Fatalf("running process PID = %d, want positive", process.PID)
		}
		if process.Readiness != "running_unverified" {
			t.Fatalf("process readiness = %q, want running_unverified", process.Readiness)
		}
	} else if process.PID != 0 {
		t.Fatalf("stopped process PID = %d, want zero", process.PID)
	}
}

func zeroConfigAssertLaunch(t *testing.T, result zeroConfigLaunchResult, outcome, source string, argv []string, running bool) {
	t.Helper()
	if result.Name != "dev" || result.Outcome != outcome || result.Source != source {
		t.Fatalf("launch result = %#v, want name=dev outcome=%q source=%q", result, outcome, source)
	}
	if !reflect.DeepEqual(result.Argv, argv) {
		t.Fatalf("launch argv = %#v, want %#v", result.Argv, argv)
	}
	if running {
		if result.PID == nil || *result.PID <= 0 {
			t.Fatalf("launch PID = %v, want positive", result.PID)
		}
		if result.Readiness != "running_unverified" {
			t.Fatalf("launch readiness = %q, want running_unverified", result.Readiness)
		}
	}
}

func zeroConfigDecodeLaunch(t *testing.T, output, operation string) zeroConfigLaunchResult {
	t.Helper()
	lines := make([]string, 0, 1)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("%s returned %d JSON lines, want one: %q", operation, len(lines), output)
	}
	var result zeroConfigLaunchResult
	if err := json.Unmarshal([]byte(lines[0]), &result); err != nil {
		t.Fatalf("decode %s JSON: %v; output=%q", operation, err, output)
	}
	return result
}

func zeroConfigDecodeList(t *testing.T, output string) []zeroConfigProcess {
	t.Helper()
	var response zeroConfigListResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &response); err != nil {
		t.Fatalf("decode list JSON: %v; output=%q", err, output)
	}
	return response.Processes
}

func zeroConfigDecodeSingleProcess(t *testing.T, result testutil.Result, operation string) zeroConfigProcess {
	t.Helper()
	zeroConfigAssertSuccess(t, result, operation)
	processes := zeroConfigDecodeList(t, result.Stdout)
	if len(processes) != 1 {
		t.Fatalf("%s returned %d processes, want one: %s", operation, len(processes), result.Stdout)
	}
	return processes[0]
}

func zeroConfigReadLaunches(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read launch log %q: %v", path, err)
	}
	lines := make([]string, 0)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "argv=") {
			lines = append(lines, strings.TrimSuffix(line, "\r"))
		}
	}
	return lines
}

func zeroConfigWaitForLaunches(t *testing.T, path string, want int) {
	t.Helper()
	deadline := time.Now().Add(zeroConfigWorkflowTimeout)
	for {
		lines := zeroConfigReadLaunches(t, path)
		if len(lines) > want {
			t.Fatalf("launch count = %d, want %d; log=%#v", len(lines), want, lines)
		}
		if len(lines) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d launches; got %#v", want, lines)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
