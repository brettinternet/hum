package integration

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"

	"hum/internal/protocol"
	"hum/internal/testutil"
)

func TestStatusPorts(t *testing.T) {
	t.Parallel()
	hum := integrationHum(t)
	fixture := integrationFixture(t)
	runtime := lifecycleNewRuntime(t)
	unrelated, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer unrelated.Close()
	defer func() {
		_ = testutil.Run(t, hum, runtime.cwd, runtime.env, "shutdown", "--stop-processes")
	}()

	marker := filepath.Join(t.TempDir(), "listener")
	started := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "ports", "--detach", "--json", "--", fixture, "listen-parent", marker)
	if started.Code != 0 || started.Err != nil || started.Stderr != "" {
		t.Fatalf("start listener launcher: code=%d err=%v stdout=%q stderr=%q", started.Code, started.Err, started.Stdout, started.Stderr)
	}
	testutil.WaitForFile(t, marker, lifecycleTimeout)
	fields, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSpace(string(fields)), "|")
	if len(parts) != 2 {
		t.Fatalf("listener marker = %q, want pid|endpoint", fields)
	}
	childPID, err := strconv.Atoi(parts[0])
	if err != nil || childPID <= 0 {
		t.Fatalf("listener child pid %q: %v", parts[0], err)
	}
	listenerHost, listenerPortText, err := net.SplitHostPort(parts[1])
	if err != nil {
		t.Fatalf("listener endpoint %q: %v", parts[1], err)
	}
	listenerPort, err := strconv.Atoi(listenerPortText)
	if err != nil || listenerPort <= 0 {
		t.Fatalf("listener port %q: %v", listenerPortText, err)
	}

	status := testutil.Run(t, hum, runtime.cwd, runtime.env, "status", "ports", "--ports", "--json")
	if status.Code != 0 || status.Err != nil || status.Stderr != "" {
		t.Fatalf("status --ports --json: code=%d err=%v stdout=%q stderr=%q", status.Code, status.Err, status.Stdout, status.Stderr)
	}
	var snapshot protocol.Process
	if err := json.Unmarshal([]byte(strings.TrimSpace(status.Stdout)), &snapshot); err != nil {
		t.Fatalf("decode ports status: %v (%q)", err, status.Stdout)
	}
	expectedState := "available"
	if goruntime.GOOS == "windows" {
		expectedState = "partial"
	}
	if snapshot.Ports == nil || snapshot.Ports.State != expectedState {
		t.Fatalf("ports inspection = %#v, want %s", snapshot.Ports, expectedState)
	}
	if expectedState == "partial" && snapshot.Ports.Diagnostic == "" {
		t.Fatal("Windows owner-only inspection omitted its diagnostic")
	}
	if len(snapshot.Ports.Listeners) != 1 {
		t.Fatalf("listeners = %#v, want only the owned child listener", snapshot.Ports.Listeners)
	}
	listener := snapshot.Ports.Listeners[0]
	if listener.Transport != "tcp" || listener.Address != listenerHost || int(listener.Port) != listenerPort {
		t.Fatalf("listener = %+v, want tcp %s:%d", listener, listenerHost, listenerPort)
	}
	if len(listener.PIDs) != 1 || listener.PIDs[0] != childPID {
		t.Fatalf("listener PIDs = %v, want child PID %d", listener.PIDs, childPID)
	}
	unrelatedAddress, unrelatedPort, err := net.SplitHostPort(unrelated.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	unrelatedPortNumber, err := strconv.Atoi(unrelatedPort)
	if err != nil {
		t.Fatal(err)
	}
	if listener.Address == unrelatedAddress && int(listener.Port) == unrelatedPortNumber {
		t.Fatalf("reported unrelated test-process listener: %+v", listener)
	}

	ordinary := testutil.Run(t, hum, runtime.cwd, runtime.env, "status", "ports", "--json")
	if ordinary.Code != 0 || ordinary.Err != nil {
		t.Fatalf("ordinary status: code=%d err=%v stdout=%q stderr=%q", ordinary.Code, ordinary.Err, ordinary.Stdout, ordinary.Stderr)
	}
	var ordinaryFields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(ordinary.Stdout)), &ordinaryFields); err != nil {
		t.Fatal(err)
	}
	if _, present := ordinaryFields["ports"]; present {
		t.Fatalf("ordinary status unexpectedly inspected ports: %s", ordinary.Stdout)
	}
	stopped := testutil.Run(t, hum, runtime.cwd, runtime.env, "stop", "ports")
	if stopped.Code != 0 || stopped.Err != nil {
		t.Fatalf("stop listener launcher: code=%d err=%v stdout=%q stderr=%q", stopped.Code, stopped.Err, stopped.Stdout, stopped.Stderr)
	}
}
