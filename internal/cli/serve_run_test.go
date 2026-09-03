package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	urfavecli "github.com/urfave/cli/v3"
	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/process"
	"hum/internal/protocol"
)

const (
	cliServeRunHelperMarker = "__hum_cli_serve_run_helper__"
	cliServeRunChildFlag    = "-test.run=TestAttachedRun"
)

// TestMain lets the acceptance tests use this test binary as a real child
// process without the testing package writing PASS/FAIL banners into managed
// stdout and stderr. Detached daemon children carry an internal environment
// marker and execute the real CLI directly; fixture/client helpers use the
// argv marker below.
func TestMain(m *testing.M) {
	if os.Getenv("HUM_DAEMON_CHILD") == "1" {
		os.Exit(cliServeRunClient(os.Args[1:]))
	}
	mode, args, ok := cliServeRunHelperArgs()
	if ok {
		var code int
		switch mode {
		case "fixture":
			code = cliServeRunFixture(args)
		case "client":
			code = cliServeRunClient(args)
		default:
			code = 2
		}
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func cliServeRunHelperArgs() (string, []string, bool) {
	for index, arg := range os.Args {
		if arg != cliServeRunHelperMarker || index+1 >= len(os.Args) {
			continue
		}
		return os.Args[index+1], os.Args[index+2:], true
	}
	return "", nil, false
}

func cliServeRunClient(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	err := cliServeRunInvoke(ctx, args, os.Stdout, os.Stderr)
	if err == nil {
		return 0
	}
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}

func cliServeRunInvoke(ctx context.Context, args []string, writer, errWriter io.Writer) error {
	command := NewRootCommand("test", "test", writer, errWriter)
	// urfave's default ExitErrHandler calls os.Exit for ExitCoder errors. The
	// test process needs to observe the returned code instead.
	command.ExitErrHandler = func(context.Context, *urfavecli.Command, error) {}
	argv := append([]string{"hum"}, args...)
	return command.Run(ctx, argv)
}

type cliServeRunFixtureSnapshot struct {
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd"`
	Env  []string `json:"env"`
}

func cliServeRunFixture(args []string) int {
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "inspect":
		return cliServeRunFixtureInspect()
	case "stream":
		if len(args) < 2 {
			return 2
		}
		return cliServeRunFixtureStream(args[1])
	case "signals":
		if len(args) < 2 {
			return 2
		}
		return cliServeRunFixtureSignals(args[1])
	case "term":
		if len(args) < 2 {
			return 2
		}
		return cliServeRunFixtureTerm(args[1])
	default:
		return 2
	}
}

func cliServeRunFixtureInspect() int {
	cwd, err := os.Getwd()
	if err != nil {
		return 2
	}
	snapshot, err := json.Marshal(cliServeRunFixtureSnapshot{
		Argv: append([]string(nil), os.Args...),
		Cwd:  cwd,
		Env:  append([]string(nil), os.Environ()...),
	})
	if err != nil {
		return 2
	}
	fmt.Fprintf(os.Stdout, "SNAPSHOT %s\n", snapshot)
	fmt.Fprint(os.Stdout, "stdout:raw with spaces \r\nstdout:partial")
	fmt.Fprint(os.Stderr, "stderr:raw with spaces \r\nstderr:partial")
	return 23
}

func cliServeRunFixtureStream(marker string) int {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	if err := os.WriteFile(marker+".started", []byte("started"), 0600); err != nil {
		return 2
	}
	fmt.Fprint(os.Stdout, "stdout:live with spaces \r\nstdout:live-partial")
	fmt.Fprint(os.Stderr, "stderr:live with spaces \r\nstderr:live-partial")
	for {
		switch <-signals {
		case syscall.SIGTERM:
			_ = os.WriteFile(marker+".terminated", []byte("terminated"), 0600)
			return 0
		case syscall.SIGINT:
			fmt.Fprint(os.Stdout, "\nfixture:sigint\n")
		case syscall.SIGHUP:
			fmt.Fprint(os.Stdout, "\nfixture:sighup\n")
		}
	}
}

func cliServeRunFixtureSignals(marker string) int {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	if err := os.WriteFile(marker+".started", []byte("started"), 0600); err != nil {
		return 2
	}
	fmt.Fprintln(os.Stdout, "fixture:ready")
	fmt.Fprintln(os.Stderr, "fixture:stderr-ready")
	interrupts := 0
	for {
		switch <-signals {
		case syscall.SIGINT:
			interrupts++
			fmt.Fprintf(os.Stdout, "fixture:sigint-%d\n", interrupts)
		case syscall.SIGTERM:
			fmt.Fprintln(os.Stdout, "fixture:sigterm")
			_ = os.WriteFile(marker+".terminated", []byte("terminated"), 0600)
			return 0
		case syscall.SIGHUP:
			fmt.Fprintln(os.Stdout, "fixture:sighup")
		}
	}
}

func cliServeRunFixtureTerm(marker string) int {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	if err := os.WriteFile(marker+".started", []byte("started"), 0600); err != nil {
		return 2
	}
	for {
		switch <-signals {
		case syscall.SIGTERM:
			if err := os.WriteFile(marker+".terminated", []byte("terminated"), 0600); err != nil {
				return 2
			}
			return 0
		case syscall.SIGINT:
			fmt.Fprintln(os.Stdout, "fixture:sigint")
		}
	}
}

func cliServeRunRuntimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "h-")
	if err != nil {
		t.Fatalf("create runtime directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestForegroundServe(t *testing.T) {
	runtimeDir := cliServeRunRuntimeDir(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	serve := cliServeRunStartClient(t, "serve")
	t.Cleanup(func() {
		if serve.exited() {
			return
		}
		_ = serve.cmd.Process.Signal(os.Interrupt)
		_ = serve.wait(5 * time.Second)
	})
	paths := daemon.NewRuntimePaths(runtimeDir)
	if err := cliServeRunWaitForDaemon(paths.Socket); err != nil {
		t.Fatalf("foreground serve readiness: %v; stderr=%q", err, serve.stderr())
	}

	marker := filepath.Join(t.TempDir(), "serve-stop")
	err := cliServeRunInvoke(context.Background(), cliServeRunWithFixtureArgs([]string{"run", "foreground", "--detach"}, "term", marker), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("start process under foreground serve: %v", err)
	}
	if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
		t.Fatalf("managed process readiness: %v", err)
	}

	if err := serve.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt foreground serve: %v", err)
	}
	if err := serve.wait(5 * time.Second); err != nil {
		t.Fatalf("foreground serve exit: %v; stdout=%q stderr=%q", err, serve.stdout(), serve.stderr())
	}
	if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
		t.Fatalf("managed process was not stopped before serve exit: %v", err)
	}
	if got := serve.stdout(); got != "" {
		t.Fatalf("foreground diagnostics leaked to stdout: %q", got)
	}
	if got := serve.stderr(); got == "" {
		t.Fatal("foreground serve emitted no stderr diagnostics")
	}
}

