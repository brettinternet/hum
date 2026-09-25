package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"hum/internal/config"
	"hum/internal/daemon"
	"hum/internal/output"
	"hum/internal/testutil"
)

const windowsDaemonChildMarker = "HUM_TEST_DAEMON_CHILD_MARKER"

func TestMain(m *testing.M) {
	if os.Getenv(daemonChildEnv) == daemonChildEnvValue {
		if marker := os.Getenv(windowsDaemonChildMarker); marker != "" {
			if err := os.WriteFile(marker, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
		command := NewRootCommand("test", "test", os.Stdout, os.Stderr)
		if err := command.Run(context.Background(), []string{"hum", "serve"}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestWindowsBuiltBinaryConcurrentAutostartAndLifecycle(t *testing.T) {
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)
	projectRoot := t.TempDir()

	t.Run("concurrent autostart selects one daemon", func(t *testing.T) {
		runtimeDir := testutil.RuntimeDir(t)
		env := testutil.RuntimeEnv(runtimeDir, "HUM_STOP_GRACE=100ms")
		paths := daemon.NewRuntimePaths(runtimeDir)
		t.Cleanup(func() {
			_ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes")
		})

		const racers = 6
		results := make(chan testutil.Result, racers)
		markers := make([]string, racers)
		for index := range racers {
			name := fmt.Sprintf("race-%d", index)
			markers[index] = filepath.Join(projectRoot, name)
			go func() {
				results <- testutil.Run(t, hum, projectRoot, env,
					"run", name, "--detach", "--", fixture, "stream", markers[index])
			}()
		}
		for range racers {
			result := <-results
			if result.Code != 0 || result.Stderr != "" {
				log, _ := os.ReadFile(paths.Log)
				t.Fatalf("concurrent run: code=%d err=%v stdout=%q stderr=%q daemon log=%q", result.Code, result.Err, result.Stdout, result.Stderr, log)
			}
		}
		for _, marker := range markers {
			testutil.WaitForFile(t, marker+".started", 10*time.Second)
		}

		pidBytes, err := os.ReadFile(paths.PID)
		if err != nil {
			t.Fatalf("read daemon PID: %v", err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
		if err != nil || pid <= 0 {
			t.Fatalf("daemon PID = %q; parse error %v", pidBytes, err)
		}
		var state daemon.RuntimeState
		stateBytes, err := os.ReadFile(paths.State)
		if err != nil {
			t.Fatalf("read daemon state: %v", err)
		}
		if err := json.Unmarshal(stateBytes, &state); err != nil {
			t.Fatalf("decode daemon state: %v", err)
		}
		if state.Daemon.PID != pid {
			t.Fatalf("runtime state daemon PID = %d, PID artifact = %d", state.Daemon.PID, pid)
		}
		client, err := daemon.DialRuntime(context.Background(), paths)
		if err != nil {
			t.Fatalf("dial concurrent daemon: %v", err)
		}
		processes, err := client.List(context.Background(), daemon.ListRequest{Cwd: projectRoot})
		_ = client.Close()
		if err != nil {
			t.Fatalf("list concurrent processes: %v", err)
		}
		if len(processes) != racers {
			t.Fatalf("managed process count = %d, want %d: %+v", len(processes), racers, processes)
		}
		for _, process := range processes {
			if process.State != "running" {
				t.Errorf("process %q state = %q, want running", process.Name, process.State)
			}
		}
		for index := range racers {
			name := fmt.Sprintf("race-%d", index)
			result := testutil.Run(t, hum, projectRoot, env, "status", name)
			if result.Code != 0 || !strings.Contains(strings.ToLower(result.Stdout), "running") {
				t.Fatalf("status %s: code=%d err=%v stdout=%q stderr=%q", name, result.Code, result.Err, result.Stdout, result.Stderr)
			}
		}
	})

	t.Run("commands work with a non-TTY fixture", func(t *testing.T) {
		runtimeDir := testutil.RuntimeDir(t)
		env := testutil.RuntimeEnv(runtimeDir, "HUM_STOP_GRACE=100ms")
		paths := daemon.NewRuntimePaths(runtimeDir)
		t.Cleanup(func() {
			_ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes")
		})

		marker := filepath.Join(projectRoot, "adhoc")
		result := testutil.Run(t, hum, projectRoot, env, "run", "adhoc", "--detach", "--", fixture, "stream", marker)
		if result.Code != 0 || result.Stderr != "" {
			t.Fatalf("run fixture: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		testutil.WaitForFile(t, marker+".started", 10*time.Second)

		result = testutil.Run(t, hum, projectRoot, env, "wait", "adhoc", "--match", "stdout:live")
		if result.Code != 0 || result.Stderr != "" {
			t.Fatalf("wait for fixture output: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		result = testutil.Run(t, hum, projectRoot, env, "status", "adhoc")
		if result.Code != 0 || !strings.Contains(strings.ToLower(result.Stdout), "running") {
			t.Fatalf("status fixture: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		result = testutil.Run(t, hum, projectRoot, env, "logs", "adhoc")
		if result.Code != 0 || !strings.Contains(result.Stdout, "stdout:live") || !strings.Contains(result.Stdout, "stderr:live") {
			t.Fatalf("logs fixture: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		result = testutil.Run(t, hum, projectRoot, env, "signal", "adhoc", "HUP")
		if result.Code == 0 || !strings.Contains(result.Stderr, "unix signals are unsupported on Windows") {
			t.Fatalf("Unix signal result: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}

		// Pin the launch cursor so the exit remains observable even if the
		// subprocess starts after stop completes under CI load.
		waiter := testutil.Start(t, hum, projectRoot, env, "wait", "adhoc", "--after-cursor", "0")
		result = testutil.Run(t, hum, projectRoot, env, "stop", "adhoc")
		if result.Code != 0 || !strings.Contains(strings.ToLower(result.Stdout), "stopped") {
			t.Fatalf("stop fixture: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		if err := waiter.Wait(10 * time.Second); err != nil {
			t.Fatalf("wait for stopped fixture: %v; stdout=%q stderr=%q", err, waiter.Stdout(), waiter.Stderr())
		}

		configuredMarker := filepath.Join(projectRoot, "configured")
		writeWindowsFixtureManifest(t, projectRoot, "configured", []string{fixture, "stream", configuredMarker})
		result = testutil.Run(t, hum, projectRoot, env, "up", "--detach")
		if result.Code != 0 {
			t.Fatalf("up fixture: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		testutil.WaitForFile(t, configuredMarker+".started", 10*time.Second)
		result = testutil.Run(t, hum, projectRoot, env, "status", "configured")
		if result.Code != 0 || !strings.Contains(strings.ToLower(result.Stdout), "running") {
			t.Fatalf("status manifest fixture: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		result = testutil.Run(t, hum, projectRoot, env, "down")
		if result.Code != 0 || !strings.Contains(result.Stdout, "configured") {
			t.Fatalf("down fixture: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		client, err := daemon.DialRuntime(context.Background(), paths)
		if err != nil {
			t.Fatalf("down shut down daemon: %v", err)
		}
		_ = client.Close()

		shutdownMarker := filepath.Join(projectRoot, "shutdown-target")
		result = testutil.Run(t, hum, projectRoot, env, "run", "shutdown-target", "--detach", "--", fixture, "stream", shutdownMarker)
		if result.Code != 0 {
			t.Fatalf("start shutdown target: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		testutil.WaitForFile(t, shutdownMarker+".started", 10*time.Second)
		pidBytes, err := os.ReadFile(paths.PID)
		if err != nil {
			t.Fatalf("read daemon PID before shutdown: %v", err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
		if err != nil || pid <= 0 {
			t.Fatalf("daemon PID before shutdown = %q; parse error %v", pidBytes, err)
		}
		result = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes")
		if result.Code != 0 {
			t.Fatalf("shutdown: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		testutil.WaitForProcessGone(t, pid, 10*time.Second)
		testutil.WaitForPathGone(t, paths.PID, 10*time.Second)
	})

	t.Run("explicit TTY requests start a supervised child", func(t *testing.T) {
		runtimeDir := testutil.RuntimeDir(t)
		env := testutil.RuntimeEnv(runtimeDir)
		t.Cleanup(func() { _ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes") })
		result := testutil.Run(t, hum, projectRoot, env, "run", "tty", "--tty", "--detach", "--", fixture, "stream", filepath.Join(projectRoot, "tty"))
		if result.Code != 0 {
			t.Fatalf("TTY run result: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		testutil.WaitForFile(t, filepath.Join(projectRoot, "tty.started"), 10*time.Second)
		result = testutil.Run(t, hum, projectRoot, env, "status", "tty")
		if result.Code != 0 || !strings.Contains(result.Stdout, "tty: true") {
			t.Fatalf("TTY status result: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
	})
}

func TestWindowsEnsureDaemonCancellationReapsChild(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	server, err := daemon.NewServer(daemon.Config{RuntimeDir: runtimeDir, StopGrace: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("prepare private startup lock: %v", err)
	}
	paths := server.Paths()
	if err := server.Close(); err != nil {
		t.Fatalf("close lock initializer: %v", err)
	}
	lock, err := os.OpenFile(paths.Lock, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open startup lock: %v", err)
	}
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(lock.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped); err != nil {
		_ = lock.Close()
		t.Fatalf("hold startup lock: %v", err)
	}
	t.Cleanup(func() {
		_ = windows.UnlockFileEx(windows.Handle(lock.Fd()), 0, 1, 0, &overlapped)
		_ = lock.Close()
	})

	marker := filepath.Join(t.TempDir(), "daemon-child.pid")
	t.Setenv(windowsDaemonChildMarker, marker)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	startupDone := make(chan struct{})
	go func() {
		defer close(startupDone)
		_, startupErr := ensureDaemon(ctx, windowsDaemonTestConfig(runtimeDir))
		result <- startupErr
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-startupDone:
		case <-time.After(10 * time.Second):
			t.Errorf("ensure daemon startup did not stop during cleanup")
		}
	})

	testutil.WaitForFile(t, marker, 10*time.Second)
	pidBytes, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil || pid <= 0 {
		t.Fatalf("daemon child PID = %q; parse error %v", pidBytes, err)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ensure daemon cancellation error = %v, want context canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ensure daemon did not return after cancellation")
	}
	testutil.WaitForProcessGone(t, pid, 5*time.Second)
	for _, path := range []string{paths.PID, paths.Ready, paths.State} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("canceled daemon left runtime artifact %q: %v", path, err)
		}
	}
}

// TestWindowsRunInterruptStopsChild drives the same follow-loop interrupt
// callback used by foreground run without requiring a console on CI runners.
func TestWindowsRunInterruptStopsChild(t *testing.T) {
	fixture := testutil.BuildFixture(t)
	root := t.TempDir()
	server, runtimeDir := stopShutdownTestServer(t, 100*time.Millisecond)
	marker := filepath.Join(root, "interrupt")
	started := stopShutdownStartProcess(t, server, root, "interrupt", []string{fixture, "stream", marker})
	testutil.WaitForFile(t, marker+".started", 10*time.Second)
	client, err := daemon.DialRuntime(context.Background(), daemon.NewRuntimePaths(runtimeDir))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	follower, err := client.Follow(context.Background(), daemon.FollowRequest{Name: "interrupt", Cwd: root, UntilExit: true, Stream: "both"})
	if err != nil {
		t.Fatal(err)
	}
	defer follower.Close()
	signals := make(chan os.Signal, 1)
	signals <- os.Interrupt
	terminal := errors.New("terminal event observed")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, err = followLoop(ctx, follower, signals, func(event output.Event) error {
		if event.Exit != nil {
			return terminal
		}
		return nil
	}, func(sig os.Signal) (bool, error) {
		if sig != os.Interrupt || !interruptStopsAttachedRun() {
			return false, fmt.Errorf("unexpected interrupt signal %v", sig)
		}
		return false, stopInterruptedRun(client, daemon.StopRequest{Name: "interrupt", Cwd: root}, 100*time.Millisecond)
	})
	if !errors.Is(err, terminal) {
		t.Fatalf("foreground interrupt follow = %v, want terminal event", err)
	}
	testutil.WaitForProcessGone(t, started.PID, 5*time.Second)
	process, err := client.Get(context.Background(), daemon.GetRequest{Name: "interrupt", Cwd: root})
	if err != nil || process.State == "running" {
		t.Fatalf("interrupt left managed child running: %+v, %v", process, err)
	}
}

func windowsDaemonTestConfig(runtimeDir string) config.Config {
	return config.Config{
		RuntimeDir: runtimeDir, StopGrace: 100 * time.Millisecond,
		OutputBytes: config.DefaultOutputBytes, CompletedRecords: config.DefaultCompletedRecords,
		ReadEntries: config.DefaultReadEntries, ReadBytes: config.DefaultReadBytes, MaxLineBytes: config.MaxLineBytes,
	}
}

func writeWindowsFixtureManifest(t *testing.T, root, name string, argv []string) {
	t.Helper()
	encoded, err := json.Marshal(argv)
	if err != nil {
		t.Fatal(err)
	}
	contents := fmt.Sprintf("version: 1\nprocesses:\n  %s:\n    argv: %s\n", name, encoded)
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture manifest: %v", err)
	}
}
