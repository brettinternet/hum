package cli

import (
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"hum/internal/protocol"
)

func TestSignalCommand(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	managed := stopShutdownStartProcess(t, server, projectRoot, "signal", []string{"/bin/sh", "-c", "trap '' HUP USR1 USR2; while :; do sleep 1; done"})
	t.Cleanup(func() {
		if stopShutdownProcessGroupAlive(managed.PGID) {
			_ = syscall.Kill(-managed.PGID, syscall.SIGKILL)
		}
	})

	human, stderr, err := stopShutdownRun(t, "signal", "signal", "hup")
	if err != nil || stderr != "" {
		t.Fatalf("human signal: err=%v stderr=%q output=%q", err, stderr, human)
	}
	if got, want := human, "sent SIGHUP (1) to signal\n"; got != want {
		t.Fatalf("human signal = %q, want %q", got, want)
	}
	jsonManaged := stopShutdownStartProcess(t, server, projectRoot, "signal-json", []string{"/bin/sh", "-c", "trap '' HUP USR1 USR2; while :; do sleep 1; done"})
	t.Cleanup(func() {
		if stopShutdownProcessGroupAlive(jsonManaged.PGID) {
			_ = syscall.Kill(-jsonManaged.PGID, syscall.SIGKILL)
		}
	})
	jsonOutput, stderr, err := stopShutdownRun(t, "signal", "signal-json", "1", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("JSON signal: err=%v stderr=%q output=%q", err, stderr, jsonOutput)
	}
	var result protocol.SignalResult
	if err := json.Unmarshal([]byte(jsonOutput), &result); err != nil {
		t.Fatal(err)
	}
	if result.Name != "signal-json" || result.Signal.Name != "SIGHUP" || result.Signal.Number != 1 || result.Status != "sent" {
		t.Fatalf("signal JSON = %#v", result)
	}
}

func TestSignalCommandErrors(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	managed := stopShutdownStartProcess(t, server, projectRoot, "signal", []string{"/bin/sh", "-c", "trap '' HUP; while :; do sleep 1; done"})
	t.Cleanup(func() {
		if stopShutdownProcessGroupAlive(managed.PGID) {
			_ = syscall.Kill(-managed.PGID, syscall.SIGKILL)
		}
	})

	for _, specification := range []string{"", "0", "-1", "SIGUSR3"} {
		args := []string{"signal", "signal", specification, "--json"}
		output, stderr, err := stopShutdownRun(t, args...)
		if err == nil || stderr != "" {
			t.Fatalf("invalid signal %q: err=%v stderr=%q output=%q", specification, err, stderr, output)
		}
		if output == "" {
			t.Fatalf("invalid signal %q produced no JSON: err=%v stderr=%q", specification, err, stderr)
		}
		got := decodeJSONErrorObject(t, output)
		if got.Code != string(protocol.ErrorInvalidSignal) {
			t.Fatalf("invalid signal %q code = %q, want %q", specification, got.Code, protocol.ErrorInvalidSignal)
		}
	}

	output, stderr, err := stopShutdownRun(t, "signal", "missing", "HUP", "--json")
	if err == nil || stderr != "" || decodeJSONErrorObject(t, output).Code != string(protocol.ErrorNotFound) {
		t.Fatalf("missing signal: err=%v stderr=%q output=%q", err, stderr, output)
	}
	if err := stopShutdownRunAndStop(t, "signal"); err != nil {
		t.Fatal(err)
	}
	output, stderr, err = stopShutdownRun(t, "signal", "signal", "HUP", "--json")
	if err == nil || stderr != "" || decodeJSONErrorObject(t, output).Code != string(protocol.ErrorNotRunning) {
		t.Fatalf("stopped signal: err=%v stderr=%q output=%q", err, stderr, output)
	}
}

func stopShutdownRunAndStop(t *testing.T, name string) error {
	t.Helper()
	_, _, err := stopShutdownRun(t, "stop", name)
	return err
}

func TestSignalDocs(t *testing.T) {
	root := NewRootCommand("test", "test", &strings.Builder{}, &strings.Builder{})
	command := root.Command("signal")
	if command == nil || !strings.Contains(command.UsageText, "signal NAME SIGNAL") || !strings.Contains(command.Description, "observational") {
		t.Fatalf("signal help contract missing: %#v", command)
	}
	for _, path := range []string{"../../docs/design.md", "../../docs/coding-agents.md"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		docs := strings.ToLower(string(data))
		for _, phrase := range []string{"signal", "invalid_signal", "not_running", "canonical"} {
			if !strings.Contains(docs, phrase) {
				t.Errorf("%s missing %q", path, phrase)
			}
		}
	}
}
