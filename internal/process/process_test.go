package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"hum/internal/output"

	"github.com/creack/pty"
	"golang.org/x/term"
)

const (
	helperEnv            = "HUM_PROCESS_HELPER"
	helperMode           = "HUM_PROCESS_MODE"
	helperReady          = "HUM_PROCESS_READY_FILE"
	orphanLeaderExitCode = 17
	bufferedOutputLines  = 4096
)

// TestMain runs helper modes as standalone processes. Keeping the helper
// outside testing.M prevents the test harness from appending its PASS banner
// to the redirected child output.
func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		runProcessHelper()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestProcessHelper is also the executable used by the behavior tests. It is
// inert during the package's normal test run and only performs work when the
// parent explicitly supplies helperEnv.
func TestProcessHelper(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	runProcessHelper()
}

func runProcessHelper() {
	switch os.Getenv(helperMode) {
	case "literal":
		for i, arg := range os.Args {
			if arg == "process-literal" && i+1 < len(os.Args) {
				fmt.Fprint(os.Stdout, os.Args[i+1])
				return
			}
		}
		os.Exit(2)
	case "stdin-eof":
		if _, err := io.ReadAll(os.Stdin); err != nil {
			os.Exit(2)
		}
		fmt.Fprint(os.Stdout, "stdin-eof")
	case "cwd-env":
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprint(os.Stdout, "cwd-error")
			os.Exit(2)
		}
		fmt.Fprintf(os.Stdout, "cwd=%s\nchild=%s\nparent=%s\n", cwd, os.Getenv("HUM_PROCESS_CHILD_ONLY"), os.Getenv("HUM_PROCESS_PARENT_ONLY"))
	case "argv0":
		fmt.Fprint(os.Stdout, os.Args[0])
	case "streams":
		fmt.Fprint(os.Stdout, "stdout-tail")
		fmt.Fprint(os.Stderr, "stderr-tail")
	case "tty-session":
		runTTYSessionHelper()
	case "tty-block":
		runTTYBlockHelper()
	case "tty-group":
		runTTYGroupHelper()
	case "idle-fragment":
		fmt.Fprint(os.Stdout, "idle-fragment")
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		<-signals
		signal.Stop(signals)
	case "exit":
		os.Exit(17)
	case "group-parent":
		runGroupParent()
	case "group-child":
		runGroupChild()
	case "group-orphan-parent":
		runGroupOrphanParent()
	case "group-orphan-child":
		runGroupOrphanChild()
	case "group-escaped-parent":
		runGroupEscapedParent()
	case "group-escaped-child":
		runGroupEscapedChild()
	case "buffered-output":
		runBufferedOutput()
	case "too-large":
		fmt.Fprint(os.Stdout, "this-entry-is-too-large")
	default:
		os.Exit(2)
	}
}

func runTTYBlockHelper() {
	fmt.Fprint(os.Stdout, "block-ready\n")
	signal.Ignore(syscall.SIGTERM, syscall.SIGINT)
	for {
		time.Sleep(time.Hour)
	}
}

func runTTYGroupHelper() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)
	cmd := exec.Command(helperBinary(), "-test.run=TestProcessHelper", "group-child")
	cmd.Env = []string{helperEnv + "=1", helperMode + "=group-child"}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		os.Exit(2)
	}
	if ready := os.Getenv(helperReady); ready != "" {
		if err := os.WriteFile(ready, []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			os.Exit(2)
		}
	}
	fmt.Fprint(os.Stdout, "tty-group-ready\n")
	<-signals
	_ = cmd.Wait()
}

func runTTYSessionHelper() {
	isatty := term.IsTerminal(int(os.Stdin.Fd()))
	devTTY := false
	if tty, err := os.Open("/dev/tty"); err == nil {
		devTTY = true
		_ = tty.Close()
	}
	pgid, _ := syscall.Getpgid(os.Getpid())
	rows, cols, _ := pty.Getsize(os.Stdin)
	fmt.Fprintf(os.Stdout, "tty=%t devtty=%t pid=%d pgid=%d size=%dx%d\n", isatty, devTTY, os.Getpid(), pgid, cols, rows)
	fmt.Fprint(os.Stderr, "stderr-marker\n")
	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		os.Exit(2)
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), state) }()
	data := make([]byte, 3)
	if _, err := io.ReadFull(os.Stdin, data); err != nil {
		os.Exit(3)
	}
	fmt.Fprintf(os.Stdout, "bytes=%s\n", string(data))
	sizes := make(chan os.Signal, 1)
	signal.Notify(sizes, syscall.SIGWINCH)
	defer signal.Stop(sizes)
	fmt.Fprint(os.Stdout, "resize-ready\n")
	select {
	case <-sizes:
		rows, cols, _ = pty.Getsize(os.Stdin)
		fmt.Fprintf(os.Stdout, "resized=%dx%d\n", cols, rows)
	case <-time.After(2 * time.Second):
		fmt.Fprint(os.Stdout, "resized=timeout\n")
	}
}

