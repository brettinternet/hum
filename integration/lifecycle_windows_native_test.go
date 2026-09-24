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

func TestWindowsNativeLifecycle(t *testing.T) {
	t.Parallel()
	hum := integrationHum(t)
	fixture := integrationFixture(t)
	runtime := lifecycleNewRuntime(t)
	t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, 0) })

	gate := filepath.Join(runtime.cwd, "release")
	started := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "native", "--detach", "--", fixture, "burst", gate, "8")
	if started.Code != 0 || started.Err != nil {
		t.Fatalf("start: %#v", started)
	}
	observed := testutil.Run(t, hum, runtime.cwd, runtime.env, "wait", "native", "--match", "stdout:0003", "--timeout", "15s", "--json")
	if observed.Code != 0 || !strings.Contains(observed.Stdout, `"outcome":"matched"`) {
		t.Fatalf("wait: %#v", observed)
	}
	status := testutil.Run(t, hum, runtime.cwd, runtime.env, "status", "native", "--json")
	var snapshot struct {
		State string `json:"state"`
		PID   int    `json:"pid"`
	}
	if status.Code != 0 || json.Unmarshal([]byte(status.Stdout), &snapshot) != nil || snapshot.State != "running" || snapshot.PID <= 0 {
		t.Fatalf("status: %#v decoded=%+v", status, snapshot)
	}
	logs := testutil.Run(t, hum, runtime.cwd, runtime.env, "logs", "native", "--json")
	if logs.Code != 0 || !strings.Contains(logs.Stdout, "stdout:0003") {
		t.Fatalf("logs: %#v", logs)
	}
	stopped := testutil.Run(t, hum, runtime.cwd, runtime.env, "stop", "native", "--json")
	if stopped.Code != 0 || !strings.Contains(stopped.Stdout, `"status":"stopped"`) {
		t.Fatalf("stop: %#v", stopped)
	}
	testutil.WaitForProcessGone(t, snapshot.PID, 10*time.Second)
	if err := os.WriteFile(gate, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	restarted := testutil.Run(t, hum, runtime.cwd, runtime.env, "start", "native", "--no-wait")
	if restarted.Code != 0 {
		t.Fatalf("restart stopped process: %#v", restarted)
	}
	if removed := testutil.Run(t, hum, runtime.cwd, runtime.env, "remove", "native"); removed.Code != 0 {
		t.Fatalf("remove: %#v", removed)
	}
}
