package daemon

import (
	"context"
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

	"hum/internal/app"
	processpkg "hum/internal/process"
)

func TestRuntimeStateFile(t *testing.T) {
	runtimeDir := filepath.Join(shortRuntimeDir(t), "runtime")
	server := testServer(t, Config{RuntimeDir: runtimeDir, StopGrace: 50 * time.Millisecond})
	paths := server.Paths()

	state := readTestRuntimeState(t, paths.State)
	if state.Version != RuntimeStateVersion || state.Daemon.PID != os.Getpid() || state.Daemon.StartIdentity == "" {
		t.Fatalf("initial runtime state = %+v", state)
	}
	info, err := os.Stat(paths.State)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("runtime state mode = %o, want 600", got)
	}

	root := t.TempDir()
	started, err := server.Supervisor().Start(app.StartRequest{
		Name: "stateful", Root: root, Cwd: root,
		Argv: []string{"/bin/sh", "-c", "trap 'exit 0' TERM; while :; do sleep 1; done"},
		Env:  []string{"PATH=/usr/bin:/bin"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	state = readTestRuntimeState(t, paths.State)
	if len(state.Groups) != 1 || state.Groups[0].LeaderPID != started.PID || state.Groups[0].PGID != started.PGID || state.Groups[0].StartIdentity == "" {
		t.Fatalf("launched runtime groups = %+v, started = %+v", state.Groups, started)
	}
	assertNoRuntimeTemps(t, runtimeDir)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Supervisor().Stop(ctx, root, "stateful"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	state = readTestRuntimeState(t, paths.State)
	if len(state.Groups) != 0 {
		t.Fatalf("runtime groups after exit = %+v, want empty", state.Groups)
	}
	assertNoRuntimeTemps(t, runtimeDir)

	if err := server.Close(); err != nil {
		t.Fatalf("graceful close: %v", err)
	}
	if _, err := os.Stat(paths.State); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime state after graceful close: %v, want not-exist", err)
	}

	if err := os.WriteFile(paths.State, []byte("{broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = NewServer(Config{RuntimeDir: runtimeDir})
	if err == nil || !strings.Contains(err.Error(), paths.State) || !strings.Contains(err.Error(), "remove the file and retry") {
		t.Fatalf("corrupt-state error = %v, want path and operator action", err)
	}
}

func TestLaunchPersistenceFailureStopsChild(t *testing.T) {
	t.Run("confirmed cleanup", func(t *testing.T) {
		runtimeDir := filepath.Join(shortRuntimeDir(t), "runtime")
		server := testServer(t, Config{RuntimeDir: runtimeDir, StopGrace: 50 * time.Millisecond})
		root := t.TempDir()
		if err := os.Chmod(runtimeDir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(runtimeDir, 0o700) })

		_, err := server.Supervisor().Start(app.StartRequest{
			Name: "unpersisted", Root: root, Cwd: root,
			Argv: []string{"/bin/sh", "-c", "trap 'exit 0' TERM; while :; do sleep 1; done"},
			Env:  []string{"PATH=/usr/bin:/bin"},
		})
		if err == nil || !strings.Contains(err.Error(), "runtime state") {
			t.Fatalf("start error = %v, want persistence failure", err)
		}
		items, listErr := server.Supervisor().List(root, false)
		if listErr != nil {
			t.Fatal(listErr)
		}
		if len(items) != 0 {
			t.Fatalf("records after confirmed cleanup = %+v, want none", items)
		}
	})

	t.Run("unconfirmed cleanup blocks duplicate", func(t *testing.T) {
		child := &stuckRuntimeChild{pid: 2147483000, done: make(chan struct{})}
		supervisor, err := app.New(app.Options{
			StopGrace:    10 * time.Millisecond,
			StartProcess: func(processpkg.Spec) (app.Child, error) { return child, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		runtimeDir := filepath.Join(shortRuntimeDir(t), "runtime")
		_ = testServer(t, Config{RuntimeDir: runtimeDir, StopGrace: 10 * time.Millisecond, Supervisor: supervisor})
		root := t.TempDir()
		if err := os.Chmod(runtimeDir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(runtimeDir, 0o700) })

		_, err = supervisor.Start(app.StartRequest{Name: "blocked", Root: root, Cwd: root, Argv: []string{"fake"}})
		if err == nil || !strings.Contains(err.Error(), "runtime state") || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("start error = %v, want persistence and unconfirmed-cleanup errors", err)
		}
		blocked, getErr := supervisor.Get(root, "blocked")
		if getErr != nil || blocked.State != app.StateUnresolved {
			t.Fatalf("unconfirmed cleanup record = %+v, err=%v", blocked, getErr)
		}
		if _, duplicateErr := supervisor.Start(app.StartRequest{Name: "blocked", Root: root, Cwd: root, Argv: []string{"fake"}}); duplicateErr == nil {
			t.Fatal("duplicate start succeeded while unresolved")
		}
	})
}

// stuckRuntimeChild models a group whose terminal cleanup cannot be confirmed.
type stuckRuntimeChild struct {
	pid  int
	done chan struct{}
}

func (c *stuckRuntimeChild) PID() int              { return c.pid }
func (c *stuckRuntimeChild) PGID() int             { return c.pid }
func (c *stuckRuntimeChild) Done() <-chan struct{} { return c.done }
func (c *stuckRuntimeChild) StartIdentity() string { return "test:stuck" }
func (c *stuckRuntimeChild) Wait() processpkg.Result {
	<-c.done
	return processpkg.Result{}
}
func (c *stuckRuntimeChild) Signal(os.Signal) error { return nil }

func TestStartupReclaimsRecordedGroups(t *testing.T) {
	for _, tc := range []struct {
		name       string
		ignoreTERM bool
	}{
		{name: "term"},
		{name: "kill_after_grace", ignoreTERM: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, done := startRuntimeTestGroup(t, tc.ignoreTERM)
			runtimeDir := filepath.Join(shortRuntimeDir(t), "runtime")
			writePriorRuntimeState(t, runtimeDir, RuntimeGroup{
				ProjectRoot: t.TempDir(), Name: tc.name, LeaderPID: cmd.Process.Pid,
				PGID: cmd.Process.Pid, StartIdentity: mustProcessIdentity(t, cmd.Process.Pid),
			})
			server := testServer(t, Config{RuntimeDir: runtimeDir, StopGrace: 50 * time.Millisecond})
			warnings := server.StartupWarnings()
			if len(warnings) != 1 || warnings[0].Outcome != "reclaimed" {
				t.Fatalf("startup warnings = %+v", warnings)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("reclaimed group was not reaped")
			}
		})
	}

	t.Run("stale_dead_entry", func(t *testing.T) {
		runtimeDir := filepath.Join(shortRuntimeDir(t), "runtime")
		writePriorRuntimeState(t, runtimeDir, RuntimeGroup{ProjectRoot: t.TempDir(), Name: "dead", LeaderPID: 2147483646, PGID: 2147483646, StartIdentity: "dead:1"})
		server := testServer(t, Config{RuntimeDir: runtimeDir, StopGrace: 20 * time.Millisecond})
		warnings := server.StartupWarnings()
		if len(warnings) != 1 || warnings[0].Outcome != "reclaimed" {
			t.Fatalf("startup warnings = %+v", warnings)
		}
		if got := readTestRuntimeState(t, server.Paths().State).Groups; len(got) != 0 {
			t.Fatalf("stale groups retained: %+v", got)
		}
	})
}

func TestStartupNeverSignalsReusedProcessIdentity(t *testing.T) {
	cmd, done := startRuntimeTestGroup(t, false)
	runtimeDir := filepath.Join(shortRuntimeDir(t), "runtime")
	root := t.TempDir()
	writePriorRuntimeState(t, runtimeDir, RuntimeGroup{
		ProjectRoot: root, Name: "reused", LeaderPID: cmd.Process.Pid, PGID: cmd.Process.Pid,
		StartIdentity: mustProcessIdentity(t, cmd.Process.Pid) + "-different",
	})
	server := testServer(t, Config{RuntimeDir: runtimeDir, StopGrace: 20 * time.Millisecond})
	warnings := server.StartupWarnings()
	if len(warnings) != 1 || warnings[0].Outcome != "unresolved" {
		t.Fatalf("startup warnings = %+v", warnings)
	}
	if !runtimeGroupAlive(cmd.Process.Pid) {
		t.Fatal("identity-mismatched group was signaled")
	}
	started, err := server.Supervisor().Start(app.StartRequest{Name: "reused", Root: root, Cwd: root, Argv: []string{"/bin/false"}})
	if err == nil && (started.State != app.StateUnresolved || started.PID != cmd.Process.Pid) {
		t.Fatalf("duplicate start = %+v, want original unresolved record", started)
	}
	if err != nil && !errors.Is(err, app.ErrNameInUse) && !errors.Is(err, app.ErrUnresolved) {
		t.Fatalf("duplicate start error = %v", err)
	}

	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("test process was not reaped")
	}
}

func readTestRuntimeState(t *testing.T, path string) RuntimeState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state RuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func assertNoRuntimeTemps(t *testing.T, runtimeDir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(runtimeDir, ".hum-artifact-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("runtime temp files remain: %v", matches)
	}
}

func writePriorRuntimeState(t *testing.T, runtimeDir string, groups ...RuntimeGroup) {
	t.Helper()
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := RuntimeState{Version: RuntimeStateVersion, Daemon: RuntimeIdentity{PID: 2147483646, StartIdentity: "dead:daemon"}, Groups: groups}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(NewRuntimePaths(runtimeDir).State, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func startRuntimeTestGroup(t *testing.T, ignoreTERM bool) (*exec.Cmd, <-chan error) {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "ready")
	trap := "trap 'exit 0' TERM"
	if ignoreTERM {
		trap = "trap '' TERM"
	}
	script := fmt.Sprintf("%s; echo ready > %q; while :; do sleep 1; done", trap, ready)
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			return cmd, done
		}
		if time.Now().After(deadline) {
			t.Fatal("process group did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func mustProcessIdentity(t *testing.T, pid int) string {
	t.Helper()
	identity, err := processpkg.ProcessStartIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}