func TestDaemonUnavailable(t *testing.T) {
	testDaemonUnavailable(t)
}

func testDaemonUnavailable(t *testing.T) {
	t.Helper()
	runtimeDir := cliServeRunRuntimeDir(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	const (
		logsError      = "Nothing is running. Start a process with hum run <name> -- <command>."
		nothingOutput  = "Nothing is running.\n"
		shutdownOutput = "No hum daemon is running.\n"
	)
	tests := []struct {
		name    string
		args    []string
		wantErr string
		wantOut string
	}{
		{name: "list", args: []string{"list"}, wantOut: nothingOutput},
		{name: "logs", args: []string{"logs", "missing"}, wantErr: logsError},
		{name: "stop", args: []string{"stop", "missing"}, wantOut: nothingOutput},
		{name: "shutdown", args: []string{"shutdown"}, wantOut: shutdownOutput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output, errorOutput bytes.Buffer
			err := cliServeRunInvoke(context.Background(), test.args, &output, &errorOutput)
			if test.wantErr != "" {
				if err == nil || err.Error() != test.wantErr {
					t.Fatalf("error = %v, want exact %q", err, test.wantErr)
				}
			} else if err != nil {
				t.Fatalf("command error: %v", err)
			}
			if test.wantOut != "" && output.String() != test.wantOut {
				t.Fatalf("stdout = %q, want exact %q", output.String(), test.wantOut)
			}
			if errorOutput.Len() != 0 {
				t.Fatalf("unexpected stderr: %q", errorOutput.String())
			}
			entries, readErr := os.ReadDir(runtimeDir)
			if readErr != nil {
				t.Fatalf("read unavailable runtime directory: %v", readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("unavailable command created runtime state: %v", entries)
			}
		})
	}
}

func TestServeDaemon(t *testing.T) {
	t.Run("other commands preserve no-daemon messages", testDaemonUnavailable)
	runtimeDir := cliServeRunRuntimeDir(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	t.Cleanup(func() {
		_, _, _ = cliServeRunInvokeForTest("shutdown", "--stop-processes")
	})
	paths := daemon.NewRuntimePaths(runtimeDir)

	stdout, stderr, err := cliServeRunInvokeForTest("serve", "--daemon")
	if err != nil {
		t.Fatalf("first detached serve: %v", err)
	}
	if stdout != "" {
		t.Fatalf("detached serve stdout = %q, want empty", stdout)
	}
	pid := cliServeRunPID(stderr)
	if pid <= 0 {
		t.Fatalf("detached serve stderr = %q, want PID", stderr)
	}
	want := fmt.Sprintf("hum serve: listening on %s (PID %d)\n", paths.Socket, pid)
	if stderr != want {
		t.Fatalf("detached serve stderr = %q, want %q", stderr, want)
	}
	if err := cliServeRunWaitForDaemon(paths.Socket); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err = cliServeRunInvokeForTest("serve", "--daemon")
	if err != nil {
		t.Fatalf("idempotent detached serve: %v", err)
	}
	if stdout != "" || stderr != want {
		t.Fatalf("idempotent serve output = stdout %q stderr %q, want empty/%q", stdout, stderr, want)
	}

	const racers = 6
	results := make(chan struct {
		stderr string
		err    error
	}, racers)
	for range racers {
		go func() {
			_, raceStderr, raceErr := cliServeRunInvokeForTest("serve", "--daemon")
			results <- struct {
				stderr string
				err    error
			}{raceStderr, raceErr}
		}()
	}
	for range racers {
		result := <-results
		if result.err != nil || result.stderr != want {
			t.Fatalf("racing detached serve = stderr %q err %v, want %q", result.stderr, result.err, want)
		}
	}

	if _, _, err := cliServeRunInvokeForTest("shutdown", "--stop-processes"); err != nil {
		t.Fatalf("shutdown before stale recovery: %v", err)
	}
	if err := cliServeRunWaitForCondition(func() bool {
		_, socketErr := os.Stat(paths.Socket)
		return errors.Is(socketErr, os.ErrNotExist)
	}); err != nil {
		t.Fatalf("wait for daemon shutdown: %v", err)
	}
	if err := os.WriteFile(paths.PID, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("write stale pid: %v", err)
	}
	if err := os.WriteFile(paths.Ready, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("write stale readiness: %v", err)
	}
	if err := os.WriteFile(paths.Socket, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale socket: %v", err)
	}
	_, recoveredStderr, err := cliServeRunInvokeForTest("serve", "--daemon")
	if err != nil {
		t.Fatalf("recover stale runtime: %v", err)
	}
	if recoveredPID := cliServeRunPID(recoveredStderr); recoveredPID <= 0 || recoveredPID == 999999 {
		t.Fatalf("recovered detached serve stderr = %q", recoveredStderr)
	}

	badRuntime := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(badRuntime, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HUM_RUNTIME_DIR", badRuntime)
	_, _, err = cliServeRunInvokeForTest("serve", "--daemon")
	if err == nil || !strings.Contains(err.Error(), "daemon startup failed") || !strings.Contains(err.Error(), "daemon.log") {
		t.Fatalf("startup failure = %v, want caller-visible daemon.log guidance", err)
	}
}

func TestAutomaticDaemonStartup(t *testing.T) {
	t.Run("attached and detached run", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		t.Cleanup(func() {
			_, _, _ = cliServeRunInvokeForTest("shutdown", "--stop-processes")
		})

		stdout, stderr, err := cliServeRunInvokeForTest("run", "automatic-attached", "--", "/bin/sh", "-c", "printf attached")
		if err != nil {
			t.Fatalf("attached automatic run: %v", err)
		}
		if stdout != "attached" || stderr != "" {
			t.Fatalf("attached automatic output = stdout %q stderr %q", stdout, stderr)
		}

		stdout, stderr, err = cliServeRunInvokeForTest("run", "automatic-detached", "--detach", "--", "/bin/sh", "-c", "sleep 30")
		if err != nil {
			t.Fatalf("detached automatic run: %v", err)
		}
		if stderr != "" || !strings.Contains(stdout, "started automatic-detached (PID ") {
			t.Fatalf("detached automatic output = stdout %q stderr %q", stdout, stderr)
		}
		if err := cliServeRunStop(t, "automatic-detached"); err != nil {
			t.Fatalf("stop automatic detached process: %v", err)
		}
	})

	t.Run("concurrent run clients select one daemon", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		t.Cleanup(func() {
			_, _, _ = cliServeRunInvokeForTest("shutdown", "--stop-processes")
		})
		const racers = 6
		errs := make(chan error, racers)
		for i := range racers {
			name := fmt.Sprintf("race-%d", i)
			go func() {
				_, stderr, err := cliServeRunInvokeForTest("run", name, "--detach", "--", "/bin/sh", "-c", "exit 0")
				if err == nil && stderr != "" {
					err = fmt.Errorf("stderr = %q", stderr)
				}
				errs <- err
			}()
		}
		for range racers {
			if err := <-errs; err != nil {
				t.Fatalf("racing automatic run: %v", err)
			}
		}
		paths := daemon.NewRuntimePaths(runtimeDir)
		pid, err := readDaemonPID(paths)
		if err != nil {
			t.Fatalf("read selected daemon PID: %v", err)
		}
		if pid <= 0 {
			t.Fatalf("selected daemon PID = %d", pid)
		}
	})
}

func TestDetachedDaemonLog(t *testing.T) {
	runtimeDir := cliServeRunRuntimeDir(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	t.Cleanup(func() {
		_, _, _ = cliServeRunInvokeForTest("shutdown", "--stop-processes")
	})
	paths := daemon.NewRuntimePaths(runtimeDir)
	stdout, stderr, err := cliServeRunInvokeForTest("serve", "--daemon")
	if err != nil {
		t.Fatalf("start detached daemon: %v", err)
	}
	if stdout != "" {
		t.Fatalf("detached daemon leaked stdout: %q", stdout)
	}
	if err := cliServeRunWaitForText(paths.Log, "hum serve: listening on "); err != nil {
		t.Fatalf("detached daemon log: %v; command stderr=%q", err, stderr)
	}
	info, err := os.Stat(paths.Log)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 1<<20 {
		t.Fatalf("detached daemon log size = %d, want <= %d", info.Size(), 1<<20)
	}

	boundedRuntime := cliServeRunRuntimeDir(t)
	server, err := daemon.NewServer(daemon.Config{RuntimeDir: boundedRuntime, LogBytes: 64})
	if err != nil {
		t.Fatalf("new bounded-log server: %v", err)
	}
	server.Logf("%s", strings.Repeat("diagnostic", 100))
	if err := server.Close(); err != nil {
		t.Fatalf("close bounded-log server: %v", err)
	}
	boundedInfo, err := os.Stat(daemon.NewRuntimePaths(boundedRuntime).Log)
	if err != nil {
		t.Fatal(err)
	}
	if boundedInfo.Size() > 64 {
		t.Fatalf("configured daemon log size = %d, want <= 64", boundedInfo.Size())
	}
}

func TestVersionMismatch(t *testing.T) {
	startMismatch := func(t *testing.T, runtimeDir string, supervisor *app.Supervisor) (*daemon.Server, chan error) {
		t.Helper()
		server, err := daemon.NewServer(daemon.Config{
			RuntimeDir: runtimeDir,
			Version:    "999",
			StopGrace:  100 * time.Millisecond,
			Supervisor: supervisor,
		})
		if err != nil {
			t.Fatalf("new mismatched daemon: %v", err)
		}
		done := make(chan error, 1)
		go func() { done <- server.Serve(context.Background()) }()
		if err := cliServeRunWaitForCondition(func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			client, dialErr := daemon.Dial(ctx, server.Paths().Socket)
			cancel()
			if client != nil {
				_ = client.Close()
			}
			var mismatch *daemon.VersionMismatchError
			return errors.As(dialErr, &mismatch)
		}); err != nil {
			_ = server.Close()
			t.Fatalf("mismatched daemon readiness: %v", err)
		}
		return server, done
	}

	t.Run("replaces idle daemon", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		server, done := startMismatch(t, runtimeDir, nil)
		oldPID := server.PID()
		t.Cleanup(func() {
			_, _, _ = cliServeRunInvokeForTest("shutdown", "--stop-processes")
			_ = server.Close()
		})

		stdout, stderr, err := cliServeRunInvokeForTest("run", "replacement", "--detach", "--", "/bin/sh", "-c", "exit 0")
		if err != nil {
			t.Fatalf("run replaces idle mismatch: %v", err)
		}
		if stderr != "" || !strings.Contains(stdout, "started replacement (PID ") {
			t.Fatalf("replacement output = stdout %q stderr %q", stdout, stderr)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("mismatched daemon did not shut down")
		}
		newPID, err := readDaemonPID(daemon.NewRuntimePaths(runtimeDir))
		if err != nil || newPID == oldPID {
			t.Fatalf("replacement daemon PID = %d err %v, old PID %d", newPID, err, oldPID)
		}
	})

	t.Run("refuses active daemon", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		socket := daemon.NewRuntimePaths(runtimeDir).Socket
		listener, err := net.Listen("unix", socket)
		if err != nil {
			t.Fatalf("listen for mismatched daemon: %v", err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		served := make(chan error, 1)
		go func() {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				served <- acceptErr
				return
			}
			defer conn.Close()
			decoder := protocol.NewDecoder(conn)
			encoder := protocol.NewEncoder(conn)
			request, decodeErr := decoder.DecodeRequest()
			if decodeErr != nil {
				served <- decodeErr
				return
			}
			if request.Op != protocol.OpHello {
				served <- fmt.Errorf("first operation = %q, want hello", request.Op)
				return
			}
			if encodeErr := encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: 999}); encodeErr != nil {
				served <- encodeErr
				return
			}
			request, decodeErr = decoder.DecodeRequest()
			if decodeErr != nil {
				served <- decodeErr
				return
			}
			if request.Op != protocol.OpShutdown {
				served <- fmt.Errorf("second operation = %q, want shutdown", request.Op)
				return
			}
			served <- encoder.EncodeResponse(protocol.ErrorResponse{
				Op: protocol.OpShutdown,
				Error: protocol.NewWireError(
					protocol.ErrorActiveProcesses,
					"active supervised processes prevent daemon shutdown: active",
					[]string{"active"},
				),
			})
		}()

		_, _, err = cliServeRunInvokeForTest("run", "blocked", "--detach", "--", "/bin/sh", "-c", "exit 0")
		if err == nil || !strings.Contains(err.Error(), "daemon version 999") || !strings.Contains(err.Error(), "hum shutdown --stop-processes") {
			t.Fatalf("active mismatch refusal = %v", err)
		}
		if serveErr := <-served; serveErr != nil {
			t.Fatalf("mismatched daemon protocol: %v", serveErr)
		}
	})
}

