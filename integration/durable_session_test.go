package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hum/internal/testutil"
)

func durableSetup(t *testing.T) (string, lifecycleRuntime) {
	t.Helper()
	lifecycleRequireUnix(t)
	hum := testutil.BuildHum(t)
	runtime := lifecycleNewRuntime(t)
	t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, 0) })
	return hum, runtime
}

func durableWaitText(t *testing.T, process *testutil.Process, stderr bool, text string) {
	t.Helper()
	deadline := time.Now().Add(lifecycleTimeout)
	for time.Now().Before(deadline) {
		value := process.Stdout()
		if stderr {
			value = process.Stderr()
		}
		if strings.Contains(value, text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process output did not contain %q: stdout=%q stderr=%q", text, process.Stdout(), process.Stderr())
}

func TestStatusReportsFollowers(t *testing.T) {
	hum, runtime := durableSetup(t)
	follower := testutil.Start(t, hum, runtime.cwd, runtime.env, "logs", "observed", "--follow")
	durableWaitText(t, follower, false, "waiting for first launch")

	waitFollowers := func(want int) {
		t.Helper()
		deadline := time.Now().Add(lifecycleTimeout)
		for time.Now().Before(deadline) {
			status := testutil.Run(t, hum, runtime.cwd, runtime.env, "status", "observed", "--json")
			var snapshot struct {
				Followers int `json:"followers"`
			}
			if status.Code == 0 && json.Unmarshal([]byte(status.Stdout), &snapshot) == nil && snapshot.Followers == want {
				return
			}
			time.Sleep(lifecyclePollInterval)
		}
		t.Fatalf("status did not report followers=%d", want)
	}
	waitFollowers(1)

	started := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "observed", "--detach", "--", "/bin/sh", "-c", "sleep 30")
	if started.Code != 0 {
		t.Fatalf("start: %#v", started)
	}
	waitFollowers(1)

	if err := follower.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := follower.Wait(lifecycleTimeout); err != nil {
		t.Fatalf("Ctrl+C follower: %v; stdout=%q stderr=%q", err, follower.Stdout(), follower.Stderr())
	}
	waitFollowers(0)

	if removed := testutil.Run(t, hum, runtime.cwd, runtime.env, "remove", "observed"); removed.Code != 0 {
		t.Fatalf("remove: %#v", removed)
	}
	if status := testutil.Run(t, hum, runtime.cwd, runtime.env, "status", "observed", "--json"); status.Code == 0 {
		t.Fatalf("removed status still exists: %#v", status)
	}
	listed := testutil.Run(t, hum, runtime.cwd, runtime.env, "list", "--all", "--json")
	if listed.Code != 0 || strings.Contains(listed.Stdout, `"name":"observed"`) {
		t.Fatalf("removed record remains listed: %#v", listed)
	}
}

func TestDurableFollowAcrossStopStart(t *testing.T) {
	hum, runtime := durableSetup(t)
	follower := testutil.Start(t, hum, runtime.cwd, runtime.env, "logs", "web", "--follow")
	attached := testutil.Start(t, hum, runtime.cwd, runtime.env, "run", "web")
	durableWaitText(t, follower, false, "waiting for first launch")
	durableWaitText(t, attached, false, "waiting for first launch")
	started := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "web", "--detach", "--", "/bin/sh", "-c", "printf 'web-output\\n'; sleep 30")
	if started.Code != 0 {
		t.Fatalf("start: %#v", started)
	}
	durableWaitText(t, follower, false, "web-output")
	durableWaitText(t, attached, false, "web-output")
	if stopped := testutil.Run(t, hum, runtime.cwd, runtime.env, "stop", "web"); stopped.Code != 0 {
		t.Fatalf("stop: %#v", stopped)
	}
	durableWaitText(t, follower, false, "waiting for next launch")
	durableWaitText(t, attached, true, "waiting for next launch")
	if err := os.WriteFile(filepath.Join(runtime.cwd, "intermediate-work"), []byte("done"), 0o600); err != nil {
		t.Fatal(err)
	}
	if started = testutil.Run(t, hum, runtime.cwd, runtime.env, "start", "web", "--no-wait"); started.Code != 0 {
		t.Fatalf("restart stopped session: %#v", started)
	}
	deadline := time.Now().Add(lifecycleTimeout)
	for (strings.Count(follower.Stdout(), "web-output") < 2 || strings.Count(attached.Stdout(), "web-output") < 2) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Count(follower.Stdout(), "web-output") < 2 {
		t.Fatalf("logs follower did not resume: %q", follower.Stdout())
	}
	if strings.Count(attached.Stdout(), "web-output") < 2 {
		t.Fatalf("attached run did not resume: %q", attached.Stdout())
	}
	if err := attached.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := attached.Wait(lifecycleTimeout); err != nil {
		t.Fatalf("attached detach: %v", err)
	}
	status := testutil.Run(t, hum, runtime.cwd, runtime.env, "status", "web", "--json")
	if status.Code != 0 || !strings.Contains(status.Stdout, `"state":"running"`) {
		t.Fatalf("Ctrl+C stopped child: %#v", status)
	}
	if removed := testutil.Run(t, hum, runtime.cwd, runtime.env, "remove", "web"); removed.Code != 0 {
		t.Fatalf("remove: %#v", removed)
	}
	if err := follower.Wait(lifecycleTimeout); err != nil {
		t.Fatalf("removed follower: %v", err)
	}
}