func runGroupParent() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	cmd := exec.Command(helperBinary(), "-test.run=TestProcessHelper", "process-group-child")
	cmd.Env = []string{helperEnv + "=1", helperMode + "=group-child", helperReady + "=" + os.Getenv(helperReady)}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		os.Exit(2)
	}
	ready := os.Getenv(helperReady)
	if ready != "" {
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(2)
			}
			time.Sleep(time.Millisecond)
		}
	}
	fmt.Fprint(os.Stdout, "group-ready\n")
	<-signals
	if err := cmd.Wait(); err != nil {
		os.Exit(3)
	}
}

func runGroupChild() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	if ready := os.Getenv(helperReady); ready != "" {
		if err := os.WriteFile(ready, []byte("ready"), 0600); err != nil {
			os.Exit(2)
		}
	}
	<-signals
	fmt.Fprint(os.Stdout, "descendant-received-sigint\n")
}

func runGroupOrphanParent() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)

	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		os.Exit(2)
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		_ = readyReader.Close()
		_ = readyWriter.Close()
		os.Exit(2)
	}
	parentPGID, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		_ = devNull.Close()
		_ = readyReader.Close()
		_ = readyWriter.Close()
		os.Exit(2)
	}

	cmd := exec.Command(helperBinary(), "-test.run=TestProcessHelper", "process-group-orphan-child")
	cmd.Env = []string{helperEnv + "=1", helperMode + "=group-orphan-child"}
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: parentPGID}
	cmd.ExtraFiles = []*os.File{readyWriter}
	if err := cmd.Start(); err != nil {
		_ = devNull.Close()
		_ = readyReader.Close()
		_ = readyWriter.Close()
		os.Exit(2)
	}
	_ = devNull.Close()
	_ = readyWriter.Close()

	ready, err := io.ReadAll(readyReader)
	_ = readyReader.Close()
	if err != nil || len(ready) == 0 {
		os.Exit(2)
	}
	fmt.Fprintf(os.Stdout, "orphan-ready %s", ready)
	<-signals
	os.Exit(orphanLeaderExitCode)
}

func runGroupOrphanChild() {
	signal.Ignore(os.Interrupt, syscall.SIGTERM)
	ready := os.NewFile(3, "orphan-ready")
	if ready == nil {
		os.Exit(2)
	}
	pgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		_ = ready.Close()
		os.Exit(2)
	}
	if _, err := fmt.Fprintf(ready, "pid=%d pgid=%d\n", os.Getpid(), pgid); err != nil {
		_ = ready.Close()
		os.Exit(2)
	}
	_ = ready.Close()
	for {
		time.Sleep(time.Hour)
	}
}

func runGroupEscapedParent() {
	ready := os.Getenv(helperReady)
	cmd := exec.Command(helperBinary(), "-test.run=TestProcessHelper", "process-group-escaped-child")
	cmd.Env = []string{
		helperEnv + "=1",
		helperMode + "=group-escaped-child",
		helperReady + "=" + ready,
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		os.Exit(2)
	}
	if ready != "" {
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(2)
			}
			time.Sleep(time.Millisecond)
		}
	}
	fmt.Fprint(os.Stdout, "escaped-ready\n")
	os.Exit(0)
}

func runGroupEscapedChild() {
	if _, err := syscall.Setsid(); err != nil {
		os.Exit(2)
	}
	pgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		os.Exit(2)
	}
	if ready := os.Getenv(helperReady); ready != "" {
		if err := os.WriteFile(ready, []byte(fmt.Sprintf("pid=%d pgid=%d\n", os.Getpid(), pgid)), 0600); err != nil {
			os.Exit(2)
		}
	}
	signal.Ignore(os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGPIPE)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		fmt.Fprint(os.Stdout, "escaped-holder-output\n")
	}
}

func runBufferedOutput() {
	for i := range bufferedOutputLines {
		fmt.Fprintf(os.Stdout, "buffered-line-%04d\n", i)
	}
}

func helperBinary() string {
	path, err := filepath.Abs(os.Args[0])
	if err != nil {
		return os.Args[0]
	}
	return path
}

func installHelper(t *testing.T, path string) {
	t.Helper()
	binary, err := os.ReadFile(helperBinary())
	if err != nil {
		t.Fatalf("read helper binary: %v", err)
	}
	if err := os.WriteFile(path, binary, 0755); err != nil {
		t.Fatalf("write helper binary %q: %v", path, err)
	}
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatalf("chmod helper binary %q: %v", path, err)
	}
}

func newStore(t *testing.T) *output.Store {
	t.Helper()
	store, err := output.NewStore(output.Limits{RetainedBytes: 1 << 20})
	if err != nil {
		t.Fatalf("create output store: %v", err)
	}
	return store
}

