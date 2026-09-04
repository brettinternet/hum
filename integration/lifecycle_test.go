package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"hum/internal/daemon"
	"hum/internal/protocol"
	"hum/internal/testutil"
)

const (
	lifecycleTimeout       = 8 * time.Second
	lifecycleDaemonLogMax  = 1 << 20
	lifecyclePollInterval  = 10 * time.Millisecond
	lifecycleStalePIDStart = 999999
)

type lifecycleRuntime struct {
	dir   string
	cwd   string
	env   []string
	paths daemon.RuntimePaths
}

func TestForegroundServe(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)
	runtime := lifecycleNewRuntime(t)

	serve := testutil.Start(t, hum, runtime.cwd, runtime.env, "serve")
	t.Cleanup(func() {
		if serve.Exited() {
			return
		}
		_ = serve.Signal(os.Interrupt)
		_ = serve.Wait(lifecycleTimeout)
	})

	// The foreground server writes its readiness artifact only after accepting
	// a real client connection. A built hum client is the readiness probe.
	list := testutil.Run(t, hum, runtime.cwd, runtime.env, "list")
	if list.Code != 0 || list.Err != nil {
		t.Fatalf("foreground readiness probe: code=%d err=%v stdout=%q stderr=%q", list.Code, list.Err, list.Stdout, list.Stderr)
	}
	if list.Stdout != "Nothing is running.\n" || list.Stderr != "" {
		t.Fatalf("foreground readiness probe output: stdout=%q stderr=%q", list.Stdout, list.Stderr)
	}
	testutil.WaitForFile(t, runtime.paths.Ready, lifecycleTimeout)
	lifecycleWaitForStderr(t, serve, "hum serve: listening on ", lifecycleTimeout)
	if serve.Stdout() != "" {
		t.Fatalf("foreground serve leaked stdout: %q", serve.Stdout())
	}
	foregroundPID := lifecycleParseListeningPID(t, serve.Stderr(), runtime.paths.Socket)
	if foregroundPID != serve.Cmd.Process.Pid {
		t.Fatalf("foreground reported PID %d, process PID is %d", foregroundPID, serve.Cmd.Process.Pid)
	}

	marker := filepath.Join(t.TempDir(), "foreground-stop")
	started := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "foreground-stop", "--detach", "--", fixture, "stream", marker)
	if started.Code != 0 || started.Err != nil {
		t.Fatalf("start managed process under foreground daemon: code=%d err=%v stdout=%q stderr=%q", started.Code, started.Err, started.Stdout, started.Stderr)
	}
	if started.Stderr != "" || !strings.Contains(started.Stdout, "started foreground-stop (PID ") {
		t.Fatalf("managed process launch output: stdout=%q stderr=%q", started.Stdout, started.Stderr)
	}
	testutil.WaitForFile(t, marker+".started", lifecycleTimeout)

	if err := serve.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt foreground daemon: %v", err)
	}
	if err := serve.Wait(lifecycleTimeout); err != nil {
		t.Fatalf("foreground daemon exit: %v; stdout=%q stderr=%q", err, serve.Stdout(), serve.Stderr())
	}
	testutil.WaitForFile(t, marker+".terminated", lifecycleTimeout)
	testutil.WaitForProcessGone(t, foregroundPID, lifecycleTimeout)
	lifecycleWaitPathGone(t, runtime.paths.Socket, lifecycleTimeout)
	lifecycleWaitPathGone(t, runtime.paths.PID, lifecycleTimeout)
	lifecycleWaitPathGone(t, runtime.paths.Ready, lifecycleTimeout)
	if serve.Stdout() != "" {
		t.Fatalf("foreground diagnostics leaked to stdout after shutdown: %q", serve.Stdout())
	}
	if serve.Stderr() != fmt.Sprintf("hum serve: listening on %s (PID %d)\n", runtime.paths.Socket, foregroundPID) {
		t.Fatalf("foreground stderr = %q, want one listening diagnostic", serve.Stderr())
	}
}

