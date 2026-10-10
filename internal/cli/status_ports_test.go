//go:build !windows

package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hum/internal/app"
)

func TestStatusPorts(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	started := stopShutdownStartProcess(t, server, projectRoot, "api", []string{"/bin/sh", "-c", "sleep 30"})
	t.Cleanup(func() { _, _, _ = stopShutdownRun(t, "stop", "api") })

	stdout, stderr, err := stopShutdownRun(t, "status", "api", "--ports", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("status --ports --json: err=%v stderr=%q output=%q", err, stderr, stdout)
	}
	inspected := statusDecodeJSON(t, stdout)
	if inspected.PID != started.PID || inspected.State != string(app.StateRunning) || inspected.Ports == nil {
		t.Fatalf("ports status = %+v, want current running process with inspection", inspected)
	}
	if inspected.Ports.State != "available" || inspected.Ports.Listeners == nil {
		t.Fatalf("empty listener inspection = %+v, want successful empty snapshot", inspected.Ports)
	}

	plain, stderr, err := stopShutdownRun(t, "status", "api", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("default status --json: err=%v stderr=%q output=%q", err, stderr, plain)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(plain), &fields); err != nil {
		t.Fatal(err)
	}
	if _, present := fields["ports"]; present {
		t.Fatalf("default status exposed or inspected ports: %s", plain)
	}

	human, stderr, err := stopShutdownRun(t, "status", "api", "--ports")
	if err != nil || stderr != "" || !strings.Contains(human, "ports_state: available") {
		t.Fatalf("human status --ports: err=%v stderr=%q output=%q", err, stderr, human)
	}

	_, _, err = stopShutdownRun(t, "status", "--ports")
	if err == nil || !strings.Contains(err.Error(), "status --ports requires exactly one process name") {
		t.Fatalf("status --ports without a name error = %v", err)
	}
}
