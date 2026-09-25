package integration

import (
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
	t.Parallel()
	hum := integrationHum(t)
	fixture := integrationFixture(t)
	root := t.TempDir()
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

	follower := testutil.Start(t, hum, root, env, "logs", "terminal", "--follow")
	testutil.WaitForOutput(t, follower, false, "\x1b[31minitial\x1b[0m\r\n", 3*time.Second)
	if err := os.WriteFile(gate, []byte("release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	testutil.WaitForOutput(t, follower, false, "\x1b[32mlive\x1b[0m\r\n", 3*time.Second)
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
