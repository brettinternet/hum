//go:build windows

package process

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"hum/internal/output"

	"golang.org/x/sys/windows"
)

const (
	windowsHelperEnv   = "HUM_PROCESS_WINDOWS_HELPER"
	windowsHelperMode  = "HUM_PROCESS_WINDOWS_MODE"
	windowsHelperReady = "HUM_PROCESS_WINDOWS_READY_FILE"
)

func TestWindowsProcessHelper(t *testing.T) {
	if os.Getenv(windowsHelperEnv) != "1" {
		return
	}
	switch os.Getenv(windowsHelperMode) {
	case "exit":
		fmt.Fprintln(os.Stdout, "windows-exit-marker")
		os.Exit(23)
	case "argv":
		encoded, err := json.Marshal(os.Args)
		if err != nil {
			os.Exit(2)
		}
		fmt.Fprintf(os.Stdout, "windows-argv:%s\n", encoded)
	case "block":
		for {
			time.Sleep(time.Hour)
		}
	case "tty-interactive":
		runWindowsTTYInteractiveHelper()
	case "tty-block":
		if ready := os.Getenv(windowsHelperReady); ready != "" {
			if err := os.WriteFile(ready, []byte(fmt.Sprint(os.Getpid())), 0600); err != nil {
				os.Exit(2)
			}
		}
		fmt.Fprintln(os.Stdout, "windows-tty-block-ready")
		for {
			time.Sleep(time.Hour)
		}
	case "tree-parent":
		executable, err := os.Executable()
		if err != nil {
			os.Exit(2)
		}
		cmd := exec.Command(executable, "-test.run=TestWindowsProcessHelper", "--")
		cmd.Env = windowsHelperEnvironment("tree-child")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Fprintf(os.Stdout, "windows-descendant-ready=%d\n", cmd.Process.Pid)
		os.Exit(17)
	case "tree-child":
		for {
			time.Sleep(time.Hour)
		}
	default:
		os.Exit(2)
	}
}

func runWindowsTTYInteractiveHelper() {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	initialColumns, initialRows, err := windowsTTYSize()
	if err != nil {
		os.Exit(2)
	}
	fmt.Fprintf(os.Stdout, "windows-tty-ready=%dx%d\n", initialColumns, initialRows)
	fmt.Fprintln(os.Stderr, "windows-tty-stderr-marker")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		os.Exit(3)
	}
	fmt.Fprintf(os.Stdout, "windows-tty-input=%q\n", line)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		columns, rows, sizeErr := windowsTTYSize()
		if sizeErr == nil && (columns != initialColumns || rows != initialRows) {
			fmt.Fprintf(os.Stdout, "windows-tty-resized=%dx%d\n", columns, rows)
			select {
			case <-interrupts:
				fmt.Fprintln(os.Stdout, "windows-tty-ctrl-c-received")
			case <-time.After(10 * time.Second):
				os.Exit(5)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Fprintln(os.Stdout, "windows-tty-resize-timeout")
	os.Exit(4)
}

func windowsTTYSize() (int16, int16, error) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(os.Stdout.Fd()), &info); err != nil {
		return 0, 0, err
	}
	return info.Size.X, info.Size.Y, nil
}

func TestWindowsStartCapturesExitAndExactArgv(t *testing.T) {
	t.Run("exit", func(t *testing.T) {
		store := windowsNewStore(t)
		child, err := Start(windowsHelperSpec(store, "exit"))
		if err != nil {
			t.Fatalf("start helper: %v", err)
		}
		result := child.Wait()
		if result.Err != nil || result.ExitCode != 23 || result.Signal != nil {
			t.Fatalf("result = %+v, want exit code 23 and no signal", result)
		}
		if got := windowsStoreText(t, store); !strings.Contains(got, "windows-exit-marker") {
			t.Fatalf("captured output = %q, missing exit marker", got)
		}
	})

	t.Run("argv", func(t *testing.T) {
		store := windowsNewStore(t)
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		args := []string{executable, "-test.run=TestWindowsProcessHelper", "--", `space and "quote"`, `backslash\\tail`, "&|<>^", "雪"}
		child, err := Start(Spec{
			Argv:         args,
			Env:          windowsHelperEnvironment("argv"),
			Output:       store,
			MaxLineBytes: 4096,
		})
		if err != nil {
			t.Fatalf("start argv helper: %v", err)
		}
		result := child.Wait()
		if result.Err != nil || result.ExitCode != 0 {
			t.Fatalf("result = %+v", result)
		}
		want, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		if got := windowsStoreText(t, store); !strings.Contains(got, "windows-argv:"+string(want)) {
			t.Fatalf("captured argv = %q, want %s", got, want)
		}
	})
}