func TestDetachedServe(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := testutil.BuildHum(t)
	runtime := lifecycleNewRuntime(t)
	daemonPID := 0
	t.Cleanup(func() {
		lifecycleCleanupDaemon(t, hum, runtime, daemonPID)
	})

	first := testutil.Run(t, hum, runtime.cwd, runtime.env, "serve", "--daemon")
	if first.Code != 0 || first.Err != nil {
		t.Fatalf("detached serve: code=%d err=%v stdout=%q stderr=%q", first.Code, first.Err, first.Stdout, first.Stderr)
	}
	if first.Stdout != "" {
		t.Fatalf("detached serve stdout = %q, want empty", first.Stdout)
	}
	daemonPID = lifecycleParseListeningPID(t, first.Stderr, runtime.paths.Socket)
	wantStderr := fmt.Sprintf("hum serve: listening on %s (PID %d)\n", runtime.paths.Socket, daemonPID)
	if first.Stderr != wantStderr {
		t.Fatalf("detached serve stderr = %q, want %q", first.Stderr, wantStderr)
	}
	testutil.WaitForFile(t, runtime.paths.Ready, lifecycleTimeout)
	testutil.WaitForText(t, runtime.paths.Log, "hum serve: listening on ", lifecycleTimeout)
	if !testutil.ProcessAlive(daemonPID) {
		t.Fatalf("detached daemon PID %d is not alive after client exit", daemonPID)
	}
	lifecycleAssertDetachedSession(t, daemonPID)
	lifecycleAssertBoundedLog(t, runtime.paths.Log, lifecycleDaemonLogMax)

	second := testutil.Run(t, hum, runtime.cwd, runtime.env, "serve", "--daemon")
	if second.Code != 0 || second.Err != nil {
		t.Fatalf("idempotent detached serve: code=%d err=%v stdout=%q stderr=%q", second.Code, second.Err, second.Stdout, second.Stderr)
	}
	if second.Stdout != "" || second.Stderr != wantStderr {
		t.Fatalf("idempotent detached serve output: stdout=%q stderr=%q, want empty/%q", second.Stdout, second.Stderr, wantStderr)
	}
	if !testutil.ProcessAlive(daemonPID) {
		t.Fatalf("idempotent detached serve lost daemon PID %d", daemonPID)
	}
	lifecycleAssertBoundedLog(t, runtime.paths.Log, lifecycleDaemonLogMax)

	shutdown := testutil.Run(t, hum, runtime.cwd, runtime.env, "shutdown", "--stop-processes")
	if shutdown.Code != 0 || shutdown.Err != nil || shutdown.Stdout != "hum daemon shut down\n" || shutdown.Stderr != "" {
		t.Fatalf("detached daemon shutdown: code=%d err=%v stdout=%q stderr=%q", shutdown.Code, shutdown.Err, shutdown.Stdout, shutdown.Stderr)
	}
	testutil.WaitForProcessGone(t, daemonPID, lifecycleTimeout)
	lifecycleWaitPathGone(t, runtime.paths.Socket, lifecycleTimeout)
	lifecycleWaitPathGone(t, runtime.paths.PID, lifecycleTimeout)
	lifecycleWaitPathGone(t, runtime.paths.Ready, lifecycleTimeout)
	daemonPID = 0

	// A stale PID, readiness marker, and non-socket file at the socket path must
	// all be retired before automatic daemon setup binds the real socket.
	stalePID := lifecycleUnusedPID()
	if err := os.WriteFile(runtime.paths.PID, []byte(strconv.Itoa(stalePID)+"\n"), 0o600); err != nil {
		t.Fatalf("write stale PID: %v", err)
	}
	if err := os.WriteFile(runtime.paths.Ready, []byte(strconv.Itoa(stalePID)+"\n"), 0o600); err != nil {
		t.Fatalf("write stale readiness: %v", err)
	}
	if err := os.WriteFile(runtime.paths.Socket, []byte("stale socket"), 0o600); err != nil {
		t.Fatalf("write stale socket: %v", err)
	}
	recovered := testutil.Run(t, hum, runtime.cwd, runtime.env, "serve", "--daemon")
	if recovered.Code != 0 || recovered.Err != nil || recovered.Stdout != "" {
		t.Fatalf("stale runtime recovery: code=%d err=%v stdout=%q stderr=%q", recovered.Code, recovered.Err, recovered.Stdout, recovered.Stderr)
	}
	daemonPID = lifecycleParseListeningPID(t, recovered.Stderr, runtime.paths.Socket)
	if daemonPID == stalePID || !testutil.ProcessAlive(daemonPID) {
		t.Fatalf("recovered daemon PID = %d, stale PID = %d", daemonPID, stalePID)
	}
	info, err := os.Stat(runtime.paths.Socket)
	if err != nil {
		t.Fatalf("stat recovered socket: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("recovered socket mode = %s, want Unix socket", info.Mode())
	}
	shutdown = testutil.Run(t, hum, runtime.cwd, runtime.env, "shutdown", "--stop-processes")
	if shutdown.Code != 0 || shutdown.Err != nil {
		t.Fatalf("shutdown after stale recovery: code=%d err=%v stdout=%q stderr=%q", shutdown.Code, shutdown.Err, shutdown.Stdout, shutdown.Stderr)
	}
	testutil.WaitForProcessGone(t, daemonPID, lifecycleTimeout)
	lifecycleWaitPathGone(t, runtime.paths.Socket, lifecycleTimeout)
	lifecycleWaitPathGone(t, runtime.paths.PID, lifecycleTimeout)
	lifecycleWaitPathGone(t, runtime.paths.Ready, lifecycleTimeout)
	daemonPID = 0

	badRuntime := filepath.Join(t.TempDir(), "runtime-file")
	if err := os.WriteFile(badRuntime, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write invalid runtime path: %v", err)
	}
	bad := testutil.Run(t, hum, runtime.cwd, testutil.RuntimeEnv(badRuntime), "serve", "--daemon")
	if bad.Code == 0 || bad.Err == nil || bad.Stdout != "" || !strings.Contains(bad.Stderr, "daemon startup failed") || !strings.Contains(bad.Stderr, filepath.Join(badRuntime, "daemon.log")) {
		t.Fatalf("startup failure = code %d err %v stdout %q stderr %q, want daemon.log guidance", bad.Code, bad.Err, bad.Stdout, bad.Stderr)
	}
}

func TestAutomaticStartup(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)

	t.Run("attached run starts a daemon automatically", func(t *testing.T) {
		runtime := lifecycleNewRuntime(t)
		daemonPID := 0
		t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, daemonPID) })

		result := testutil.Start(t, hum, runtime.cwd, runtime.env, "run", "automatic-attached", "--", "/bin/sh", "-c", "printf attached")
		durableWaitText(t, result, false, "attached")
		durableWaitText(t, result, true, "waiting for next launch")
		if err := result.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		if err := result.Wait(lifecycleTimeout); err != nil {
			t.Fatalf("attached automatic detach: %v stdout=%q stderr=%q", err, result.Stdout(), result.Stderr())
		}
		testutil.WaitForFile(t, runtime.paths.PID, lifecycleTimeout)
		testutil.WaitForFile(t, runtime.paths.Ready, lifecycleTimeout)
		daemonPID = lifecycleReadPID(t, runtime.paths.PID)
		if !testutil.ProcessAlive(daemonPID) {
			t.Fatalf("automatic attached run daemon PID %d is not alive", daemonPID)
		}
	})

	t.Run("detached run starts a daemon automatically", func(t *testing.T) {
		runtime := lifecycleNewRuntime(t)
		daemonPID := 0
		t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, daemonPID) })

		marker := filepath.Join(t.TempDir(), "automatic-detached")
		result := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "automatic-detached", "--detach", "--", fixture, "stream", marker)
		if result.Code != 0 || result.Err != nil || result.Stderr != "" || !strings.Contains(result.Stdout, "started automatic-detached (PID ") {
			t.Fatalf("detached automatic run: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		managedPID := lifecycleParseManagedPID(t, result.Stdout, "automatic-detached")
		testutil.WaitForFile(t, marker+".started", lifecycleTimeout)
		testutil.WaitForFile(t, runtime.paths.Ready, lifecycleTimeout)
		daemonPID = lifecycleReadPID(t, runtime.paths.PID)
		if !testutil.ProcessAlive(daemonPID) || !testutil.ProcessAlive(managedPID) {
			t.Fatalf("automatic detached liveness: daemon PID %d managed PID %d", daemonPID, managedPID)
		}

		stopped := testutil.Run(t, hum, runtime.cwd, runtime.env, "stop", "automatic-detached")
		if stopped.Code != 0 || stopped.Err != nil || stopped.Stdout != "automatic-detached stopped\n" || stopped.Stderr != "" {
			t.Fatalf("stop automatic detached process: code=%d err=%v stdout=%q stderr=%q", stopped.Code, stopped.Err, stopped.Stdout, stopped.Stderr)
		}
		testutil.WaitForFile(t, marker+".terminated", lifecycleTimeout)
		testutil.WaitForProcessGone(t, managedPID, lifecycleTimeout)
	})

	t.Run("concurrent clients converge on one daemon", func(t *testing.T) {
		runtime := lifecycleNewRuntime(t)
		daemonPID := 0
		t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, daemonPID) })

		const racers = 6
		type lifecycleRaceResult struct {
			name   string
			result testutil.Result
		}
		results := make(chan lifecycleRaceResult, racers)
		for i := range racers {
			name := fmt.Sprintf("automatic-race-%d", i)
			go func(name string) {
				results <- lifecycleRaceResult{
					name:   name,
					result: testutil.Run(t, hum, runtime.cwd, runtime.env, "run", name, "--detach", "--", "/bin/sh", "-c", "sleep 2"),
				}
			}(name)
		}
		for range racers {
			result := <-results
			if result.result.Code != 0 || result.result.Err != nil || result.result.Stderr != "" || !strings.Contains(result.result.Stdout, "started "+result.name+" (PID ") {
				t.Fatalf("concurrent automatic run %s: code=%d err=%v stdout=%q stderr=%q", result.name, result.result.Code, result.result.Err, result.result.Stdout, result.result.Stderr)
			}
		}

		testutil.WaitForFile(t, runtime.paths.PID, lifecycleTimeout)
		daemonPID = lifecycleReadPID(t, runtime.paths.PID)
		if !testutil.ProcessAlive(daemonPID) {
			t.Fatalf("concurrent automatic daemon PID %d is not alive", daemonPID)
		}
		list := testutil.Run(t, hum, runtime.cwd, runtime.env, "list", "--json")
		if list.Code != 0 || list.Err != nil || list.Stderr != "" {
			t.Fatalf("list after concurrent startup: code=%d err=%v stdout=%q stderr=%q", list.Code, list.Err, list.Stdout, list.Stderr)
		}
		var listing struct {
			Processes []struct {
				Name string `json:"name"`
			} `json:"processes"`
		}
		if err := json.Unmarshal([]byte(list.Stdout), &listing); err != nil {
			t.Fatalf("decode concurrent process list: %v; stdout=%q", err, list.Stdout)
		}
		if len(listing.Processes) != racers {
			t.Fatalf("concurrent process count = %d, want %d", len(listing.Processes), racers)
		}
		logData, err := os.ReadFile(runtime.paths.Log)
		if err != nil {
			t.Fatalf("read concurrent daemon log: %v", err)
		}
		if count := strings.Count(string(logData), "hum serve: listening on "); count != 1 {
			t.Fatalf("concurrent daemon listening records = %d, want one; log=%q", count, logData)
		}
		shutdown := testutil.Run(t, hum, runtime.cwd, runtime.env, "shutdown", "--stop-processes")
		if shutdown.Code != 0 || shutdown.Err != nil {
			t.Fatalf("shutdown concurrent daemon: code=%d err=%v stdout=%q stderr=%q", shutdown.Code, shutdown.Err, shutdown.Stdout, shutdown.Stderr)
		}
		testutil.WaitForProcessGone(t, daemonPID, lifecycleTimeout)
		lifecycleWaitPathGone(t, runtime.paths.Socket, lifecycleTimeout)
		daemonPID = 0
	})

	t.Run("stale artifacts recover and startup failure names daemon log", func(t *testing.T) {
		runtime := lifecycleNewRuntime(t)
		daemonPID := 0
		t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, daemonPID) })

		stalePID := lifecycleUnusedPID()
		if err := os.WriteFile(runtime.paths.PID, []byte(strconv.Itoa(stalePID)+"\n"), 0o600); err != nil {
			t.Fatalf("write stale PID: %v", err)
		}
		if err := os.WriteFile(runtime.paths.Ready, []byte(strconv.Itoa(stalePID)+"\n"), 0o600); err != nil {
			t.Fatalf("write stale readiness: %v", err)
		}
		if err := os.WriteFile(runtime.paths.Socket, []byte("stale socket"), 0o600); err != nil {
			t.Fatalf("write stale socket: %v", err)
		}
		recovered := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "stale-recovery", "--detach", "--", "/bin/sh", "-c", "exit 0")
		if recovered.Code != 0 || recovered.Err != nil || recovered.Stderr != "" || !strings.Contains(recovered.Stdout, "started stale-recovery (PID ") {
			t.Fatalf("automatic stale recovery: code=%d err=%v stdout=%q stderr=%q", recovered.Code, recovered.Err, recovered.Stdout, recovered.Stderr)
		}
		testutil.WaitForFile(t, runtime.paths.Ready, lifecycleTimeout)
		daemonPID = lifecycleReadPID(t, runtime.paths.PID)
		if daemonPID == stalePID || !testutil.ProcessAlive(daemonPID) {
			t.Fatalf("automatic stale recovery daemon PID = %d, stale PID = %d", daemonPID, stalePID)
		}
		info, err := os.Stat(runtime.paths.Socket)
		if err != nil {
			t.Fatalf("stat automatically recovered socket: %v", err)
		}
		if info.Mode()&os.ModeSocket == 0 {
			t.Fatalf("automatically recovered socket mode = %s, want Unix socket", info.Mode())
		}
		shutdown := testutil.Run(t, hum, runtime.cwd, runtime.env, "shutdown", "--stop-processes")
		if shutdown.Code != 0 || shutdown.Err != nil {
			t.Fatalf("shutdown after automatic stale recovery: code=%d err=%v stdout=%q stderr=%q", shutdown.Code, shutdown.Err, shutdown.Stdout, shutdown.Stderr)
		}
		testutil.WaitForProcessGone(t, daemonPID, lifecycleTimeout)
		lifecycleWaitPathGone(t, runtime.paths.Socket, lifecycleTimeout)
		daemonPID = 0

		badRuntime := filepath.Join(t.TempDir(), "runtime-file")
		if err := os.WriteFile(badRuntime, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write invalid runtime path: %v", err)
		}
		bad := testutil.Run(t, hum, runtime.cwd, testutil.RuntimeEnv(badRuntime), "run", "startup-failure", "--detach", "--", "/bin/sh", "-c", "exit 0")
		if bad.Code == 0 || bad.Err == nil || bad.Stdout != "" || !strings.Contains(bad.Stderr, "daemon startup failed") || !strings.Contains(bad.Stderr, filepath.Join(badRuntime, "daemon.log")) {
			t.Fatalf("automatic startup failure = code %d err %v stdout %q stderr %q, want daemon.log guidance", bad.Code, bad.Err, bad.Stdout, bad.Stderr)
		}
	})
}

