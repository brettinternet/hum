//go:build darwin || linux

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"hum/internal/daemon"
	"hum/internal/testutil"
)

type relaunchIntegrationStatus struct {
	Name         string     `json:"name"`
	Source       string     `json:"source"`
	Argv         []string   `json:"argv"`
	State        string     `json:"state"`
	Readiness    string     `json:"readiness"`
	LaunchCursor uint64     `json:"launch_cursor"`
	Restart      string     `json:"restart"`
	Relaunches   int        `json:"relaunches"`
	NextLaunchAt *time.Time `json:"next_launch_at"`
	ExitStatus   *int       `json:"exit_status"`
}

type upRecoveryIntegrationResult struct {
	Name         string     `json:"name"`
	Outcome      string     `json:"outcome"`
	Source       string     `json:"source"`
	Argv         []string   `json:"argv"`
	State        string     `json:"state"`
	Readiness    string     `json:"readiness"`
	LaunchCursor uint64     `json:"launch_cursor"`
	Restart      string     `json:"restart"`
	Relaunches   int        `json:"relaunches"`
	NextLaunchAt *time.Time `json:"next_launch_at"`
}

type mcpRecoveryReadiness struct {
	State string `json:"state"`
	Match string `json:"match"`
}

type mcpUpRecoveryIntegrationProcess struct {
	Name         string                `json:"name"`
	Source       string                `json:"source"`
	Argv         []string              `json:"argv"`
	State        string                `json:"state"`
	Readiness    *mcpRecoveryReadiness `json:"readiness"`
	LaunchCursor uint64                `json:"launch_cursor"`
	Restart      string                `json:"restart"`
	Relaunches   int                   `json:"relaunches"`
	NextLaunchAt *time.Time            `json:"next_launch_at"`
}

type mcpUpRecoveryIntegrationResult struct {
	Name    string                           `json:"name"`
	Outcome string                           `json:"outcome"`
	Process *mcpUpRecoveryIntegrationProcess `json:"process"`
}

type mcpRestartIntegrationProcess struct {
	Name         string          `json:"name"`
	Source       string          `json:"source"`
	Argv         []string        `json:"argv"`
	State        string          `json:"state"`
	LaunchCursor uint64          `json:"launch_cursor"`
	Restart      string          `json:"restart"`
	Relaunches   int             `json:"relaunches"`
	NextLaunchAt *time.Time      `json:"next_launch_at"`
	Readiness    json.RawMessage `json:"readiness"`
}

type relaunchIntegrationListProcess struct {
	Name         string     `json:"name"`
	Restart      string     `json:"restart"`
	Relaunches   int        `json:"relaunches"`
	NextLaunchAt *time.Time `json:"next_launch_at"`
}

type relaunchIntegrationList struct {
	Processes []relaunchIntegrationListProcess `json:"processes"`
}