func helperSpec(store *output.Store, mode string, argv ...string) Spec {
	args := []string{helperBinary(), "-test.run=TestProcessHelper"}
	args = append(args, argv...)
	return Spec{
		Argv:         args,
		Env:          []string{helperEnv + "=1", helperMode + "=" + mode},
		Output:       store,
		MaxLineBytes: 1024,
	}
}

func entries(t *testing.T, store *output.Store) []output.Entry {
	t.Helper()
	result, err := store.Read(output.ReadOptions{MaxEntries: 100, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return result.Entries
}

func TestTTYProcess(t *testing.T) {
	store := newStore(t)
	subscription := store.Subscribe(output.ReadOptions{})
	spec := helperSpec(store, "tty-session")
	spec.TTY = true
	spec.TTYSize = &TTYSize{Columns: 80, Rows: 24}
	child, err := Start(spec)
	if err != nil {
		t.Fatalf("start tty child: %v", err)
	}
	if !child.IsTTY() || child.PID() != child.PGID() {
		t.Fatalf("tty child pid/pgid = %d/%d", child.PID(), child.PGID())
	}
	next := func() (output.Event, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		return subscription.Next(ctx)
	}
	for {
		event, nextErr := next()
		if nextErr != nil {
			t.Fatalf("tty initial subscription: %v", nextErr)
		}
		if event.Read != nil {
			var initial string
			for _, entry := range event.Read.Entries {
				initial += entry.Text
			}
			if strings.Contains(initial, "tty=true") {
				break
			}
		}
	}
	if _, err := child.Write([]byte{0, 0xff, 'x'}); err != nil {
		t.Fatalf("write tty bytes: %v", err)
	}
	bytesSeen, resizeReady := false, false
	for {
		event, nextErr := next()
		if nextErr != nil {
			t.Fatalf("tty bytes subscription: %v", nextErr)
		}
		if event.Read != nil {
			var outputText string
			for _, entry := range event.Read.Entries {
				outputText += entry.Text
			}
			bytesSeen = bytesSeen || strings.Contains(outputText, "bytes=")
			resizeReady = resizeReady || strings.Contains(outputText, "resize-ready")
			if bytesSeen && resizeReady {
				break
			}
		}
	}
	if err := child.Resize(120, 40); err != nil {
		t.Fatalf("resize tty child: %v", err)
	}
	if err := child.Signal(syscall.SIGWINCH); err != nil {
		t.Fatalf("signal tty resize: %v", err)
	}
	result := child.Wait()
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("tty result = %+v", result)
	}
	child.mu.Lock()
	master := child.ttyMaster
	child.mu.Unlock()
	if master != nil {
		t.Fatal("TTY master remained open after child completion")
	}
	var text string
	for _, entry := range entries(t, store) {
		if entry.Stream != output.Stdout {
			t.Fatalf("tty entry stream = %v, want stdout", entry.Stream)
		}
		text += entry.Text
	}
	for _, want := range []string{"tty=true", "devtty=true", "pgid=", "size=80x24", "stderr-marker", "bytes=", "resized=120x40"} {
		if !strings.Contains(text, want) {
			t.Fatalf("tty output %q missing %q", text, want)
		}
	}
	if !strings.Contains(text, string([]byte{0, 0xff, 'x'})) {
		t.Fatalf("tty output lost exact bytes: %q", text)
	}
	for {
		event, nextErr := next()
		if nextErr != nil {
			t.Fatalf("tty subscription: %v", nextErr)
		}
		if event.Exit != nil {
			break
		}
	}
}

func TestTTYProcessGroupCleanup(t *testing.T) {
	store := newStore(t)
	subscription := store.Subscribe(output.ReadOptions{})
	readyPath := filepath.Join(t.TempDir(), "tty-group-ready")
	spec := helperSpec(store, "tty-group")
	spec.TTY = true
	spec.Env = append(spec.Env, helperReady+"="+readyPath)
	child, err := Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		event, nextErr := subscription.Next(ctx)
		if nextErr != nil {
			t.Fatalf("tty group readiness: %v", nextErr)
		}
		if event.Read != nil {
			for _, entry := range event.Read.Entries {
				if strings.Contains(entry.Text, "tty-group-ready") {
					goto ready
				}
			}
		}
	}
ready:
	data, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatal(err)
	}
	descendantPID, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("descendant pid = %q: %v", data, err)
	}
	if err := child.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("terminate tty group: %v", err)
	}
	result := child.Wait()
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("tty group result = %+v", result)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if err := syscall.Kill(descendantPID, 0); errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tty descendant %d remained after group termination", descendantPID)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTTYWriteContextCancellation(t *testing.T) {
	store := newStore(t)
	subscription := store.Subscribe(output.ReadOptions{})
	spec := helperSpec(store, "tty-block")
	spec.TTY = true
	child, err := Start(spec)
	if err != nil {
		t.Fatalf("start blocked tty child: %v", err)
	}
	defer func() {
		_ = child.Signal(syscall.SIGKILL)
		<-child.Done()
	}()
	readyCtx, readyCancel := context.WithTimeout(context.Background(), time.Second)
	defer readyCancel()
	for {
		event, nextErr := subscription.Next(readyCtx)
		if nextErr != nil {
			t.Fatalf("blocked tty readiness: %v", nextErr)
		}
		if event.Read != nil && len(event.Read.Entries) != 0 {
			break
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, writeErr := child.WriteContext(ctx, bytes.Repeat([]byte{'x'}, 8*1024*1024))
		result <- writeErr
	}()
	select {
	case <-time.After(100 * time.Millisecond):
	case err := <-result:
		t.Fatalf("blocked tty write completed early: %v", err)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled tty write = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled tty write remained blocked")
	}
	if err := child.Resize(100, 40); err != nil {
		t.Fatalf("resize after canceled write: %v", err)
	}
	if err := child.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("tty child ended after canceled write: %v", err)
	}
}