func TestWindowsStopTerminatesDescendantsAfterLeaderExit(t *testing.T) {
	store := windowsNewStore(t)
	child, err := Start(windowsHelperSpec(store, "tree-parent"))
	if err != nil {
		t.Fatalf("start tree helper: %v", err)
	}
	defer func() {
		select {
		case <-child.Done():
		default:
			if err := child.Stop(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Errorf("cleanup stop: %v", err)
			}
			select {
			case <-child.Done():
			case <-time.After(10 * time.Second):
				t.Error("cleanup timed out waiting for child")
			}
		}
	}()
	select {
	case <-child.LeaderDone():
	case <-time.After(10 * time.Second):
		t.Fatal("tree leader did not exit")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !child.HasSurvivingDescendants() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !child.HasSurvivingDescendants() {
		t.Fatalf("job did not retain descendant; output=%q", windowsStoreText(t, store))
	}
	if err := child.Stop(); err != nil {
		t.Fatalf("stop owned descendant tree after leader exit: %v", err)
	}
	result := child.Wait()
	if result.Err != nil || result.ExitCode != 17 {
		t.Fatalf("result = %+v, want leader exit code 17", result)
	}
	text := windowsStoreText(t, store)
	var descendantPID int
	if _, err := fmt.Sscanf(text, "windows-descendant-ready=%d", &descendantPID); err != nil || descendantPID <= 0 {
		t.Fatalf("descendant PID not captured: %q (err %v)", text, err)
	}
	if ProcessGroupAlive(descendantPID) {
		t.Fatalf("descendant %d remained alive after Stop", descendantPID)
	}
}

func TestWindowsStopFailsClosedOnIdentityOrOwnershipMismatch(t *testing.T) {
	t.Run("creation identity", func(t *testing.T) {
		child, err := Start(windowsHelperSpec(windowsNewStore(t), "block"))
		if err != nil {
			t.Fatal(err)
		}
		original := child.startIdentity
		t.Cleanup(func() {
			child.startIdentity = original
			_ = child.Stop()
			<-child.Done()
		})
		child.startIdentity = "windows-filetime:wrong"
		if err := child.Stop(); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
			t.Fatalf("Stop with mismatched identity = %v, want fail-closed identity error", err)
		}
		if !ProcessGroupAlive(child.PID()) {
			t.Fatal("identity mismatch stop terminated the child")
		}
		child.startIdentity = original
		if err := child.Stop(); err != nil {
			t.Fatalf("stop after restoring identity: %v", err)
		}
		if result := child.Wait(); result.Err != nil {
			t.Fatalf("wait after stop: %+v", result)
		}
	})

	t.Run("exited root with foreign live job", func(t *testing.T) {
		child, err := Start(windowsHelperSpec(windowsNewStore(t), "tree-parent"))
		if err != nil {
			t.Fatal(err)
		}
		foreign, err := Start(windowsHelperSpec(windowsNewStore(t), "block"))
		if err != nil {
			_ = child.Stop()
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = child.Stop()
			_ = foreign.Stop()
			<-child.Done()
			<-foreign.Done()
		})
		select {
		case <-child.LeaderDone():
		case <-time.After(10 * time.Second):
			t.Fatal("tree leader did not exit")
		}
		child.mu.Lock()
		original := child.jobHandle
		child.jobHandle = foreign.jobHandle
		child.mu.Unlock()
		err = child.Stop()
		child.mu.Lock()
		child.jobHandle = original
		child.mu.Unlock()
		if err == nil || !strings.Contains(err.Error(), "ownership mismatch") {
			t.Fatalf("stop against foreign populated job = %v", err)
		}
		if !ProcessGroupAlive(foreign.PID()) || !child.HasSurvivingDescendants() {
			t.Fatal("mismatch terminated foreign job or original descendants")
		}
	})

	t.Run("job ownership", func(t *testing.T) {
		child, err := Start(windowsHelperSpec(windowsNewStore(t), "block"))
		if err != nil {
			t.Fatal(err)
		}
		wrongJob, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			_ = child.Stop()
			t.Fatal(err)
		}
		ownedJob := child.jobHandle
		t.Cleanup(func() {
			child.jobHandle = ownedJob
			_ = windows.CloseHandle(wrongJob)
			_ = child.Stop()
			<-child.Done()
		})
		child.jobHandle = wrongJob
		if err := child.Stop(); err == nil || !strings.Contains(err.Error(), "ownership mismatch") {
			t.Fatalf("Stop with mismatched job = %v, want fail-closed ownership error", err)
		}
		if !ProcessGroupAlive(child.PID()) {
			t.Fatal("ownership mismatch stop terminated the child")
		}
		child.jobHandle = ownedJob
		_ = windows.CloseHandle(wrongJob)
		if err := child.Stop(); err != nil {
			t.Fatalf("stop after restoring job ownership: %v", err)
		}
		if result := child.Wait(); result.Err != nil {
			t.Fatalf("wait after stop: %+v", result)
		}
	})
}