func TestAttachedRun(t *testing.T) {
	t.Run("argv cwd environment streams and exit", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		fixtureArgs := cliServeRunFixtureArgv("inspect", "arg with spaces", "arg\twith-tabs", "--literal")
		clientDir := t.TempDir()
		oldwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("get working directory: %v", err)
		}
		if err := os.Chdir(clientDir); err != nil {
			t.Fatalf("change working directory to %q: %v", clientDir, err)
		}
		// Resolve the path the client will report after chdir. On macOS, the
		// temporary-directory root can be a symlink, so os.Getwd may return a
		// canonical path such as /private/var while TempDir returns /var.
		expectedCwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("get working directory after chdir: %v", err)
		}
		if err := os.Chdir(oldwd); err != nil {
			t.Fatalf("restore working directory to %q: %v", oldwd, err)
		}
		runArgs := append([]string{"run", "inspect", "--"}, fixtureArgs...)
		client := cliServeRunStartClientInDir(t, clientDir, runArgs...)
		if err := client.wait(5 * time.Second); cliServeRunExitCode(err) != 23 {
			t.Fatalf("attached run exit = %v (code %d), want managed code 23; stdout=%q stderr=%q", err, cliServeRunExitCode(err), client.stdout(), client.stderr())
		}

		var snapshot cliServeRunFixtureSnapshot
		line := ""
		for _, candidate := range strings.Split(client.stdout(), "\n") {
			if strings.HasPrefix(candidate, "SNAPSHOT ") {
				line = strings.TrimPrefix(candidate, "SNAPSHOT ")
				break
			}
		}
		if line == "" {
			t.Fatalf("attached output omitted argv snapshot: %q", client.stdout())
		}
		if err := json.Unmarshal([]byte(line), &snapshot); err != nil {
			t.Fatalf("decode argv snapshot: %v; output=%q", err, client.stdout())
		}
		expectedArgv := append([]string(nil), fixtureArgs...)
		if !cliServeRunEqualStrings(snapshot.Argv, expectedArgv) {
			t.Fatalf("child argv = %#v, want exact %#v", snapshot.Argv, expectedArgv)
		}
		if snapshot.Cwd != expectedCwd {
			t.Fatalf("child cwd = %q, want %q", snapshot.Cwd, expectedCwd)
		}
		if !cliServeRunEqualStrings(snapshot.Env, os.Environ()) {
			t.Fatalf("child environment was not forwarded exactly:\nchild=%#v\nwant=%#v", snapshot.Env, os.Environ())
		}
		if !strings.Contains(client.stdout(), "stdout:raw with spaces \r\n") || !strings.Contains(client.stdout(), "stdout:partial") {
			t.Fatalf("stdout lost raw line content: %q", client.stdout())
		}
		if !strings.Contains(client.stderr(), "stderr:raw with spaces \r\n") || !strings.Contains(client.stderr(), "stderr:partial") {
			t.Fatalf("stderr lost raw line content: %q", client.stderr())
		}
	})
	t.Run("json attached run streams raw child output", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)

		client := cliServeRunStartClient(t, cliServeRunWithFixtureArgs([]string{"run", "attached-json", "--json"}, "inspect")...)
		if err := client.wait(5 * time.Second); cliServeRunExitCode(err) != 23 {
			t.Fatalf("attached JSON run exit = %v (code %d), want managed code 23; stdout=%q stderr=%q", err, cliServeRunExitCode(err), client.stdout(), client.stderr())
		}

		stdout, stderr := client.stdout(), client.stderr()
		if !strings.Contains(stdout, "SNAPSHOT ") || !strings.Contains(stdout, "stdout:raw with spaces \r\n") || !strings.Contains(stdout, "stdout:partial") {
			t.Fatalf("attached --json stdout = %q, want raw child stdout", stdout)
		}
		if !strings.Contains(stderr, "stderr:raw with spaces \r\n") || !strings.Contains(stderr, "stderr:partial") {
			t.Fatalf("attached --json stderr = %q, want raw child stderr", stderr)
		}
		for _, line := range strings.Split(stdout, "\n") {
			var result runResult
			if err := json.Unmarshal([]byte(line), &result); err == nil && result.Name == "attached-json" {
				t.Fatalf("attached --json emitted a JSON start result: %q", line)
			}
		}
	})

	t.Run("already-exited child returns managed code without hanging", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		supervisor, err := app.New(app.Options{
			StopGrace: 100 * time.Millisecond,
			StartProcess: func(spec process.Spec) (app.Child, error) {
				child, err := process.Start(spec)
				if err != nil {
					return nil, err
				}
				<-child.Done()
				return child, nil
			},
		})
		if err != nil {
			t.Fatalf("create immediate-exit supervisor: %v", err)
		}
		cliServeRunStartDaemonWithSupervisor(t, runtimeDir, supervisor)

		client := cliServeRunStartClient(t, "run", "already-exited", "--", "/bin/sh", "-c", "exit 37")
		if err := client.wait(5 * time.Second); cliServeRunExitCode(err) != 37 {
			t.Fatalf("already-exited attached run = %v (code %d), want managed code 37; stdout=%q stderr=%q", err, cliServeRunExitCode(err), client.stdout(), client.stderr())
		}
	})

	t.Run("queued SIGINT during start-to-follow handoff forwards and stays attached", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		marker := filepath.Join(t.TempDir(), "queued-signals")
		entered := filepath.Join(t.TempDir(), "start-entered")
		release := make(chan struct{})
		var releaseOnce sync.Once
		releaseStart := func() { releaseOnce.Do(func() { close(release) }) }

		supervisor, err := app.New(app.Options{
			StopGrace: 100 * time.Millisecond,
			StartProcess: func(spec process.Spec) (app.Child, error) {
				if err := os.WriteFile(entered, []byte("entered"), 0600); err != nil {
					return nil, err
				}
				<-release
				child, err := process.Start(spec)
				if err != nil {
					return nil, err
				}
				if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
					_ = child.Signal(syscall.SIGTERM)
					<-child.Done()
					return nil, err
				}
				return child, nil
			},
		})
		if err != nil {
			t.Fatalf("create start-barrier supervisor: %v", err)
		}
		cliServeRunStartDaemonWithSupervisor(t, runtimeDir, supervisor)
		t.Cleanup(releaseStart)

		client := cliServeRunStartClient(t, cliServeRunWithFixtureArgs([]string{"run", "queued-signals"}, "signals", marker)...)
		if err := cliServeRunWaitForFile(entered); err != nil {
			t.Fatalf("wait for Start barrier: %v", err)
		}
		if err := client.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("queued first interrupt: %v", err)
		}
		releaseStart()

		if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
			t.Fatalf("managed process readiness after queued interrupt: %v", err)
		}
		if err := cliServeRunWaitForText(client.stdoutPath, "fixture:sigint-1\n"); err != nil {
			t.Fatalf("queued first SIGINT was not forwarded: %v; output=%q", err, client.stdout())
		}
		if client.exited() {
			t.Fatal("queued first SIGINT detached or terminated the attached run")
		}

		if err := client.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("second interrupt: %v", err)
		}
		if err := cliServeRunWaitForText(client.stdoutPath, "fixture:sigterm\n"); err != nil {
			t.Fatalf("second SIGINT did not initiate graceful stop: %v; output=%q", err, client.stdout())
		}
		if err := client.wait(5 * time.Second); err != nil {
			t.Fatalf("attached run after queued graceful stop: %v; stdout=%q stderr=%q", err, client.stdout(), client.stderr())
		}
		if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("first interrupt forwards and second stops", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		marker := filepath.Join(t.TempDir(), "signals")
		client := cliServeRunStartClient(t, cliServeRunWithFixtureArgs([]string{"run", "signals"}, "signals", marker)...)
		if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunWaitForText(client.stdoutPath, "fixture:ready\n"); err != nil {
			t.Fatalf("live stdout: %v; output=%q", err, client.stdout())
		}
		if err := cliServeRunWaitForText(client.stderrPath, "fixture:stderr-ready\n"); err != nil {
			t.Fatalf("live stderr: %v; output=%q", err, client.stderr())
		}

		if err := client.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("first interrupt: %v", err)
		}
		if err := cliServeRunWaitForText(client.stdoutPath, "fixture:sigint-1\n"); err != nil {
			t.Fatalf("first SIGINT was not forwarded: %v; output=%q", err, client.stdout())
		}
		if client.exited() {
			t.Fatal("first SIGINT detached the run instead of staying attached")
		}

		if err := client.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("second interrupt: %v", err)
		}
		if err := cliServeRunWaitForText(client.stdoutPath, "fixture:sigterm\n"); err != nil {
			t.Fatalf("second SIGINT did not initiate graceful stop: %v; output=%q", err, client.stdout())
		}
		if err := client.wait(5 * time.Second); err != nil {
			t.Fatalf("attached run after graceful stop: %v; stdout=%q stderr=%q", err, client.stdout(), client.stderr())
		}
		if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SIGTERM detaches without terminating", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		marker := filepath.Join(t.TempDir(), "term-detach")
		client := cliServeRunStartClient(t, cliServeRunWithFixtureArgs([]string{"run", "term-detach"}, "stream", marker)...)
		if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunWaitForText(client.stdoutPath, "stdout:live with spaces \r\n"); err != nil {
			t.Fatalf("live stdout before detach: %v; output=%q", err, client.stdout())
		}
		if err := cliServeRunWaitForText(client.stderrPath, "stderr:live with spaces \r\n"); err != nil {
			t.Fatalf("live stderr before detach: %v; output=%q", err, client.stderr())
		}
		if err := cliServeRunWaitForText(client.stdoutPath, "stdout:live-partial"); err != nil {
			t.Fatalf("live stdout partial before detach: %v; output=%q", err, client.stdout())
		}
		if err := cliServeRunWaitForText(client.stderrPath, "stderr:live-partial"); err != nil {
			t.Fatalf("live stderr partial before detach: %v; output=%q", err, client.stderr())
		}
		if err := client.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatalf("SIGTERM client: %v", err)
		}
		if err := client.wait(5 * time.Second); err != nil {
			t.Fatalf("SIGTERM-detached client exit: %v; stderr=%q", err, client.stderr())
		}
		cliServeRunAssertRunning(t, daemon.NewRuntimePaths(runtimeDir), "term-detach")
		if err := cliServeRunStop(t, "term-detach"); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(client.stdout(), "stdout:live with spaces \r\n") || !strings.Contains(client.stdout(), "stdout:live-partial") {
			t.Fatalf("live stdout lost raw bytes: %q", client.stdout())
		}
		if !strings.Contains(client.stderr(), "stderr:live with spaces \r\n") || !strings.Contains(client.stderr(), "stderr:live-partial") {
			t.Fatalf("live stderr lost raw bytes: %q", client.stderr())
		}
	})

	t.Run("connection loss detaches without terminating", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		marker := filepath.Join(t.TempDir(), "connection-loss")
		client := cliServeRunStartClient(t, cliServeRunWithFixtureArgs([]string{"run", "connection-loss"}, "stream", marker)...)
		if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
			t.Fatal(err)
		}
		if err := client.cmd.Process.Kill(); err != nil {
			t.Fatalf("kill attached client: %v", err)
		}
		_ = client.wait(5 * time.Second)
		cliServeRunAssertRunning(t, daemon.NewRuntimePaths(runtimeDir), "connection-loss")
		if err := cliServeRunStop(t, "connection-loss"); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("duplicate names identify pid and logs suggestion", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		marker := filepath.Join(t.TempDir(), "duplicate")
		firstOut, _, err := cliServeRunInvokeForTest(cliServeRunWithFixtureArgs([]string{"run", "duplicate", "--detach"}, "stream", marker)...)
		if err != nil {
			t.Fatalf("first duplicate process: %v", err)
		}
		if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
			t.Fatal(err)
		}
		firstPID := cliServeRunPID(firstOut)
		if firstPID <= 0 {
			t.Fatalf("first detached output omitted PID: %q", firstOut)
		}

		second := cliServeRunStartClient(t, "run", "duplicate", "--detach", "--", "/bin/true")
		secondErr := second.wait(5 * time.Second)
		if cliServeRunExitCode(secondErr) != 1 {
			t.Fatalf("duplicate run exit = %v (code %d), want 1; stderr=%q", secondErr, cliServeRunExitCode(secondErr), second.stderr())
		}
		message := second.stderr() + second.stdout()
		if !strings.Contains(message, "duplicate") || !strings.Contains(message, strconv.Itoa(firstPID)) || !strings.Contains(message, "hum logs duplicate --follow") {
			t.Fatalf("duplicate error = %q, want name, PID %d, and follow suggestion", message, firstPID)
		}
		if err := cliServeRunStop(t, "duplicate"); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDetachedRun(t *testing.T) {
	runtimeDir := cliServeRunRuntimeDir(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	cliServeRunStartDaemon(t, runtimeDir)

	t.Run("human name pid cursor", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "human")
		var output, errorOutput bytes.Buffer
		err := cliServeRunInvoke(context.Background(), cliServeRunWithFixtureArgs([]string{"run", "detached-human", "--detach"}, "stream", marker), &output, &errorOutput)
		if err != nil {
			t.Fatalf("detached human run: %v", err)
		}
		if errorOutput.Len() != 0 {
			t.Fatalf("unexpected detached stderr: %q", errorOutput.String())
		}
		if !strings.Contains(output.String(), "detached-human") || cliServeRunPID(output.String()) <= 0 || !strings.Contains(strings.ToLower(output.String()), "cursor") {
			t.Fatalf("detached human output = %q, want name, PID, and cursor", output.String())
		}
		if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunStop(t, "detached-human"); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("json stable process fields", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "json")
		var output, errorOutput bytes.Buffer
		err := cliServeRunInvoke(context.Background(), cliServeRunWithFixtureArgs([]string{"run", "detached-json", "--detach", "--json"}, "stream", marker), &output, &errorOutput)
		if err != nil {
			t.Fatalf("detached JSON run: %v", err)
		}
		if errorOutput.Len() != 0 {
			t.Fatalf("unexpected detached JSON stderr: %q", errorOutput.String())
		}
		name, pid, cursor, err := cliServeRunDecodeProcessSummary(output.Bytes())
		if err != nil {
			t.Fatalf("decode detached JSON: %v; output=%q", err, output.String())
		}
		if name != "detached-json" || pid <= 0 || cursor < 0 {
			t.Fatalf("detached JSON summary = name=%q pid=%d cursor=%d", name, pid, cursor)
		}
		if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunStop(t, "detached-json"); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
			t.Fatal(err)
		}
	})

}

