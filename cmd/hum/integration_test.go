package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"hum/internal/protocol"
)

type integrationCommandResult struct {
	stdout string
	stderr string
	code   int
	err    error
}

type integrationProcess struct {
	Name         string           `json:"name"`
	Root         string           `json:"root"`
	PID          int              `json:"pid"`
	PGID         int              `json:"pgid"`
	Cwd          string           `json:"cwd"`
	Argv         []string         `json:"argv"`
	Start        time.Time        `json:"start"`
	Cursor       uint64           `json:"cursor"`
	LaunchCursor uint64           `json:"launch_cursor"`
	State        string           `json:"state"`
	Exit         *integrationExit `json:"exit"`
	ExitCode     *int             `json:"exit_code"`
}

type integrationOutputEntry struct {
	Cursor uint64    `json:"cursor"`
	Stream string    `json:"stream"`
	Time   time.Time `json:"time"`
	Text   string    `json:"text"`
}

type integrationReadResult struct {
	Op             string                   `json:"op"`
	OK             *bool                    `json:"ok"`
	Entries        []integrationOutputEntry `json:"entries"`
	Next           *uint64                  `json:"next"`
	Oldest         *uint64                  `json:"oldest"`
	Latest         *uint64                  `json:"latest"`
	EvictedThrough *uint64                  `json:"evicted_through"`
	Truncated      bool                     `json:"truncated"`
	More           bool                     `json:"more"`
}

type integrationExit struct {
	Code int       `json:"code"`
	Time time.Time `json:"time"`
}

type integrationFollowEvent struct {
	Op             string                   `json:"op"`
	Type           string                   `json:"type"`
	Name           string                   `json:"name"`
	Entries        []integrationOutputEntry `json:"entries"`
	Next           *uint64                  `json:"next"`
	Oldest         *uint64                  `json:"oldest"`
	Latest         *uint64                  `json:"latest"`
	EvictedThrough *uint64                  `json:"evicted_through"`
	Truncated      bool                     `json:"truncated"`
	More           bool                     `json:"more"`
	Cursor         *uint64                  `json:"cursor"`
	Exit           *integrationExit         `json:"exit"`
	Error          json.RawMessage          `json:"error"`
}

type integrationActionResult struct {
	Name    string          `json:"name"`
	Status  string          `json:"status"`
	Message string          `json:"message"`
	OK      *bool           `json:"ok"`
	Stopped *bool           `json:"stopped"`
	Error   json.RawMessage `json:"error"`
}

type integrationFollower struct {
	cmd    *exec.Cmd
	lines  chan string
	stderr *bytes.Buffer
	wait   chan error
}