func TestRelaunchAfterCrash(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	fixture := integrationFixture(t)
	project := t.TempDir()
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir, "HUM_OUTPUT_BYTES=65536", "HUM_COMPLETED_RECORDS=20", "HUM_STOP_GRACE=1s")
	marker := filepath.Join(project, "launches")
	if err := os.WriteFile(filepath.Join(project, "hum.yaml"), []byte(`version: 1
processes:
  crash:
    argv: ["`+fixture+`", relaunch, "`+marker+`"]
    ready:
      match: "ready"
      timeout: 5s
    restart: on-failure
  never:
    argv: [/bin/sh, -c, "exit 1"]
`), 0o600); err != nil {
		t.Fatal(err)
	}

	follower := testutil.Start(t, hum, project, env, "logs", "crash", "--follow", "--json", "--stream", "both")
	t.Cleanup(func() {
		if !follower.Exited() {
			_ = follower.Signal(os.Interrupt)
			_ = follower.Wait(10 * time.Second)
		}
		shutdown := testutil.Run(t, hum, project, env, "shutdown", "--stop-processes", "--json")
		if shutdown.Code != 0 {
			t.Logf("shutdown cleanup: code=%d stdout=%q stderr=%q", shutdown.Code, shutdown.Stdout, shutdown.Stderr)
		}
	})

	up := testutil.Run(t, hum, project, env, "up", "--json")
	if up.Code != 3 || up.Err == nil || up.Stderr != "" {
		t.Fatalf("first up = code %d err=%v stdout=%q stderr=%q, want early exit code 3", up.Code, up.Err, up.Stdout, up.Stderr)
	}
	if !strings.Contains(up.Stdout, `"outcome":"exited_before_ready"`) {
		t.Fatalf("first up result = %q, want exited_before_ready", up.Stdout)
	}
	logsitWaitFollowerText(t, follower, "waiting for next launch")

	pending := relaunchIntegrationWaitStatus(t, hum, project, env, "crash", func(status relaunchIntegrationStatus) bool {
		return status.State == "exited" && status.Restart == "on-failure" && status.Relaunches == 0 && status.NextLaunchAt != nil
	})
	if pending.NextLaunchAt == nil {
		t.Fatal("pending status omitted next_launch_at")
	}
	statusResult := testutil.Run(t, hum, project, env, "status", "crash", "--json")
	if statusResult.Code != 0 {
		t.Fatalf("pending status command: code=%d stdout=%q stderr=%q", statusResult.Code, statusResult.Stdout, statusResult.Stderr)
	}

	ready := relaunchIntegrationWaitStatus(t, hum, project, env, "crash", func(status relaunchIntegrationStatus) bool {
		return status.State == "running" && status.Readiness == "ready" && status.Restart == "on-failure" && status.Relaunches == 2 && status.NextLaunchAt == nil
	})
	if ready.Relaunches != 2 {
		t.Fatalf("ready status = %#v, want two completed automatic relaunches", ready)
	}
	logs := testutil.Run(t, hum, project, env, "logs", "crash", "--json", "--stream", "both")
	if logs.Code != 0 {
		t.Fatalf("bounded crash logs: code=%d stdout=%q stderr=%q", logs.Code, logs.Stdout, logs.Stderr)
	}
	lines := logsitDecodeJSONLines(t, logs.Stdout)
	joined := logs.Stdout
	for _, text := range []string{"launch-1\\n", "launch-2\\n", "launch-3\\n", "relaunching in 1s", "relaunching in 2s"} {
		if !strings.Contains(joined, text) {
			t.Fatalf("crash logs missing %q: %q", text, joined)
		}
	}
	if len(lines) == 0 {
		t.Fatalf("crash logs returned no JSON lines: %q", logs.Stdout)
	}
	listed := testutil.Run(t, hum, project, env, "list", "--json")
	if listed.Code != 0 {
		t.Fatalf("list after relaunch: code=%d stdout=%q stderr=%q", listed.Code, listed.Stdout, listed.Stderr)
	}
	var list relaunchIntegrationList
	if err := json.Unmarshal([]byte(listed.Stdout), &list); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, process := range list.Processes {
		if process.Name == "crash" {
			found = true
			if process.Restart != "on-failure" || process.Relaunches != 2 || process.NextLaunchAt != nil {
				t.Fatalf("list crash process = %#v", process)
			}
		}
	}
	if !found {
		t.Fatalf("list omitted crash process: %#v", list)
	}
	logsitWaitFollowerText(t, follower, "launch-3")
	if follower.Exited() {
		t.Fatalf("durable follower exited after automatic relaunch: stdout=%q stderr=%q", follower.Stdout(), follower.Stderr())
	}

	never := relaunchIntegrationWaitStatus(t, hum, project, env, "never", func(status relaunchIntegrationStatus) bool {
		return status.State == "exited" && status.ExitStatus != nil
	})
	if never.NextLaunchAt != nil || never.Relaunches != 0 || never.Restart != "never" {
		t.Fatalf("never policy status = %#v", never)
	}
	stopped := testutil.Run(t, hum, project, env, "stop", "crash", "--json")
	if stopped.Code != 0 {
		t.Fatalf("stop recovered process: code=%d stdout=%q stderr=%q", stopped.Code, stopped.Stdout, stopped.Stderr)
	}
	if follower.Exited() {
		t.Fatalf("follower closed on operator stop: stdout=%q stderr=%q", follower.Stdout(), follower.Stderr())
	}
}

