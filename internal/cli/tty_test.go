//go:build !windows

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

	"github.com/creack/pty"
	"golang.org/x/term"
	"reflect"
)

func TestTTYAttachDetach(t *testing.T) {
	for _, test := range []struct {
		input, forwarded string
		detach           bool
	}{
		{"Bob\n", "Bob\n", false},
		{"Bob\n\x1dignored", "Bob\n", true},
		{"\x1dignored", "", true},
	} {
		got, detach := ttyInputBeforeDetach([]byte(test.input))
		if string(got) != test.forwarded || detach != test.detach {
			t.Fatalf("input %q: forwarded %q detach %t", test.input, got, detach)
		}
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	before, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := term.MakeRaw(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	input := &ttyInput{stdin: slave, restore: func() { _ = term.Restore(int(slave.Fd()), raw) }}
	input.restoreLocal()
	input.restoreLocal() // idempotent even on concurrent detach/close paths
	after, err := term.GetState(int(slave.Fd()))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("terminal state not restored: before=%v after=%v err=%v", before, after, err)
	}
}

func TestTTYCLI(t *testing.T) {
	if runCLIIsolatedTest(t) {
		return
	}
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
	stoppedCtx, cancelStopped := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStopped()
	for {
		event, nextErr := owner.Next(stoppedCtx)
		if nextErr != nil {
			t.Fatalf("owner did not receive stopped state: %v", nextErr)
		}
		if event.State == "stopped" {
			break
		}
	}
	process, getErr := client.Get(context.Background(), daemon.GetRequest{Name: "dev", Cwd: cwd})
	if getErr != nil || process.State != app.StateExited {
		t.Fatalf("dev did not stop: process=%+v err=%v", process, getErr)
	}
	if err := owner.Release(); err != nil {
		t.Fatalf("release stopped input owner: %v", err)
	}
	conflictCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output.Reset()
	errors.Reset()
	err = cliServeRunInvoke(conflictCtx, []string{"run", "dev", "--tty", "--", "/bin/echo", "replacement"}, &output, &errors)
	if err != nil || !strings.Contains(output.String(), "replacement") {
		latest, latestErr := client.Get(context.Background(), daemon.GetRequest{Name: "dev", Cwd: cwd})
		logs, logsErr := client.Output(context.Background(), daemon.OutputRequest{Name: "dev", Cwd: cwd})
		t.Fatalf("replacement TTY run = %v, output %q stderr %q; process=%#v err=%v exit=%#v logs=%+v logsErr=%v", err, output.String(), errors.String(), latest, latestErr, latest.Exit, logs, logsErr)
	}

	if _, _, err := cliServeRunInvokeForTest("shutdown", "--stop-processes"); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
