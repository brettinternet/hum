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
		if len(raw) != 4 || raw["name"] == nil || raw["pid"] == nil || raw["restarts"] == nil || raw["launch_cursor"] == nil {
			t.Fatalf("restart result %d keys = %#v", index, raw)
		}
		var result restartResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatal(err)
		}
		wantName := []string{"alpha", "beta"}[index]
		oldPID := []int{firstAlpha.PID, firstBeta.PID}[index]
		if result.Name != wantName || result.PID == oldPID || result.Restarts != 1 {
			t.Fatalf("restart result %d = %#v, old PID %d", index, result, oldPID)
		}
	}

	stdout, stderr, err = stopShutdownRun(t, "restart", "alpha")
	if err != nil || stderr != "" {
		t.Fatalf("restart human: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "alpha restarted pid=") || !strings.Contains(stdout, "restarts=2 launch_cursor=") {
		t.Fatalf("restart human output = %q", stdout)
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