func TestUpPreservesPendingRecovery(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	fixture := integrationFixture(t)
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir, "HUM_OUTPUT_BYTES=65536", "HUM_COMPLETED_RECORDS=20", "HUM_STOP_GRACE=1s")
	marker := filepath.Join(project, "launches")
	manifest := fmt.Sprintf(`version: 1
processes:
  crash:
    argv: [%q, relaunch, %q]
    ready:
      match: ready
      timeout: 5s
    restart: on-failure
`, fixture, marker)
	if err := os.WriteFile(filepath.Join(project, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shutdown := testutil.Run(t, hum, project, env, "shutdown", "--stop-processes", "--json")
		if shutdown.Code != 0 {
			t.Logf("shutdown cleanup: code=%d stdout=%q stderr=%q", shutdown.Code, shutdown.Stdout, shutdown.Stderr)
		}
	})

	session := newMCPTestSession(t, hum, project, env)
	initial := testutil.Run(t, hum, project, env, "start", "--json", "--no-wait", "crash")
	if initial.Code != 0 || initial.Err != nil || initial.Stderr != "" {
		t.Fatalf("initial start = code %d err=%v stdout=%q stderr=%q", initial.Code, initial.Err, initial.Stdout, initial.Stderr)
	}
	relaunchIntegrationWaitLaunchCount(t, marker, 1)
	pending := relaunchIntegrationWaitDaemonPending(t, runtimeDir, project, "crash", nil)
	if pending.Source != "manifest" || !reflect.DeepEqual(pending.Argv, []string{fixture, "relaunch", marker}) || pending.NextLaunchAt == nil {
		t.Fatalf("initial pending status = %#v", pending)
	}
	pendingDeadline := *pending.NextLaunchAt
	pendingCursor := pending.LaunchCursor

	assertPending := func(label string, state relaunchIntegrationStatus) {
		if state.Source != "manifest" || !reflect.DeepEqual(state.Argv, []string{fixture, "relaunch", marker}) || state.State != "exited" || state.Restart != "on-failure" || state.Relaunches != 0 || state.NextLaunchAt == nil || !state.NextLaunchAt.Equal(pendingDeadline) || state.LaunchCursor != pendingCursor || state.Readiness != "" {
			t.Fatalf("%s pending state = %#v, want unchanged recovery", label, state)
		}
	}

	for attempt := 1; attempt <= 2; attempt++ {
		raw, isErr := session.call(t, "up", project, nil)
		if isErr {
			t.Fatalf("MCP up attempt %d returned tool error: %s", attempt, raw)
		}
		var results []mcpUpRecoveryIntegrationResult
		if err := json.Unmarshal(raw, &results); err != nil {
			t.Fatalf("decode MCP up attempt %d %q: %v", attempt, raw, err)
		}
		if len(results) != 1 || results[0].Name != "crash" || results[0].Outcome != "recovery_pending" || results[0].Process == nil {
			t.Fatalf("MCP up attempt %d result = %s, want one recovery_pending process", attempt, raw)
		}
		mcpProcess := results[0].Process
		if mcpProcess.Readiness == nil || mcpProcess.Readiness.State != "starting" || mcpProcess.Readiness.Match != "ready" {
			t.Fatalf("MCP up attempt %d readiness = %#v, want retained ready matcher", attempt, mcpProcess.Readiness)
		}
		assertPending(fmt.Sprintf("MCP up attempt %d", attempt), relaunchIntegrationStatus{
			Name: mcpProcess.Name, Source: mcpProcess.Source, Argv: mcpProcess.Argv, State: mcpProcess.State,
			LaunchCursor: mcpProcess.LaunchCursor, Restart: mcpProcess.Restart, Relaunches: mcpProcess.Relaunches,
			NextLaunchAt: mcpProcess.NextLaunchAt,
		})
	}

	for attempt := 1; attempt <= 2; attempt++ {
		result := testutil.Run(t, hum, project, env, "up", "--json")
		if result.Code != 3 || result.Err == nil || result.Stderr != "" {
			t.Fatalf("CLI up attempt %d = code %d err=%v stdout=%q stderr=%q, want exit 3", attempt, result.Code, result.Err, result.Stdout, result.Stderr)
		}
		var launch upRecoveryIntegrationResult
		if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &launch); err != nil {
			t.Fatalf("decode CLI up attempt %d %q: %v", attempt, result.Stdout, err)
		}
		if launch.Name != "crash" || launch.Outcome != "recovery_pending" {
			t.Fatalf("CLI up attempt %d result = %#v, want recovery_pending", attempt, launch)
		}
		assertPending(fmt.Sprintf("CLI up attempt %d", attempt), relaunchIntegrationStatus{
			Name: launch.Name, Source: launch.Source, Argv: launch.Argv, State: launch.State,
			Readiness: launch.Readiness, LaunchCursor: launch.LaunchCursor, Restart: launch.Restart,
			Relaunches: launch.Relaunches, NextLaunchAt: launch.NextLaunchAt,
		})
	}
	relaunchIntegrationWaitLaunchCount(t, marker, 1)

	started := testutil.Run(t, hum, project, env, "start", "--json", "--no-wait", "crash")
	if started.Code != 0 || started.Err != nil || started.Stderr != "" {
		t.Fatalf("targeted CLI start = code %d err=%v stdout=%q stderr=%q", started.Code, started.Err, started.Stdout, started.Stderr)
	}
	var startResult upRecoveryIntegrationResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(started.Stdout)), &startResult); err != nil {
		t.Fatalf("decode targeted CLI start %q: %v", started.Stdout, err)
	}
	if startResult.Name != "crash" || startResult.Outcome != "started" || startResult.State != "running" || startResult.LaunchCursor == pendingCursor || startResult.NextLaunchAt != nil || startResult.Relaunches != 0 {
		t.Fatalf("targeted CLI start result = %#v, want immediate new running incarnation", startResult)
	}
	relaunchIntegrationWaitLaunchCount(t, marker, 2)
	pendingAfterStart := relaunchIntegrationWaitDaemonPending(t, runtimeDir, project, "crash", &pendingCursor)
	if pendingAfterStart.NextLaunchAt == nil {
		t.Fatal("targeted CLI start did not leave a new pending recovery")
	}

	restartedRaw, isErr := session.call(t, "restart", project, map[string]any{"name": "crash"})
	if isErr {
		t.Fatalf("targeted MCP restart returned tool error: %s", restartedRaw)
	}
	var restarted mcpRestartIntegrationProcess
	if err := json.Unmarshal(restartedRaw, &restarted); err != nil {
		t.Fatalf("decode targeted MCP restart %q: %v", restartedRaw, err)
	}
	if restarted.Name != "crash" || restarted.Source != "manifest" || !reflect.DeepEqual(restarted.Argv, []string{fixture, "relaunch", marker}) || restarted.State != "running" || restarted.Restart != "on-failure" || restarted.Relaunches != 0 || restarted.NextLaunchAt != nil || restarted.LaunchCursor == pendingAfterStart.LaunchCursor {
		t.Fatalf("targeted MCP restart result = %#v, want immediate new running incarnation", restarted)
	}
	relaunchIntegrationWaitLaunchCount(t, marker, 3)
	stable := relaunchIntegrationWaitStatus(t, hum, project, env, "crash", func(status relaunchIntegrationStatus) bool {
		return status.State == "running" && status.Restart == "on-failure" && status.Relaunches == 0 && status.NextLaunchAt == nil
	})
	if stable.State != "running" {
		t.Fatalf("MCP restart did not leave a running incarnation: response=%#v status=%#v", restarted, stable)
	}
}

