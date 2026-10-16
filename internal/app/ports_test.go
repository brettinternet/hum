package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hum/internal/process"
	"hum/internal/testutil"
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
func (c *portsTimedChild) Signal(os.Signal) error { return c.Stop() }
func (c *portsTimedChild) Stop() error {
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

// A context-aware inspector that stalls must return a best-effort snapshot,
// retaining valid listeners rather than failing status or reporting empty success.
func TestPortsStatusInspectionDeadline(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "unavailable", true: "partial"}[partial], func(t *testing.T) {
			root := portsMakeProject(t)
			var observedDeadline bool
			listeners := []process.Port{}
			if partial {
				listeners = append(listeners, process.Port{Transport: "tcp", Address: "127.0.0.1", Port: 43123, PIDs: []int{77}})
			}
			child := &portsTestChild{portsTimedChild: &portsTimedChild{pid: os.Getpid(), done: make(chan struct{})}, inspect: func(ctx context.Context) process.PortsResult {
				_, observedDeadline = ctx.Deadline()
				if !observedDeadline {
					t.Error("inspection context has no deadline")
					return process.PortsResult{}
				}
				<-ctx.Done()
				if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
					t.Errorf("inspection context error = %v", ctx.Err())
				}
				return process.PortsResult{State: process.PortsAvailable, Listeners: listeners}
			}}
			s := portsNewSupervisor(t, Options{StartProcess: func(process.Spec) (Child, error) { return child, nil }})
			if _, err := s.Start(StartRequest{Name: "web", Cwd: root, Argv: []string{"web"}}); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetPortsScoped(context.Background(), ScopeProject, root, "web")
			wantState := process.PortsUnavailable
			if partial {
				wantState = process.PortsPartial
			}
			if err != nil || !observedDeadline || got.Ports == nil || got.Ports.State != wantState || got.Ports.Diagnostic != context.DeadlineExceeded.Error() || !reflect.DeepEqual(got.Ports.Listeners, listeners) {
				t.Fatalf("deadline snapshot = %+v, err=%v", got.Ports, err)
			}
		})
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

func TestPortLookupAcrossScopesAndIncompleteResults(t *testing.T) {
	rootA, rootB := portsMakeProject(t), portsMakeProject(t)
	const queriedPort = 43123
	var inspected atomic.Int32
	children := map[string]*portsTestChild{}
	newChild := func(name string, inspect func() process.PortsResult) *portsTestChild {
		return &portsTestChild{
			portsTimedChild: &portsTimedChild{pid: os.Getpid(), done: make(chan struct{})},
			inspect: func(context.Context) process.PortsResult {
				inspected.Add(1)
				return inspect()
			},
		}
	}
	children["holder-a"] = newChild("holder-a", func() process.PortsResult {
		return process.PortsResult{State: process.PortsAvailable, Listeners: []process.Port{
			{Transport: "tcp", Address: "127.0.0.1", Port: queriedPort, PIDs: []int{101}},
			{Transport: "tcp", Address: "127.0.0.1", Port: 43124, PIDs: []int{101}},
		}}
	})
	children["holder-b"] = newChild("holder-b", func() process.PortsResult {
		return process.PortsResult{State: process.PortsPartial, Diagnostic: "one member was unreadable", Listeners: []process.Port{
			{Transport: "tcp", Address: "::1", Port: queriedPort, PIDs: []int{102}},
		}}
	})
	children["denied"] = newChild("denied", func() process.PortsResult {
		return process.PortsResult{State: process.PortsDenied, Diagnostic: "permission denied", Listeners: []process.Port{}}
	})
	children["unavailable"] = newChild("unavailable", func() process.PortsResult {
		return process.PortsResult{State: process.PortsUnavailable, Diagnostic: "inspector unavailable", Listeners: []process.Port{}}
	})
	children["partial"] = newChild("partial", func() process.PortsResult {
		return process.PortsResult{State: process.PortsPartial, Diagnostic: "one member was unreadable", Listeners: []process.Port{{Transport: "tcp", Address: "127.0.0.1", Port: 43124, PIDs: []int{103}}}}
	})
	children["miss"] = newChild("miss", func() process.PortsResult {
		return process.PortsResult{State: process.PortsAvailable, Listeners: []process.Port{}}
	})
	children["stopped"] = newChild("stopped", func() process.PortsResult {
		return process.PortsResult{State: process.PortsAvailable, Listeners: []process.Port{{Transport: "tcp", Address: "127.0.0.1", Port: queriedPort, PIDs: []int{104}}}}
	})
	children["terminal"] = newChild("terminal", func() process.PortsResult {
		return process.PortsResult{State: process.PortsAvailable, Listeners: []process.Port{{Transport: "tcp", Address: "127.0.0.1", Port: queriedPort, PIDs: []int{105}}}}
	})
	s := portsNewSupervisor(t, Options{StartProcess: func(spec process.Spec) (Child, error) {
		return children[spec.Argv[0]], nil
	}})
	start := func(name, scope, root string) {
		t.Helper()
		if _, err := s.Start(StartRequest{Name: name, Scope: scope, Cwd: root, Argv: []string{name}}); err != nil {
			t.Fatal(err)
		}
	}
	start("holder-a", ScopeProject, rootA)
	start("holder-b", ScopeProject, rootB)
	start("denied", ScopeGlobal, rootA)
	start("unavailable", ScopeProject, rootA)
	start("partial", ScopeProject, rootB)
	start("miss", ScopeProject, rootA)
	start("stopped", ScopeProject, rootA)
	if err := s.Stop(context.Background(), rootA, "stopped"); err != nil {
		t.Fatal(err)
	}
	start("terminal", ScopeProject, rootB)
	children["terminal"].once.Do(func() { close(children["terminal"].done) })
	if !testutil.WaitUntil(time.Second, func() bool {
		terminal, err := s.GetScoped(ScopeProject, rootB, "terminal")
		return err == nil && terminal.State == StateExited
	}) {
		t.Fatal("terminal record did not reconcile")
	}

	got := s.LookupPort(context.Background(), queriedPort)
	if len(got.Processes) != 2 || got.Processes[0].Name != "holder-a" || got.Processes[1].Name != "holder-b" {
		t.Fatalf("holders = %#v, want holder-a and holder-b", got.Processes)
	}
	for _, holder := range got.Processes {
		if holder.Ports == nil || len(holder.Ports.Listeners) != 1 || holder.Ports.Listeners[0].Port != queriedPort {
			t.Fatalf("holder %q did not retain only queried listener: %#v", holder.Name, holder.Ports)
		}
	}
	if got.Processes[0].Root != rootA || got.Processes[1].Root != rootB || got.Processes[1].Ports.State != process.PortsPartial {
		t.Fatalf("holder scope/root/inspection state mismatch: %#v", got.Processes)
	}
	if len(got.Incomplete) != 3 {
		t.Fatalf("incomplete records = %#v, want denied, unavailable, and non-matching partial", got.Incomplete)
	}
	wantIncomplete := map[string]string{"denied": process.PortsDenied, "unavailable": process.PortsUnavailable, "partial": process.PortsPartial}
	for _, item := range got.Incomplete {
		if wantIncomplete[item.Name] != item.State || item.Diagnostic == "" {
			t.Fatalf("incomplete record = %#v", item)
		}
		delete(wantIncomplete, item.Name)
	}
	if len(wantIncomplete) != 0 {
		t.Fatalf("missing incomplete records: %#v", wantIncomplete)
	}
	if inspected.Load() != 6 {
		t.Fatalf("inspected %d records; stopped and terminal records must not be inspected", inspected.Load())
	}
}