func TestStartCapturesLiteralArguments(t *testing.T) {
	store := newStore(t)
	literal := `literal;$(not-a-command)|&*?`
	spec := helperSpec(store, "literal", "process-literal", literal)

	child, err := Start(spec)
	if err != nil {
		t.Fatalf("start child: %v", err)
	}
	result := child.Wait()
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	got := entries(t, store)
	if len(got) != 1 || got[0].Stream != output.Stdout || got[0].Text != literal {
		t.Fatalf("literal argv output = %#v, want one stdout entry %q", got, literal)
	}
}

func TestStartedCallbackPrecedesOutputCapture(t *testing.T) {
	store := newStore(t)
	spec := helperSpec(store, "literal", "process-literal", "child-output")
	spec.Started = func() error {
		_, err := store.Append(output.System, time.Now(), "launched\n")
		return err
	}
	child, err := Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	if result := child.Wait(); result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("result = %#v", result)
	}
	got := entries(t, store)
	if len(got) != 2 || got[0].Text != "launched\n" || got[1].Text != "child-output" {
		t.Fatalf("callback/capture order = %#v", got)
	}
}

func TestStartProvidesEOFStdin(t *testing.T) {
	store := newStore(t)
	child, err := Start(helperSpec(store, "stdin-eof"))
	if err != nil {
		t.Fatalf("start child: %v", err)
	}
	result := child.Wait()
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	got := entries(t, store)
	if len(got) != 1 || got[0].Stream != output.Stdout || got[0].Text != "stdin-eof" {
		t.Fatalf("stdin output = %#v, want one stdout entry %q", got, "stdin-eof")
	}
}

func TestStartResolvesExecutableFromSuppliedPath(t *testing.T) {
	t.Setenv("PATH", "")
	dir := t.TempDir()
	name := "hum-process-path-helper"
	executable := filepath.Join(dir, name)
	installHelper(t, executable)

	store := newStore(t)
	spec := Spec{
		Argv:         []string{name, "-test.run=TestProcessHelper"},
		Env:          []string{helperEnv + "=1", helperMode + "=argv0", "PATH=" + dir},
		Output:       store,
		MaxLineBytes: 1024,
	}
	child, err := Start(spec)
	if err != nil {
		t.Fatalf("start child: %v", err)
	}
	result := child.Wait()
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	got := entries(t, store)
	if len(got) != 1 || got[0].Stream != output.Stdout || got[0].Text != name {
		t.Fatalf("resolved argv output = %#v, want one stdout entry %q", got, name)
	}
}

func TestStartResolvesRelativeAndEmptyPathComponentsFromSpecDirectory(t *testing.T) {
	dir := t.TempDir()
	relativeDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(relativeDir, 0755); err != nil {
		t.Fatalf("create relative PATH directory: %v", err)
	}

	relativeName := "hum-process-relative-path-helper"
	installHelper(t, filepath.Join(relativeDir, relativeName))
	emptyName := "hum-process-empty-path-helper"
	installHelper(t, filepath.Join(dir, emptyName))

	tests := []struct {
		name string
		path string
	}{
		{name: relativeName, path: "bin"},
		{name: emptyName, path: "missing" + string(os.PathListSeparator)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore(t)
			child, err := Start(Spec{
				Dir:  dir,
				Argv: []string{tc.name, "-test.run=TestProcessHelper"},
				Env: []string{
					helperEnv + "=1",
					helperMode + "=argv0",
					"PATH=" + tc.path,
				},
				Output:       store,
				MaxLineBytes: 1024,
			})
			if err != nil {
				t.Fatalf("start child: %v", err)
			}
			result := child.Wait()
			if result.Err != nil || result.ExitCode != 0 {
				t.Fatalf("unexpected result: %+v", result)
			}
			got := entries(t, store)
			if len(got) != 1 || got[0].Stream != output.Stdout || got[0].Text != tc.name {
				t.Fatalf("resolved argv output = %#v, want one stdout entry %q", got, tc.name)
			}
		})
	}
}

