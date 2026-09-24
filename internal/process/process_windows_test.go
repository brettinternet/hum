//go:build windows

package process

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"hum/internal/output"

	"golang.org/x/sys/windows"
)

const (
	windowsHelperEnv  = "HUM_PROCESS_WINDOWS_HELPER"
	windowsHelperMode = "HUM_PROCESS_WINDOWS_MODE"
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
			_ = child.Stop()
			<-child.Done()
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
		if err := child.Stop(); err == nil || !strings.Contains(err.Error(), "not a member") {
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

func TestWindowsRejectsTTYAndUnixSignals(t *testing.T) {
	spec := windowsHelperSpec(windowsNewStore(t), "block")
	spec.TTY = true
	if _, err := Start(spec); err == nil || !strings.Contains(err.Error(), "tty mode is unsupported") {
		t.Fatalf("Start with TTY = %v, want explicit unsupported error", err)
	}

	child, err := Start(windowsHelperSpec(windowsNewStore(t), "block"))
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