func TestFollowBeforeFirstLaunch(t *testing.T) {
	hum, runtime := durableSetup(t)
	follower := testutil.Start(t, hum, runtime.cwd, runtime.env, "logs", "future", "--follow")
	durableWaitText(t, follower, false, "does not resolve")
	started := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "future", "--detach", "--", "/bin/sh", "-c", "printf 'future-ready\\n'; sleep 30")
	if started.Code != 0 {
		t.Fatalf("start: %#v", started)
	}
	durableWaitText(t, follower, false, "future-ready")
	if removed := testutil.Run(t, hum, runtime.cwd, runtime.env, "remove", "future"); removed.Code != 0 {
		t.Fatalf("remove: %#v", removed)
	}
	_ = follower.Wait(lifecycleTimeout)
}

func TestRunAttachesToRunning(t *testing.T) {
	hum, runtime := durableSetup(t)
	started := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "join", "--detach", "--", "/bin/sh", "-c", "printf 'joined\\n'; sleep 30")
	if started.Code != 0 {
		t.Fatalf("start: %#v", started)
	}
	attached := testutil.Start(t, hum, runtime.cwd, runtime.env, "run", "join")
	durableWaitText(t, attached, false, "joined")
	if err := attached.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := attached.Wait(lifecycleTimeout); err != nil {
		t.Fatalf("detach: %v", err)
	}
	status := testutil.Run(t, hum, runtime.cwd, runtime.env, "status", "join", "--json")
	if status.Code != 0 || !strings.Contains(status.Stdout, `"state":"running"`) {
		t.Fatalf("child stopped on detach: %#v", status)
	}
	_ = testutil.Run(t, hum, runtime.cwd, runtime.env, "remove", "join")
}

func TestWaitBeforeStart(t *testing.T) {
	hum, runtime := durableSetup(t)
	waiter := testutil.Start(t, hum, runtime.cwd, runtime.env, "wait", "later", "--match", "ready", "--timeout", "3s", "--json")
	time.Sleep(100 * time.Millisecond)
	started := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "later", "--detach", "--", "/bin/sh", "-c", "printf 'ready\\n'")
	if started.Code != 0 {
		t.Fatalf("start: %#v", started)
	}
	if err := waiter.Wait(lifecycleTimeout); err != nil {
		t.Fatalf("wait: %v stdout=%q stderr=%q", err, waiter.Stdout(), waiter.Stderr())
	}
	if !strings.Contains(waiter.Stdout(), `"outcome":"matched"`) {
		t.Fatalf("wait outcome: %q", waiter.Stdout())
	}
}

