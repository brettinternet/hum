package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	urfavecli "github.com/urfave/cli/v3"
	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/testutil"
)

func cliServeRunInvoke(ctx context.Context, args []string, writer, errWriter io.Writer) error {
	command := NewRootCommand("test", "test", writer, errWriter)
	command.ExitErrHandler = func(context.Context, *urfavecli.Command, error) {}
	return command.Run(ctx, append([]string{"hum"}, args...))
}

func cliServeRunInvokeForTest(args ...string) (string, string, error) {
	var output, errorOutput bytes.Buffer
	err := cliServeRunInvoke(context.Background(), args, &output, &errorOutput)
	return output.String(), errorOutput.String(), err
}

func cliServeRunRuntimeDir(t *testing.T) string {
	t.Helper()
	dir := testutil.RuntimeDir(t)
	t.Cleanup(func() { cliServeRunCleanupRuntime(t, dir) })
	return dir
}

func cliServeRunCleanupRuntime(t *testing.T, runtimeDir string) {
	t.Helper()
	paths := daemon.NewRuntimePaths(runtimeDir)
	pid := 0
	if data, err := os.ReadFile(paths.PID); err == nil {
		pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if client, err := daemon.DialRuntime(ctx, paths); err == nil {
		if shutdownErr := client.Shutdown(ctx, daemon.ShutdownRequest{Force: true}); shutdownErr != nil {
			t.Errorf("clean up daemon runtime %s: %v", runtimeDir, shutdownErr)
		}
		_ = client.Close()
	}
	if pid > 0 && pid != os.Getpid() {
		testutil.WaitForProcessGone(t, pid, 5*time.Second)
	}
	_ = os.RemoveAll(runtimeDir)
}

func stopShutdownTestServer(t *testing.T, stopGrace time.Duration) (*daemon.Server, string) {
	t.Helper()
	runtimeDir := testutil.RuntimeDir(t)
	server, err := daemon.NewServer(daemon.Config{RuntimeDir: runtimeDir, StopGrace: stopGrace})
	if err != nil {
		t.Fatalf("create runtime daemon: %v", err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	readyCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.WaitReady(readyCtx); err != nil {
		_ = server.Close()
		t.Fatalf("wait for runtime daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Close()
		select {
		case <-serveDone:
		case <-time.After(3 * time.Second):
			t.Errorf("runtime daemon did not exit during cleanup")
		}
	})
	return server, runtimeDir
}

func stopShutdownTestProject(t *testing.T) string {
	t.Helper()
	projectRoot, err := os.MkdirTemp("", "h-proj-")
	if err != nil {
		t.Fatalf("create project directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(projectRoot) })
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("change working directory to %q: %v", projectRoot, err)
	}
	projectRoot, err = os.Getwd()
	if err != nil {
		t.Fatalf("get project working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Errorf("restore working directory to %q: %v", oldwd, err)
		}
	})
	return projectRoot
}

func stopShutdownStartProcess(t *testing.T, server *daemon.Server, projectRoot, name string, argv []string) app.Process {
	t.Helper()
	client, err := daemon.DialRuntime(context.Background(), server.Paths())
	if err != nil {
		t.Fatalf("dial daemon for %q: %v", name, err)
	}
	defer client.Close()
	process, err := client.Start(context.Background(), daemon.StartRequest{
		Name: name, Cwd: projectRoot, Argv: append([]string(nil), argv...), Env: os.Environ(),
	})
	if err != nil {
		t.Fatalf("start %q: %v", name, err)
	}
	return process
}

func stopShutdownRun(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := cliServeRunInvoke(context.Background(), args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func stopShutdownListActive(t *testing.T, server *daemon.Server, projectRoot string) []app.Process {
	t.Helper()
	client, err := daemon.DialRuntime(context.Background(), server.Paths())
	if err != nil {
		t.Fatalf("dial daemon to list active processes: %v", err)
	}
	defer client.Close()
	items, err := client.List(context.Background(), daemon.ListRequest{Cwd: projectRoot})
	if err != nil {
		t.Fatalf("list active processes: %v", err)
	}
	return items
}

func stopShutdownAssertHumanResults(t *testing.T, output string, names []string) {
	t.Helper()
	lines := stopShutdownNonEmptyLines(output)
	if len(lines) != len(names) {
		t.Fatalf("human result count = %d, want %d: %q", len(lines), len(names), output)
	}
	for i, name := range names {
		line := strings.ToLower(lines[i])
		if !strings.Contains(line, strings.ToLower(name)) {
			t.Errorf("human result %d = %q, missing name %q", i, lines[i], name)
		}
		if i < 2 && !strings.Contains(line, "stopped") {
			t.Errorf("human result %d = %q, want stopped status", i, lines[i])
		}
		if i >= 2 && !strings.Contains(line, "not running") && !strings.Contains(line, "not_running") && !strings.Contains(line, "already stopped") && !strings.Contains(line, "already-stopped") {
			t.Errorf("human result %d = %q, want already-stopped/not-running status", i, lines[i])
		}
	}
}

func stopShutdownDecodeResults(t *testing.T, output string) []stopShutdownJSONResult {
	t.Helper()
	lines := stopShutdownNonEmptyLines(output)
	results := make([]stopShutdownJSONResult, 0, len(lines))
	for i, line := range lines {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatalf("decode JSON result %d: %v (%q)", i, err, line)
		}
		if _, ok := raw["name"]; !ok {
			t.Fatalf("JSON result %d has no stable name field: %q", i, line)
		}
		if _, ok := raw["status"]; !ok {
			t.Fatalf("JSON result %d has no stable status field: %q", i, line)
		}
		var result stopShutdownJSONResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatalf("decode JSON result %d fields: %v (%q)", i, err, line)
		}
		results = append(results, result)
	}
	return results
}

func stopShutdownNonEmptyLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func stopShutdownNormalizeStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	status = strings.ReplaceAll(status, "-", "_")
	status = strings.ReplaceAll(status, " ", "_")
	return status
}

func stopShutdownWaitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func stopShutdownWaitForPathGone(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q to disappear (err=%v)", path, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type stopShutdownJSONResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