func relaunchIntegrationWaitLaunchCount(t *testing.T, marker string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(marker)
		if err == nil && strings.TrimSpace(string(contents)) == strconv.Itoa(want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	contents, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read launch count %q: %v", marker, err)
	}
	t.Fatalf("launch count = %q, want %d", strings.TrimSpace(string(contents)), want)
}

func relaunchIntegrationWaitDaemonPending(t *testing.T, runtimeDir, root, name string, afterCursor *uint64) relaunchIntegrationStatus {
	t.Helper()
	client, err := daemon.DialRuntime(context.Background(), daemon.NewRuntimePaths(runtimeDir))
	if err != nil {
		t.Fatalf("dial daemon for pending recovery: %v", err)
	}
	defer client.Close()
	deadline := time.Now().Add(2 * time.Second)
	var status relaunchIntegrationStatus
	var lastErr error
	for time.Now().Before(deadline) {
		process, getErr := client.Get(context.Background(), daemon.GetRequest{Name: name, Cwd: root})
		lastErr = getErr
		if getErr == nil {
			status = relaunchIntegrationStatus{
				Name: process.Name, Source: process.Source, Argv: append([]string(nil), process.Argv...),
				State: string(process.State), LaunchCursor: uint64(process.LaunchCursor),
				Restart: string(process.Restart), Relaunches: process.Relaunches, NextLaunchAt: process.NextLaunchAt,
			}
			cursorChanged := afterCursor == nil || status.LaunchCursor != *afterCursor
			if status.State == "exited" && status.Restart == "on-failure" && status.Relaunches == 0 && status.NextLaunchAt != nil && cursorChanged {
				return status
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pending recovery did not satisfy predicate: %#v (last error: %v)", status, lastErr)
	return status
}

func relaunchIntegrationWaitStatus(t *testing.T, hum, project string, env []string, name string, ready func(relaunchIntegrationStatus) bool) relaunchIntegrationStatus {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	var status relaunchIntegrationStatus
	for time.Now().Before(deadline) {
		result := testutil.Run(t, hum, project, env, "status", name, "--json")
		if result.Code == 0 {
			status = relaunchIntegrationStatus{}
			if err := json.Unmarshal([]byte(result.Stdout), &status); err == nil && ready(status) {
				return status
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("status did not satisfy predicate: %#v", status)
	return status
}