func TestRemoveSupervisionSession(t *testing.T) {
	hum, runtime := durableSetup(t)
	if got := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "gone", "--detach", "--", "/bin/sh", "-c", "sleep 30"); got.Code != 0 {
		t.Fatalf("start: %#v", got)
	}
	if got := testutil.Run(t, hum, runtime.cwd, runtime.env, "remove", "gone"); got.Code != 0 {
		t.Fatalf("remove: %#v", got)
	}
	if got := testutil.Run(t, hum, runtime.cwd, runtime.env, "start", "gone"); got.Code == 0 {
		t.Fatalf("removed launch spec was reused: %#v", got)
	}
	if _, err := os.Stat(filepath.Join(runtime.cwd, "hum.yaml")); !os.IsNotExist(err) {
		t.Fatalf("remove edited manifest: %v", err)
	}
}

func TestUpWithDurableFollowers(t *testing.T) {
	hum, runtime := durableSetup(t)
	manifest := fmt.Sprintf("version: 1\nprocesses:\n  web:\n    argv: [%q, %q, %q]\n", "/bin/sh", "-c", "printf 'up-output\\n'; sleep 30")
	if err := os.WriteFile(filepath.Join(runtime.cwd, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	follower := testutil.Start(t, hum, runtime.cwd, runtime.env, "logs", "web", "--follow")
	durableWaitText(t, follower, false, "waiting for first launch")
	if strings.Contains(follower.Stdout(), "does not resolve") {
		t.Fatalf("declared follower misreported resolution: %q", follower.Stdout())
	}
	if got := testutil.Run(t, hum, runtime.cwd, runtime.env, "up", "--no-wait"); got.Code != 0 {
		t.Fatalf("up: %#v", got)
	}
	durableWaitText(t, follower, false, "up-output")
	if got := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "adhoc", "--detach", "--", "/bin/sh", "-c", "printf 'adhoc-output\\n'; sleep 30"); got.Code != 0 {
		t.Fatalf("ad hoc start: %#v", got)
	}
	adhocFollower := testutil.Start(t, hum, runtime.cwd, runtime.env, "logs", "adhoc", "--follow")
	durableWaitText(t, adhocFollower, false, "adhoc-output")
	if got := testutil.Run(t, hum, runtime.cwd, runtime.env, "down"); got.Code != 0 {
		t.Fatalf("down: %#v", got)
	}
	durableWaitText(t, adhocFollower, false, "waiting for next launch")
	if got := testutil.Run(t, hum, runtime.cwd, runtime.env, "up", "--no-wait"); got.Code != 0 {
		t.Fatalf("second up: %#v", got)
	}
	deadline := time.Now().Add(lifecycleTimeout)
	for strings.Count(follower.Stdout(), "up-output") < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Count(follower.Stdout(), "up-output") < 2 {
		t.Fatalf("up follower did not resume: %q", follower.Stdout())
	}
	adhocStatus := testutil.Run(t, hum, runtime.cwd, runtime.env, "status", "adhoc", "--json")
	if adhocStatus.Code != 0 || strings.Contains(adhocStatus.Stdout, `"state":"running"`) {
		t.Fatalf("up adopted ad hoc session: %#v", adhocStatus)
	}
	if adhocFollower.Exited() {
		t.Fatal("ad hoc follower exited across down/up")
	}
	_ = testutil.Run(t, hum, runtime.cwd, runtime.env, "remove", "adhoc", "web")
	_ = adhocFollower.Wait(lifecycleTimeout)
	_ = follower.Wait(lifecycleTimeout)
}

func TestFollowerExitsOnDaemonShutdown(t *testing.T) {
	hum, runtime := durableSetup(t)
	follower := testutil.Start(t, hum, runtime.cwd, runtime.env, "logs", "shutdown-follow", "--follow")
	durableWaitText(t, follower, false, "waiting")
	shutdown := testutil.Run(t, hum, runtime.cwd, runtime.env, "shutdown", "--stop-processes")
	if shutdown.Code != 0 {
		t.Fatalf("shutdown: %#v", shutdown)
	}
	if err := follower.Wait(lifecycleTimeout); err == nil {
		t.Fatalf("follower exited zero on daemon shutdown: stderr=%q", follower.Stderr())
	}
	if !strings.Contains(strings.ToLower(follower.Stderr()), "supervisor is shut down") {
		t.Fatalf("unclear shutdown error: %q", follower.Stderr())
	}
}
