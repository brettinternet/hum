package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMCPHelp(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)
	if err := root.Run(context.Background(), []string{"hum", "mcp", "--help"}); err != nil {
		t.Fatalf("mcp help: %v", err)
	}
	help := strings.ToLower(output.String())
	for _, want := range []string{
		"stdio", "one-time", "project_root", "absolute existing", "start and up", "resolved", "status, logs, wait, input, restart, stop, remove, and signal",
		"ad_hoc", "hum run", "daemon shutdown or replacement", "argv-based environment activation",
		"twelve tools", "run, serve, and shutdown are not mcp tools",
		"64", "-32001", "-32600", "-32800", "notifications/cancelled", "serialized", "parent cancellation",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("mcp help missing %q: %q", want, output.String())
		}
	}
}

func TestImplicitMixDiscoveryDoesNotExecuteProjectCode(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*testing.T) error
	}{
		{name: "list", run: func(t *testing.T) error {
			_, _, err := stopShutdownRun(t, "list", "--json")
			return err
		}},
		{name: "status", run: func(t *testing.T) error {
			output, _, err := stopShutdownRun(t, "status", "--json", "dev")
			if err != nil {
				return err
			}
			if got := statusDecodeJSON(t, output); got.State != "stopped" {
				return fmt.Errorf("discovered status state = %q, want stopped", got.State)
			}
			return nil
		}},
		{name: "completion", run: func(t *testing.T) error {
			_, _, err := runCompletionForTest(t, "start", "--generate-shell-completion")
			return err
		}},
		{name: "init", run: func(t *testing.T) error {
			_, _, err := stopShutdownRun(t, "init", "--json")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := stopShutdownTestProject(t)
			runtimeDir := filepath.Join(t.TempDir(), "runtime")
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			sentinel := filepath.Join(root, "mix-evaluated")
			mixSource := fmt.Sprintf("defmodule App.MixProject do\n  File.write!(%q, \"evaluated\")\n  defp deps, do: [{:phoenix, \"~> 1.7\"}]\nend\n", sentinel)
			if err := os.WriteFile(filepath.Join(root, "mix.exs"), []byte(mixSource), 0o600); err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			mixCommand := fmt.Sprintf("#!/bin/sh\ntouch %s\nexit 99\n", sentinel)
			if err := os.WriteFile(filepath.Join(bin, "mix"), []byte(mixCommand), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)

			if err := test.run(t); err != nil {
				t.Fatalf("%s discovery: %v", test.name, err)
			}
			if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s evaluated mix.exs or invoked Mix: %v", test.name, err)
			}
		})
	}
}

func TestMCPDiscoveryCancellationReapsCommand(t *testing.T) {
	root := t.TempDir()
	bin := t.TempDir()
	pidPath := filepath.Join(root, "mise.pid")
	misePath := filepath.Join(bin, "mise")
	script := fmt.Sprintf("#!/bin/sh\necho $$ > %s\nexec /bin/sleep 30\n", pidPath)
	if err := os.WriteFile(misePath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := (mcpResolver{}).Resolve(ctx, root)
		result <- err
	}()

	var pid int
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(pidPath)
		if err == nil {
			pid, err = strconv.Atoi(strings.TrimSpace(string(contents)))
			if err != nil {
				t.Fatalf("parse discovery pid: %v", err)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("MCP discovery command did not start")
	}

	cancelledAt := time.Now()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("resolver error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("MCP discovery did not cancel within two seconds")
	}
	if elapsed := time.Since(cancelledAt); elapsed >= 2*time.Second {
		t.Fatalf("MCP discovery cancellation took %s", elapsed)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.Signal(0)); !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("discovery process %d was not reaped: %v", pid, err)
	}
}

func TestCLIDiscoveryCancellationReapsCommand(t *testing.T) {
	root := stopShutdownTestProject(t)
	t.Setenv("HUM_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	bin := t.TempDir()
	pidPath := filepath.Join(root, "mise.pid")
	script := fmt.Sprintf("#!/bin/sh\necho $$ > %s\nexec /bin/sleep 30\n", pidPath)
	if err := os.WriteFile(filepath.Join(bin, "mise"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	var stdout, stderr bytes.Buffer
	go func() {
		result <- cliServeRunInvoke(ctx, []string{"list", "--json"}, &stdout, &stderr)
	}()

	var pid int
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(pidPath)
		if err == nil {
			pid, err = strconv.Atoi(strings.TrimSpace(string(contents)))
			if err != nil {
				t.Fatalf("parse discovery pid: %v", err)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("CLI discovery command did not start")
	}

	cancelledAt := time.Now()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("list error = %v, want context.Canceled; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CLI discovery did not cancel within two seconds")
	}
	if elapsed := time.Since(cancelledAt); elapsed >= 2*time.Second {
		t.Fatalf("CLI discovery cancellation took %s", elapsed)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.Signal(0)); !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("discovery process %d was not reaped: %v", pid, err)
	}
}

func TestMCPConcurrencyDescription(t *testing.T) {
	description := strings.ToLower(mcpCLICommand("dev", "unknown", &bytes.Buffer{}).Description)
	for _, want := range []string{"64", "-32001", "-32600", "-32800", "notifications/cancelled", "serialized", "parent cancellation"} {
		if !strings.Contains(description, want) {
			t.Errorf("mcp description missing %q: %q", want, description)
		}
	}
}