type cliServeRunProcess struct {
	cmd        *exec.Cmd
	stdoutPath string
	stderrPath string
	finished   chan struct{}
	mu         sync.Mutex
	hasExited  bool
	waitErr    error
}

func cliServeRunStartClient(t *testing.T, args ...string) *cliServeRunProcess {
	return cliServeRunStartClientInDir(t, "", args...)
}

func cliServeRunStartClientInDir(t *testing.T, workDir string, args ...string) *cliServeRunProcess {
	t.Helper()
	dir := t.TempDir()
	stdoutPath := filepath.Join(dir, "stdout")
	stderrPath := filepath.Join(dir, "stderr")
	stdout, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.Create(stderrPath)
	if err != nil {
		_ = stdout.Close()
		t.Fatal(err)
	}
	argv := []string{cliServeRunChildFlag, cliServeRunHelperMarker, "client"}
	argv = append(argv, args...)
	command := exec.Command(os.Args[0], argv...)
	command.Env = append([]string(nil), os.Environ()...)
	if workDir != "" {
		command.Dir = workDir
	}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatal(err)
	}
	_ = stdout.Close()
	_ = stderr.Close()
	process := &cliServeRunProcess{cmd: command, stdoutPath: stdoutPath, stderrPath: stderrPath, finished: make(chan struct{})}
	go func() {
		err := command.Wait()
		process.mu.Lock()
		process.waitErr = err
		process.hasExited = true
		process.mu.Unlock()
		close(process.finished)
	}()
	t.Cleanup(func() {
		if process.exited() {
			return
		}
		_ = command.Process.Kill()
		_ = process.wait(2 * time.Second)
	})
	return process
}

