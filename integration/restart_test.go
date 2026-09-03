package integration

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"hum/internal/testutil"
)

type restartIntegrationResult struct {
	Name         string `json:"name"`
	PID          int    `json:"pid"`
	Restarts     int    `json:"restarts"`
	LaunchCursor uint64 `json:"launch_cursor"`
}

func TestRestart(t *testing.T) {
	lifecycleRequireUnix(t)
	harness := logsitNewHarness(t)
	name := "api"
	marker := filepath.Join(harness.project, "restart-incarnation")
	script := `if [ -e "$1" ]; then printf 'new-launch\n'; else : > "$1"; printf 'prelude\nold-only\n'; fi; trap 'exit 0' TERM; i=0; while :; do printf 'heartbeat-%s\n' "$i"; i=$((i+1)); sleep 0.05; done`
	started := logsitStartDetached(t, harness, name, "/bin/sh", "-c", script, "restart-fixture", marker)
	preRestartCursor := waitAssertStarted(t, started, name)
	first, ok := logsitProcessByName(logsitRunList(t, harness), name)
	if !ok {
		t.Fatal("started process missing from list")
	}
	logsitWaitOutput(t, harness, name, []string{"--json", "--stream", "both"}, func(lines []logsitJSONLine) bool {
		return len(lines) == 1 && logsitHasEntryText(lines[0].Event.Entries, "old-only\n")
	})

	follower := testutil.Start(t, harness.hum, harness.project, harness.env, "logs", name, "--follow", "--json", "--stream", "both")
	logsitWaitFollowerText(t, follower, "heartbeat-")
	if follower.Exited() {
		t.Fatal("follower exited before restart")
	}

	restarted := testutil.Run(t, harness.hum, harness.project, harness.env, "restart", name, "--json")
	if restarted.Code != 0 || restarted.Err != nil || restarted.Stderr != "" {
		t.Fatalf("restart: code=%d err=%v stdout=%q stderr=%q", restarted.Code, restarted.Err, restarted.Stdout, restarted.Stderr)
	}
	var result restartIntegrationResult
	if err := json.Unmarshal([]byte(restarted.Stdout), &result); err != nil {
		t.Fatalf("decode restart JSON %q: %v", restarted.Stdout, err)
	}
	if result.Name != name || result.PID == first.PID || result.Restarts != 1 || result.LaunchCursor <= preRestartCursor {
		t.Fatalf("restart result = %#v, first PID=%d pre cursor=%d", result, first.PID, preRestartCursor)
	}

	logsitWaitFollowerText(t, follower, "restarted")
	logsitWaitFollowerText(t, follower, "new-launch")
	followed := follower.Stdout()
	markerIndex := strings.Index(followed, "restarted")
	newIndex := strings.Index(followed, "new-launch")
	if markerIndex < 0 || newIndex <= markerIndex {
		t.Fatalf("follower ordering: %q", followed)
	}
	if err := follower.Kill(); err != nil {
		t.Fatalf("stop follower: %v", err)
	}
	_ = follower.Wait(logsitFollowerTimeout)

	spanning := logsitRunLogs(t, harness, name, "--after-cursor", strconv.FormatUint(preRestartCursor, 10), "--json", "--stream", "both")
	lines := logsitDecodeJSONLines(t, spanning.Stdout)
	if len(lines) != 1 || !logsitHasEntryText(lines[0].Event.Entries, "old-only\n") ||
		!logsitHasEntryText(lines[0].Event.Entries, "new-launch\n") {
		t.Fatalf("spanning logs = %q", spanning.Stdout)
	}

	waited := testutil.Run(t, harness.hum, harness.project, harness.env, "wait", name, "--match", "^old-only$", "--timeout", "1s", "--json")
	if waited.Code != 2 || waited.Err == nil || waited.Stderr != "" {
		t.Fatalf("post-restart wait: code=%d err=%v stdout=%q stderr=%q", waited.Code, waited.Err, waited.Stdout, waited.Stderr)
	}
	response := waitDecodeJSON(t, waited.Stdout)
	if response.Outcome != "timed_out" {
		t.Fatalf("post-restart wait outcome = %q", response.Outcome)
	}
}