func TestWindowsTTYAndUnixSignals(t *testing.T) {
	store := windowsNewStore(t)
	spec := windowsHelperSpec(store, "tty-block")
	spec.TTY = true
	child, err := Start(spec)
	if err != nil {
		t.Fatalf("Start with TTY: %v", err)
	}
	if !child.IsTTY() {
		t.Fatal("child IsTTY() = false after TTY start")
	}
	windowsWaitForOutput(t, store, "windows-tty-block-ready")
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGKILL} {
		if err := child.Signal(sig); err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Errorf("Signal(%v) = %v, want explicit unsupported error", sig, err)
		}
	}
	if err := child.Stop(); err != nil {
		t.Fatalf("Stop tty child: %v", err)
	}
	if result := child.Wait(); result.Err != nil {
		t.Fatalf("wait after stopping tty child: %+v", result)
	}

	child, err = Start(windowsHelperSpec(windowsNewStore(t), "block"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGKILL} {
		if err := child.Signal(sig); err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Errorf("Signal(%v) = %v, want explicit unsupported error", sig, err)
		}
	}
	if err := child.Stop(); err != nil {
		t.Fatalf("Stop child: %v", err)
	}
	if result := child.Wait(); result.Err != nil {
		t.Fatalf("wait after Stop: %+v", result)
	}
}

func TestWindowsTTYInteractiveInputOutputAndResize(t *testing.T) {
	store := windowsNewStore(t)
	spec := windowsHelperSpec(store, "tty-interactive")
	spec.TTY = true
	spec.TTYSize = &TTYSize{Columns: 80, Rows: 24}
	child, err := Start(spec)
	if err != nil {
		t.Fatalf("start interactive TTY helper: %v", err)
	}
	windowsCleanupChild(t, child)
	if !child.IsTTY() {
		t.Fatal("interactive child IsTTY() = false")
	}
	windowsWaitForOutput(t, store, "windows-tty-ready=80x24")
	text := windowsWaitForOutput(t, store, "windows-tty-stderr-marker")
	if !strings.Contains(text, "windows-tty-ready=80x24") {
		t.Fatalf("merged TTY output = %q, missing stderr marker", text)
	}
	input := []byte("hello from windows\r")
	if n, err := child.WriteContext(context.Background(), input); err != nil || n != len(input) {
		t.Fatalf("WriteContext = %d, %v; want %d bytes", n, err, len(input))
	}
	text = windowsWaitForOutput(t, store, "windows-tty-input=")
	if !strings.Contains(text, "hello from windows") {
		t.Fatalf("TTY input was not received: %q", text)
	}
	if err := child.ResizeContext(context.Background(), 91, 33); err != nil {
		t.Fatalf("resize TTY: %v", err)
	}
	text = windowsWaitForOutput(t, store, "windows-tty-resized=91x33")
	if !strings.Contains(text, "windows-tty-stderr-marker") {
		t.Fatalf("merged TTY output lost stderr: %q", text)
	}
	if n, err := child.WriteContext(context.Background(), []byte{0x03}); err != nil || n != 1 {
		t.Fatalf("send Ctrl+C to console app: %d, %v", n, err)
	}
	windowsWaitForOutput(t, store, "windows-tty-ctrl-c-received")
	select {
	case <-child.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("interactive TTY child did not exit")
	}
	if result := child.Wait(); result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("interactive child result = %+v", result)
	}
}