func (p *cliServeRunProcess) wait(timeout time.Duration) error {
	select {
	case <-p.finished:
		p.mu.Lock()
		err := p.waitErr
		p.mu.Unlock()
		return err
	case <-time.After(timeout):
		return fmt.Errorf("process did not exit within %s", timeout)
	}
}

func (p *cliServeRunProcess) exited() bool {
	p.mu.Lock()
	hasExited := p.hasExited
	p.mu.Unlock()
	return hasExited
}

func (p *cliServeRunProcess) stdout() string {
	data, _ := os.ReadFile(p.stdoutPath)
	return string(data)
}

func (p *cliServeRunProcess) stderr() string {
	data, _ := os.ReadFile(p.stderrPath)
	return string(data)
}

func cliServeRunStartDaemon(t *testing.T, runtimeDir string) {
	cliServeRunStartDaemonWithSupervisor(t, runtimeDir, nil)
}

func cliServeRunStartDaemonWithSupervisor(t *testing.T, runtimeDir string, supervisor *app.Supervisor) {
	t.Helper()
	server, err := daemon.NewServer(daemon.Config{
		RuntimeDir: runtimeDir,
		StopGrace:  100 * time.Millisecond,
		Supervisor: supervisor,
	})
	if err != nil {
		if supervisor != nil {
			_ = supervisor.Shutdown(context.Background())
		}
		t.Fatalf("new daemon: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	if err := cliServeRunWaitForDaemon(server.SocketPath()); err != nil {
		_ = server.Close()
		cancel()
		t.Fatalf("daemon readiness: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("daemon did not stop during cleanup")
		}
	})
}

