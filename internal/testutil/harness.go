package testutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	pollInterval    = 10 * time.Millisecond
	processKillWait = time.Second
	stdoutFileName  = "stdout"
	stderrFileName  = "stderr"
)

// Result is the captured result of a synchronous command invocation.
type Result struct {
	Stdout string
	Stderr string
	Code   int
	Err    error
}

// Process is a running command whose output can be read while it runs.
type Process struct {
	Cmd *exec.Cmd

	stdoutPath string
	stderrPath string
	done       chan struct{}

	mu      sync.RWMutex
	waitErr error
}

// BuildHum builds the hum command and returns its temporary executable path.
func BuildHum(t testing.TB) string {
	t.Helper()
	return buildBinary(t, "./cmd/hum", "hum")
}

// BuildFixture builds the deterministic fixture command and returns its temporary executable path.
func BuildFixture(t testing.TB) string {
	t.Helper()
	return buildBinary(t, "./internal/testutil/cmd/hum-fixture", "hum-fixture")
}

func buildBinary(t testing.TB, packagePath, name string) string {
	t.Helper()
	root := repositoryRoot(t)
	output := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", output, packagePath)
	cmd.Dir = root
	buildOutput, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, buildOutput)
	}
	return output
}

func repositoryRoot(t testing.TB) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("canonicalize repository root: %v", err)
	}
	return filepath.Clean(root)
}

// RuntimeDir creates a short temporary directory for HUM_RUNTIME_DIR and removes it when the test completes.
func RuntimeDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "h-")
	if err != nil {
		t.Fatalf("create runtime directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// RuntimeEnv copies the current environment, sets HUM_RUNTIME_DIR, and applies overrides.
func RuntimeEnv(runtimeDir string, overrides ...string) []string {
	updates := make([]string, 0, len(overrides)+1)
	updates = append(updates, "HUM_RUNTIME_DIR="+runtimeDir)
	updates = append(updates, overrides...)

	base := os.Environ()
	env := make([]string, 0, len(base)+len(updates))
	positions := make(map[string]int, len(base)+len(updates))
	for _, entry := range base {
		key, _, hasValue := strings.Cut(entry, "=")
		if !hasValue {
			env = append(env, entry)
			continue
		}
		if index, ok := positions[key]; ok {
			env[index] = entry
			continue
		}
		positions[key] = len(env)
		env = append(env, entry)
	}
	for _, update := range updates {
		key, _, hasValue := strings.Cut(update, "=")
		if !hasValue {
			env = append(env, update)
			continue
		}
		if index, ok := positions[key]; ok {
			env[index] = update
			continue
		}
		positions[key] = len(env)
		env = append(env, update)
	}
	return env
}

// Run executes a command synchronously and captures both output streams.
func Run(t testing.TB, binary, cwd string, env []string, args ...string) Result {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	cmd.Env = copyEnv(env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   commandCode(err),
		Err:    err,
	}
}

// Start launches a command and captures output in files for safe live polling.
func Start(t testing.TB, binary, cwd string, env []string, args ...string) *Process {
	t.Helper()
	outputDir := t.TempDir()
	stdoutPath := filepath.Join(outputDir, stdoutFileName)
	stderrPath := filepath.Join(outputDir, stderrFileName)
	stdoutFile, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("create stdout capture: %v", err)
	}
	stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_ = stdoutFile.Close()
		t.Fatalf("create stderr capture: %v", err)
	}

	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	cmd.Env = copyEnv(env)
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	if err := cmd.Start(); err != nil {
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
		t.Fatalf("start %s: %v", binary, err)
	}
	_ = stdoutFile.Close()
	_ = stderrFile.Close()

	process := &Process{
		Cmd:        cmd,
		stdoutPath: stdoutPath,
		stderrPath: stderrPath,
		done:       make(chan struct{}),
	}
	go process.wait()
	t.Cleanup(process.cleanup)
	return process
}

func copyEnv(env []string) []string {
	if env == nil {
		return nil
	}
	copied := make([]string, len(env))
	copy(copied, env)
	return copied
}

func commandCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func (p *Process) wait() {
	err := p.Cmd.Wait()
	p.mu.Lock()
	p.waitErr = err
	p.mu.Unlock()
	close(p.done)
}