func TestVersionMismatch(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)

	t.Run("idle mismatched daemon is replaced", func(t *testing.T) {
		runtime := lifecycleNewRuntime(t)
		daemonPID := 0
		t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, daemonPID) })
		server, done := lifecycleStartMismatchedServer(t, runtime.dir)
		oldPID := server.PID()

		result := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "replacement", "--detach", "--", "/bin/sh", "-c", "exit 0")
		if result.Code != 0 || result.Err != nil || result.Stderr != "" || !strings.Contains(result.Stdout, "started replacement (PID ") {
			t.Fatalf("run against idle mismatched daemon: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("idle mismatched daemon shutdown: %v", err)
			}
		case <-time.After(lifecycleTimeout):
			t.Fatal("idle mismatched daemon did not shut down")
		}
		testutil.WaitForFile(t, runtime.paths.PID, lifecycleTimeout)
		daemonPID = lifecycleReadPID(t, runtime.paths.PID)
		if daemonPID == oldPID || !testutil.ProcessAlive(daemonPID) {
			t.Fatalf("replacement daemon PID = %d, old PID = %d", daemonPID, oldPID)
		}
		check := testutil.Run(t, hum, runtime.cwd, runtime.env, "list")
		if check.Code != 0 || check.Err != nil || check.Stderr != "" {
			t.Fatalf("list through replacement daemon: code=%d err=%v stdout=%q stderr=%q", check.Code, check.Err, check.Stdout, check.Stderr)
		}
	})

	t.Run("active mismatch refuses and names forced shutdown", func(t *testing.T) {
		runtime := lifecycleNewRuntime(t)
		marker := filepath.Join(t.TempDir(), "active-mismatch")
		active := testutil.Start(t, fixture, runtime.cwd, runtime.env, "stream", marker)
		testutil.WaitForFile(t, marker+".started", lifecycleTimeout)
		fake := lifecycleStartActiveMismatch(t, runtime.dir, active)

		refused := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "blocked", "--detach", "--", "/bin/sh", "-c", "exit 0")
		if refused.Code == 0 || refused.Err == nil || refused.Stdout != "" || !strings.Contains(refused.Stderr, "daemon version 999") || !strings.Contains(refused.Stderr, "hum shutdown --stop-processes") || !strings.Contains(refused.Stderr, "active") {
			t.Fatalf("active mismatch refusal: code=%d err=%v stdout=%q stderr=%q", refused.Code, refused.Err, refused.Stdout, refused.Stderr)
		}

		forced := testutil.Run(t, hum, runtime.cwd, runtime.env, "shutdown", "--stop-processes")
		if forced.Code != 0 || forced.Err != nil || forced.Stdout != "hum daemon shut down\n" || forced.Stderr != "" {
			t.Fatalf("forced shutdown after active mismatch: code=%d err=%v stdout=%q stderr=%q", forced.Code, forced.Err, forced.Stdout, forced.Stderr)
		}
		testutil.WaitForFile(t, marker+".terminated", lifecycleTimeout)
		if err := active.Wait(lifecycleTimeout); err != nil {
			t.Fatalf("active process exit after forced shutdown: %v", err)
		}
		if !active.Exited() {
			t.Fatal("active process remained running after forced shutdown")
		}
		select {
		case err := <-fake.done:
			if err != nil {
				t.Fatalf("active mismatch protocol peer: %v", err)
			}
		case <-time.After(lifecycleTimeout):
			t.Fatal("active mismatch protocol peer did not finish")
		}
	})
}

func lifecycleRequireUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("integration lifecycle tests require macOS or Linux (got %s)", runtime.GOOS)
	}
}

func lifecycleNewRuntime(t *testing.T) lifecycleRuntime {
	t.Helper()
	dir := testutil.RuntimeDir(t)
	cwd := t.TempDir()
	return lifecycleRuntime{dir: dir, cwd: cwd, env: testutil.RuntimeEnv(dir), paths: daemon.NewRuntimePaths(dir)}
}

func lifecycleWaitForStderr(t *testing.T, process *testutil.Process, text string, timeout time.Duration) {
	t.Helper()
	if lifecycleWaitCondition(timeout, func() bool { return strings.Contains(process.Stderr(), text) }) {
		return
	}
	t.Fatalf("timed out waiting for process stderr %q; got %q", text, process.Stderr())
}

func lifecycleParseListeningPID(t *testing.T, stderr, socket string) int {
	t.Helper()
	prefix := fmt.Sprintf("hum serve: listening on %s (PID ", socket)
	if !strings.HasPrefix(stderr, prefix) || !strings.HasSuffix(stderr, ")\n") {
		t.Fatalf("serve stderr = %q, want exact listening line for %s", stderr, socket)
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(stderr, prefix), ")\n")
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		t.Fatalf("serve stderr PID = %q: %v", raw, err)
	}
	return pid
}

