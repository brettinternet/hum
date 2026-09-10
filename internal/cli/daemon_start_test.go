package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"hum/internal/config"
	"hum/internal/daemon"
	"hum/internal/process"
)

func TestEnsureDaemonWaitsForSequentialRecovery(t *testing.T) {
	groups := make([]daemon.RuntimeGroup, 0, 2)
	done := make([]<-chan struct{}, 0, 2)
	for _, name := range []string{"one", "two"} {
		group, groupDone := startCLIStaleGroup(t, name, "")
		groups = append(groups, group)
		done = append(done, groupDone)
	}
	runtimeDir := cliServeRunRuntimeDir(t)
	paths := writeCLIStaleState(t, runtimeDir, groups)
	cfg := cliDaemonTestConfig(runtimeDir, 3*time.Second)

	startedAt := time.Now()
	pid, err := ensureDaemon(context.Background(), cfg)
	if err != nil {
		log, _ := os.ReadFile(paths.Log)
		t.Fatalf("ensure daemon: %v; log=%q", err, log)
	}
	if elapsed := time.Since(startedAt); elapsed < daemonStartupTimeout {
		t.Fatalf("daemon became ready after %s, want sequential recovery to exceed %s", elapsed, daemonStartupTimeout)
	}
	client, err := daemon.DialRuntime(context.Background(), paths)
	if err != nil {
		t.Fatalf("dial recovered daemon: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("daemon pid = %d, want positive", pid)
	}
	if err := client.Shutdown(context.Background(), daemon.ShutdownRequest{Force: true}); err != nil {
		t.Fatalf("shutdown recovered daemon: %v", err)
	}
	_ = client.Close()
	for index, groupDone := range done {
		select {
		case <-groupDone:
		case <-time.After(3 * time.Second):
			t.Fatalf("stale group %d was not reaped", index)
		}
	}
}

func TestEnsureDaemonCancellationReapsChild(t *testing.T) {
	termMarker := filepath.Join(t.TempDir(), "term-observed")
	group, groupDone := startCLIStaleGroup(t, "cancel", termMarker)
	runtimeDir := cliServeRunRuntimeDir(t)
	paths := writeCLIStaleState(t, runtimeDir, []daemon.RuntimeGroup{group})
	cfg := cliDaemonTestConfig(runtimeDir, 3*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	startedAt := time.Now()
	go func() {
		_, err := ensureDaemon(ctx, cfg)
		result <- err
	}()

	waitCLIPath(t, termMarker, 3*time.Second)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ensure daemon cancellation error = %v, want context canceled", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ensure daemon did not return promptly after cancellation")
	}
	if elapsed := time.Since(startedAt); elapsed >= time.Second {
		t.Fatalf("canceled startup took %s", elapsed)
	}
	select {
	case <-groupDone:
	case <-time.After(5 * time.Second):
		t.Fatal("reconciled stale group was not reaped")
	}
	for _, path := range []string{paths.Socket, paths.PID, paths.Ready, paths.State} {
		waitCLIPathAbsent(t, path, 3*time.Second)
	}
}

func startCLIStaleGroup(t *testing.T, name, termMarker string) (daemon.RuntimeGroup, <-chan struct{}) {
	t.Helper()
	readyMarker := filepath.Join(t.TempDir(), "ready")
	script := "trap '' TERM; printf ready > \"$READY_MARKER\"; while :; do :; done"
	if termMarker != "" {
		script = "trap 'printf term > \"$TERM_MARKER\"' TERM; printf ready > \"$READY_MARKER\"; while :; do :; done"
	}
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = append(os.Environ(), "READY_MARKER="+readyMarker, "TERM_MARKER="+termMarker)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	})
	waitCLIPath(t, readyMarker, 3*time.Second)
	identity, err := process.ProcessStartIdentity(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	return daemon.RuntimeGroup{
		ProjectRoot: t.TempDir(), Name: name, LeaderPID: cmd.Process.Pid,
		PGID: cmd.Process.Pid, StartIdentity: identity,
	}, done
}

func cliDaemonTestConfig(runtimeDir string, stopGrace time.Duration) config.Config {
	return config.Config{
		RuntimeDir: runtimeDir, StopGrace: stopGrace,
		OutputBytes: config.DefaultOutputBytes, CompletedRecords: config.DefaultCompletedRecords,
		ReadEntries: config.DefaultReadEntries, ReadBytes: config.DefaultReadBytes, MaxLineBytes: config.MaxLineBytes,
	}
}

func writeCLIStaleState(t *testing.T, runtimeDir string, groups []daemon.RuntimeGroup) daemon.RuntimePaths {
	t.Helper()
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := daemon.NewRuntimePaths(runtimeDir)
	state := daemon.RuntimeState{
		Version: daemon.RuntimeStateVersion,
		Daemon:  daemon.RuntimeIdentity{PID: 2147483646, StartIdentity: "dead:daemon"},
		Groups:  groups,
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.State, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return paths
}

func waitCLIPathAbsent(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for removal of %s", path)
}

func waitCLIPath(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func TestEnsureDaemonFailsFastWhenChildExitsEarly(t *testing.T) {
	runtimeDir := cliServeRunRuntimeDir(t)
	groups := []daemon.RuntimeGroup{
		{ProjectRoot: "/project", Name: "one", LeaderPID: 2147483644, PGID: 2147483644, StartIdentity: "dead:one"},
		{ProjectRoot: "/project", Name: "two", LeaderPID: 2147483645, PGID: 2147483645, StartIdentity: "dead:two"},
	}
	paths := writeCLIStaleState(t, runtimeDir, groups)
	// A live process recorded as the daemon owner makes the child refuse the
	// runtime immediately; the recorded groups only inflate the budget.
	if err := os.WriteFile(paths.PID, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := cliDaemonTestConfig(runtimeDir, 3*time.Second)

	startedAt := time.Now()
	_, err := ensureDaemon(context.Background(), cfg)
	if err == nil {
		t.Fatal("ensure daemon succeeded although the runtime is owned by a live process")
	}
	if !strings.Contains(err.Error(), "exited before readiness") {
		t.Fatalf("ensure daemon error = %v, want the child's early exit", err)
	}
	if elapsed := time.Since(startedAt); elapsed >= 2*time.Second {
		t.Fatalf("early child exit was reported after %s, want well under the %s recovery budget", elapsed, 2*2*cfg.StopGrace+daemonStartupTimeout)
	}
}