func TestStartDoesNotSearchDaemonPathForNilEnv(t *testing.T) {
	t.Setenv("PATH", "/bin")
	store := newStore(t)
	_, err := Start(Spec{
		Argv:         []string{"true"},
		Output:       store,
		MaxLineBytes: 1024,
	})
	if err == nil {
		t.Fatal("Start unexpectedly resolved a bare executable from the daemon PATH")
	}
}

func TestStartUsesExactDirectoryAndEnvironment(t *testing.T) {
	store := newStore(t)
	t.Setenv("HUM_PROCESS_PARENT_ONLY", "must-not-leak")
	cwd := t.TempDir()
	spec := helperSpec(store, "cwd-env")
	spec.Dir = cwd
	spec.Env = append(spec.Env, "HUM_PROCESS_CHILD_ONLY=present")

	child, err := Start(spec)
	if err != nil {
		t.Fatalf("start child: %v", err)
	}
	result := child.Wait()
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	var text string
	for _, entry := range entries(t, store) {
		if entry.Stream == output.Stdout {
			text += entry.Text
		}
	}
	const cwdPrefix = "cwd="
	var reportedCwd string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, cwdPrefix) {
			reportedCwd = strings.TrimPrefix(line, cwdPrefix)
			break
		}
	}
	if reportedCwd == "" || !filepath.IsAbs(reportedCwd) {
		t.Fatalf("child reported invalid cwd %q in output %q", reportedCwd, text)
	}
	wantInfo, err := os.Stat(cwd)
	if err != nil {
		t.Fatalf("stat requested cwd %q: %v", cwd, err)
	}
	gotInfo, err := os.Stat(reportedCwd)
	if err != nil {
		t.Fatalf("stat child cwd %q: %v", reportedCwd, err)
	}
	if !os.SameFile(wantInfo, gotInfo) {
		t.Fatalf("child cwd %q is not the requested directory %q", reportedCwd, cwd)
	}
	if !strings.Contains(text, "child=present\n") {
		t.Fatalf("child environment missing from %q", text)
	}
	if !strings.Contains(text, "parent=\n") {
		t.Fatalf("parent environment leaked into %q", text)
	}
}