// Stdout returns all stdout captured so far.
func (p *Process) Stdout() string {
	if p == nil {
		return ""
	}
	data, err := os.ReadFile(p.stdoutPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// Stderr returns all stderr captured so far.
func (p *Process) Stderr() string {
	if p == nil {
		return ""
	}
	data, err := os.ReadFile(p.stderrPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// Wait waits at most timeout for the process, killing it if it remains unfinished.
func (p *Process) Wait(timeout time.Duration) error {
	if p == nil {
		return errors.New("nil process")
	}
	if p.Cmd == nil {
		return errors.New("process has no command")
	}
	if !waitDone(p.done, timeout) {
		timeoutErr := fmt.Errorf("timed out waiting for process after %s", timeout)
		if killErr := p.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			return fmt.Errorf("%w: kill process: %v", timeoutErr, killErr)
		}
		_ = waitDone(p.done, processKillWait)
		return timeoutErr
	}
	return p.waitError()
}

// Exited reports whether the process has been reaped.
func (p *Process) Exited() bool {
	if p == nil || p.Cmd == nil {
		return true
	}
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// Signal sends signal to the process.
func (p *Process) Signal(signal os.Signal) error {
	if p == nil || p.Cmd == nil || p.Cmd.Process == nil {
		return errors.New("process has not started")
	}
	if signal == nil {
		return errors.New("nil signal")
	}
	return p.Cmd.Process.Signal(signal)
}

// Kill terminates the process if it is still running.
func (p *Process) Kill() error {
	if p == nil || p.Cmd == nil || p.Cmd.Process == nil {
		return nil
	}
	err := p.Cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (p *Process) waitError() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.waitErr
}

func (p *Process) cleanup() {
	if p == nil || p.Exited() {
		return
	}
	_ = p.Kill()
	_ = waitDone(p.done, processKillWait)
}

func waitDone(done <-chan struct{}, timeout time.Duration) bool {
	if timeout <= 0 {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// WaitForFile waits for path to appear, failing the test if it does not.
func WaitForFile(t testing.TB, path string, timeout time.Duration) {
	t.Helper()
	if waitForCondition(timeout, func() bool {
		_, err := os.Stat(path)
		return err == nil
	}) {
		return
	}
	t.Fatalf("timed out waiting for file %q after %s", path, timeout)
}

// WaitForText waits until text appears in path, failing the test if it does not.
func WaitForText(t testing.TB, path, text string, timeout time.Duration) {
	t.Helper()
	if waitForCondition(timeout, func() bool {
		data, err := os.ReadFile(path)
		return err == nil && bytes.Contains(data, []byte(text))
	}) {
		return
	}
	t.Fatalf("timed out waiting for text %q in %q after %s", text, path, timeout)
}

// ProcessAlive reports whether pid currently exists and can be probed.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// WaitForProcessGone waits until pid no longer exists, failing the test on timeout.
func WaitForProcessGone(t testing.TB, pid int, timeout time.Duration) {
	t.Helper()
	if pid <= 0 {
		t.Fatalf("invalid process ID %d", pid)
	}
	if waitForCondition(timeout, func() bool { return !ProcessAlive(pid) }) {
		return
	}
	t.Fatalf("process %d remained alive after %s", pid, timeout)
}

// WaitForProcessGroupGone waits until pgid no longer exists, failing the test on timeout.
func WaitForProcessGroupGone(t testing.TB, pgid int, timeout time.Duration) {
	t.Helper()
	if pgid <= 0 {
		t.Fatalf("invalid process-group ID %d", pgid)
	}
	if waitForCondition(timeout, func() bool { return !processGroupAlive(pgid) }) {
		return
	}
	t.Fatalf("process group %d remained alive after %s", pgid, timeout)
}

func processGroupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func waitForCondition(timeout time.Duration, condition func() bool) bool {
	if condition() {
		return true
	}
	if timeout <= 0 {
		return false
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	poll := time.NewTicker(pollInterval)
	defer poll.Stop()
	for {
		select {
		case <-poll.C:
			if condition() {
				return true
			}
		case <-deadline.C:
			return condition()
		}
	}
}