func TestBuiltBinaryIntegration(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	runtimeDir := t.TempDir()
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	t.Setenv("HUM_INTEGRATION_MARKER", "marker-for-child")
	detachedGateDir := t.TempDir()
	detachedGate := filepath.Join(detachedGateDir, "release")
	if err := syscall.Mkfifo(detachedGate, 0o600); err != nil {
		t.Fatalf("create detached release FIFO: %v", err)
	}

	binary := filepath.Join(t.TempDir(), "hum")
	build := exec.Command("go", "build", "-o", binary, "./cmd/hum")
	build.Dir = repoRoot
	buildOutput, buildErr := build.CombinedOutput()
	if buildErr != nil {
		t.Fatalf("build hum binary: %v\n%s", buildErr, buildOutput)
	}
	t.Run("daemon autostart", func(t *testing.T) {
		autostartRuntimeDir, err := os.MkdirTemp("", "hum-autostart-")
		if err != nil {
			t.Fatalf("create short daemon runtime directory: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(autostartRuntimeDir) })
		t.Setenv("HUM_RUNTIME_DIR", autostartRuntimeDir)
		t.Cleanup(func() {
			integrationForceShutdown(binary, cwd)
		})

		run := integrationRunBinary(binary, cwd, "run", "demo", "--detach", "--", "sleep", "30")
		if run.code != 0 {
			t.Fatalf("autostart run exit code = %d: stdout=%q stderr=%q err=%v", run.code, run.stdout, run.stderr, run.err)
		}
		if run.stderr != "" {
			t.Fatalf("autostart run leaked daemon diagnostics to stderr: %q", run.stderr)
		}
		demoPID, err := integrationStartedProcessPID(run.stdout, "demo")
		if err != nil {
			t.Fatalf("decode autostart run output: %v (stdout=%q)", err, run.stdout)
		}
		if err := integrationAssertProcessAlive(demoPID); err != nil {
			t.Fatalf("demo process after autostart run: %v", err)
		}

		socket := filepath.Join(autostartRuntimeDir, "hum.sock")
		integrationWaitForRuntimeFile(t, socket, "autostart daemon socket")
		serveStatus := integrationRunBinary(binary, cwd, "serve", "--daemon")
		if serveStatus.code != 0 {
			t.Fatalf("first daemon serve exit code = %d: stdout=%q stderr=%q err=%v", serveStatus.code, serveStatus.stdout, serveStatus.stderr, serveStatus.err)
		}
		if serveStatus.stdout != "" {
			t.Fatalf("first daemon serve wrote diagnostics to stdout: %q", serveStatus.stdout)
		}
		serveSocket, daemonPID, err := integrationDaemonStatus(serveStatus.stderr)
		if err != nil {
			t.Fatalf("decode first daemon serve status: %v (stderr=%q)", err, serveStatus.stderr)
		}
		if serveSocket != socket {
			t.Fatalf("first daemon serve socket = %q, want %q", serveSocket, socket)
		}
		wantServeStatus := fmt.Sprintf("hum serve: listening on %s (PID %d)\n", socket, daemonPID)
		if serveStatus.stderr != wantServeStatus {
			t.Fatalf("first daemon serve stderr = %q, want exact status %q", serveStatus.stderr, wantServeStatus)
		}
		if err := integrationAssertProcessAlive(daemonPID); err != nil {
			t.Fatalf("daemon after first daemon serve exits: %v", err)
		}
		integrationWaitForRuntimeFile(t, filepath.Join(autostartRuntimeDir, "daemon.log"), "autostart daemon log")

		serveAgain := integrationRunBinary(binary, cwd, "serve", "--daemon")
		if serveAgain.code != 0 {
			t.Fatalf("second daemon serve exit code = %d: stdout=%q stderr=%q err=%v", serveAgain.code, serveAgain.stdout, serveAgain.stderr, serveAgain.err)
		}
		if serveAgain.stdout != "" {
			t.Fatalf("second daemon serve wrote diagnostics to stdout: %q", serveAgain.stdout)
		}
		serveAgainSocket, serveAgainPID, err := integrationDaemonStatus(serveAgain.stderr)
		if err != nil {
			t.Fatalf("decode second daemon serve status: %v (stderr=%q)", err, serveAgain.stderr)
		}
		if serveAgainSocket != socket || serveAgainPID != daemonPID {
			t.Fatalf("second daemon serve status = socket %q PID %d, want socket %q PID %d", serveAgainSocket, serveAgainPID, socket, daemonPID)
		}
		wantServeAgainStatus := fmt.Sprintf("hum serve: listening on %s (PID %d)\n", socket, daemonPID)
		if serveAgain.stderr != wantServeAgainStatus {
			t.Fatalf("second daemon serve stderr = %q, want exact status %q", serveAgain.stderr, wantServeAgainStatus)
		}
		if err := integrationAssertProcessAlive(daemonPID); err != nil {
			t.Fatalf("daemon after second daemon serve exits: %v", err)
		}
	})

	serveStdout, serveStderr := new(bytes.Buffer), new(bytes.Buffer)
	serve := exec.Command(binary, "serve")
	serve.Dir = repoRoot
	serve.Env = append([]string(nil), os.Environ()...)
	serve.Stdout = serveStdout
	serve.Stderr = serveStderr
	if err := serve.Start(); err != nil {
		t.Fatalf("start foreground serve: %v", err)
	}
	t.Cleanup(func() {
		integrationStopServe(serve)
	})
	t.Cleanup(func() {
		integrationForceShutdown(binary, cwd)
	})
	integrationWaitForSocket(t, serve, filepath.Join(runtimeDir, "hum.sock"))

	attachedScript := "printf 'attached-stdout:%s\\n' \"$HUM_INTEGRATION_MARKER\"; printf 'attached-raw   \\n'; printf 'attached-argv:%s:%s\\n' \"$1\" \"$2\"; printf 'attached-stderr\\n' >&2; exit 7"
	attached, err := integrationStartFollower(binary, cwd, "run", "attached", "--", "/bin/sh", "-c", attachedScript, "hum-child", "alpha", "beta")
	if err != nil {
		t.Fatalf("start attached run: %v", err)
	}
	wantAttachedLines := []string{"attached-stdout:marker-for-child\n", "attached-raw   \n", "attached-argv:alpha:beta\n"}
	for index, want := range wantAttachedLines {
		line, lineErr := integrationFollowerLine(attached, 8*time.Second)
		if lineErr != nil || line != want {
			t.Fatalf("attached line %d = %q, %v; want %q", index, line, lineErr, want)
		}
	}
	attachedDeadline := time.Now().Add(8 * time.Second)
	for !strings.Contains(attached.stderr.String(), "attached waiting for next launch\n") {
		if time.Now().After(attachedDeadline) {
			t.Fatalf("attached lifecycle stderr = %q", attached.stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if attached.cmd.ProcessState != nil {
		t.Fatal("attached run exited instead of waiting for the next launch")
	}
	if err := attached.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("detach attached run: %v", err)
	}
	select {
	case err := <-attached.wait:
		if err != nil {
			t.Fatalf("attached detach: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("attached run did not detach")
	}
	wantAttachedStderr := "attached launched\nattached-stderr\nattached exited with code 7\nattached waiting for next launch\n"
	if attached.stderr.String() != wantAttachedStderr {
		t.Errorf("attached stderr = %q, want %q", attached.stderr.String(), wantAttachedStderr)
	}

	t.Setenv("HUM_INTEGRATION_GATE", detachedGate)
	detachedScript := "exec 3<> \"$HUM_INTEGRATION_GATE\"; printf 'detached-initial-stdout\\n'; printf 'detached-initial-stderr\\n' >&2; IFS= read -r _ <&3; printf 'detached-follow-stdout\\n'; printf 'detached-follow-stderr\\n' >&2; sleep 30"
	detached := integrationRunBinary(binary, cwd, "run", "detached", "--detach", "--json", "--", "/bin/sh", "-c", detachedScript)
	if detached.code != 0 {
		t.Fatalf("detached run exit code = %d: stdout=%q stderr=%q err=%v", detached.code, detached.stdout, detached.stderr, detached.err)
	}
	detachedProcess, err := integrationProcessFromJSON(detached.stdout)
	if err != nil {
		t.Fatalf("decode detached run JSON: %v (stdout=%q)", err, detached.stdout)
	}
	if detachedProcess.Name != "detached" || detachedProcess.PID <= 0 {
		t.Fatalf("detached run process = %+v, want name detached and positive PID", detachedProcess)
	}

	list := integrationRunBinary(binary, cwd, "list", "--all", "--json")
	if list.code != 0 {
		t.Fatalf("list exit code = %d: stdout=%q stderr=%q", list.code, list.stdout, list.stderr)
	}
	processes, err := integrationProcessesFromJSON(list.stdout)
	if err != nil {
		t.Fatalf("decode list JSON: %v (stdout=%q)", err, list.stdout)
	}
	listed, ok := integrationFindNamedProcess(processes, "detached")
	if !ok {
		t.Fatalf("list did not contain detached process: %+v", processes)
	}
	if listed.PID != detachedProcess.PID || listed.Cwd != cwd {
		t.Fatalf("listed detached process = %+v, want PID %d and cwd %q", listed, detachedProcess.PID, cwd)
	}
	wantArgv := []string{"/bin/sh", "-c", detachedScript}
	if !equalIntegrationStrings(listed.Argv, wantArgv) {
		t.Fatalf("listed detached argv = %#v, want %#v", listed.Argv, wantArgv)
	}
	if listed.State != "running" {
		t.Fatalf("listed detached state = %q, want running", listed.State)
	}

	var bounded integrationReadResult
	boundedDeadline := time.Now().Add(5 * time.Second)
	for {
		boundedResult := integrationRunBinary(binary, cwd, "logs", "detached", "--stream", "stdout", "--tail", "1", "--limit-bytes", "4096", "--json")
		if boundedResult.code != 0 {
			t.Fatalf("bounded logs exit code = %d: stdout=%q stderr=%q", boundedResult.code, boundedResult.stdout, boundedResult.stderr)
		}
		bounded, err = integrationReadResultFromJSON(boundedResult.stdout)
		if err == nil && len(bounded.Entries) > 0 {
			break
		}
		if time.Now().After(boundedDeadline) {
			t.Fatalf("bounded logs did not yield an entry: decode=%v stdout=%q stderr=%q", err, boundedResult.stdout, boundedResult.stderr)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(bounded.Entries) != 1 {
		t.Fatalf("bounded logs entries = %d, want one tail entry: %+v", len(bounded.Entries), bounded)
	}
	if bounded.Entries[0].Stream != "stdout" || bounded.Entries[0].Text != "detached-initial-stdout\n" {
		t.Fatalf("bounded logs entry = %+v, want exact stdout line %q", bounded.Entries[0], "detached-initial-stdout\n")
	}
	if bounded.Next == nil {
		t.Fatalf("bounded logs omitted next cursor: %+v", bounded)
	}

	follower, err := integrationStartFollower(binary, cwd, "logs", "detached", "--stream", "both", "--tail", "2", "--limit-bytes", "4096", "--follow", "--json")
	if err != nil {
		t.Fatalf("start logs follower: %v", err)
	}
	t.Cleanup(func() {
		integrationStopFollower(follower, 3*time.Second)
	})
	initialLine, err := integrationFollowerLine(follower, 8*time.Second)
	if err != nil {
		t.Fatalf("read initial logs follower event: %v (stderr=%q)", err, follower.stderr.String())
	}
	initialEvent, err := integrationFollowEventFromJSON(initialLine)
	if err != nil {
		t.Fatalf("decode initial logs follower NDJSON: %v (line=%q)", err, initialLine)
	}
	if initialEvent.Type != string(protocol.EventOutput) || initialEvent.Name != "detached" {
		t.Fatalf("initial logs follower event = %+v, want output event for detached", initialEvent)
	}
	if err := integrationRequireOutputTexts(initialEvent.Entries, map[string]string{
		"stdout": "detached-initial-stdout\n",
		"stderr": "detached-initial-stderr\n",
	}); err != nil {
		t.Fatalf("initial logs follower entries: %v", err)
	}
	release, err := os.OpenFile(detachedGate, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open detached release FIFO: %v", err)
	}
	if _, err := io.WriteString(release, "\n"); err != nil {
		_ = release.Close()
		t.Fatalf("release detached process: %v", err)
	}
	if err := release.Close(); err != nil {
		t.Fatalf("close detached release FIFO: %v", err)
	}
	seenFollowOutput := false
	seenFollowError := false
	followerDeadline := time.NewTimer(8 * time.Second)
	defer followerDeadline.Stop()
	for !seenFollowOutput || !seenFollowError {
		select {
		case line, ok := <-follower.lines:
			if !ok {
				t.Fatalf("logs follower closed before delayed output (stdout=%q)", follower.stderr.String())
			}
			event, decodeErr := integrationFollowEventFromJSON(line)
			if decodeErr != nil {
				t.Fatalf("decode logs follower NDJSON: %v (line=%q)", decodeErr, line)
			}
			if event.Type != string(protocol.EventOutput) || event.Name != "detached" {
				t.Fatalf("delayed logs follower event = %+v, want output event for detached", event)
			}
			for _, entry := range event.Entries {
				if entry.Text == "detached-follow-stdout\n" {
					seenFollowOutput = true
				}
				if entry.Text == "detached-follow-stderr\n" {
					seenFollowError = true
				}
			}
		case <-followerDeadline.C:
			t.Fatalf("logs follower did not deliver delayed stdout/stderr")
		}
	}

	stillRunning := integrationRunBinary(binary, cwd, "list", "--json")
	if stillRunning.code != 0 {
		t.Fatalf("list while following exit code = %d: stdout=%q stderr=%q", stillRunning.code, stillRunning.stdout, stillRunning.stderr)
	}
	stillRunningProcesses, err := integrationProcessesFromJSON(stillRunning.stdout)
	if err != nil {
		t.Fatalf("decode list while following JSON: %v", err)
	}
	if process, ok := integrationFindNamedProcess(stillRunningProcesses, "detached"); !ok || process.State != "running" {
		t.Fatalf("follower changed process lifecycle: %+v", stillRunningProcesses)
	}

	stopDetached := integrationRunBinary(binary, cwd, "stop", "detached", "--json")
	if stopDetached.code != 0 {
		t.Fatalf("stop detached exit code = %d: stdout=%q stderr=%q", stopDetached.code, stopDetached.stdout, stopDetached.stderr)
	}
	if err := integrationRequireActionResults(stopDetached.stdout, []integrationActionResult{{Name: "detached", Status: "stopped"}}); err != nil {
		t.Fatalf("decode stop detached JSON: %v (stdout=%q)", err, stopDetached.stdout)
	}

	var waitingLine string
	for !strings.Contains(waitingLine, "waiting for next launch") {
		waitingLine, err = integrationFollowerLine(follower, 8*time.Second)
		if err != nil {
			t.Fatalf("logs follower after stop: line=%q err=%v stderr=%q", waitingLine, err, follower.stderr.String())
		}
	}
	if follower.cmd.ProcessState != nil {
		t.Fatal("logs follower exited after stop")
	}
	if err := follower.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("detach logs follower: %v", err)
	}
	if err := integrationDrainFollower(follower, 8*time.Second); err != nil {
		t.Fatalf("logs follower detach: %v (stderr=%q)", err, follower.stderr.String())
	}

	stopAlreadyStopped := integrationRunBinary(binary, cwd, "stop", "detached", "detached", "missing", "--json")
	if stopAlreadyStopped.code != 0 {
		t.Fatalf("multi-name already-stopped stop exit code = %d: stdout=%q stderr=%q", stopAlreadyStopped.code, stopAlreadyStopped.stdout, stopAlreadyStopped.stderr)
	}
	if err := integrationRequireActionResults(stopAlreadyStopped.stdout, []integrationActionResult{
		{Name: "detached", Status: "not_running"},
		{Name: "detached", Status: "not_running"},
		{Name: "missing", Status: "not_running"},
	}); err != nil {
		t.Fatalf("decode multi-name stop JSON: %v (stdout=%q)", err, stopAlreadyStopped.stdout)
	}

	guardScript := "printf 'guard-running\\n'; sleep 30"
	guard := integrationRunBinary(binary, cwd, "run", "guard", "--detach", "--json", "--", "/bin/sh", "-c", guardScript)
	if guard.code != 0 {
		t.Fatalf("guard detached run exit code = %d: stdout=%q stderr=%q", guard.code, guard.stdout, guard.stderr)
	}
	guardProcess, err := integrationProcessFromJSON(guard.stdout)
	if err != nil {
		t.Fatalf("decode guard run JSON: %v (stdout=%q)", err, guard.stdout)
	}

	refusedShutdown := integrationRunBinary(binary, cwd, "shutdown", "--json")
	if refusedShutdown.code == 0 {
		t.Fatalf("shutdown without force unexpectedly succeeded: stdout=%q stderr=%q", refusedShutdown.stdout, refusedShutdown.stderr)
	}
	refusedStatus, err := integrationShutdownStatusFromJSON(refusedShutdown.stdout)
	if err != nil {
		t.Fatalf("decode refused shutdown JSON: %v (stdout=%q)", err, refusedShutdown.stdout)
	}
	if refusedStatus != "error" {
		t.Fatalf("refused shutdown status = %q, want error", refusedStatus)
	}
	if !strings.Contains(refusedShutdown.stdout+refusedShutdown.stderr, "guard") {
		t.Fatalf("refused shutdown did not name active guard: stdout=%q stderr=%q", refusedShutdown.stdout, refusedShutdown.stderr)
	}
	if err := integrationAssertProcessAlive(guardProcess.PID); err != nil {
		t.Fatalf("guard process after refused shutdown: %v", err)
	}
	afterRefusal := integrationRunBinary(binary, cwd, "list", "--json")
	if afterRefusal.code != 0 {
		t.Fatalf("list after refused shutdown exit code = %d: stdout=%q stderr=%q", afterRefusal.code, afterRefusal.stdout, afterRefusal.stderr)
	}
	afterRefusalProcesses, err := integrationProcessesFromJSON(afterRefusal.stdout)
	if err != nil {
		t.Fatalf("decode list after refused shutdown JSON: %v", err)
	}
	refusedGuard, ok := integrationFindNamedProcess(afterRefusalProcesses, "guard")
	if !ok || refusedGuard.PID != guardProcess.PID || refusedGuard.PGID <= 0 || refusedGuard.State != "running" {
		t.Fatalf("guard after refused shutdown = %+v, want running PID %d and positive PGID", refusedGuard, guardProcess.PID)
	}
	guardPGID := refusedGuard.PGID
	if err := integrationAssertProcessGroupAlive(guardPGID); err != nil {
		t.Fatalf("guard process group after refused shutdown: %v", err)
	}

	forcedShutdown := integrationRunBinary(binary, cwd, "shutdown", "--stop-processes", "--json")
	if forcedShutdown.code != 0 {
		t.Fatalf("forced shutdown exit code = %d: stdout=%q stderr=%q", forcedShutdown.code, forcedShutdown.stdout, forcedShutdown.stderr)
	}
	forcedStatus, err := integrationShutdownStatusFromJSON(forcedShutdown.stdout)
	if err != nil {
		t.Fatalf("decode forced shutdown JSON: %v (stdout=%q)", err, forcedShutdown.stdout)
	}
	if forcedStatus != "stopped" {
		t.Fatalf("forced shutdown status = %q, want stopped", forcedStatus)
	}
	if err := integrationAssertProcessGone(guardProcess.PID); err != nil {
		t.Fatalf("guard process after forced shutdown: %v", err)
	}
	if err := integrationAssertProcessGroupGone(guardPGID); err != nil {
		t.Fatalf("guard process group after forced shutdown: %v", err)
	}
	afterForced := integrationRunBinary(binary, cwd, "list", "--json")
	if afterForced.code != 0 {
		t.Fatalf("list after forced shutdown exit code = %d: stdout=%q stderr=%q", afterForced.code, afterForced.stdout, afterForced.stderr)
	}
	afterForcedProcesses, err := integrationProcessesFromJSON(afterForced.stdout)
	if err != nil {
		t.Fatalf("decode list after forced shutdown JSON: %v", err)
	}
	if _, ok := integrationFindNamedProcess(afterForcedProcesses, "guard"); ok {
		t.Fatalf("guard remained listed after forced shutdown: %+v", afterForcedProcesses)
	}

	serveWaitErr := integrationWaitProcess(serve, 8*time.Second)
	if serveWaitErr != nil {
		t.Fatalf("foreground serve after forced shutdown: %v (stdout=%q stderr=%q)", serveWaitErr, serveStdout.String(), serveStderr.String())
	}
	if code := serve.ProcessState.ExitCode(); code != 0 {
		t.Fatalf("foreground serve exit code = %d, want 0 (stderr=%q)", code, serveStderr.String())
	}
	if strings.TrimSpace(serveStdout.String()) != "" {
		t.Errorf("foreground serve wrote diagnostics to stdout: %q", serveStdout.String())
	}
}

func integrationRunBinary(binary, cwd string, args ...string) integrationCommandResult {
	return integrationRunBinaryContext(context.Background(), binary, cwd, args...)
}

func integrationRunBinaryContext(ctx context.Context, binary, cwd string, args ...string) integrationCommandResult {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = cwd
	cmd.Env = append([]string(nil), os.Environ()...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
	}
	return integrationCommandResult{stdout: stdout.String(), stderr: stderr.String(), code: code, err: err}
}

func integrationForceShutdown(binary, cwd string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = integrationRunBinaryContext(ctx, binary, cwd, "shutdown", "--stop-processes", "--json")
}

func integrationStartFollower(binary, cwd string, args ...string) (*integrationFollower, error) {
	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	cmd.Env = append([]string(nil), os.Environ()...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	wait := make(chan error, 1)
	go func() {
		wait <- cmd.Wait()
	}()
	lines := make(chan string, 128)
	go func() {
		reader := bufio.NewReader(stdout)
		for {
			line, readErr := reader.ReadString('\n')
			if line != "" {
				lines <- line
			}
			if readErr != nil {
				break
			}
		}
		close(lines)
	}()
	return &integrationFollower{cmd: cmd, lines: lines, stderr: stderr, wait: wait}, nil
}

func integrationFollowerLine(follower *integrationFollower, timeout time.Duration) (string, error) {
	if follower == nil {
		return "", errors.New("nil follower")
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case line, ok := <-follower.lines:
		if !ok {
			return "", errors.New("follower closed")
		}
		return line, nil
	case <-timer.C:
		return "", errors.New("timed out waiting for follower event")
	}
}

func integrationStopFollower(follower *integrationFollower, timeout time.Duration) {
	if follower == nil || follower.cmd == nil {
		return
	}
	if follower.cmd.ProcessState != nil {
		return
	}
	if follower.cmd.ProcessState == nil && follower.cmd.Process != nil {
		_ = follower.cmd.Process.Signal(syscall.SIGTERM)
	}
	if follower.wait == nil {
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-follower.wait:
		return
	case <-timer.C:
	}
	if follower.cmd.ProcessState == nil && follower.cmd.Process != nil {
		_ = follower.cmd.Process.Kill()
	}
	timer.Reset(time.Second)
	select {
	case <-follower.wait:
	case <-timer.C:
	}
}

func integrationDrainFollower(follower *integrationFollower, timeout time.Duration) error {
	if follower == nil || follower.wait == nil {
		return errors.New("nil follower")
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var waitErr error
	waitDone := false
	lines := follower.lines
	wait := follower.wait
	for lines != nil || !waitDone {
		select {
		case line, ok := <-lines:
			if !ok {
				lines = nil
				continue
			}
			if _, err := integrationFollowEventFromJSON(line); err != nil {
				return fmt.Errorf("decode event %q: %w", line, err)
			}
		case waitErr = <-wait:
			waitDone = true
			wait = nil
		case <-timer.C:
			return errors.New("timed out waiting for follower to close")
		}
	}
	if waitErr != nil {
		return waitErr
	}
	return nil
}

func integrationRequireOutputTexts(entries []integrationOutputEntry, want map[string]string) error {
	if len(entries) != len(want) {
		return fmt.Errorf("got %d output entries, want %d: %+v", len(entries), len(want), entries)
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		expected, ok := want[entry.Stream]
		if !ok {
			return fmt.Errorf("unexpected output stream %q in %+v", entry.Stream, entries)
		}
		if seen[entry.Stream] {
			return fmt.Errorf("duplicate output stream %q in %+v", entry.Stream, entries)
		}
		if entry.Text != expected {
			return fmt.Errorf("output %s text = %q, want exact raw line %q", entry.Stream, entry.Text, expected)
		}
		seen[entry.Stream] = true
	}
	for stream := range want {
		if !seen[stream] {
			return fmt.Errorf("missing output stream %q in %+v", stream, entries)
		}
	}
	return nil
}

func integrationStartedProcessPID(raw, name string) (int, error) {
	if name == "" {
		return 0, errors.New("empty process name")
	}
	if !strings.HasSuffix(raw, "\n") {
		return 0, errors.New("started process output omitted trailing newline")
	}
	line := strings.TrimSuffix(raw, "\n")
	if strings.Contains(line, "\n") {
		return 0, errors.New("started process output contains multiple lines")
	}
	prefix := fmt.Sprintf("started %s (PID ", name)
	if !strings.HasPrefix(line, prefix) {
		return 0, fmt.Errorf("started process output has prefix %q, want %q", line, prefix)
	}
	cursorMarker := ", cursor "
	cursorOffset := strings.LastIndex(line, cursorMarker)
	if cursorOffset <= len(prefix) || !strings.HasSuffix(line, ")") {
		return 0, fmt.Errorf("started process output has malformed cursor: %q", line)
	}
	pid, err := strconv.Atoi(line[len(prefix):cursorOffset])
	if err != nil || pid <= 0 {
		if err == nil {
			err = errors.New("PID is not positive")
		}
		return 0, fmt.Errorf("started process output has invalid PID: %w", err)
	}
	cursor := line[cursorOffset+len(cursorMarker) : len(line)-1]
	if _, err := strconv.ParseUint(cursor, 10, 64); err != nil {
		return 0, fmt.Errorf("started process output has invalid cursor %q: %w", cursor, err)
	}
	return pid, nil
}

func integrationDaemonStatus(raw string) (string, int, error) {
	const prefix = "hum serve: listening on "
	if !strings.HasSuffix(raw, "\n") {
		return "", 0, errors.New("daemon status omitted trailing newline")
	}
	line := strings.TrimSuffix(raw, "\n")
	if strings.Contains(line, "\n") {
		return "", 0, errors.New("daemon status contains multiple lines")
	}
	if !strings.HasPrefix(line, prefix) {
		return "", 0, fmt.Errorf("daemon status = %q, want prefix %q", line, prefix)
	}
	rest := strings.TrimPrefix(line, prefix)
	pidMarker := " (PID "
	pidOffset := strings.LastIndex(rest, pidMarker)
	if pidOffset <= 0 || !strings.HasSuffix(rest, ")") {
		return "", 0, fmt.Errorf("daemon status has malformed PID: %q", line)
	}
	socket := rest[:pidOffset]
	pid, err := strconv.Atoi(rest[pidOffset+len(pidMarker) : len(rest)-1])
	if err != nil || pid <= 0 {
		if err == nil {
			err = errors.New("PID is not positive")
		}
		return "", 0, fmt.Errorf("daemon status has invalid PID: %w", err)
	}
	return socket, pid, nil
}

func integrationWaitForRuntimeFile(t *testing.T, path, description string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var lastErr error
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not appear at %q: %v", description, path, lastErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func integrationWaitForSocket(t *testing.T, serve *exec.Cmd, socket string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("foreground serve did not create socket %q", socket)
		}
		if serve.ProcessState != nil {
			t.Fatalf("foreground serve exited before readiness (exit=%d)", serve.ProcessState.ExitCode())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func integrationStopServe(serve *exec.Cmd) {
	if serve == nil || serve.Process == nil || serve.ProcessState != nil {
		return
	}
	done := make(chan error, 1)
	go func() { done <- serve.Wait() }()
	_ = serve.Process.Signal(syscall.SIGTERM)
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		return
	case <-timer.C:
	}
	_ = serve.Process.Kill()
	timer.Reset(time.Second)
	select {
	case <-done:
	case <-timer.C:
	}
}

func integrationWaitProcess(cmd *exec.Cmd, timeout time.Duration) error {
	if cmd == nil || cmd.ProcessState != nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		timer.Reset(time.Second)
		select {
		case <-done:
		case <-timer.C:
		}
		return errors.New("timed out waiting for process")
	}
}

func integrationJSONDocuments(raw string) ([]json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	var documents []json.RawMessage
	for {
		var document json.RawMessage
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		documents = append(documents, append(json.RawMessage(nil), document...))
	}
	if len(documents) == 0 {
		return nil, errors.New("no JSON documents")
	}
	return documents, nil
}

func integrationJSONObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("empty JSON document")
	}
	if trimmed[0] != '{' {
		return nil, fmt.Errorf("JSON document is not an object: %s", trimmed)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, errors.New("JSON document is null")
	}
	return object, nil
}

func integrationRequiredJSONField(object map[string]json.RawMessage, name string) (json.RawMessage, error) {
	value, ok := object[name]
	if !ok {
		return nil, fmt.Errorf("JSON object omitted required field %q", name)
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, fmt.Errorf("JSON field %q is null", name)
	}
	return value, nil
}

func integrationProcessFromJSON(raw string) (integrationProcess, error) {
	documents, err := integrationJSONDocuments(raw)
	if err != nil {
		return integrationProcess{}, err
	}
	if len(documents) != 1 {
		return integrationProcess{}, fmt.Errorf("expected one detached JSON document, got %d", len(documents))
	}
	object, err := integrationJSONObject(documents[0])
	if err != nil {
		return integrationProcess{}, err
	}
	for _, field := range []string{"name", "pid", "cursor"} {
		if _, err := integrationRequiredJSONField(object, field); err != nil {
			return integrationProcess{}, err
		}
	}
	var process integrationProcess
	if err := json.Unmarshal(documents[0], &process); err != nil {
		return integrationProcess{}, err
	}
	if process.Name == "" || process.PID <= 0 {
		return integrationProcess{}, fmt.Errorf("JSON detached response has invalid stable fields: %+v", process)
	}
	return process, nil
}

func integrationDecodeProcess(raw json.RawMessage) (integrationProcess, error) {
	object, err := integrationJSONObject(raw)
	if err != nil {
		return integrationProcess{}, err
	}
	for _, field := range []string{"name", "root", "pid", "pgid", "cwd", "argv", "start", "launch_cursor", "state"} {
		if _, err := integrationRequiredJSONField(object, field); err != nil {
			return integrationProcess{}, err
		}
	}
	var process integrationProcess
	if err := json.Unmarshal(raw, &process); err != nil {
		return integrationProcess{}, err
	}
	if process.Name == "" || process.Root == "" || process.PID <= 0 || process.PGID <= 0 || process.Cwd == "" || process.Argv == nil || process.Start.IsZero() || process.State == "" {
		return integrationProcess{}, fmt.Errorf("JSON process has invalid stable fields: %+v", process)
	}
	return process, nil
}

func integrationProcessesFromJSON(raw string) ([]integrationProcess, error) {
	documents, err := integrationJSONDocuments(raw)
	if err != nil {
		return nil, err
	}
	if len(documents) != 1 {
		return nil, fmt.Errorf("expected one list JSON document, got %d", len(documents))
	}
	object, err := integrationJSONObject(documents[0])
	if err != nil {
		return nil, err
	}
	value, err := integrationRequiredJSONField(object, "processes")
	if err != nil {
		return nil, err
	}
	if trimmed := bytes.TrimSpace(value); len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, errors.New("JSON processes field is not an array")
	}
	var values []json.RawMessage
	if err := json.Unmarshal(value, &values); err != nil {
		return nil, err
	}
	processes := make([]integrationProcess, 0, len(values))
	for _, item := range values {
		process, err := integrationDecodeProcess(item)
		if err != nil {
			return nil, err
		}
		processes = append(processes, process)
	}
	return processes, nil
}

func integrationFindNamedProcess(processes []integrationProcess, name string) (integrationProcess, bool) {
	for _, process := range processes {
		if process.Name == name {
			return process, true
		}
	}
	return integrationProcess{}, false
}

func integrationReadResultFromJSON(raw string) (integrationReadResult, error) {
	documents, err := integrationJSONDocuments(raw)
	if err != nil {
		return integrationReadResult{}, err
	}
	if len(documents) != 1 {
		return integrationReadResult{}, fmt.Errorf("expected one output JSON document, got %d", len(documents))
	}
	object, err := integrationJSONObject(documents[0])
	if err != nil {
		return integrationReadResult{}, err
	}
	opRaw, err := integrationRequiredJSONField(object, "op")
	if err != nil {
		return integrationReadResult{}, err
	}
	var op string
	if err := json.Unmarshal(opRaw, &op); err != nil {
		return integrationReadResult{}, fmt.Errorf("decode output JSON op: %w", err)
	}
	if op != string(protocol.OpOutput) {
		return integrationReadResult{}, fmt.Errorf("output JSON op = %q, want %q", op, protocol.OpOutput)
	}
	okRaw, err := integrationRequiredJSONField(object, "ok")
	if err != nil {
		return integrationReadResult{}, err
	}
	var ok bool
	if err := json.Unmarshal(okRaw, &ok); err != nil {
		return integrationReadResult{}, fmt.Errorf("decode output JSON ok: %w", err)
	}
	if !ok {
		return integrationReadResult{}, errors.New("output JSON response is not successful")
	}
	entriesRaw, err := integrationRequiredJSONField(object, "entries")
	if err != nil {
		return integrationReadResult{}, err
	}
	if trimmed := bytes.TrimSpace(entriesRaw); len(trimmed) == 0 || trimmed[0] != '[' {
		return integrationReadResult{}, errors.New("output JSON entries field is not an array")
	}
	nextRaw, err := integrationRequiredJSONField(object, "next")
	if err != nil {
		return integrationReadResult{}, err
	}
	if trimmed := bytes.TrimSpace(nextRaw); len(trimmed) == 0 {
		return integrationReadResult{}, errors.New("JSON output next field is empty")
	}
	var result integrationReadResult
	if err := json.Unmarshal(documents[0], &result); err != nil {
		return integrationReadResult{}, err
	}
	result.Op = op
	result.OK = &ok
	return result, nil
}

func integrationFollowEventFromJSON(raw string) (integrationFollowEvent, error) {
	object, err := integrationJSONObject(json.RawMessage(raw))
	if err != nil {
		return integrationFollowEvent{}, err
	}
	for _, field := range []string{"op", "type", "name"} {
		if _, err := integrationRequiredJSONField(object, field); err != nil {
			return integrationFollowEvent{}, err
		}
	}
	var event integrationFollowEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return integrationFollowEvent{}, err
	}
	if event.Op != string(protocol.OpEvent) {
		return integrationFollowEvent{}, fmt.Errorf("follow event op = %q, want %q", event.Op, protocol.OpEvent)
	}
	if event.Type == "" {
		return integrationFollowEvent{}, errors.New("follow event omitted stable type")
	}
	switch event.Type {
	case string(protocol.EventOutput), string(protocol.EventCursor), string(protocol.EventEviction), string(protocol.EventExit), string(protocol.EventReady), string(protocol.EventError):
	default:
		return integrationFollowEvent{}, fmt.Errorf("follow event has unknown type %q", event.Type)
	}
	if event.Name == "" {
		return integrationFollowEvent{}, errors.New("follow event omitted stable name")
	}
	return event, nil
}

func integrationRequireActionResults(raw string, want []integrationActionResult) error {
	documents, err := integrationJSONDocuments(raw)
	if err != nil {
		return err
	}
	if len(documents) != len(want) {
		return fmt.Errorf("got %d action results, want %d", len(documents), len(want))
	}
	for i, document := range documents {
		object, err := integrationJSONObject(document)
		if err != nil {
			return fmt.Errorf("action result %d: %w", i, err)
		}
		for _, field := range []string{"name", "status"} {
			if _, err := integrationRequiredJSONField(object, field); err != nil {
				return fmt.Errorf("action result %d: %w", i, err)
			}
		}
		var got integrationActionResult
		if err := json.Unmarshal(document, &got); err != nil {
			return fmt.Errorf("decode action result %d: %w", i, err)
		}
		if got.Name != want[i].Name || got.Status != want[i].Status {
			return fmt.Errorf("action result %d = name %q status %q, want name %q status %q", i, got.Name, got.Status, want[i].Name, want[i].Status)
		}
	}
	return nil
}

func integrationShutdownStatusFromJSON(raw string) (string, error) {
	documents, err := integrationJSONDocuments(raw)
	if err != nil {
		return "", err
	}
	if len(documents) != 1 {
		return "", fmt.Errorf("expected one shutdown JSON document, got %d", len(documents))
	}
	object, err := integrationJSONObject(documents[0])
	if err != nil {
		return "", err
	}
	statusRaw, err := integrationRequiredJSONField(object, "status")
	if err != nil {
		return "", err
	}
	var status string
	if err := json.Unmarshal(statusRaw, &status); err != nil {
		return "", fmt.Errorf("decode shutdown status: %w", err)
	}
	if status == "" {
		return "", errors.New("shutdown JSON status is empty")
	}
	return status, nil
}

func integrationAssertProcessAlive(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process PID %d", pid)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return fmt.Errorf("probe PID %d: %w", pid, err)
	}
	return nil
}

func integrationAssertProcessGone(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process PID %d", pid)
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return fmt.Errorf("PID %d is still alive", pid)
	}
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return fmt.Errorf("probe PID %d: %w", pid, err)
}

func equalIntegrationStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
func integrationAssertProcessGroupAlive(pgid int) error {
	if pgid <= 0 {
		return fmt.Errorf("invalid process-group ID %d", pgid)
	}
	if err := syscall.Kill(-pgid, 0); err != nil {
		return fmt.Errorf("probe process group %d: %w", pgid, err)
	}
	return nil
}

func integrationAssertProcessGroupGone(pgid int) error {
	if pgid <= 0 {
		return fmt.Errorf("invalid process-group ID %d", pgid)
	}
	err := syscall.Kill(-pgid, 0)
	if err == nil {
		return fmt.Errorf("process group %d is still alive", pgid)
	}
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return fmt.Errorf("probe process group %d: %w", pgid, err)
}