func lifecycleParseManagedPID(t *testing.T, stdout, name string) int {
	t.Helper()
	prefix := fmt.Sprintf("started %s (PID ", name)
	if !strings.HasPrefix(stdout, prefix) || !strings.HasSuffix(stdout, ")\n") {
		t.Fatalf("managed launch output = %q, want launch line for %s", stdout, name)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(stdout, prefix), ")\n")
	parts := strings.Split(body, ", cursor ")
	if len(parts) != 2 {
		t.Fatalf("managed launch body = %q, want PID and cursor", body)
	}
	pid, err := strconv.Atoi(parts[0])
	if err != nil || pid <= 0 {
		t.Fatalf("managed launch PID = %q: %v", parts[0], err)
	}
	return pid
}

func lifecycleReadPID(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read PID %s: %v", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("PID file %s = %q: %v", path, data, err)
	}
	return pid
}

func lifecycleReadPIDNoFatal(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func lifecycleAssertDetachedSession(t *testing.T, pid int) {
	t.Helper()
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("get detached daemon process group: %v", err)
	}
	if pgid != pid {
		t.Fatalf("detached daemon PGID = %d, want session/process-group leader PID %d", pgid, pid)
	}
	sid := lifecycleProcessSessionID(t, pid)
	callerSID := lifecycleProcessSessionID(t, os.Getpid())
	if sid != pid {
		t.Fatalf("detached daemon SID = %d, want PID %d from setsid", sid, pid)
	}
	if sid == callerSID {
		t.Fatalf("detached daemon SID = %d is caller session %d", sid, callerSID)
	}
}

