package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"hum/internal/process"
)

func TestWindowsAppHelper(t *testing.T) {
	if os.Getenv("HUM_APP_WINDOWS_HELPER") != "1" {
		return
	}
	if len(os.Args) > 3 && os.Args[len(os.Args)-1] == "exit" {
		os.Exit(27)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func windowsAppFixture(t *testing.T, mode string) (string, []string, []string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	return root, []string{binary, "-test.run=TestWindowsAppHelper", "--", mode}, []string{"HUM_APP_WINDOWS_HELPER=1"}
}

func TestWindowsAppStopRejectsSignalAndSupportsTTY(t *testing.T) {
	root, argv, env := windowsAppFixture(t, "block")
	s, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer s.Shutdown(ctx)
	tty, err := s.Start(StartRequest{Name: "tty", Cwd: root, Argv: argv, Env: env, TTY: true})
	if err != nil || !tty.TTY || tty.PID <= 0 {
		t.Fatalf("TTY start = %+v, %v; want running TTY", tty, err)
	}
	if err := s.Stop(ctx, root, "tty"); err != nil {
		t.Fatalf("stop TTY child: %v", err)
	}
	started, err := s.Start(StartRequest{Name: "fixture", Cwd: root, Argv: argv, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if started.PID <= 0 {
		t.Fatalf("missing child PID: %+v", started)
	}
	if err := s.Signal(root, "fixture", syscall.SIGTERM); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("Unix signal = %v; want explicit unsupported error", err)
	}
	if err := s.Stop(ctx, root, "fixture"); err != nil {
		t.Fatalf("stop owned tree: %v", err)
	}
	finished, err := s.Get(root, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	// An operator-stopped snapshot intentionally suppresses exit details.
	if finished.State != StateStopped || finished.Exit != nil {
		t.Fatalf("operator stop snapshot = %+v", finished)
	}
}

// TestWindowsTTYRejectsOversizedConsoleBeforeRetaining keeps a size ConPTY
// cannot apply out of the lease, so it cannot block the next launch.
func TestWindowsTTYRejectsOversizedConsoleBeforeRetaining(t *testing.T) {
	root, argv, env := windowsAppFixture(t, "block")
	s, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer s.Shutdown(ctx)
	started, err := s.Start(StartRequest{Name: "tty", Cwd: root, Argv: argv, Env: env, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireInput(root, "tty", true, TTYSize{Columns: 32768, Rows: 24}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("oversized attach = %v, want invalid request", err)
	}
	lease, err := s.AcquireInput(root, "tty", true, TTYSize{Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if err := lease.Resize(started.LaunchCursor, 32768, 24); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("oversized resize = %v, want invalid request", err)
	}
	if _, err := s.Restart(ctx, root, "tty"); err != nil {
		t.Fatalf("restart after rejected resize: %v", err)
	}
}

func TestWindowsShutdownDuringLaunchStopsOrphan(t *testing.T) {
	root, argv, env := windowsAppFixture(t, "block")
	started := make(chan struct{})
	release := make(chan struct{})
	var child *process.Child
	s, err := New(Options{StartProcess: func(spec process.Spec) (Child, error) {
		c, err := process.Start(spec)
		if err != nil {
			return nil, err
		}
		child = c
		close(started)
		<-release
		return c, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	launchDone := make(chan error, 1)
	go func() {
		_, err := s.Start(StartRequest{Name: "orphan", Cwd: root, Argv: argv, Env: env})
		launchDone <- err
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("launch never reached publication boundary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- s.Shutdown(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.RLock()
		closed := s.closed
		s.mu.RUnlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("shutdown did not close registry")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	select {
	case err := <-launchDone:
		if !errors.Is(err, ErrSupervisorClosed) {
			t.Errorf("orphan launch = %v, want closed", err)
		}
	case <-ctx.Done():
		t.Fatal("orphan launch hung")
	}
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("shutdown hung")
	}
	select {
	case <-child.Done():
	case <-ctx.Done():
		t.Fatal("orphan child survived shutdown")
	}
}

func TestWindowsReadinessProbeExitAndCancellation(t *testing.T) {
	root, argv, env := windowsAppFixture(t, "exit")
	message, err := runReadinessProbe(context.Background(), argv, root, env, 1024)
	if err == nil || !strings.Contains(message, "27") {
		t.Fatalf("probe result (%q, %v), want exit 27", message, err)
	}
	_, argv, env = windowsAppFixture(t, "block")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = runReadinessProbe(ctx, argv, root, env, 1024)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled probe = %v", err)
	}
}

type refusedStopChild struct {
	done chan struct{}
	once sync.Once
}

func (c *refusedStopChild) PID() int               { return 4242 }
func (c *refusedStopChild) PGID() int              { return 4242 }
func (c *refusedStopChild) Done() <-chan struct{}  { return c.done }
func (c *refusedStopChild) Signal(os.Signal) error { return errors.New("unsupported") }
func (c *refusedStopChild) Stop() error            { return errors.New("job ownership cannot be proven") }
func (c *refusedStopChild) exit()                  { c.once.Do(func() { close(c.done) }) }
func (c *refusedStopChild) Wait() process.Result {
	<-c.done
	return process.Result{ExitCode: 3, ExitedAt: time.Now()}
}

func TestWindowsRefusedStopKeepsAutonomousExit(t *testing.T) {
	root, argv, env := windowsAppFixture(t, "block")
	child := &refusedStopChild{done: make(chan struct{})}
	s, err := New(Options{StartProcess: func(process.Spec) (Child, error) { return child, nil }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer s.Shutdown(ctx)
	if _, err := s.Start(StartRequest{Name: "refused", Cwd: root, Argv: argv, Env: env}); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(ctx, root, "refused"); err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("refused stop = %v; want ownership error", err)
	}
	child.exit()
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := s.Get(root, "refused")
		if err != nil {
			t.Fatal(err)
		}
		if current.State != StateRunning {
			if current.State != StateExited || current.ExitCode != 3 {
				t.Fatalf("autonomous exit after refused stop = %+v; want exited with code 3", current)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child exit was never reconciled: %+v", current)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