func cliServeRunInvokeForTest(args ...string) (string, string, error) {
	var output, errorOutput bytes.Buffer
	err := cliServeRunInvoke(context.Background(), args, &output, &errorOutput)
	return output.String(), errorOutput.String(), err
}

func cliServeRunStop(t *testing.T, name string) error {
	t.Helper()
	_, _, err := cliServeRunInvokeForTest("stop", name)
	return err
}

func cliServeRunAssertRunning(t *testing.T, paths daemon.RuntimePaths, name string) {
	t.Helper()
	client, err := daemon.Dial(context.Background(), paths.Socket)
	if err != nil {
		t.Fatalf("dial daemon to inspect %q: %v", name, err)
	}
	defer client.Close()
	items, err := client.List(context.Background(), daemon.ListRequest{Cwd: mustWorkingDirectory(t)})
	if err != nil {
		t.Fatalf("list %q: %v", name, err)
	}
	for _, item := range items {
		if item.Name == name {
			if item.State != app.StateRunning {
				t.Fatalf("managed process %q state = %q, want running", name, item.State)
			}
			return
		}
	}
	t.Fatalf("managed process %q disappeared after client detach", name)
}

func mustWorkingDirectory(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return cwd
}

func cliServeRunFixtureArgv(mode string, args ...string) []string {
	argv := []string{os.Args[0], cliServeRunChildFlag, cliServeRunHelperMarker, "fixture", mode}
	return append(argv, args...)
}