func TestPortLookupProtectsLaunchIdentity(t *testing.T) {
	root := portsMakeProject(t)
	entered, release := make(chan struct{}), make(chan struct{})
	old := &portsTestChild{portsTimedChild: &portsTimedChild{pid: os.Getpid(), done: make(chan struct{})}, inspect: func(context.Context) process.PortsResult {
		close(entered)
		<-release
		return process.PortsResult{State: process.PortsAvailable, Listeners: []process.Port{{Transport: "tcp", Address: "127.0.0.1", Port: 43123, PIDs: []int{os.Getpid()}}}}
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
	result := make(chan PortLookupResult, 1)
	go func() { result <- s.LookupPort(context.Background(), 43123) }()
	<-entered
	if _, err := s.Restart(context.Background(), root, "web"); err != nil {
		t.Fatalf("restart during lookup: %v", err)
	}
	close(release)
	got := <-result
	if len(got.Processes) != 0 || len(got.Incomplete) != 1 || got.Incomplete[0].Name != "web" || got.Incomplete[0].State != process.PortsUnavailable || got.Incomplete[0].Diagnostic != "launch changed during listener inspection" {
		t.Fatalf("lookup returned stale launch: %#v", got)
	}
}

func TestPortLookupDeadlineAndConcurrencyBound(t *testing.T) {
	root := portsMakeProject(t)
	const count, bound = 7, 3
	entered := make(chan struct{}, count)
	release := make(chan struct{})
	children := make(map[string]*portsTestChild, count)
	for index := 0; index < count; index++ {
		name := fmt.Sprintf("worker-%d", index)
		children[name] = &portsTestChild{portsTimedChild: &portsTimedChild{pid: os.Getpid(), done: make(chan struct{})}, inspect: func(context.Context) process.PortsResult {
			entered <- struct{}{}
			<-release
			return process.PortsResult{State: process.PortsAvailable, Listeners: []process.Port{}}
		}}
	}
	var active, maximum atomic.Int32
	s := portsNewSupervisor(t, Options{StartProcess: func(spec process.Spec) (Child, error) {
		child := children[spec.Argv[0]]
		base := child.inspect
		child.inspect = func(ctx context.Context) process.PortsResult {
			current := active.Add(1)
			for observed := maximum.Load(); current > observed && !maximum.CompareAndSwap(observed, current); observed = maximum.Load() {
			}
			defer active.Add(-1)
			return base(ctx)
		}
		return child, nil
	}})
	for name := range children {
		if _, err := s.Start(StartRequest{Name: name, Cwd: root, Argv: []string{name}}); err != nil {
			t.Fatal(err)
		}
	}
	result := make(chan PortLookupResult, 1)
	go func() { result <- s.lookupPortWithBounds(context.Background(), 43123, 100*time.Millisecond, bound) }()
	for index := 0; index < bound; index++ {
		<-entered
	}
	got := <-result
	close(release)
	if maximum.Load() != bound {
		t.Fatalf("maximum concurrent inspections = %d, want %d", maximum.Load(), bound)
	}
	if len(got.Incomplete) != count || len(got.Processes) != 0 {
		t.Fatalf("deadline result = holders %d, incomplete %d; want 0 holders and all %d incomplete", len(got.Processes), len(got.Incomplete), count)
	}
}
