package cli

import (
	"bufio"
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

	"github.com/creack/pty"
	urfavecli "github.com/urfave/cli/v3"
	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/process"
	"hum/internal/project"
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

type cliServeRunFailWriter struct{}

func (cliServeRunFailWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

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
	case "unhandled":
		if len(args) < 2 {
			return 2
		}
		return cliServeRunFixtureUnhandled(args[1])
	case "stubborn":
		if len(args) < 2 {
			return 2
		}
		return cliServeRunFixtureStubborn(args[1])
	case "term":
		if len(args) < 2 {
			return 2
		}
		return cliServeRunFixtureTerm(args[1])
	case "tail":
		if len(args) < 2 {
			return 2
		}
		return cliServeRunFixtureTail(args[1])
	case "flood":
		if len(args) < 3 {
			return 2
		}
		count, err := strconv.Atoi(args[2])
		if err != nil || count <= 0 {
			return 2
		}
		return cliServeRunFixtureFlood(args[1], count)
	case "tty":
		if len(args) < 2 {
			return 2
		}
		return cliServeRunFixtureTTY(args[1])
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

// cliServeRunFixtureFlood emits a bounded burst faster than a client can
// format it, which is what makes a follower's local buffer fill.
func cliServeRunFixtureFlood(marker string, count int) int {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	if err := os.WriteFile(marker+".started", []byte("started"), 0600); err != nil {
		return 2
	}
	for {
		if _, err := os.Stat(marker + ".release"); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	writer := bufio.NewWriterSize(os.Stdout, 32*1024)
	for index := 0; index < count; index++ {
		if _, err := fmt.Fprintf(writer, "flood:%05d\n", index); err != nil {
			return 2
		}
	}
	if err := writer.Flush(); err != nil {
		return 2
	}
	for {
		if <-signals == syscall.SIGTERM {
			_ = os.WriteFile(marker+".terminated", []byte("terminated"), 0600)
			return 0
		}
	}
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

func cliServeRunFixtureStubborn(marker string) int {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	if err := os.WriteFile(marker+".started", []byte("started"), 0o600); err != nil {
		return 2
	}
	for range signals {
		fmt.Fprintln(os.Stdout, "fixture:ignored-term")
	}
	return 0
}

func cliServeRunFixtureUnhandled(marker string) int {
	if err := os.WriteFile(marker+".started", []byte("started"), 0o600); err != nil {
		return 2
	}
	fmt.Fprintln(os.Stdout, "fixture:ready")
	for {
		time.Sleep(time.Hour)
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

func cliServeRunFixtureTail(marker string) int {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGTERM)
	defer signal.Stop(signals)
	for _, line := range []string{
		"tail-zero" + strings.Repeat("w", 24<<10),
		"tail-one" + strings.Repeat("x", 24<<10),
		"tail-two" + strings.Repeat("y", 24<<10),
		"tail-three" + strings.Repeat("z", 24<<10),
	} {
		fmt.Fprintln(os.Stdout, line)
		time.Sleep(20 * time.Millisecond)
	}
	if err := os.WriteFile(marker+".started", []byte("started"), 0600); err != nil {
		return 2
	}
	for {
		switch <-signals {
		case syscall.SIGHUP:
			fmt.Fprintln(os.Stdout, "tail-live")
		case syscall.SIGTERM:
			if err := os.WriteFile(marker+".terminated", []byte("terminated"), 0600); err != nil {
				return 2
			}
			return 0
		}
	}
}

func cliServeRunFixtureTTY(marker string) int {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	if err := os.WriteFile(marker+".started", []byte("started"), 0600); err != nil {
		return 2
	}
	lastCols, lastRows := 0, 0
	input := make(chan []byte, 1)
	go func() {
		buffer := make([]byte, 256)
		for {
			count, err := os.Stdin.Read(buffer)
			if count > 0 {
				payload := append([]byte(nil), buffer[:count]...)
				select {
				case input <- payload:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rows, cols, err := pty.Getsize(os.Stdin)
			if err == nil && cols > 0 && rows > 0 && (cols != lastCols || rows != lastRows) {
				lastCols, lastRows = cols, rows
				fmt.Fprintf(os.Stdout, "tty:size=%dx%d\n", cols, rows)
			}
		case payload := <-input:
			fmt.Fprintf(os.Stdout, "tty:input=%q\n", payload)
		case sig := <-signals:
			switch sig {
			case syscall.SIGTERM:
				if err := os.WriteFile(marker+".terminated", []byte("terminated"), 0600); err != nil {
					return 2
				}
				return 0
			case syscall.SIGINT:
				fmt.Fprintln(os.Stdout, "tty:sigint")
			}
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
	currentRoot, err := project.DiscoverProjectRoot("")
	if err != nil {
		t.Fatal(err)
	}
	listOutput := fmt.Sprintf("Nothing is running in %s. Use hum list --all to see every scope.\n", currentRoot)
	tests := []struct {
		name    string
		args    []string
		wantErr string
		wantOut string
	}{
		{name: "list", args: []string{"list"}, wantOut: listOutput},
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
	if _, _, err := cliServeRunInvokeForTest("shutdown", "--stop-processes"); err != nil {
		t.Fatalf("shutdown recovered daemon: %v", err)
	}
	if err := cliServeRunWaitForCondition(func() bool {
		_, socketErr := os.Stat(paths.Socket)
		return errors.Is(socketErr, os.ErrNotExist)
	}); err != nil {
		t.Fatalf("wait for recovered daemon shutdown: %v", err)
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

		attachedCtx, cancelAttached := context.WithTimeout(context.Background(), 5*time.Second)
		var attachedOut, attachedErr strings.Builder
		err := cliServeRunInvoke(attachedCtx, []string{"run", "automatic-attached", "--", "/bin/sh", "-c", "printf attached"}, &attachedOut, &attachedErr)
		cancelAttached()
		if err != nil {
			t.Fatalf("attached automatic run: %v", err)
		}
		stdout, stderr := attachedOut.String(), attachedErr.String()
		if stdout != "attached" || stderr != "" {
			t.Fatalf("attached automatic output = stdout %q stderr %q, want raw child output only", stdout, stderr)
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
			RuntimeDir:  runtimeDir,
			WireVersion: 999,
			StopGrace:   100 * time.Millisecond,
			Supervisor:  supervisor,
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

func TestAttachedRunOneIncarnation(t *testing.T) {
	t.Run("first entry of a silent record's successor is not swallowed", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		clientDir := t.TempDir()
		marker := filepath.Join(clientDir, "marker")
		// Cursors are zero-based and the follow window is exclusive, so a record
		// that retained nothing must not start the successor's window at zero.
		script := fmt.Sprintf("if [ -f %q ]; then echo FIRST-LINE; echo SECOND-LINE; else : > %q; fi", marker, marker)
		for attempt := 0; attempt < 2; attempt++ {
			client := cliServeRunStartClientInDir(t, clientDir, "run", "silent", "--", "/bin/sh", "-c", script)
			if err := client.wait(10 * time.Second); err != nil {
				t.Fatalf("attempt %d: %v; stdout=%q stderr=%q", attempt, err, client.stdout(), client.stderr())
			}
			if attempt == 0 {
				if got := client.stdout(); got != "" {
					t.Fatalf("silent incarnation stdout = %q, want empty", got)
				}
				continue
			}
			for _, want := range []string{"FIRST-LINE", "SECOND-LINE"} {
				if !strings.Contains(client.stdout(), want) {
					t.Fatalf("successor stdout = %q, want %q", client.stdout(), want)
				}
			}
		}
	})

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
			t.Fatalf("attached run exit = %v (code %d), want 23; stdout=%q stderr=%q", err, cliServeRunExitCode(err), client.stdout(), client.stderr())
		}
		for _, boundary := range []string{" launched\n", " exited", "waiting for"} {
			if strings.Contains(client.stdout()+client.stderr(), boundary) {
				t.Fatalf("attached output contains boundary %q: stdout=%q stderr=%q", boundary, client.stdout(), client.stderr())
			}
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
	t.Run("json is detached only and fails before launch", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)

		client := cliServeRunStartClient(t, cliServeRunWithFixtureArgs([]string{"run", "attached-json", "--json"}, "inspect")...)
		if err := client.wait(5 * time.Second); cliServeRunExitCode(err) != 1 {
			t.Fatalf("attached JSON exit = %v (code %d), want 1; stdout=%q stderr=%q", err, cliServeRunExitCode(err), client.stdout(), client.stderr())
		}
		if !strings.Contains(client.stderr(), "--json is supported only with --detach") {
			t.Fatalf("attached --json stderr = %q", client.stderr())
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
			t.Fatalf("already-exited run = %v (code %d), want 37; stdout=%q stderr=%q", err, cliServeRunExitCode(err), client.stdout(), client.stderr())
		}
	})

	t.Run("declared and discovered stopped definitions", func(t *testing.T) {
		for _, testCase := range []struct {
			name  string
			setup func(string) error
			want  string
		}{
			{name: "declared", want: "declared-output\n", setup: func(root string) error {
				return os.WriteFile(filepath.Join(root, "hum.yaml"), []byte("version: 1\nprocesses:\n  declared:\n    argv: [/bin/echo, declared-output]\n"), 0o600)
			}},
			{name: "dev", want: "discovered-output\n", setup: func(root string) error {
				if err := os.Mkdir(filepath.Join(root, "bin"), 0o700); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(root, "bin", "dev"), []byte("#!/bin/sh\nprintf 'discovered-output\\n'\n"), 0o700)
			}},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				runtimeDir := cliServeRunRuntimeDir(t)
				t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
				cliServeRunStartDaemon(t, runtimeDir)
				root := t.TempDir()
				if err := testCase.setup(root); err != nil {
					t.Fatal(err)
				}
				oldwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Chdir(root); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = os.Chdir(oldwd) }()
				stdout, stderr, err := cliServeRunInvokeForTest("run", testCase.name)
				if err != nil || stdout != testCase.want || stderr != "" {
					t.Fatalf("resolved run = stdout %q stderr %q err %v", stdout, stderr, err)
				}
			})
		}
	})

	t.Run("retained definition launches without replay and remains loggable", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		marker := filepath.Join(t.TempDir(), "retained")
		script := fmt.Sprintf("if test -e %s; then printf 'new-output\\n'; else : > %s; printf 'old-output\\n'; fi", strconv.Quote(marker), strconv.Quote(marker))
		if _, _, err := cliServeRunInvokeForTest("run", "retained", "--detach", "--", "/bin/sh", "-c", script); err != nil {
			t.Fatal(err)
		}
		paths := daemon.NewRuntimePaths(runtimeDir)
		if err := cliServeRunWaitForCondition(func() bool {
			client, dialErr := daemon.Dial(context.Background(), paths.Socket)
			if dialErr != nil {
				return false
			}
			defer client.Close()
			process, getErr := client.Get(context.Background(), daemon.GetRequest{Name: "retained", Cwd: mustWorkingDirectory(t)})
			return getErr == nil && process.State == app.StateExited
		}); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := cliServeRunInvokeForTest("run", "retained")
		if err != nil || stdout != "new-output\n" || stderr != "" {
			t.Fatalf("retained foreground run = stdout %q stderr %q err %v", stdout, stderr, err)
		}
		logs, logsErr, err := cliServeRunInvokeForTest("logs", "retained")
		if err != nil || !strings.Contains(logs, "old-output") || !strings.Contains(logs, "new-output") {
			t.Fatalf("retained logs = stdout %q stderr %q err %v", logs, logsErr, err)
		}
	})

	t.Run("signal exit maps to 128 plus signal", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		client := cliServeRunStartClient(t, "run", "signaled", "--", "/bin/sh", "-c", "kill -TERM $$")
		if err := client.wait(5 * time.Second); cliServeRunExitCode(err) != 143 {
			t.Fatalf("signal exit = %v (code %d), want 143; stderr=%q", err, cliServeRunExitCode(err), client.stderr())
		}
	})
}

func TestAttachedRunInterruptLifecycle(t *testing.T) {
	t.Run("queued SIGINT during start-to-follow handoff forwards and stays attached", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		marker := filepath.Join(t.TempDir(), "queued-signals")
		entered := filepath.Join(t.TempDir(), "start-entered")
		release := make(chan struct{})
		var releaseOnce sync.Once
		releaseStart := func() { releaseOnce.Do(func() { close(release) }) }

		supervisor, err := app.New(app.Options{
			StopGrace: 2 * time.Second,
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
			t.Fatalf("queued SIGINT was not forwarded: %v; stdout=%q stderr=%q", err, client.stdout(), client.stderr())
		}
		if client.exited() {
			t.Fatal("queued first SIGINT detached the client")
		}
		if err := client.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("second interrupt: %v", err)
		}
		if err := client.wait(5 * time.Second); err != nil {
			t.Fatalf("second interrupt stop = %v; stdout=%q stderr=%q", err, client.stdout(), client.stderr())
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
			t.Fatalf("first SIGINT was not forwarded: %v; stdout=%q stderr=%q", err, client.stdout(), client.stderr())
		}
		if !strings.Contains(client.stderr(), "interrupt sent to signals; press Ctrl+C again to stop") {
			t.Fatalf("first SIGINT hint missing: %q", client.stderr())
		}
		if client.exited() {
			t.Fatal("first SIGINT detached the client")
		}
		if err := client.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("second interrupt: %v", err)
		}
		if err := client.wait(5 * time.Second); err != nil {
			t.Fatalf("second interrupt stop = %v; stdout=%q stderr=%q", err, client.stdout(), client.stderr())
		}
		if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SIGTERM stops and waits for terminal event", func(t *testing.T) {
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
			t.Fatalf("SIGTERM stop exit: %v; stderr=%q", err, client.stderr())
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

	t.Run("duplicate names identify attach and stop guidance", func(t *testing.T) {
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
		if !strings.Contains(message, "duplicate is already running") || !strings.Contains(message, "hum attach duplicate") || !strings.Contains(message, "hum stop duplicate") {
			t.Fatalf("duplicate error = %q, want attach and stop guidance (running PID %d)", message, firstPID)
		}
		if err := cliServeRunStop(t, "duplicate"); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("first SIGINT and SIGTERM preserve signal status", func(t *testing.T) {
		for _, testCase := range []struct {
			name   string
			signal os.Signal
			code   int
		}{
			{name: "interrupt", signal: os.Interrupt, code: 130},
			{name: "terminate", signal: syscall.SIGTERM, code: 143},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				runtimeDir := cliServeRunRuntimeDir(t)
				t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
				cliServeRunStartDaemon(t, runtimeDir)
				marker := filepath.Join(t.TempDir(), testCase.name)
				client := cliServeRunStartClient(t, cliServeRunWithFixtureArgs([]string{"run", testCase.name}, "unhandled", marker)...)
				if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
					t.Fatal(err)
				}
				if err := client.cmd.Process.Signal(testCase.signal); err != nil {
					t.Fatal(err)
				}
				if err := client.wait(5 * time.Second); cliServeRunExitCode(err) != testCase.code {
					t.Fatalf("signal exit = %v (code %d), want %d; stderr=%q", err, cliServeRunExitCode(err), testCase.code, client.stderr())
				}
			})
		}
	})

	t.Run("SIGHUP detaches without stopping", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		marker := filepath.Join(t.TempDir(), "hup")
		client := cliServeRunStartClient(t, cliServeRunWithFixtureArgs([]string{"run", "hup"}, "stream", marker)...)
		if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
			t.Fatal(err)
		}
		if err := client.cmd.Process.Signal(syscall.SIGHUP); err != nil {
			t.Fatal(err)
		}
		if err := client.wait(5 * time.Second); err != nil {
			t.Fatalf("SIGHUP detach = %v", err)
		}
		if !strings.Contains(client.stderr(), "detached from hup; it keeps running (hum attach hup)") {
			t.Fatalf("detach notice = %q", client.stderr())
		}
		cliServeRunAssertRunning(t, daemon.NewRuntimePaths(runtimeDir), "hup")
		if err := cliServeRunStop(t, "hup"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("context cancellation and output failure detach", func(t *testing.T) {
		for _, testCase := range []struct {
			name       string
			failOutput bool
		}{
			{name: "context"},
			{name: "output", failOutput: true},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				runtimeDir := cliServeRunRuntimeDir(t)
				t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
				cliServeRunStartDaemon(t, runtimeDir)
				marker := filepath.Join(t.TempDir(), testCase.name)
				args := cliServeRunWithFixtureArgs([]string{"run", testCase.name}, "stream", marker)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var stdout, stderr bytes.Buffer
				done := make(chan error, 1)
				go func() {
					var writer io.Writer = &stdout
					if testCase.failOutput {
						writer = cliServeRunFailWriter{}
					}
					done <- cliServeRunInvoke(ctx, args, writer, &stderr)
				}()
				if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
					t.Fatal(err)
				}
				if !testCase.failOutput {
					cancel()
				}
				select {
				case err := <-done:
					if err != nil {
						t.Fatalf("detach = %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("detach timed out")
				}
				if !strings.Contains(stderr.String(), "detached from "+testCase.name) {
					t.Fatalf("detach notice = %q", stderr.String())
				}
				cliServeRunAssertRunning(t, daemon.NewRuntimePaths(runtimeDir), testCase.name)
				if err := cliServeRunStop(t, testCase.name); err != nil {
					t.Fatal(err)
				}
			})
		}
	})

	t.Run("in-flight bounded stop survives client loss", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		marker := filepath.Join(t.TempDir(), "stubborn")
		client := cliServeRunStartClient(t, cliServeRunWithFixtureArgs([]string{"run", "stubborn"}, "stubborn", marker)...)
		if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
			t.Fatal(err)
		}
		if err := client.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunWaitForText(client.stderrPath, "interrupt sent"); err != nil {
			t.Fatal(err)
		}
		if err := client.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
		_ = client.cmd.Process.Kill()
		_ = client.wait(2 * time.Second)
		if err := cliServeRunWaitForCondition(func() bool {
			probe, dialErr := daemon.Dial(context.Background(), daemon.NewRuntimePaths(runtimeDir).Socket)
			if dialErr != nil {
				return false
			}
			defer probe.Close()
			process, getErr := probe.Get(context.Background(), daemon.GetRequest{Name: "stubborn", Cwd: mustWorkingDirectory(t)})
			return getErr == nil && !app.IsActiveState(process.State)
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("control exits suppress on-failure successors", func(t *testing.T) {
		for _, testCase := range []struct {
			name   string
			signal os.Signal
			code   int
		}{
			{name: "interrupt", signal: os.Interrupt, code: 130},
			{name: "terminate", signal: syscall.SIGTERM, code: 143},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				runtimeDir := cliServeRunRuntimeDir(t)
				t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
				cliServeRunStartDaemon(t, runtimeDir)
				root := t.TempDir()
				marker := filepath.Join(root, "restart")
				argv := cliServeRunFixtureArgv("unhandled", marker)
				encodedArgv, err := json.Marshal(argv)
				if err != nil {
					t.Fatal(err)
				}
				manifest := fmt.Sprintf("version: 1\nprocesses:\n  controlled:\n    argv: %s\n    restart: on-failure\n", encodedArgv)
				if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(manifest), 0o600); err != nil {
					t.Fatal(err)
				}
				canonicalRoot, err := filepath.EvalSymlinks(root)
				if err != nil {
					t.Fatal(err)
				}
				client := cliServeRunStartClientInDir(t, canonicalRoot, "run", "controlled")
				if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
					t.Fatal(err)
				}
				if err := client.cmd.Process.Signal(testCase.signal); err != nil {
					t.Fatal(err)
				}
				if err := client.wait(5 * time.Second); cliServeRunExitCode(err) != testCase.code {
					t.Fatalf("controlled exit = %v (code %d), want %d", err, cliServeRunExitCode(err), testCase.code)
				}
				time.Sleep(1100 * time.Millisecond)
				probe, err := daemon.Dial(context.Background(), daemon.NewRuntimePaths(runtimeDir).Socket)
				if err != nil {
					t.Fatal(err)
				}
				defer probe.Close()
				process, err := probe.Get(context.Background(), daemon.GetRequest{Name: "controlled", Cwd: canonicalRoot})
				if err != nil || app.IsActiveState(process.State) || process.NextLaunchAt != nil || process.Relaunches != 0 {
					t.Fatalf("controlled process = %+v err %v, want terminal state with no successor", process, err)
				}
			})
		}
	})
}

func TestAttachRunningSession(t *testing.T) {
	runtimeDir := cliServeRunRuntimeDir(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	cliServeRunStartDaemon(t, runtimeDir)
	marker := filepath.Join(t.TempDir(), "attach-running")
	if _, _, err := cliServeRunInvokeForTest(cliServeRunWithFixtureArgs([]string{"run", "attach-running", "--tty", "--detach"}, "tty", marker)...); err != nil {
		t.Fatalf("start running tty session: %v", err)
	}
	if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
		t.Fatal(err)
	}

	first := cliServeRunStartPTYClient(t, "attach", "attach-running")
	if err := first.waitForText("tty:size="); err != nil {
		t.Fatalf("attach initial replay: %v; output=%q", err, first.output())
	}
	if _, err := first.master.Write([]byte("attach-input\n")); err != nil {
		t.Fatalf("write through attach terminal: %v", err)
	}
	if err := cliServeRunWaitForTextIn(first.output, "tty:input=\"attach-input\\n\""); err != nil {
		t.Fatalf("attach raw input forwarding: %v; output=%q", err, first.output())
	}
	if err := pty.Setsize(first.master, &pty.Winsize{Cols: 111, Rows: 37}); err != nil {
		t.Fatalf("resize attached terminal: %v", err)
	}
	if err := first.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
		t.Fatalf("signal attached terminal resize: %v", err)
	}
	if err := cliServeRunWaitForTextIn(first.output, "tty:size=111x37"); err != nil {
		t.Fatalf("attach resize forwarding: %v; output=%q", err, first.output())
	}

	second := cliServeRunStartPTYClient(t, "attach", "attach-running")
	if err := second.waitForText("following output only"); err != nil {
		t.Fatalf("second attach ownership notice: %v; output=%q", err, second.output())
	}
	if _, err := second.master.Write([]byte("second-input\n")); err != nil {
		t.Fatalf("write through second attach terminal: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(second.output(), "tty:input=\"second-input\\n\"") {
		t.Fatalf("second attach unexpectedly forwarded input: %q", second.output())
	}
	if err := second.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("detach second attach: %v", err)
	}
	if err := second.wait(5 * time.Second); err != nil {
		t.Fatalf("second attach detach: %v; output=%q", err, second.output())
	}
	if err := first.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("detach first attach: %v", err)
	}
	if err := first.wait(5 * time.Second); err != nil {
		t.Fatalf("first attach detach: %v; output=%q", err, first.output())
	}
	cliServeRunAssertRunning(t, daemon.NewRuntimePaths(runtimeDir), "attach-running")
	if err := cliServeRunStop(t, "attach-running"); err != nil {
		t.Fatal(err)
	}
	if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
		t.Fatal(err)
	}
}

