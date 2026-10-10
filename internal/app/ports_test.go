package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hum/internal/process"
)

type portsTimedChild struct {
	pid  int
	done chan struct{}
	once sync.Once
}

func (c *portsTimedChild) PID() int              { return c.pid }
func (c *portsTimedChild) PGID() int             { return c.pid }
func (c *portsTimedChild) Done() <-chan struct{} { return c.done }
func (c *portsTimedChild) Wait() process.Result {
	<-c.done
	return process.Result{}
}
func (c *portsTimedChild) Signal(os.Signal) error {
	c.once.Do(func() { close(c.done) })
	return os.ErrProcessDone
}

func portsMakeProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func portsNewSupervisor(t *testing.T, options Options) *Supervisor {
	t.Helper()
	supervisor, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = supervisor.Shutdown(ctx)
	})
	return supervisor
}

type portsTestChild struct {
	*portsTimedChild
	inspect func(context.Context) process.PortsResult
}

func (c *portsTestChild) InspectPorts(ctx context.Context) process.PortsResult {
	return c.inspect(ctx)
}

func TestPortsStatusIsOptInAndPreservesInspectionOutcomes(t *testing.T) {
	root := portsMakeProject(t)
	var calls atomic.Int32
	child := &portsTestChild{portsTimedChild: &portsTimedChild{pid: os.Getpid(), done: make(chan struct{})}, inspect: func(context.Context) process.PortsResult {
		calls.Add(1)
		return process.PortsResult{State: process.PortsPartial, Diagnostic: "one member was unreadable", Listeners: []process.Port{{Transport: "tcp", Address: "127.0.0.1", Port: 43123, PIDs: []int{77}}}}
	}}
	s := portsNewSupervisor(t, Options{StartProcess: func(process.Spec) (Child, error) { return child, nil }})
	if _, err := s.Start(StartRequest{Name: "web", Cwd: root, Argv: []string{"web"}}); err != nil {
		t.Fatal(err)
	}
	ordinary, err := s.GetScoped(ScopeProject, root, "web")
	if err != nil {
		t.Fatal(err)
	}
	if ordinary.Ports != nil || calls.Load() != 0 {
		t.Fatalf("ordinary status inspected ports: process=%+v calls=%d", ordinary, calls.Load())
	}
	before := ordinary.State
	inspected, err := s.GetPortsScoped(context.Background(), ScopeProject, root, "web")
	if err != nil {
		t.Fatal(err)
	}
	if inspected.Ports == nil || inspected.Ports.State != process.PortsPartial || inspected.Ports.Diagnostic != "one member was unreadable" || len(inspected.Ports.Listeners) != 1 {
		t.Fatalf("ports status lost partial result: %+v", inspected.Ports)
	}
	if inspected.State != before || calls.Load() != 1 {
		t.Fatalf("ports inspection changed lifecycle state %q -> %q or ran %d times", before, inspected.State, calls.Load())
	}

	for _, state := range []string{process.PortsAvailable, process.PortsDenied, process.PortsUnavailable} {
		t.Run(state, func(t *testing.T) {
			expected := process.PortsResult{State: state, Listeners: []process.Port{}}
			if state != process.PortsAvailable {
				expected.Diagnostic = "inspection could not read listeners"
			}
			child.inspect = func(context.Context) process.PortsResult { return expected }
			got, err := s.GetPortsScoped(context.Background(), ScopeProject, root, "web")
			if err != nil || got.Ports == nil || !reflect.DeepEqual(*got.Ports, expected) || got.State != before {
				t.Fatalf("inspection outcome = %+v, err=%v; want %+v, lifecycle %s", got, err, expected, before)
			}
		})
	}

	child.inspect = func(ctx context.Context) process.PortsResult {
		if ctx.Err() == nil {
			t.Error("ports inspector received a live context after caller cancellation")
		}
		return process.PortsResult{State: process.PortsUnavailable, Diagnostic: ctx.Err().Error(), Listeners: []process.Port{}}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := s.GetPortsScoped(cancelled, ScopeProject, root, "web")
	if err != nil || got.Ports == nil || got.Ports.State != process.PortsUnavailable {
		t.Fatalf("cancelled inspection = %+v, err=%v", got.Ports, err)
	}
}

func TestPortsStatusDoesNotInspectStoppedOrMissingProcesses(t *testing.T) {
	root := portsMakeProject(t)
	var calls atomic.Int32
	child := &portsTestChild{portsTimedChild: &portsTimedChild{pid: os.Getpid(), done: make(chan struct{})}, inspect: func(context.Context) process.PortsResult {
		calls.Add(1)
		return process.PortsResult{State: process.PortsAvailable, Listeners: []process.Port{}}
	}}
	s := portsNewSupervisor(t, Options{StartProcess: func(process.Spec) (Child, error) { return child, nil }})
	if _, err := s.Start(StartRequest{Name: "web", Cwd: root, Argv: []string{"web"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(context.Background(), root, "web"); err != nil {
		t.Fatal(err)
	}
	stopped, err := s.GetPortsScoped(context.Background(), ScopeProject, root, "web")
	if err != nil || stopped.State != StateStopped || stopped.Ports != nil {
		t.Fatalf("stopped ports status = %+v, err=%v", stopped, err)
	}
	if _, err := s.GetPortsScoped(context.Background(), ScopeProject, root, "missing"); err == nil {
		t.Fatal("missing process status unexpectedly succeeded")
	}
	if calls.Load() != 0 {
		t.Fatalf("inspector ran %d times for stopped or missing process", calls.Load())
	}
}

func TestPortsStatusDoesNotReturnOldLaunchAfterRestart(t *testing.T) {
	root := portsMakeProject(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	old := &portsTestChild{portsTimedChild: &portsTimedChild{pid: os.Getpid(), done: make(chan struct{})}, inspect: func(context.Context) process.PortsResult {
		close(entered)
		<-release
		return process.PortsResult{State: process.PortsAvailable, Listeners: []process.Port{{Transport: "tcp", Address: "127.0.0.1", Port: 40001, PIDs: []int{os.Getpid()}}}}
	}}
	replacement := &portsTestChild{portsTimedChild: &portsTimedChild{pid: os.Getpid(), done: make(chan struct{})}, inspect: func(context.Context) process.PortsResult {
		return process.PortsResult{State: process.PortsAvailable, Listeners: []process.Port{}}
	}}
	launches := 0
	s := portsNewSupervisor(t, Options{StartProcess: func(process.Spec) (Child, error) {
		launches++
		if launches == 1 {
			return old, nil
		}
		return replacement, nil
	}})
	if _, err := s.Start(StartRequest{Name: "web", Cwd: root, Argv: []string{"web"}}); err != nil {
		t.Fatal(err)
	}
	result := make(chan Process, 1)
	failure := make(chan error, 1)
	go func() {
		process, err := s.GetPortsScoped(context.Background(), ScopeProject, root, "web")
		if err != nil {
			failure <- err
			return
		}
		result <- process
	}()
	<-entered
	if _, err := s.Restart(context.Background(), root, "web"); err != nil {
		t.Fatalf("restart while inspection was outside the registry lock: %v", err)
	}
	close(release)
	select {
	case err := <-failure:
		t.Fatal(err)
	case snapshot := <-result:
		if snapshot.Ports == nil || snapshot.Ports.State != process.PortsUnavailable || len(snapshot.Ports.Listeners) != 0 {
			t.Fatalf("stale launch ports returned after restart: %+v", snapshot.Ports)
		}
		if !reflect.DeepEqual(snapshot.Ports.Listeners, []process.Port{}) {
			t.Fatalf("stale endpoints retained: %#v", snapshot.Ports.Listeners)
		}
	}
}
