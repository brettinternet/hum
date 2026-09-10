package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"hum/internal/daemon"
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

func TestSignalGlobalSelectorPlacement(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	globalMarker := filepath.Join(t.TempDir(), "global-signal")
	projectMarker := filepath.Join(t.TempDir(), "project-signal")
	start := func(scope, marker string) int {
		client, err := daemon.Dial(context.Background(), server.Paths().Socket)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		script := fmt.Sprintf("trap 'printf hit > %s' HUP; : > %s.ready; while :; do sleep 1; done", shellEscape(marker), shellEscape(marker))
		process, err := client.Start(context.Background(), daemon.StartRequest{
			Scope: scope,
			Name:  "scoped-signal",
			Cwd:   projectRoot,
			Argv:  []string{"/bin/sh", "-c", script},
			Env:   os.Environ(),
		})
		if err != nil {
			t.Fatalf("start %s process: %v", scope, err)
		}
		return process.PGID
	}
	projectPGID := start("project", projectMarker)
	globalPGID := start("global", globalMarker)
	t.Cleanup(func() {
		_ = syscall.Kill(-projectPGID, syscall.SIGKILL)
		_ = syscall.Kill(-globalPGID, syscall.SIGKILL)
	})
	if err := cliServeRunWaitForFile(projectMarker + ".ready"); err != nil {
		t.Fatal(err)
	}
	if err := cliServeRunWaitForFile(globalMarker + ".ready"); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"signal", "--global", "scoped-signal", "HUP"},
		{"signal", "scoped-signal", "HUP", "--global"},
	} {
		if err := os.WriteFile(globalMarker, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := stopShutdownRun(t, args...)
		if err != nil || stderr != "" {
			t.Fatalf("signal placement %v: err=%v stderr=%q", args, err, stderr)
		}
		if err := cliServeRunWaitForText(globalMarker, "hit"); err != nil {
			t.Fatalf("signal placement %v did not target global process: %v", args, err)
		}
	}
	if _, err := os.Stat(projectMarker); !os.IsNotExist(err) {
		t.Fatalf("project process received global signal: marker stat error = %v", err)
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

// TestSignalWithoutDaemon pins the no-daemon diagnostic to the same guidance
// the other project-scoped commands emit instead of the socket path.
func TestSignalWithoutDaemon(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	t.Setenv("HUM_RUNTIME_DIR", t.TempDir())
	writeManifestCLITestFile(t, projectRoot, `version: 1
processes:
  api:
    argv: [echo, api]
`)
	for _, test := range []struct {
		name string
		want string
	}{
		{name: "api", want: "Nothing is running in this project. Start it with hum start api."},
		{name: "adhoc", want: "Nothing is running."},
	} {
		_, stderr, err := stopShutdownRun(t, "signal", test.name, "HUP")
		if err == nil {
			t.Fatalf("signal %s without a daemon unexpectedly succeeded", test.name)
		}
		if !strings.Contains(err.Error(), test.want) {
			t.Fatalf("signal %s without a daemon = %q, want %q", test.name, err.Error(), test.want)
		}
		if strings.Contains(err.Error(), "dial unix") || strings.Contains(stderr, "dial unix") {
			t.Fatalf("signal %s leaked the socket path: err=%q stderr=%q", test.name, err.Error(), stderr)
		}
	}
	output, stderr, err := stopShutdownRun(t, "signal", "api", "HUP", "--json")
	if err == nil || stderr != "" {
		t.Fatalf("JSON signal without a daemon: err=%v stderr=%q output=%q", err, stderr, output)
	}
	if got := decodeJSONErrorObject(t, output); got.Code != string(jsonErrorDaemonUnavailable) {
		t.Fatalf("JSON signal without a daemon code = %q, want %q", got.Code, jsonErrorDaemonUnavailable)
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