func TestAttachNeverStartsSession(t *testing.T) {
	runtimeDir := cliServeRunRuntimeDir(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	cliServeRunStartDaemon(t, runtimeDir)

	if _, _, err := cliServeRunInvokeForTest("attach", "missing"); err == nil || !strings.Contains(err.Error(), "hum start missing") {
		t.Fatalf("missing attach error = %v, want actionable start guidance", err)
	}
	client, err := daemon.Dial(context.Background(), daemon.NewRuntimePaths(runtimeDir).Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Get(context.Background(), daemon.GetRequest{Name: "missing", Cwd: mustWorkingDirectory(t)}); !isNotFound(err) {
		t.Fatalf("missing attach created a record: %v", err)
	}

	marker := filepath.Join(t.TempDir(), "attach-stopped")
	if _, _, err := cliServeRunInvokeForTest(cliServeRunWithFixtureArgs([]string{"run", "attach-stopped", "--detach"}, "term", marker)...); err != nil {
		t.Fatalf("start stopped-session fixture: %v", err)
	}
	if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
		t.Fatal(err)
	}
	before, err := client.Get(context.Background(), daemon.GetRequest{Name: "attach-stopped", Cwd: mustWorkingDirectory(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := cliServeRunStop(t, "attach-stopped"); err != nil {
		t.Fatal(err)
	}
	afterStop, err := client.Get(context.Background(), daemon.GetRequest{Name: "attach-stopped", Cwd: mustWorkingDirectory(t)})
	if err != nil {
		t.Fatal(err)
	}
	if afterStop.State != app.StateStopped {
		t.Fatalf("stopped fixture state = %q, want stopped", afterStop.State)
	}
	if _, _, err := cliServeRunInvokeForTest("attach", "attach-stopped"); err == nil || !strings.Contains(err.Error(), "is not running") {
		t.Fatalf("stopped attach error = %v, want not-running guidance", err)
	}
	afterAttach, err := client.Get(context.Background(), daemon.GetRequest{Name: "attach-stopped", Cwd: mustWorkingDirectory(t)})
	if err != nil {
		t.Fatal(err)
	}
	if afterAttach.State != afterStop.State || afterAttach.LaunchCursor != afterStop.LaunchCursor || afterAttach.PID != afterStop.PID {
		t.Fatalf("stopped record changed after attach: before=%+v after-stop=%+v after-attach=%+v", before, afterStop, afterAttach)
	}
}

func TestAttachStreamsBurstWithoutAborting(t *testing.T) {
	runtimeDir := cliServeRunRuntimeDir(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	cliServeRunStartDaemon(t, runtimeDir)
	const floodLines = 12000
	marker := filepath.Join(t.TempDir(), "attach-flood")
	if _, _, err := cliServeRunInvokeForTest(cliServeRunWithFixtureArgs([]string{"run", "attach-flood", "--detach"}, "flood", marker, strconv.Itoa(floodLines))...); err != nil {
		t.Fatalf("start flood fixture: %v", err)
	}
	if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
		t.Fatal(err)
	}
	projectRoot := mustWorkingDirectory(t)
	client, err := daemon.Dial(context.Background(), daemon.NewRuntimePaths(runtimeDir).Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	attached := cliServeRunStartClient(t, "attach", "attach-flood", "--tail", "0")
	if err := cliServeRunWaitForCondition(func() bool {
		process, getErr := client.Get(context.Background(), daemon.GetRequest{Name: "attach-flood", Cwd: projectRoot})
		return getErr == nil && process.Followers == 1
	}); err != nil {
		t.Fatalf("flood attach readiness: %v", err)
	}
	// A burst that outpaces the local writer is backpressure, not a failure:
	// attach must stream every line instead of aborting once its buffer fills.
	if err := os.WriteFile(marker+".release", []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cliServeRunWaitForCondition(func() bool {
		return strings.Contains(attached.stdout(), fmt.Sprintf("flood:%05d\n", floodLines-1))
	}); err != nil {
		t.Fatalf("flood attach did not stream the whole burst: %v; exited=%v lines=%d stderr=%q", err, attached.exited(), strings.Count(attached.stdout(), "flood:"), attached.stderr())
	}
	if attached.exited() {
		t.Fatalf("flood attach exited during the burst: stderr=%q", attached.stderr())
	}
	if got := attached.stderr(); got != "" {
		t.Fatalf("flood attach stderr = %q, want no diagnostic", got)
	}
	if got := strings.Count(attached.stdout(), "flood:"); got != floodLines {
		t.Fatalf("flood attach streamed %d of %d lines", got, floodLines)
	}
	if err := attached.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := attached.wait(5 * time.Second); err != nil {
		t.Fatalf("detach flood attach: %v; stderr=%q", err, attached.stderr())
	}
	if err := cliServeRunStop(t, "attach-flood"); err != nil {
		t.Fatal(err)
	}
	if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
		t.Fatal(err)
	}
}

func TestAttachTail(t *testing.T) {
	runtimeDir := cliServeRunRuntimeDir(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	if _, _, err := cliServeRunInvokeForTest("attach", "attach-tail", "--tail", "-1"); err == nil || !strings.Contains(err.Error(), "tail must not be negative") {
		t.Fatalf("negative attach tail error before daemon contact = %v", err)
	}
	cliServeRunStartDaemon(t, runtimeDir)
	marker := filepath.Join(t.TempDir(), "attach-tail")
	if _, _, err := cliServeRunInvokeForTest(cliServeRunWithFixtureArgs([]string{"run", "attach-tail", "--detach"}, "tail", marker)...); err != nil {
		t.Fatalf("start tail fixture: %v", err)
	}
	if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
		t.Fatal(err)
	}
	projectRoot := mustWorkingDirectory(t)
	client, err := daemon.Dial(context.Background(), daemon.NewRuntimePaths(runtimeDir).Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	before, err := client.Output(context.Background(), daemon.OutputRequest{Name: "attach-tail", Cwd: projectRoot, Tail: 10, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	attached := cliServeRunStartClient(t, "attach", "attach-tail", "--tail", "3")
	// Emit live output once replay has started; attach must buffer it and
	// preserve replay-before-live ordering.
	if err := cliServeRunWaitForText(attached.stdoutPath, "tail-one"); err != nil {
		t.Fatalf("tail replay start: %v; output bytes=%d", err, len(attached.stdout()))
	}
	if err := client.Signal(context.Background(), daemon.SignalRequest{Name: "attach-tail", Cwd: projectRoot, Signal: "SIGHUP"}); err != nil {
		t.Fatal(err)
	}
	if err := cliServeRunWaitForText(attached.stdoutPath, "tail-live\n"); err != nil {
		t.Fatalf("tail replay and live output: %v; output bytes=%d", err, len(attached.stdout()))
	}
	replay := attached.stdout()
	oneIndex, twoIndex := strings.Index(replay, "tail-one"), strings.Index(replay, "tail-two")
	threeIndex, liveIndex := strings.Index(replay, "tail-three"), strings.Index(replay, "tail-live\n")
	if strings.Contains(replay, "tail-zero") || oneIndex < 0 || twoIndex < 0 || threeIndex < 0 || liveIndex < 0 || oneIndex >= twoIndex || twoIndex >= threeIndex || threeIndex >= liveIndex {
		t.Fatalf("tail output bytes=%d indexes=(%d,%d,%d,%d), want exactly final three retained entries in source order before live output", len(replay), oneIndex, twoIndex, threeIndex, liveIndex)
	}
	if err := attached.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := attached.wait(5 * time.Second); err != nil {
		t.Fatalf("detach tail attach: %v; output=%q", err, attached.stdout())
	}
	if err := cliServeRunWaitForCondition(func() bool {
		process, getErr := client.Get(context.Background(), daemon.GetRequest{Name: "attach-tail", Cwd: projectRoot})
		return getErr == nil && process.Followers == 0
	}); err != nil {
		t.Fatalf("tail attach follower cleanup: %v", err)
	}

	liveOnly := cliServeRunStartClient(t, "attach", "attach-tail", "--tail", "0")
	if err := cliServeRunWaitForCondition(func() bool {
		process, getErr := client.Get(context.Background(), daemon.GetRequest{Name: "attach-tail", Cwd: projectRoot})
		return getErr == nil && process.Followers == 1
	}); err != nil {
		t.Fatalf("tail zero attach readiness: %v", err)
	}
	if got := liveOnly.stdout(); strings.Contains(got, "tail-zero") || strings.Contains(got, "tail-one") || strings.Contains(got, "tail-two") || strings.Contains(got, "tail-three") {
		t.Fatalf("tail zero replayed retained output: %q", got)
	}
	if err := client.Signal(context.Background(), daemon.SignalRequest{Name: "attach-tail", Cwd: projectRoot, Signal: "SIGHUP"}); err != nil {
		t.Fatal(err)
	}
	if err := cliServeRunWaitForText(liveOnly.stdoutPath, "tail-live\n"); err != nil {
		t.Fatalf("tail zero live output: %v; output=%q", err, liveOnly.stdout())
	}
	if err := liveOnly.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := liveOnly.wait(5 * time.Second); err != nil {
		t.Fatalf("detach tail zero attach: %v; output=%q", err, liveOnly.stdout())
	}
	after, err := client.Output(context.Background(), daemon.OutputRequest{Name: "attach-tail", Cwd: projectRoot, Tail: 10, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Entries) != len(before.Entries)+2 {
		t.Fatalf("attach tail changed retained output unexpectedly: before=%d after=%d", len(before.Entries), len(after.Entries))
	}
	for index, entry := range before.Entries {
		if after.Entries[index].Cursor != entry.Cursor || after.Entries[index].Text != entry.Text {
			t.Fatalf("attach tail changed retained entry %d: before=%+v after=%+v", index, entry, after.Entries[index])
		}
	}
	if err := cliServeRunStop(t, "attach-tail"); err != nil {
		t.Fatal(err)
	}
	if err := cliServeRunWaitForFile(marker + ".terminated"); err != nil {
		t.Fatal(err)
	}
}

func TestDetachedAndObserverLifecycleUnchanged(t *testing.T) {
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

	t.Run("attach and logs signals detach observers only", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		marker := filepath.Join(t.TempDir(), "observer")
		if _, _, err := cliServeRunInvokeForTest(cliServeRunWithFixtureArgs([]string{"run", "observer", "--detach"}, "stream", marker)...); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
			t.Fatal(err)
		}
		for _, command := range []string{"attach", "logs"} {
			for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP} {
				args := []string{command, "observer"}
				if command == "logs" {
					args = append(args, "--follow")
				}
				observer := cliServeRunStartClient(t, args...)
				if err := cliServeRunWaitForText(observer.stdoutPath, "stdout:live"); err != nil {
					t.Fatalf("%s observer startup: %v", command, err)
				}
				if err := observer.cmd.Process.Signal(sig); err != nil {
					t.Fatal(err)
				}
				if err := observer.wait(5 * time.Second); err != nil {
					t.Fatalf("%s detach on %v = %v", command, sig, err)
				}
				cliServeRunAssertRunning(t, daemon.NewRuntimePaths(runtimeDir), "observer")
			}
		}
		if err := cliServeRunStop(t, "observer"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("logs follow remains durable across stop and start", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		marker := filepath.Join(t.TempDir(), "durable")
		args := cliServeRunWithFixtureArgs([]string{"run", "durable", "--detach"}, "stream", marker)
		if _, _, err := cliServeRunInvokeForTest(args...); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
			t.Fatal(err)
		}
		follower := cliServeRunStartClient(t, "logs", "durable", "--follow")
		if err := cliServeRunWaitForText(follower.stdoutPath, "stdout:live"); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunStop(t, "durable"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := cliServeRunInvokeForTest("start", "durable", "--no-wait"); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunWaitForCondition(func() bool {
			return strings.Count(follower.stdout(), "stdout:live") >= 2
		}); err != nil {
			t.Fatalf("durable follower did not cross restart: %v; stdout=%q stderr=%q", err, follower.stdout(), follower.stderr())
		}
		if err := follower.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		if err := follower.wait(5 * time.Second); err != nil {
			t.Fatal(err)
		}
		if err := cliServeRunStop(t, "durable"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("up and start hand ownership to daemon", func(t *testing.T) {
		runtimeDir := cliServeRunRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		cliServeRunStartDaemon(t, runtimeDir)
		root := t.TempDir()
		manifest := "version: 1\nprocesses:\n  started:\n    argv: [/bin/sh, -c, 'sleep 30']\n  upped:\n    argv: [/bin/sh, -c, 'sleep 30']\n"
		if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		oldwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(root); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chdir(oldwd) }()
		canonicalRoot, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := cliServeRunInvokeForTest("start", "started", "--no-wait"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := cliServeRunInvokeForTest("up", "--no-wait"); err != nil {
			t.Fatal(err)
		}
		probe, err := daemon.Dial(context.Background(), daemon.NewRuntimePaths(runtimeDir).Socket)
		if err != nil {
			t.Fatal(err)
		}
		defer probe.Close()
		for _, name := range []string{"started", "upped"} {
			process, getErr := probe.Get(context.Background(), daemon.GetRequest{Name: name, Cwd: canonicalRoot})
			if getErr != nil || process.State != app.StateRunning {
				t.Fatalf("%s after launcher exit = %+v err %v", name, process, getErr)
			}
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

type cliServeRunPTYProcess struct {
	cmd       *exec.Cmd
	master    *os.File
	finished  chan struct{}
	mu        sync.Mutex
	buffer    bytes.Buffer
	hasExited bool
	waitErr   error
}

func cliServeRunStartPTYClient(t *testing.T, args ...string) *cliServeRunPTYProcess {
	t.Helper()
	argv := []string{cliServeRunChildFlag, cliServeRunHelperMarker, "client"}
	argv = append(argv, args...)
	command := exec.Command(os.Args[0], argv...)
	command.Env = append([]string(nil), os.Environ()...)
	master, err := pty.Start(command)
	if err != nil {
		t.Fatal(err)
	}
	process := &cliServeRunPTYProcess{cmd: command, master: master, finished: make(chan struct{})}
	go func() {
		buffer := make([]byte, 4096)
		for {
			count, readErr := master.Read(buffer)
			if count > 0 {
				process.mu.Lock()
				_, _ = process.buffer.Write(buffer[:count])
				process.mu.Unlock()
			}
			if readErr != nil {
				return
			}
		}
	}()
	go func() {
		err := command.Wait()
		process.mu.Lock()
		process.waitErr = err
		process.hasExited = true
		process.mu.Unlock()
		close(process.finished)
	}()
	t.Cleanup(func() {
		if !process.exited() {
			_ = command.Process.Kill()
			_ = process.wait(2 * time.Second)
		}
		_ = master.Close()
	})
	return process
}

func (p *cliServeRunPTYProcess) outputString() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buffer.String()
}

func (p *cliServeRunPTYProcess) output() string { return p.outputString() }

func (p *cliServeRunPTYProcess) waitForText(want string) error {
	return cliServeRunWaitForTextIn(p.outputString, want)
}

func (p *cliServeRunPTYProcess) wait(timeout time.Duration) error {
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

func (p *cliServeRunPTYProcess) exited() bool {
	p.mu.Lock()
	exited := p.hasExited
	p.mu.Unlock()
	return exited
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
		StopGrace:  2 * time.Second,
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

func cliServeRunWaitForTextIn(text func() string, want string) error {
	return cliServeRunWaitForCondition(func() bool {
		return strings.Contains(text(), want)
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