func TestCaptureSeparatesStreamsAndFlushesTailsBeforeExit(t *testing.T) {
	store := newStore(t)
	subscription := store.Subscribe(output.ReadOptions{})
	child, err := Start(helperSpec(store, "streams"))
	if err != nil {
		t.Fatalf("start child: %v", err)
	}
	result := child.Wait()
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	var stdout, stderr string
	for _, entry := range entries(t, store) {
		switch entry.Stream {
		case output.Stdout:
			stdout += entry.Text
		case output.Stderr:
			stderr += entry.Text
		default:
			t.Fatalf("unexpected stream %v", entry.Stream)
		}
	}
	if stdout != "stdout-tail" || stderr != "stderr-tail" {
		t.Fatalf("stream output = stdout %q stderr %q", stdout, stderr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var sawExit bool
	for !sawExit {
		event, err := subscription.Next(ctx)
		if err != nil {
			t.Fatalf("read subscription: %v", err)
		}
		if event.Exit != nil {
			sawExit = true
		}
	}
}

func TestCaptureFlushesIdleFragmentBeforeExit(t *testing.T) {
	store := newStore(t)
	subscription := store.Subscribe(output.ReadOptions{})
	child, err := Start(helperSpec(store, "idle-fragment"))
	if err != nil {
		t.Fatalf("start child: %v", err)
	}
	defer func() {
		_ = child.Signal(syscall.SIGKILL)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		event, err := subscription.Next(ctx)
		if err != nil {
			t.Fatalf("read idle fragment: %v", err)
		}
		if event.Exit != nil {
			t.Fatal("process exited before idle fragment flush")
		}
		for _, entry := range event.Read.Entries {
			if entry.Stream == output.Stdout && entry.Text == "idle-fragment" {
				if err := child.Signal(syscall.SIGTERM); err != nil {
					t.Fatalf("signal child: %v", err)
				}
				result := child.Wait()
				if result.Err != nil || result.ExitCode != 0 {
					t.Fatalf("unexpected result: %+v", result)
				}
				return
			}
		}
	}
}

func TestExitCodeProcessGroupAndRepeatSafeWait(t *testing.T) {
	store := newStore(t)
	child, err := Start(helperSpec(store, "exit"))
	if err != nil {
		t.Fatalf("start child: %v", err)
	}
	if child.PID() <= 0 || child.PGID() != child.PID() {
		t.Fatalf("pid/pgid = %d/%d, want same positive id", child.PID(), child.PGID())
	}
	if pgid, err := syscall.Getpgid(child.PID()); err != nil || pgid != child.PGID() {
		t.Fatalf("kernel pgid = %d, err %v; child pgid %d", pgid, err, child.PGID())
	}

	first := child.Wait()
	second := child.Wait()
	if first.ExitCode != 17 || first.Err != nil {
		t.Fatalf("first result = %+v, want ordinary exit 17 without error", first)
	}
	if second.ExitCode != first.ExitCode || second.Err != nil || !second.ExitedAt.Equal(first.ExitedAt) {
		t.Fatalf("repeat result = %+v, first %+v", second, first)
	}
	select {
	case <-child.Done():
	default:
		t.Fatal("Done remained open after Wait")
	}
}

func TestSignalReachesDescendantsInProcessGroup(t *testing.T) {
	store := newStore(t)
	subscription := store.Subscribe(output.ReadOptions{})
	readyPath := filepath.Join(t.TempDir(), "descendant-ready")
	spec := helperSpec(store, "group-parent")
	spec.Env = append(spec.Env, helperReady+"="+readyPath)
	child, err := Start(spec)
	if err != nil {
		t.Fatalf("start child: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		event, err := subscription.Next(ctx)
		if err != nil {
			t.Fatalf("wait for descendant readiness: %v", err)
		}
		if event.Exit != nil {
			t.Fatal("process exited before descendant became ready")
		}
		for _, entry := range event.Read.Entries {
			if strings.Contains(entry.Text, "group-ready\n") {
				goto ready
			}
		}
	}

ready:
	if err := child.Signal(os.Interrupt); err != nil {
		t.Fatalf("signal child group: %v", err)
	}
	result := child.Wait()
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("unexpected result after SIGINT: %+v", result)
	}
	var text string
	for _, entry := range entries(t, store) {
		if entry.Stream == output.Stdout {
			text += entry.Text
		}
	}
	if !strings.Contains(text, "descendant-received-sigint\n") {
		t.Fatalf("descendant did not report SIGINT: %q", text)
	}
}

func TestWaitTracksRedirectedProcessGroupDescendant(t *testing.T) {
	store := newStore(t)
	subscription := store.Subscribe(output.ReadOptions{})
	child, err := Start(helperSpec(store, "group-orphan-parent"))
	if err != nil {
		t.Fatalf("start child: %v", err)
	}
	defer func() {
		_ = child.Signal(syscall.SIGKILL)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var readyText string
	for {
		event, err := subscription.Next(ctx)
		if err != nil {
			t.Fatalf("wait for orphan readiness: %v", err)
		}
		if event.Exit != nil {
			t.Fatal("process exited before descendant was signaled")
		}
		for _, entry := range event.Read.Entries {
			const readyPrefix = "orphan-ready "
			if entry.Stream == output.Stdout && strings.HasPrefix(entry.Text, readyPrefix) {
				readyText = strings.TrimPrefix(entry.Text, readyPrefix)
				goto ready
			}
		}
	}

ready:
	var descendantPID, descendantPGID int
	if n, scanErr := fmt.Sscanf(strings.TrimSpace(readyText), "pid=%d pgid=%d", &descendantPID, &descendantPGID); n != 2 || scanErr != nil {
		t.Fatalf("invalid descendant readiness %q (assignments=%d, err=%v)", readyText, n, scanErr)
	}
	if descendantPID <= 0 || descendantPGID != child.PGID() {
		t.Fatalf("descendant pid/pgid = %d/%d, want positive pid in child pgid %d", descendantPID, descendantPGID, child.PGID())
	}
	kernelPGID, err := syscall.Getpgid(descendantPID)
	if err != nil || kernelPGID != child.PGID() {
		t.Fatalf("kernel descendant pgid = %d, err %v; child pgid %d", kernelPGID, err, child.PGID())
	}

	if err := child.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal process group with SIGTERM: %v", err)
	}
	leaderCtx, cancelLeader := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelLeader()
	select {
	case <-child.leaderDone:
	case <-leaderCtx.Done():
		t.Fatal("leader did not report its exit after SIGTERM")
	}
	if err := syscall.Kill(descendantPID, 0); err != nil {
		t.Fatalf("descendant exited after ignoring SIGTERM: %v", err)
	}
	select {
	case <-child.Done():
		t.Fatal("Done closed while redirected descendant remained alive")
	default:
	}
	if err := child.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("signal process group with SIGKILL: %v", err)
	}

	done := make(chan Result, 1)
	go func() {
		done <- child.Wait()
	}()
	select {
	case result := <-done:
		if result.Err != nil || result.ExitCode != orphanLeaderExitCode {
			t.Fatalf("unexpected result after group termination: %+v, want leader exit %d", result, orphanLeaderExitCode)
		}
		if err := syscall.Kill(descendantPID, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("descendant remained after group termination (kill probe err=%v)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not complete after SIGKILL")
	}
}

func TestWaitDoesNotFollowEscapedCaptureHolder(t *testing.T) {
	store := newStore(t)
	readyPath := filepath.Join(t.TempDir(), "escaped-ready")
	spec := helperSpec(store, "group-escaped-parent")
	spec.Env = append(spec.Env, helperReady+"="+readyPath)
	child, err := Start(spec)
	if err != nil {
		t.Fatalf("start child: %v", err)
	}

	var escapedPID int
	defer func() {
		if escapedPID > 0 {
			_ = syscall.Kill(escapedPID, syscall.SIGKILL)
		}
	}()

	readyCtx, cancelReady := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelReady()
	readyTicker := time.NewTicker(time.Millisecond)
	defer readyTicker.Stop()
	var escapedPGID int
	for escapedPID == 0 {
		if data, readErr := os.ReadFile(readyPath); readErr == nil {
			n, scanErr := fmt.Sscanf(strings.TrimSpace(string(data)), "pid=%d pgid=%d", &escapedPID, &escapedPGID)
			if n != 2 || scanErr != nil {
				escapedPID = 0
			}
		}
		if escapedPID != 0 {
			break
		}
		select {
		case <-readyCtx.Done():
			t.Fatalf("escaped descendant did not become ready: %v", readyCtx.Err())
		case <-readyTicker.C:
		}
	}
	if escapedPID <= 0 || escapedPGID <= 0 || escapedPGID == child.PGID() {
		t.Fatalf("escaped descendant pid/pgid = %d/%d, original pgid %d", escapedPID, escapedPGID, child.PGID())
	}
	if err := syscall.Kill(escapedPID, 0); err != nil {
		t.Fatalf("escaped descendant exited before lifecycle check: %v", err)
	}
	leaderCtx, cancelLeader := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelLeader()
	select {
	case <-child.leaderDone:
	case <-leaderCtx.Done():
		t.Fatal("escaped leader did not report exit after readiness")
	}
	if pgid, err := syscall.Getpgid(child.PID()); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("leader pgid after leaderDone = %d, err %v; want ESRCH", pgid, err)
	}
	groupCtx, cancelGroup := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelGroup()
	select {
	case <-child.groupGone:
	case <-groupCtx.Done():
		t.Fatal("original process group did not disappear after escaped leader exit")
	}

	waitStarted := time.Now()
	done := make(chan Result, 1)
	go func() {
		done <- child.Wait()
	}()
	var result Result
	waitCtx, cancelWait := context.WithTimeout(context.Background(), captureHardTimeout+2*captureDrainTimeout)
	defer cancelWait()
	select {
	case result = <-done:
	case <-waitCtx.Done():
		t.Fatal("Wait remained blocked by escaped capture holder")
	}
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("unexpected result after escaped group exit: %+v", result)
	}
	if elapsed := time.Since(waitStarted); elapsed > captureHardTimeout+2*captureDrainTimeout {
		t.Fatalf("Wait took %v with escaped continuous writer, hard bound %v", elapsed, captureHardTimeout)
	}
	if err := child.Signal(syscall.SIGTERM); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("Signal after group exit = %v, want %v", err, os.ErrProcessDone)
	}
	if err := syscall.Kill(escapedPID, 0); err != nil {
		t.Fatalf("escaped descendant was signaled through stale pgid: %v", err)
	}

	readResult, err := store.Read(output.ReadOptions{
		MaxEntries: 1000,
		MaxBytes:   1 << 20,
	})
	if err != nil {
		t.Fatalf("read escaped output: %v", err)
	}
	var sawReady bool
	for _, entry := range readResult.Entries {
		if entry.Stream == output.Stdout && strings.Contains(entry.Text, "escaped-ready\n") {
			sawReady = true
		}
	}
	if !sawReady {
		t.Fatal("leader output was lost before escaped-group completion")
	}
}