func lifecycleAssertBoundedLog(t *testing.T, path string, limit int64) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat daemon log %s: %v", path, err)
	}
	if info.Size() > limit {
		t.Fatalf("daemon log size = %d, want <= %d", info.Size(), limit)
	}
}

func lifecycleUnusedPID() int {
	pid := lifecycleStalePIDStart
	for testutil.ProcessAlive(pid) {
		pid++
	}
	return pid
}

func lifecycleCleanupDaemon(t *testing.T, hum string, runtime lifecycleRuntime, knownPID int) {
	if _, err := os.Stat(runtime.paths.Socket); err != nil {
		return
	}
	result := testutil.Run(t, hum, runtime.cwd, runtime.env, "shutdown", "--stop-processes")
	if result.Code != 0 {
		return
	}
	pid := knownPID
	if pid <= 0 {
		pid = lifecycleReadPIDNoFatal(runtime.paths.PID)
	}
	if pid > 0 {
		lifecycleWaitCondition(lifecycleTimeout, func() bool { return !testutil.ProcessAlive(pid) })
	}
	lifecycleWaitCondition(lifecycleTimeout, func() bool {
		_, err := os.Stat(runtime.paths.Socket)
		return errors.Is(err, os.ErrNotExist)
	})
}

func lifecycleWaitPathGone(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	if lifecycleWaitCondition(timeout, func() bool {
		_, err := os.Stat(path)
		return errors.Is(err, os.ErrNotExist)
	}) {
		return
	}
	t.Fatalf("path %s remained after %s", path, timeout)
}