func TestWindowsTTYStartupFailureCleansUpConsole(t *testing.T) {
	spec := windowsHelperSpec(windowsNewStore(t), "tty-block")
	spec.TTY = true
	spec.Dir = filepath.Join(t.TempDir(), "missing-working-directory")
	if _, err := Start(spec); err == nil {
		t.Fatal("Start with missing working directory succeeded")
	}
	// A valid path containing invalid PE bytes fails CreateProcess only after
	// CreatePseudoConsole has allocated its pipes and console handle.
	invalid := filepath.Join(t.TempDir(), "invalid.EXE")
	if err := os.WriteFile(invalid, []byte("not a PE executable"), 0600); err != nil {
		t.Fatal(err)
	}
	spec.Argv[0] = invalid
	spec.Dir = t.TempDir()
	before := windowsTTYHandleCount(t)
	for range 10 {
		if _, err := Start(spec); err == nil {
			t.Fatal("invalid PE started under ConPTY")
		}
	}
	if after := windowsTTYHandleCount(t); after > before+2 {
		t.Fatalf("ConPTY startup leaked handles: before=%d after=%d", before, after)
	}
}

func windowsTTYHandleCount(t *testing.T) uint32 {
	t.Helper()
	var count uint32
	result, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount").Call(
		uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&count)),
	)
	if result == 0 {
		t.Fatalf("GetProcessHandleCount: %v", err)
	}
	return count
}

