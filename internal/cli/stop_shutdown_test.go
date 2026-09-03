package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
)

func TestStop(t *testing.T) {
	t.Run("human reports one result for every name and is idempotent", func(t *testing.T) {
		projectRoot := stopShutdownTestProject(t)
		server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

		stopShutdownStartProcess(t, server, projectRoot, "alpha", []string{"/bin/sh", "-c", "sleep 30"})
		stopShutdownStartProcess(t, server, projectRoot, "beta", []string{"/bin/sh", "-c", "sleep 30"})

		names := []string{"alpha", "beta", "alpha", "missing"}
		args := append([]string{"stop"}, names...)
		stdout, stderr, err := stopShutdownRun(t, args...)
		if err != nil {
			t.Fatalf("stop: %v", err)
		}
		if stderr != "" {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
		stopShutdownAssertHumanResults(t, stdout, names)
		if active := stopShutdownListActive(t, server, projectRoot); len(active) != 0 {
			t.Fatalf("active processes after stop = %#v, want none", active)
		}
	})

	t.Run("json reports one result for every name and is idempotent", func(t *testing.T) {
		projectRoot := stopShutdownTestProject(t)
		server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

		stopShutdownStartProcess(t, server, projectRoot, "alpha", []string{"/bin/sh", "-c", "sleep 30"})
		stopShutdownStartProcess(t, server, projectRoot, "beta", []string{"/bin/sh", "-c", "sleep 30"})

		names := []string{"alpha", "beta", "alpha", "missing"}
		args := append([]string{"stop", "--json"}, names...)
		stdout, stderr, err := stopShutdownRun(t, args...)
		if err != nil {
			t.Fatalf("stop --json: %v", err)
		}
		if stderr != "" {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
		results := stopShutdownDecodeResults(t, stdout)
		if len(results) != len(names) {
			t.Fatalf("JSON result count = %d, want %d: %q", len(results), len(names), stdout)
		}
		for i, result := range results {
			if result.Name != names[i] {
				t.Errorf("JSON result %d name = %q, want %q", i, result.Name, names[i])
			}
			if result.Status == "" {
				t.Errorf("JSON result %d has empty status: %q", i, stdout)
				continue
			}
			if i < 2 && stopShutdownNormalizeStatus(result.Status) != "stopped" {
				t.Errorf("JSON result %d status = %q, want stopped", i, result.Status)
			}
			if i >= 2 && stopShutdownNormalizeStatus(result.Status) != "not_running" {
				t.Errorf("JSON result %d status = %q, want not_running", i, result.Status)
			}
		}
		if active := stopShutdownListActive(t, server, projectRoot); len(active) != 0 {
			t.Fatalf("active processes after JSON stop = %#v, want none", active)
		}
	})
}

func TestShutdown(t *testing.T) {
	t.Run("default refuses and lists each active project/name", func(t *testing.T) {
		projectRoot := stopShutdownTestProject(t)
		server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

		stopShutdownStartProcess(t, server, projectRoot, "alpha", []string{"/bin/sh", "-c", "sleep 30"})
		stopShutdownStartProcess(t, server, projectRoot, "beta", []string{"/bin/sh", "-c", "sleep 30"})

		_, stderr, err := stopShutdownRun(t, "shutdown")
		if err == nil {
			t.Fatal("shutdown unexpectedly succeeded with active processes")
		}
		refusal := err.Error() + "\n" + stderr
		for _, name := range []string{"alpha", "beta"} {
			want := projectRoot + ": " + name
			if !strings.Contains(refusal, want) {
				t.Errorf("shutdown refusal missing %q: %q", want, refusal)
			}
		}
		if _, statErr := os.Stat(server.Paths().Socket); statErr != nil {
			t.Fatalf("daemon socket after refusal: %v", statErr)
		}
		active := stopShutdownListActive(t, server, projectRoot)
		if len(active) != 2 {
			t.Fatalf("active processes after refusal = %#v, want two", active)
		}
	})

	t.Run("--stop-processes waits for graceful process-tree termination", func(t *testing.T) {
		projectRoot := stopShutdownTestProject(t)
		server, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

		childPIDPath := filepath.Join(projectRoot, "child.pid")
		termPath := filepath.Join(projectRoot, "term")
		releasePath := filepath.Join(projectRoot, "release")
		donePath := filepath.Join(projectRoot, "done")
		// If an assertion fails before the release is written, cleanup must still
		// let the graceful child leave its barrier before forcing the daemon down.
		t.Cleanup(func() { _ = os.WriteFile(releasePath, []byte("release"), 0o600) })

		script := `
(
  trap 'printf term > "$2"; while [ ! -e "$3" ]; do sleep 0.01; done; printf done > "$4"; exit 0' TERM
  while :; do sleep 1; done
) &
printf '%s' "$!" > "$1"
trap 'exit 0' TERM
while :; do sleep 1; done
`
		managed := stopShutdownStartProcess(t, server, projectRoot, "tree", []string{"/bin/sh", "-c", script, "hum-test", childPIDPath, termPath, releasePath, donePath})
		stopShutdownWaitForFile(t, childPIDPath, 3*time.Second)

		commandDone := make(chan error, 1)
		go func() {
			var stdout, stderr bytes.Buffer
			commandDone <- NewRootCommand("test", "test", &stdout, &stderr).Run(context.Background(), []string{"hum", "shutdown", "--stop-processes"})
		}()

		stopShutdownWaitForFile(t, termPath, 3*time.Second)
		select {
		case err := <-commandDone:
			t.Fatalf("forced shutdown returned before graceful child release: %v", err)
		default:
		}
		if !stopShutdownProcessGroupAlive(managed.PGID) {
			t.Fatal("managed process group exited before graceful child release")
		}
		for _, path := range []string{server.Paths().Socket, server.Paths().PID, server.Paths().Ready} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("runtime artifact %q disappeared before graceful child release: %v", path, err)
			}
		}

		if err := os.WriteFile(releasePath, []byte("release"), 0o600); err != nil {
			t.Fatalf("release graceful child: %v", err)
		}
		stopShutdownWaitForFile(t, donePath, 3*time.Second)
		select {
		case err := <-commandDone:
			if err != nil {
				t.Fatalf("shutdown --stop-processes: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("shutdown --stop-processes did not wait for process tree completion")
		}

		stopShutdownWaitForPathGone(t, server.Paths().Socket, 3*time.Second)
		stopShutdownWaitForPathGone(t, server.Paths().PID, 3*time.Second)
		stopShutdownWaitForPathGone(t, server.Paths().Ready, 3*time.Second)
		stopShutdownWaitForProcessGroupGone(t, managed.PGID, 3*time.Second)
	})
}

type stopShutdownJSONResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func stopShutdownTestServer(t *testing.T, stopGrace time.Duration) (*daemon.Server, string) {
	t.Helper()
	runtimeDir, err := os.MkdirTemp("/tmp", "h-")
	if err != nil {
		t.Fatalf("create runtime directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })

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
	projectRoot, err := os.MkdirTemp("/tmp", "h-proj-")
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
	// Resolve the path the CLI will report after chdir. On macOS, /tmp is a
	// symlink, so os.Getwd can return /private/tmp while MkdirTemp returns /tmp.
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
	client, err := daemon.Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatalf("dial daemon for %q: %v", name, err)
	}
	defer client.Close()
	process, err := client.Start(context.Background(), daemon.StartRequest{
		Name: name,
		Cwd:  projectRoot,
		Argv: append([]string(nil), argv...),
		Env:  os.Environ(),
	})
	if err != nil {
		t.Fatalf("start %q: %v", name, err)
	}
	return process
}

func stopShutdownRun(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	argv := append([]string{"hum"}, args...)
	err := NewRootCommand("test", "test", &stdout, &stderr).Run(context.Background(), argv)
	return stdout.String(), stderr.String(), err
}

func stopShutdownListActive(t *testing.T, server *daemon.Server, projectRoot string) []app.Process {
	t.Helper()
	client, err := daemon.Dial(context.Background(), server.Paths().Socket)
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

func stopShutdownProcessGroupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || !errors.Is(err, syscall.ESRCH)
}

func stopShutdownWaitForProcessGroupGone(t *testing.T, pgid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for stopShutdownProcessGroupAlive(pgid) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for process group %d to disappear", pgid)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
