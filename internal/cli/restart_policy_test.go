package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/process"
)

func TestRestartPolicyCLI(t *testing.T) {
	root := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  crash:
    argv: [/bin/sh, -c, "exit 1"]
    restart: on-failure
`)

	stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "crash")
	if err != nil || stderr != "" {
		t.Fatalf("start: err=%v stderr=%q stdout=%q", err, stderr, stdout)
	}
	var launch manifestLaunchResult
	if err := json.Unmarshal([]byte(stdout), &launch); err != nil {
		t.Fatalf("decode start result %q: %v", stdout, err)
	}
	if launch.Restart != string(app.RestartOnFailure) || launch.Relaunches != 0 {
		t.Fatalf("start policy fields = %#v", launch)
	}

	client, err := daemon.Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var pending app.Process
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending, err = client.Get(context.Background(), daemon.GetRequest{Name: "crash", Cwd: root})
		if err == nil && pending.NextLaunchAt != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil || pending.NextLaunchAt == nil || pending.Restart != app.RestartOnFailure || pending.Relaunches != 0 {
		t.Fatalf("pending process = %#v, err=%v", pending, err)
	}

	stdout, stderr, err = stopShutdownRun(t, "status", "--json", "crash")
	if err != nil || stderr != "" {
		t.Fatalf("status JSON: err=%v stderr=%q stdout=%q", err, stderr, stdout)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatal(err)
	}
	if status["restart"] != "on-failure" || status["relaunches"] != float64(0) || status["next_launch_at"] == nil {
		t.Fatalf("status JSON policy fields = %#v", status)
	}

	stdout, stderr, err = stopShutdownRun(t, "list", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("list JSON: err=%v stderr=%q stdout=%q", err, stderr, stdout)
	}
	var listed listJSON
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Processes) != 1 || listed.Processes[0].Restart != "on-failure" || listed.Processes[0].Relaunches != 0 || listed.Processes[0].NextLaunchAt == nil {
		t.Fatalf("list JSON policy fields = %#v", listed.Processes)
	}

	stdout, stderr, err = stopShutdownRun(t, "status", "crash")
	if err != nil || stderr != "" || !strings.Contains(stdout, "restart: on-failure") || !strings.Contains(stdout, "relaunching in ") || !strings.Contains(stdout, "attempt 1/5") {
		t.Fatalf("status human: err=%v stderr=%q stdout=%q", err, stderr, stdout)
	}

	stdout, stderr, err = stopShutdownRun(t, "stop", "--json", "crash")
	if err != nil || stderr != "" || !strings.Contains(stdout, `"status":"stopped"`) {
		t.Fatalf("stop pending: err=%v stderr=%q stdout=%q", err, stderr, stdout)
	}
	stopped, err := client.Get(context.Background(), daemon.GetRequest{Name: "crash", Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	if stopped.NextLaunchAt != nil || stopped.Relaunches != 0 {
		t.Fatalf("stopped policy fields = %#v", stopped)
	}

	// Human output uses a ceiling countdown and a single exhausted restart line.
	next := time.Now().Add(1500 * time.Millisecond)
	var rendered bytes.Buffer
	if err := renderStatusHuman(&rendered, app.Process{Name: "pending", Source: "manifest", Root: root, State: app.StateExited, Restart: app.RestartOnFailure, Relaunches: 2, NextLaunchAt: &next}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.String(), "relaunching in 2s (attempt 3/5)") {
		t.Fatalf("ceiling status = %q", rendered.String())
	}
	rendered.Reset()
	exit := &processResultForCLITest
	if err := renderStatusHuman(&rendered, app.Process{Name: "exhausted", Source: "manifest", Root: root, State: app.StateExited, Restart: app.RestartOnFailure, Relaunches: 5, ExitCode: 1, Exit: exit}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(rendered.String(), "gave up after 5 relaunch attempts") != 1 {
		t.Fatalf("exhausted status = %q", rendered.String())
	}
}

func TestUpRecoveryDocs(t *testing.T) {
	documents := map[string]string{
		"README.md":      restartPolicyDoc(t, "../../README.md"),
		"docs/design.md": restartPolicyDoc(t, "../../docs/design.md"),
	}
	for path, content := range documents {
		lower := strings.ToLower(strings.Join(strings.Fields(content), " "))
		for _, phrase := range []string{"recovery_pending", "recovery_exhausted", "bounded", "automatic successor", "start request", "not running", "targeted", "hum start name", "hum restart name"} {
			if !strings.Contains(lower, phrase) {
				t.Errorf("%s missing recovery guidance %q", path, phrase)
			}
		}
		if !strings.Contains(lower, "exit 3") && !strings.Contains(lower, "exits 3") {
			t.Errorf("%s missing exit-3 guidance", path)
		}
		if strings.Contains(lower, "explicit start/up/restart adopts changed definitions") {
			t.Errorf("%s incorrectly claims recovery-preserving up adopts changed definitions", path)
		}
	}

	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	if err := root.Run(context.Background(), []string{"hum", "up", "--help"}); err != nil {
		t.Fatal(err)
	}
	help := strings.ToLower(strings.Join(strings.Fields(stdout.String()), " "))
	for _, phrase := range []string{"recovery_pending", "recovery_exhausted", "bounded", "automatic successor", "start request", "exit 3", "not running", "targeted start name", "restart name"} {
		if !strings.Contains(help, phrase) {
			t.Errorf("CLI up help missing %q: %q", phrase, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("CLI up help stderr = %q", stderr.String())
	}
}

var processResultForCLITest = process.Result{ExitCode: 1, ExitedAt: time.Unix(1, 0)}