func TestCaptureDrainsFastExitOutput(t *testing.T) {
	store := newStore(t)
	child, err := Start(helperSpec(store, "buffered-output"))
	if err != nil {
		t.Fatalf("start child: %v", err)
	}
	result := child.Wait()
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	readResult, err := store.Read(output.ReadOptions{
		MaxEntries: bufferedOutputLines + 1,
		MaxBytes:   1 << 20,
	})
	if err != nil {
		t.Fatalf("read buffered output: %v", err)
	}
	if len(readResult.Entries) != bufferedOutputLines {
		t.Fatalf("captured %d entries, want %d", len(readResult.Entries), bufferedOutputLines)
	}
	for i := range bufferedOutputLines {
		entry := readResult.Entries[i]
		want := fmt.Sprintf("buffered-line-%04d\n", i)
		if entry.Stream != output.Stdout || entry.Text != want {
			t.Fatalf("buffered output entry %d = %#v, want stdout %q", i, entry, want)
		}
	}
}

func TestExitedAtSamplesAfterGroupTerminalBarrier(t *testing.T) {
	store := newStore(t)
	subscription := store.Subscribe(output.ReadOptions{})
	readyPath := filepath.Join(t.TempDir(), "timing-ready")
	before := time.Unix(100, 0)
	after := time.Unix(200, 0)
	var childRef atomic.Pointer[Child]
	now := func() time.Time {
		if child := childRef.Load(); child != nil {
			select {
			case <-child.groupGone:
				return after
			default:
			}
		}
		return before
	}
	spec := helperSpec(store, "group-orphan-parent")
	spec.Env = append(spec.Env, helperReady+"="+readyPath)
	spec.Now = now
	child, err := Start(spec)
	if err != nil {
		t.Fatalf("start child: %v", err)
	}
	childRef.Store(child)
	defer func() {
		_ = child.Signal(syscall.SIGKILL)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var descendantPID int
	for {
		event, err := subscription.Next(ctx)
		if err != nil {
			t.Fatalf("wait for timing descendant readiness: %v", err)
		}
		if event.Exit != nil {
			t.Fatal("process exited before timing descendant became ready")
		}
		for _, entry := range event.Read.Entries {
			const readyPrefix = "orphan-ready "
			if entry.Stream != output.Stdout || !strings.HasPrefix(entry.Text, readyPrefix) {
				continue
			}
			var descendantPGID int
			readyText := strings.TrimPrefix(entry.Text, readyPrefix)
			if n, scanErr := fmt.Sscanf(strings.TrimSpace(readyText), "pid=%d pgid=%d", &descendantPID, &descendantPGID); n != 2 || scanErr != nil {
				t.Fatalf("invalid timing descendant readiness %q (assignments=%d, err=%v)", readyText, n, scanErr)
			}
			if descendantPID <= 0 || descendantPGID != child.PGID() {
				t.Fatalf("timing descendant pid/pgid = %d/%d, original pgid %d", descendantPID, descendantPGID, child.PGID())
			}
			goto ready
		}
	}

ready:
	if err := child.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal timing leader: %v", err)
	}
	select {
	case <-child.leaderDone:
	case <-ctx.Done():
		t.Fatal("timing leader did not report exit")
	}
	if err := syscall.Kill(descendantPID, 0); err != nil {
		t.Fatalf("timing descendant exited before terminal barrier: %v", err)
	}
	select {
	case <-child.Done():
		t.Fatal("Done closed before final in-group member exited")
	default:
	}
	if err := child.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("signal timing group: %v", err)
	}

	done := make(chan Result, 1)
	go func() {
		done <- child.Wait()
	}()
	select {
	case result := <-done:
		if !result.ExitedAt.Equal(after) {
			t.Fatalf("ExitedAt = %v, want terminal-barrier time %v", result.ExitedAt, after)
		}
	case <-ctx.Done():
		t.Fatal("timing Wait did not complete")
	}
}

