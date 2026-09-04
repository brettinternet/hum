package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"hum/internal/testutil"
)

const waitIntegrationTimeout = 8 * time.Second

type waitJSONResponse struct {
	Op      string `json:"op"`
	OK      bool   `json:"ok"`
	Outcome string `json:"outcome"`
	Cursor  uint64 `json:"cursor"`
}

func TestWait(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)

	t.Run("buffered and subsequent output matches", func(t *testing.T) {
		runtime := lifecycleNewRuntime(t)
		daemonPID := 0
		t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, daemonPID) })

		name := "wait-buffered"
		gate := filepath.Join(t.TempDir(), "burst-gate")
		started := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", name, "--detach", "--", fixture, "burst", gate, "4")
		launchCursor := waitAssertStarted(t, started, name)
		testutil.WaitForFile(t, runtime.paths.PID, waitIntegrationTimeout)
		daemonPID = lifecycleReadPID(t, runtime.paths.PID)
		waitForRetainedOutput(t, hum, runtime.cwd, runtime.env, name)

		buffered := testutil.Run(t, hum, runtime.cwd, runtime.env, "wait", name, "--match", "stdout:0000")

		if buffered.Code != 0 || buffered.Err != nil || buffered.Stderr != "" {
			t.Fatalf("buffered wait: code=%d err=%v stdout=%q stderr=%q", buffered.Code, buffered.Err, buffered.Stdout, buffered.Stderr)
		}
		matchedCursor := waitParseHumanMatched(t, buffered.Stdout)
		if matchedCursor < launchCursor {
			t.Fatalf("buffered match cursor = %d, launch cursor = %d", matchedCursor, launchCursor)
		}

		follower := testutil.Start(t, hum, runtime.cwd, runtime.env, "wait", name, "--after-cursor", strconv.FormatUint(matchedCursor, 10), "--match", "stdout:0002", "--timeout", "2s", "--json")
		if follower.Exited() {
			t.Fatalf("second wait exited before its matching output: stdout=%q stderr=%q", follower.Stdout(), follower.Stderr())
		}
		if err := os.WriteFile(gate, []byte("release"), 0o600); err != nil {
			t.Fatalf("release burst gate: %v", err)
		}
		if err := follower.Wait(waitIntegrationTimeout); err != nil {
			t.Fatalf("second wait: %v; stdout=%q stderr=%q", err, follower.Stdout(), follower.Stderr())
		}
		if follower.Stderr() != "" {
			t.Fatalf("second wait stderr = %q", follower.Stderr())
		}
		response := waitDecodeJSON(t, follower.Stdout())
		if response.Outcome != "matched" {
			t.Fatalf("second wait outcome = %q, want matched; response=%q", response.Outcome, follower.Stdout())
		}
		if response.Cursor <= matchedCursor {
			t.Fatalf("second wait cursor = %d, want > first cursor %d", response.Cursor, matchedCursor)
		}
	})

	t.Run("exit before regex match returns code three", func(t *testing.T) {
		runtime := lifecycleNewRuntime(t)
		daemonPID := 0
		t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, daemonPID) })

		name := "wait-exit-before-match"
		gate := filepath.Join(t.TempDir(), "burst-gate")
		started := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", name, "--detach", "--", fixture, "burst", gate, "4")
		launchCursor := waitAssertStarted(t, started, name)
		testutil.WaitForFile(t, runtime.paths.PID, waitIntegrationTimeout)
		daemonPID = lifecycleReadPID(t, runtime.paths.PID)

		waiter := testutil.Start(t, hum, runtime.cwd, runtime.env, "wait", name, "--match", "stdout:never-present", "--timeout", "2s", "--json")
		time.Sleep(100 * time.Millisecond)
		if err := os.WriteFile(gate, []byte("release"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := waiter.Wait(waitIntegrationTimeout); err == nil {
			t.Fatalf("exit-before-match wait exited zero: %q", waiter.Stdout())
		}
		if waiter.Stderr() != "" {
			t.Fatalf("exit-before-match stderr=%q", waiter.Stderr())
		}
		response := waitDecodeJSON(t, waiter.Stdout())
		if response.Outcome != "exited" {
			t.Fatalf("exit before match outcome = %q, want exited; response=%q", response.Outcome, waiter.Stdout())
		}
		if response.Cursor <= launchCursor {
			t.Fatalf("exit before match cursor = %d, want > launch cursor %d", response.Cursor, launchCursor)
		}
	})

	t.Run("no-match wait succeeds when process exits", func(t *testing.T) {
		runtime := lifecycleNewRuntime(t)
		daemonPID := 0
		t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, daemonPID) })

		name := "wait-no-match"
		gate := filepath.Join(t.TempDir(), "burst-gate")
		started := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", name, "--detach", "--", fixture, "burst", gate, "4")
		launchCursor := waitAssertStarted(t, started, name)
		testutil.WaitForFile(t, runtime.paths.PID, waitIntegrationTimeout)
		daemonPID = lifecycleReadPID(t, runtime.paths.PID)

		waiter := testutil.Start(t, hum, runtime.cwd, runtime.env, "wait", name, "--timeout", "2s", "--json")
		if waiter.Exited() {
			t.Fatalf("no-match wait exited before process exit: stdout=%q stderr=%q", waiter.Stdout(), waiter.Stderr())
		}
		time.Sleep(100 * time.Millisecond)
		if err := os.WriteFile(gate, []byte("release"), 0o600); err != nil {
			t.Fatalf("release no-match burst gate: %v", err)
		}
		if err := waiter.Wait(waitIntegrationTimeout); err != nil {
			t.Fatalf("no-match wait: %v; stdout=%q stderr=%q", err, waiter.Stdout(), waiter.Stderr())
		}
		if waiter.Stderr() != "" {
			t.Fatalf("no-match wait stderr = %q", waiter.Stderr())
		}
		response := waitDecodeJSON(t, waiter.Stdout())
		if response.Outcome != "exited" {
			t.Fatalf("no-match wait outcome = %q, want exited; response=%q", response.Outcome, waiter.Stdout())
		}
		if response.Cursor <= launchCursor {
			t.Fatalf("no-match wait cursor = %d, want > launch cursor %d", response.Cursor, launchCursor)
		}
	})

	t.Run("timeout returns consumed cursor", func(t *testing.T) {
		runtime := lifecycleNewRuntime(t)
		daemonPID := 0
		t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, daemonPID) })

		name := "wait-timeout"
		marker := filepath.Join(t.TempDir(), "stream")
		started := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", name, "--detach", "--", fixture, "stream", marker)
		launchCursor := waitAssertStarted(t, started, name)
		testutil.WaitForFile(t, runtime.paths.PID, waitIntegrationTimeout)
		daemonPID = lifecycleReadPID(t, runtime.paths.PID)
		testutil.WaitForFile(t, marker+".started", waitIntegrationTimeout)

		result := testutil.Run(t, hum, runtime.cwd, runtime.env, "wait", name, "--match", "never-present", "--timeout", "200ms", "--json")
		if result.Code != 2 || result.Err == nil || result.Stderr != "" {
			t.Fatalf("timeout wait: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		response := waitDecodeJSON(t, result.Stdout)
		if response.Outcome != "timed_out" {
			t.Fatalf("timeout outcome = %q, want timed_out; response=%q", response.Outcome, result.Stdout)
		}
		if response.Cursor <= launchCursor {
			t.Fatalf("timeout cursor = %d, want > launch cursor %d", response.Cursor, launchCursor)
		}

		stopped := testutil.Run(t, hum, runtime.cwd, runtime.env, "stop", name)
		if stopped.Code != 0 || stopped.Err != nil || stopped.Stdout != name+" stopped\n" || stopped.Stderr != "" {
			t.Fatalf("stop timed-out process: code=%d err=%v stdout=%q stderr=%q", stopped.Code, stopped.Err, stopped.Stdout, stopped.Stderr)
		}
		testutil.WaitForFile(t, marker+".terminated", waitIntegrationTimeout)
	})

	t.Run("pre-launch wait creates daemon and times out", func(t *testing.T) {
		runtime := lifecycleNewRuntime(t)
		t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, 0) })
		result := testutil.Run(t, hum, runtime.cwd, runtime.env, "wait", "missing", "--timeout", "100ms", "--json")
		if result.Code != 2 || result.Err == nil || !strings.Contains(result.Stdout, `"outcome":"timed_out"`) || result.Stderr != "" {
			t.Fatalf("pre-launch timeout: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		testutil.WaitForFile(t, runtime.paths.Ready, waitIntegrationTimeout)
	})
}

