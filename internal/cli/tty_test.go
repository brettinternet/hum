package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/project"
)

func TestTTYCLI(t *testing.T) {
	if err := manifestTTYUpgradeError(project.Definition{Name: "dev", TTY: true}, app.Process{Name: "dev", State: app.StateRunning, TTY: false}); err == nil || !strings.Contains(err.Error(), "stop it and rerun") {
		t.Fatalf("running non-tty upgrade error = %v", err)
	}

	runtimeDir := t.TempDir()
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	var output, errors bytes.Buffer
	if err := cliServeRunInvoke(context.Background(), []string{"run", "dev", "--tty", "--detach", "--", "/bin/sh", "-c", "printf tty; sleep 1"}, &output, &errors); err != nil {
		t.Fatalf("tty detached run: %v (stderr=%q)", err, errors.String())
	}
	if !strings.Contains(output.String(), "started dev") {
		t.Fatalf("run output = %q", output.String())
	}
	output.Reset()
	errors.Reset()
	if err := cliServeRunInvoke(context.Background(), []string{"status", "dev"}, &output, &errors); err != nil {
		t.Fatalf("tty status: %v", err)
	}
	if !strings.Contains(output.String(), "tty: true") {
		t.Fatalf("status output = %q", output.String())
	}
	output.Reset()
	errors.Reset()
	if err := cliServeRunInvoke(context.Background(), []string{"run", "missing", "--tty"}, &output, &errors); err == nil || !strings.Contains(err.Error(), "--tty requires") {
		t.Fatalf("missing tty command error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	client, err := daemon.Dial(context.Background(), daemon.NewRuntimePaths(runtimeDir).Socket)
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	defer client.Close()
	current, err := client.Get(context.Background(), daemon.GetRequest{Name: "dev", Cwd: cwd})
	if err != nil {
		t.Fatalf("get tty process: %v", err)
	}
	owner, err := client.InputAttach(context.Background(), daemon.InputAttachRequest{Name: "dev", Cwd: current.Cwd, Root: current.Root, TTY: true, Argv: current.Argv, Source: current.Source})
	if err != nil {
		t.Fatalf("attach owner for %+v: %v", current, err)
	}
	defer owner.Release()
	deadline := time.Now().Add(3 * time.Second)
	for {
		process, getErr := client.Get(context.Background(), daemon.GetRequest{Name: "dev", Cwd: cwd})
		if getErr == nil && process.State == app.StateExited {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dev did not stop: process=%+v err=%v", process, getErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-owner.Events():
	case <-time.After(time.Second):
		t.Fatal("owner did not receive stopped state")
	}
	conflictCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	output.Reset()
	errors.Reset()
	_ = cliServeRunInvoke(conflictCtx, []string{"run", "dev", "--tty", "--", "/bin/echo", "replacement"}, &output, &errors)
	if !strings.Contains(errors.String(), "following output only") {
		t.Fatalf("conflict notice = %q", errors.String())
	}
	select {
	case event := <-owner.Events():
		t.Fatalf("competing output follower launched a successor: %+v", event)
	default:
	}

	if _, _, err := cliServeRunInvokeForTest("shutdown", "--stop-processes"); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestTTYHelpAndDocs(t *testing.T) {
	var output, errors bytes.Buffer
	root := NewRootCommand("test", "test", &output, &errors)
	if err := root.Run(context.Background(), []string{"hum", "run", "--help"}); err != nil {
		t.Fatal(err)
	}
	help := strings.ToLower(output.String())
	for _, want := range []string{"--tty", "pseudo-terminal", "ctrl-]", "ctrl+c"} {
		if !strings.Contains(help, strings.ToLower(want)) {
			t.Errorf("run help missing %q: %q", want, output.String())
		}
	}
	for _, path := range []string{"../../README.md", "../../docs/design.md", "../../docs/coding-agents.md", "../skill/SKILL.md", "../../plugins/hum/skills/hum/SKILL.md"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		content := strings.ToLower(string(data))
		for _, want := range []string{"tty: true", "ctrl-]", "raw mode", "mcp", "shutdown"} {
			if !strings.Contains(content, strings.ToLower(want)) {
				t.Errorf("%s missing %q", path, want)
			}
		}
	}
}