func TestWindowsTTYStartedFailureCleansUpChild(t *testing.T) {
	readyPath := filepath.Join(t.TempDir(), "tty-child.pid")
	spec := windowsHelperSpec(windowsNewStore(t), "tty-block")
	spec.TTY = true
	spec.Env = append(spec.Env, windowsHelperReady+"="+readyPath)
	callbackErr := errors.New("reject started TTY child")
	spec.Started = func() error {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(readyPath); err == nil {
				return callbackErr
			}
			time.Sleep(10 * time.Millisecond)
		}
		return errors.New("TTY child did not write its PID")
	}
	if _, err := Start(spec); !errors.Is(err, callbackErr) {
		t.Fatalf("Start error = %v, want Started callback error", err)
	}
	pidBytes, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatalf("read failed child PID: %v", err)
	}
	var pid int
	if _, err := fmt.Sscan(string(pidBytes), &pid); err != nil || pid <= 0 {
		t.Fatalf("failed child PID = %q, err %v", pidBytes, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for ProcessGroupAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if ProcessGroupAlive(pid) {
		t.Fatalf("child %d remained alive after Started failure", pid)
	}
}

func TestWindowsTTYWriteContextCancellation(t *testing.T) {
	store := windowsNewStore(t)
	spec := windowsHelperSpec(store, "tty-block")
	spec.TTY = true
	child, err := Start(spec)
	if err != nil {
		t.Fatalf("start TTY helper: %v", err)
	}
	windowsCleanupChild(t, child)
	windowsWaitForOutput(t, store, "windows-tty-block-ready")

	ctx, cancel := context.WithCancel(context.Background())
	writeResult := make(chan error, 1)
	go func() {
		_, writeErr := child.WriteContext(ctx, bytes.Repeat([]byte{'x'}, 64<<20))
		writeResult <- writeErr
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-writeResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("WriteContext after cancellation = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled TTY write did not return")
	}
	windowsStopAndWait(t, child)
}

func TestWindowsTTYStopInterruptsWrite(t *testing.T) {
	store := windowsNewStore(t)
	spec := windowsHelperSpec(store, "tty-block")
	spec.TTY = true
	child, err := Start(spec)
	if err != nil {
		t.Fatalf("start TTY helper: %v", err)
	}
	windowsCleanupChild(t, child)
	windowsWaitForOutput(t, store, "windows-tty-block-ready")

	writeResult := make(chan error, 1)
	go func() {
		_, writeErr := child.WriteContext(context.Background(), bytes.Repeat([]byte{'x'}, 64<<20))
		writeResult <- writeErr
	}()
	time.Sleep(100 * time.Millisecond)
	if err := child.Stop(); err != nil {
		t.Fatalf("stop child while writing: %v", err)
	}
	select {
	case err := <-writeResult:
		if !errors.Is(err, os.ErrProcessDone) {
			t.Fatalf("WriteContext after Stop = %v, want os.ErrProcessDone", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TTY write did not return after Stop")
	}
	if result := child.Wait(); result.Err != nil {
		t.Fatalf("wait after Stop: %+v", result)
	}
}

func TestWindowsResolveUsesSuppliedPATHAndPATHEXT(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	installed := filepath.Join(binDir, "hum-process-path-helper.EXE")
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, binary, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("PATHEXT", ".CMD")
	resolved, err := ResolveExecutable("hum-process-path-helper", []string{"path=" + binDir, "pathext=.exe"}, "")
	if err != nil {
		t.Fatalf("resolve with supplied PATH/PATHEXT: %v", err)
	}
	if !strings.EqualFold(resolved, installed) {
		t.Fatalf("resolved path = %q, want %q", resolved, installed)
	}
	if _, err := ResolveExecutable("hum-process-path-helper", []string{"PATHEXT=.EXE"}, ""); err == nil {
		t.Fatal("ResolveExecutable used ambient PATH")
	}

	if err := os.WriteFile(filepath.Join(binDir, "hum-process-path-helper"), []byte("not an executable"), 0600); err != nil {
		t.Fatal(err)
	}
	store := windowsNewStore(t)
	child, err := Start(Spec{
		Argv:         []string{"hum-process-path-helper", "-test.run=TestWindowsProcessHelper", "--"},
		Env:          windowsHelperEnvironment("argv", "PATH="+binDir, "PATHEXT=.EXE"),
		Output:       store,
		MaxLineBytes: 1024,
	})
	if err != nil {
		t.Fatalf("Start via supplied PATH/PATHEXT: %v", err)
	}
	if result := child.Wait(); result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("result = %+v", result)
	}
	if got := windowsStoreText(t, store); !strings.Contains(got, `windows-argv:["hum-process-path-helper"`) {
		t.Fatalf("PATH helper did not receive argv[0] unchanged: %q", got)
	}
}

func windowsHelperSpec(store *output.Store, mode string) Spec {
	executable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	return Spec{
		Argv:         []string{executable, "-test.run=TestWindowsProcessHelper", "--"},
		Env:          windowsHelperEnvironment(mode),
		Output:       store,
		MaxLineBytes: 4096,
	}
}

func windowsHelperEnvironment(mode string, extra ...string) []string {
	env := []string{windowsHelperEnv + "=1", windowsHelperMode + "=" + mode}
	return append(env, extra...)
}

func windowsWaitForOutput(t *testing.T, store *output.Store, want string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		text := windowsStoreText(t, store)
		if strings.Contains(text, want) {
			return text
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for output %q; got %q", want, windowsStoreText(t, store))
	return ""
}

func windowsCleanupChild(t *testing.T, child *Child) {
	t.Helper()
	t.Cleanup(func() {
		select {
		case <-child.Done():
			return
		default:
		}
		windowsStopAndWait(t, child)
	})
}

func windowsStopAndWait(t *testing.T, child *Child) {
	t.Helper()
	if err := child.Stop(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("stop child: %v", err)
	}
	select {
	case <-child.Done():
	case <-time.After(10 * time.Second):
		t.Error("timed out waiting for stopped child")
	}
}

func windowsNewStore(t *testing.T) *output.Store {
	t.Helper()
	store, err := output.NewStore(output.Limits{RetainedBytes: 1 << 20})
	if err != nil {
		t.Fatalf("create output store: %v", err)
	}
	return store
}

func windowsStoreText(t *testing.T, store *output.Store) string {
	t.Helper()
	entries, err := store.Read(output.ReadOptions{MaxEntries: 100, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var text strings.Builder
	for _, entry := range entries.Entries {
		text.WriteString(entry.Text)
	}
	return text.String()
}

func TestWindowsProcessStartIdentityAndDone(t *testing.T) {
	identity, err := ProcessStartIdentity(os.Getpid())
	if err != nil || identity == "" {
		t.Fatalf("current process identity = %q, err %v", identity, err)
	}
	if _, err := ProcessStartIdentity(0); err == nil {
		t.Fatal("invalid PID returned an identity")
	}
	if ProcessGroupAlive(0) {
		t.Fatal("invalid PID reported alive")
	}

	child, err := Start(windowsHelperSpec(windowsNewStore(t), "exit"))
	if err != nil {
		t.Fatal(err)
	}
	result := child.Wait()
	if result.ExitCode != 23 || result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if child.StartIdentity() == "" || !errors.Is(child.Stop(), os.ErrProcessDone) {
		t.Fatalf("completed child did not preserve terminal identity: identity=%q", child.StartIdentity())
	}
}
