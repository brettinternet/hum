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

func TestListPortLookup(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("port lookup integration requires Unix process-group inspection")
	}
	t.Parallel()
	hum := integrationHum(t)
	fixture := integrationFixture(t)
	runtime := lifecycleNewRuntime(t)
	rootA, rootB := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	for _, root := range []*string{&rootA, &rootB} {
		if err := os.MkdirAll(filepath.Join(*root, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		canonical, err := filepath.EvalSymlinks(*root)
		if err != nil {
			t.Fatal(err)
		}
		*root = canonical
	}
	defer func() {
		_ = testutil.Run(t, hum, runtime.cwd, runtime.env, "shutdown", "--stop-processes")
	}()

	marker := filepath.Join(t.TempDir(), "listener")
	started := testutil.Run(t, hum, runtime.cwd, runtime.env, "-C", rootB, "run", "ports", "--detach", "--json", "--", fixture, "listen-parent", marker)
	if started.Code != 0 || started.Err != nil || started.Stderr != "" {
		t.Fatalf("start project B listener: code=%d err=%v stdout=%q stderr=%q", started.Code, started.Err, started.Stdout, started.Stderr)
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

	listed := testutil.Run(t, hum, runtime.cwd, runtime.env, "-C", rootA, "list", "--port", strconv.Itoa(listenerPort), "--json")
	if listed.Code != 0 || listed.Err != nil || listed.Stderr != "" {
		t.Fatalf("list --port from project A: code=%d err=%v stdout=%q stderr=%q", listed.Code, listed.Err, listed.Stdout, listed.Stderr)
	}
	var lookup struct {
		Processes  []protocol.Process              `json:"processes"`
		Incomplete []protocol.PortLookupIncomplete `json:"incomplete"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(listed.Stdout)), &lookup); err != nil {
		t.Fatalf("decode cross-project port lookup: %v (%q)", err, listed.Stdout)
	}
	if len(lookup.Processes) != 1 {
		t.Fatalf("project A found %d holders: %s", len(lookup.Processes), listed.Stdout)
	}
	holder := lookup.Processes[0]
	if holder.Name != "ports" || holder.Root != rootB || holder.Ports == nil || len(holder.Ports.Listeners) != 1 {
		t.Fatalf("cross-project holder = %#v; want project B and one filtered listener", holder)
	}
	listener := holder.Ports.Listeners[0]
	if listener.Transport != "tcp" || listener.Address != listenerHost || int(listener.Port) != listenerPort || len(listener.PIDs) != 1 || listener.PIDs[0] != childPID {
		t.Fatalf("cross-project listener = %+v, want tcp %s:%d held by child %d", listener, listenerHost, listenerPort, childPID)
	}

	foreign, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	foreignPID := os.Getpid()
	_, foreignPortText, err := net.SplitHostPort(foreign.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	foreignPort, err := strconv.Atoi(foreignPortText)
	if err != nil {
		t.Fatal(err)
	}
	miss := testutil.Run(t, hum, runtime.cwd, runtime.env, "-C", rootA, "list", "--port", strconv.Itoa(foreignPort), "--json")
	if miss.Code != 0 || miss.Err != nil || miss.Stderr != "" {
		t.Fatalf("list foreign-only port: code=%d err=%v stdout=%q stderr=%q", miss.Code, miss.Err, miss.Stdout, miss.Stderr)
	}
	var noHolders struct {
		Processes []protocol.Process `json:"processes"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(miss.Stdout)), &noHolders); err != nil {
		t.Fatalf("decode foreign-only port lookup: %v (%q)", err, miss.Stdout)
	}
	if len(noHolders.Processes) != 0 || strings.Contains(miss.Stdout+miss.Stderr, strconv.Itoa(foreignPID)) {
		t.Fatalf("foreign listener leaked into output: pid=%d stdout=%q stderr=%q", foreignPID, miss.Stdout, miss.Stderr)
	}
}

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

	status := testutil.Run(t, hum, runtime.cwd, runtime.env, "status", "ports", "--json")
	if status.Code != 0 || status.Err != nil || status.Stderr != "" {
		t.Fatalf("status --json: code=%d err=%v stdout=%q stderr=%q", status.Code, status.Err, status.Stdout, status.Stderr)
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

	ordinary := testutil.Run(t, hum, runtime.cwd, runtime.env, "status", "--json")
	if ordinary.Code != 0 || ordinary.Err != nil {
		t.Fatalf("aggregate status: code=%d err=%v stdout=%q stderr=%q", ordinary.Code, ordinary.Err, ordinary.Stdout, ordinary.Stderr)
	}
	var ordinaryFields struct {
		Processes []map[string]json.RawMessage `json:"processes"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(ordinary.Stdout)), &ordinaryFields); err != nil {
		t.Fatal(err)
	}
	if len(ordinaryFields.Processes) != 1 {
		t.Fatalf("aggregate status processes = %v", ordinaryFields.Processes)
	}
	if _, present := ordinaryFields.Processes[0]["ports"]; present {
		t.Fatalf("aggregate status unexpectedly inspected ports: %s", ordinary.Stdout)
	}
	stopped := testutil.Run(t, hum, runtime.cwd, runtime.env, "stop", "ports")
	if stopped.Code != 0 || stopped.Err != nil {
		t.Fatalf("stop listener launcher: code=%d err=%v stdout=%q stderr=%q", stopped.Code, stopped.Err, stopped.Stdout, stopped.Stderr)
	}
}
