package integration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hum/internal/testutil"
)

func TestBoundedLogsStripTerminalControl(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir)
	gate := filepath.Join(root, "terminal.release")
	t.Cleanup(func() {
		result := testutil.Run(t, hum, root, env, "shutdown", "--stop-processes")
		if result.Code != 0 {
			t.Logf("terminal-control cleanup shutdown: code=%d stdout=%q stderr=%q err=%v", result.Code, result.Stdout, result.Stderr, result.Err)
		}
	})
	started := testutil.Run(t, hum, root, env, "run", "terminal", "--detach", "--", fixture, "terminal", gate)
	if started.Code != 0 || started.Err != nil {
		t.Fatalf("start terminal fixture: code=%d stdout=%q stderr=%q err=%v", started.Code, started.Stdout, started.Stderr, started.Err)
	}

	wait := testutil.Run(t, hum, root, env, "wait", "terminal", "--match", "^ready", "--timeout", "3s")
	if wait.Code != 0 || wait.Err != nil {
		t.Fatalf("wait for stripped readiness: code=%d stdout=%q stderr=%q err=%v", wait.Code, wait.Stdout, wait.Stderr, wait.Err)
	}

	human := testutil.Run(t, hum, root, env, "logs", "terminal")
	if human.Code != 0 || human.Err != nil {
		t.Fatalf("human bounded logs: code=%d stdout=%q stderr=%q err=%v", human.Code, human.Stdout, human.Stderr, human.Err)
	}
	if !strings.Contains(human.Stdout, "initial\n") || !strings.Contains(human.Stdout, "ready\n") || strings.Contains(human.Stdout, "\x1b") || strings.Contains(human.Stdout, "\r") {
		t.Fatalf("human bounded logs = %q, want stripped initial and ready text", human.Stdout)
	}

	jsonLogs := testutil.Run(t, hum, root, env, "logs", "terminal", "--json")
	if jsonLogs.Code != 0 || jsonLogs.Err != nil {
		t.Fatalf("JSON bounded logs: code=%d stdout=%q stderr=%q err=%v", jsonLogs.Code, jsonLogs.Stdout, jsonLogs.Stderr, jsonLogs.Err)
	}
	var bounded struct {
		Entries []struct {
			Text string `json:"text"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(jsonLogs.Stdout), &bounded); err != nil {
		t.Fatalf("decode bounded JSON logs %q: %v", jsonLogs.Stdout, err)
	}
	if len(bounded.Entries) != 2 || bounded.Entries[0].Text != "initial\n" || bounded.Entries[1].Text != "ready\n" {
		t.Fatalf("bounded JSON entries = %#v, want stripped initial and ready text", bounded.Entries)
	}
	for _, entry := range bounded.Entries {
		if strings.Contains(entry.Text, "\x1b") || strings.Contains(entry.Text, "\r") {
			t.Fatalf("bounded JSON entry retained terminal control: %q", entry.Text)
		}
	}

	mcp := newMCPTestSession(t, hum, root, env)
	mcpListRaw, mcpListErr := mcp.call(t, "list", canonicalRoot, nil)
	if mcpListErr || !strings.Contains(string(mcpListRaw), `"name":"terminal"`) {
		t.Fatalf("MCP terminal list = %s, error=%v", mcpListRaw, mcpListErr)
	}
	mcpRaw, isErr := mcp.call(t, "logs", canonicalRoot, map[string]any{"name": "terminal"})
	if isErr {
		t.Fatalf("MCP terminal logs error: %s", mcpRaw)
	}
	var mcpLogs struct {
		Entries []struct {
			Cursor uint64 `json:"cursor"`
			Text   string `json:"text"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(mcpRaw, &mcpLogs); err != nil {
		t.Fatalf("decode MCP terminal logs %q: %v", mcpRaw, err)
	}
	if len(mcpLogs.Entries) != 2 || mcpLogs.Entries[0].Text != "initial\n" || mcpLogs.Entries[1].Text != "ready\n" || mcpLogs.Entries[0].Cursor != 0 || mcpLogs.Entries[1].Cursor != 1 {
		t.Fatalf("MCP terminal logs = %#v, want stripped entries with raw cursors", mcpLogs.Entries)
	}
	for _, entry := range mcpLogs.Entries {
		if strings.Contains(entry.Text, "\x1b") || strings.Contains(entry.Text, "\r") {
			t.Fatalf("MCP logs entry retained terminal control: %q", entry.Text)
		}
	}

	follower := testutil.Start(t, hum, root, env, "logs", "terminal", "--follow")
	terminalWaitForProcessText(t, follower, "\x1b[31minitial\x1b[0m\r\n", 3*time.Second)
	if err := os.WriteFile(gate, []byte("release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	terminalWaitForProcessText(t, follower, "\x1b[32mlive\x1b[0m\r\n", 3*time.Second)
	if err := follower.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("interrupt raw follow: %v", err)
	}
	if err := follower.Wait(3 * time.Second); err != nil {
		t.Fatalf("raw follow wait: %v; stdout=%q stderr=%q", err, follower.Stdout(), follower.Stderr())
	}
	followOutput := follower.Stdout()
	if !strings.Contains(followOutput, "\x1b[31minitial\x1b[0m\r\n") || !strings.Contains(followOutput, "\x1b[32mlive\x1b[0m\r\n") {
		t.Fatalf("raw follow output = %q, want initial replay and later raw event", followOutput)
	}
}

func terminalWaitForProcessText(t *testing.T, process *testutil.Process, text string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(process.Stdout(), text) {
			return
		}
		if process.Exited() {
			t.Fatalf("process exited before %q: stdout=%q stderr=%q", text, process.Stdout(), process.Stderr())
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for process text %q: stdout=%q stderr=%q", text, process.Stdout(), process.Stderr())
}