func lifecycleWaitCondition(timeout time.Duration, condition func() bool) bool {
	if condition() {
		return true
	}
	if timeout <= 0 {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(lifecyclePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if condition() {
				return true
			}
		case <-timer.C:
			return condition()
		}
	}
}

func lifecycleStartMismatchedServer(t *testing.T, runtimeDir string) (*daemon.Server, chan error) {
	t.Helper()
	server, err := daemon.NewServer(daemon.Config{RuntimeDir: runtimeDir, Version: "999", StopGrace: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("create mismatched daemon server: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		err := server.Serve(context.Background())
		done <- err
		close(done)
	}()
	t.Cleanup(func() {
		_ = server.Close()
		select {
		case <-done:
		case <-time.After(lifecycleTimeout):
		}
	})
	testutil.WaitForFile(t, server.Paths().Socket, lifecycleTimeout)
	return server, done
}

type lifecycleActiveMismatch struct {
	listener net.Listener
	paths    daemon.RuntimePaths
	active   *testutil.Process
	done     chan error
}

type lifecycleShutdownOK struct {
	Op protocol.Operation `json:"op"`
	OK bool               `json:"ok"`
}

func lifecycleStartActiveMismatch(t *testing.T, runtimeDir string, active *testutil.Process) *lifecycleActiveMismatch {
	t.Helper()
	paths := daemon.NewRuntimePaths(runtimeDir)
	listener, err := net.Listen("unix", paths.Socket)
	if err != nil {
		t.Fatalf("listen for active mismatched daemon: %v", err)
	}
	if err := os.Chmod(paths.Socket, 0o600); err != nil {
		_ = listener.Close()
		t.Fatalf("secure active mismatched socket: %v", err)
	}
	for _, item := range []struct {
		path string
		data string
	}{
		{paths.PID, "999\n"},
		{paths.Ready, "999\n"},
	} {
		if err := os.WriteFile(item.path, []byte(item.data), 0o600); err != nil {
			_ = listener.Close()
			t.Fatalf("write active mismatched artifact %s: %v", item.path, err)
		}
	}
	fake := &lifecycleActiveMismatch{listener: listener, paths: paths, active: active, done: make(chan error, 1)}
	go fake.serve()
	t.Cleanup(fake.cleanup)
	testutil.WaitForFile(t, paths.Socket, lifecycleTimeout)
	return fake
}

func (f *lifecycleActiveMismatch) serve() {
	var result error
	first := true
	defer func() {
		f.done <- result
		close(f.done)
	}()
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			result = err
			return
		}
		handleErr := f.handle(conn, first)
		_ = conn.Close()
		if handleErr != nil {
			result = handleErr
			return
		}
		if !first {
			return
		}
		first = false
	}
}

