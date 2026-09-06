//go:build darwin || linux

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hum/internal/testutil"
)

type relaunchIntegrationStatus struct {
	State        string     `json:"state"`
	Readiness    string     `json:"readiness"`
	Restart      string     `json:"restart"`
	Relaunches   int        `json:"relaunches"`
	NextLaunchAt *time.Time `json:"next_launch_at"`
	ExitStatus   *int       `json:"exit_status"`
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
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)
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