func TestCaptureErrorStillNotifiesAndTerminates(t *testing.T) {
	store, err := output.NewStore(output.Limits{RetainedBytes: 1})
	if err != nil {
		t.Fatalf("create tiny output store: %v", err)
	}
	subscription := store.Subscribe(output.ReadOptions{})
	child, err := Start(helperSpec(store, "too-large"))
	if err != nil {
		t.Fatalf("start child: %v", err)
	}
	result := child.Wait()
	if result.Err == nil {
		t.Fatal("capture failure was not reported")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		event, err := subscription.Next(ctx)
		if err != nil {
			t.Fatalf("read exit notification: %v", err)
		}
		if event.Exit != nil {
			return
		}
	}
}

func TestStartRejectsNegativeIdleFlush(t *testing.T) {
	store := newStore(t)
	_, err := Start(Spec{
		Argv:         []string{"/bin/true"},
		Output:       store,
		MaxLineBytes: 1,
		IdleFlush:    -time.Nanosecond,
	})
	if err == nil {
		t.Fatal("Start unexpectedly accepted a negative idle flush duration")
	}
}

func TestStartRejectsMissingRequiredFields(t *testing.T) {
	store := newStore(t)
	cases := []Spec{
		{Output: store, MaxLineBytes: 1},
		{Argv: []string{"/bin/true"}, MaxLineBytes: 1},
		{Argv: []string{"/bin/true"}, Output: store},
	}
	for _, spec := range cases {
		if _, err := Start(spec); err == nil {
			t.Fatalf("Start(%+v) unexpectedly succeeded", spec)
		}
	}
}