func cliServeRunPID(output string) int {
	match := regexp.MustCompile(`(?i)\bpid\D+(\d+)`).FindStringSubmatch(output)
	if len(match) != 2 {
		return 0
	}
	pid, _ := strconv.Atoi(match[1])
	return pid
}

func cliServeRunWithFixtureArgs(prefix []string, mode string, args ...string) []string {
	command := append([]string(nil), prefix...)
	command = append(command, "--")
	return append(command, cliServeRunFixtureArgv(mode, args...)...)
}

func cliServeRunExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	return -1
}

func cliServeRunDecodeProcessSummary(data []byte) (string, int, int64, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(data), &object); err != nil {
		return "", 0, 0, err
	}
	if process, ok := object["process"]; ok {
		if err := json.Unmarshal(process, &object); err != nil {
			return "", 0, 0, err
		}
	}
	var name string
	if raw, ok := object["name"]; ok {
		if err := json.Unmarshal(raw, &name); err != nil {
			return "", 0, 0, err
		}
	}
	var pid int
	if raw, ok := object["pid"]; ok {
		if err := json.Unmarshal(raw, &pid); err != nil {
			return "", 0, 0, err
		}
	}
	var cursor int64 = -1
	for _, key := range []string{"launch_cursor", "cursor", "next_cursor"} {
		if raw, ok := object[key]; ok {
			if err := json.Unmarshal(raw, &cursor); err != nil {
				return "", 0, 0, err
			}
			break
		}
	}
	if name == "" || pid <= 0 || cursor < 0 {
		return "", 0, 0, fmt.Errorf("missing stable name/pid/cursor fields")
	}
	return name, pid, cursor, nil
}

func cliServeRunWaitForDaemon(socket string) error {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		client, err := daemon.Dial(ctx, socket)
		cancel()
		if err == nil {
			_ = client.Close()
			return nil
		}
		lastErr = err
		select {
		case <-ticker.C:
		case <-deadline.C:
			return fmt.Errorf("daemon did not become ready: %w", lastErr)
		}
	}
}

func cliServeRunWaitForFile(path string) error {
	return cliServeRunWaitForCondition(func() bool {
		_, err := os.Stat(path)
		return err == nil
	})
}

func cliServeRunWaitForText(path, want string) error {
	return cliServeRunWaitForCondition(func() bool {
		data, err := os.ReadFile(path)
		return err == nil && strings.Contains(string(data), want)
	})
}

func cliServeRunWaitForCondition(condition func() bool) error {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return nil
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			return errors.New("condition did not become true before timeout")
		}
	}
}

func cliServeRunEqualStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