func (f *lifecycleActiveMismatch) handle(conn net.Conn, first bool) error {
	_ = conn.SetDeadline(time.Now().Add(lifecycleTimeout))
	decoder := protocol.NewDecoder(conn)
	encoder := protocol.NewEncoder(conn)
	request, err := decoder.DecodeRequest()
	if err != nil {
		return fmt.Errorf("decode hello: %w", err)
	}
	if request.Op != protocol.OpHello || request.Hello == nil {
		return fmt.Errorf("first operation = %q, want hello", request.Op)
	}
	if err := encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: 999}); err != nil {
		return fmt.Errorf("encode mismatched hello: %w", err)
	}
	request, err = decoder.DecodeRequest()
	if err != nil {
		return fmt.Errorf("decode shutdown: %w", err)
	}
	if request.Op != protocol.OpShutdown || request.Shutdown == nil {
		return fmt.Errorf("second operation = %q, want shutdown", request.Op)
	}
	if first && !request.Shutdown.Force {
		message := "active supervised processes prevent daemon shutdown: active"
		return encoder.EncodeResponse(protocol.ErrorResponse{
			Op:    protocol.OpShutdown,
			Error: protocol.NewWireError(protocol.ErrorActiveProcesses, message, []string{"active"}),
		})
	}
	if first {
		return errors.New("first shutdown unexpectedly forced")
	}
	if !request.Shutdown.Force {
		return errors.New("forced shutdown omitted --stop-processes")
	}
	if f.active != nil {
		if err := f.active.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("signal active process: %w", err)
		}
	}
	if err := encoder.EncodeResponse(lifecycleShutdownOK{Op: protocol.OpShutdown, OK: true}); err != nil {
		return fmt.Errorf("encode forced shutdown: %w", err)
	}
	_ = f.listener.Close()
	return nil
}

func (f *lifecycleActiveMismatch) cleanup() {
	if f == nil {
		return
	}
	_ = f.listener.Close()
	select {
	case <-f.done:
	case <-time.After(lifecycleTimeout):
	}
	for _, path := range []string{f.paths.Socket, f.paths.PID, f.paths.Ready} {
		_ = os.Remove(path)
	}
}