type waitLogsResponse struct {
	Op      string          `json:"op"`
	OK      bool            `json:"ok"`
	Entries []waitLogsEntry `json:"entries"`
}

type waitLogsEntry struct {
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

func waitForRetainedOutput(t testing.TB, hum, cwd string, env []string, name string) {
	t.Helper()
	deadline := time.NewTimer(waitIntegrationTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	var last testutil.Result
	for {
		last = testutil.Run(t, hum, cwd, env, "logs", name, "--json", "--stream", "stdout", "--match", "stdout:0000")
		if last.Code == 0 && last.Err == nil {
			var response waitLogsResponse
			if err := json.Unmarshal([]byte(last.Stdout), &response); err == nil && response.Op == "output" && response.OK {
				for _, entry := range response.Entries {
					if entry.Stream == "stdout" && entry.Text == "stdout:0000\n" {
						return
					}
				}
			}
		}

		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for retained output: code=%d err=%v stdout=%q stderr=%q", last.Code, last.Err, last.Stdout, last.Stderr)
		case <-ticker.C:
		}
	}
}

func waitAssertStarted(t *testing.T, result testutil.Result, name string) uint64 {
	t.Helper()
	if result.Code != 0 || result.Err != nil || result.Stderr != "" {
		t.Fatalf("start %s: code=%d err=%v stdout=%q stderr=%q", name, result.Code, result.Err, result.Stdout, result.Stderr)
	}
	lifecycleParseManagedPID(t, result.Stdout, name)
	prefix := fmt.Sprintf("started %s (PID ", name)
	body := strings.TrimSuffix(strings.TrimPrefix(result.Stdout, prefix), ")\n")
	parts := strings.Split(body, ", cursor ")
	if len(parts) != 2 {
		t.Fatalf("start %s body = %q, want PID and cursor", name, body)
	}
	cursor, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		t.Fatalf("start %s cursor = %q: %v", name, parts[1], err)
	}
	return cursor
}

func waitParseHumanMatched(t *testing.T, output string) uint64 {
	t.Helper()
	const prefix = "outcome: matched\ncursor: "
	if !strings.HasPrefix(output, prefix) || !strings.HasSuffix(output, "\n") {
		t.Fatalf("human wait output = %q, want matched outcome and cursor", output)
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(output, prefix), "\n")
	cursor, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		t.Fatalf("human wait cursor = %q: %v", raw, err)
	}
	return cursor
}

func waitDecodeJSON(t *testing.T, output string) waitJSONResponse {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(output), &fields); err != nil {
		t.Fatalf("decode wait JSON: %v; output=%q", err, output)
	}
	for _, field := range []string{"op", "ok", "outcome", "cursor"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("wait JSON missing %q: %q", field, output)
		}
	}
	var response waitJSONResponse
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatalf("decode wait response: %v; output=%q", err, output)
	}
	if response.Op != "wait" || !response.OK {
		t.Fatalf("wait JSON envelope = %+v; output=%q", response, output)
	}
	return response
}
