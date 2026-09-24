package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
	managedprocess "hum/internal/process"
	"hum/internal/protocol"
)

func TestDownProjectSelectionAndDeclaredEntries(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	otherRoot := t.TempDir()
	server, runtimeDir := stopShutdownTestServer(t, 500*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, projectRoot, `version: 1
processes:
  alpha:
    argv: [/bin/sh, -c, "sleep 30"]
  bravo:
    argv: [/bin/sh, -c, "sleep 30"]
`)

	downStartProcess(t, server, projectRoot, "alpha", "manifest", []string{"/bin/sh", "-c", "sleep 30"})
	downStartProcess(t, server, projectRoot, "charlie", "ad_hoc", []string{"/bin/sh", "-c", "sleep 30"})
	downStartProcess(t, server, otherRoot, "other", "ad_hoc", []string{"/bin/sh", "-c", "sleep 30"})

	stdout, stderr, err := stopShutdownRun(t, "down")
	if err != nil {
		t.Fatalf("down: %v", err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if want := "alpha stopped\nbravo not running\ncharlie stopped\n"; stdout != want {
		t.Fatalf("down output = %q, want %q", stdout, want)
	}

	active := stopShutdownListActive(t, server, projectRoot)
	if len(active) != 0 {
		t.Fatalf("current-project processes after down = %#v, want none", active)
	}
	otherActive := stopShutdownListActive(t, server, otherRoot)
	if len(otherActive) != 1 || otherActive[0].Name != "other" {
		t.Fatalf("other-project processes after down = %#v, want only other", otherActive)
	}
}

func TestDownJSONResultsAreStable(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 500*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, projectRoot, `version: 1
processes:
  alpha:
    argv: [/bin/sh, -c, "sleep 30"]
  bravo:
    argv: [/bin/sh, -c, "sleep 30"]
`)

	downStartProcess(t, server, projectRoot, "alpha", "manifest", []string{"/bin/sh", "-c", "sleep 30"})
	downStartProcess(t, server, projectRoot, "charlie", "ad_hoc", []string{"/bin/sh", "-c", "sleep 30"})

	stdout, stderr, err := stopShutdownRun(t, "down", "--json")
	if err != nil {
		t.Fatalf("down --json: %v", err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	results := stopShutdownDecodeResults(t, stdout)
	want := []stopShutdownJSONResult{
		{Name: "alpha", Status: "stopped"},
		{Name: "bravo", Status: "not_running"},
		{Name: "charlie", Status: "stopped"},
	}
	if len(results) != len(want) {
		t.Fatalf("down JSON result count = %d, want %d: %q", len(results), len(want), stdout)
	}
	for i := range want {
		if results[i] != want[i] {
			t.Errorf("down JSON result %d = %+v, want %+v", i, results[i], want[i])
		}
	}
}

func TestDownNoDaemonDoesNotCreateRuntimeState(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	canonicalRoot := projectDirCanonical(t, projectRoot)
	runtimeDir := t.TempDir()
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, projectRoot, `version: 1
processes:
  declared:
    argv: [/bin/sh, -c, "sleep 30"]
`)

	stdout, stderr, err := stopShutdownRun(t, "down")
	if err != nil {
		t.Fatalf("down without daemon: %v", err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if want := fmt.Sprintf("Nothing is running in %s. Use hum list --all to see every scope.\n", canonicalRoot); stdout != want {
		t.Fatalf("down without daemon output = %q, want %q", stdout, want)
	}
	assertDownRuntimeAbsent(t, runtimeDir)

	stdout, stderr, err = stopShutdownRun(t, "down", "--json")
	if err != nil {
		t.Fatalf("down --json without daemon: %v", err)
	}
	if stderr != "" || stdout != "" {
		t.Fatalf("down --json without daemon output = %q stderr = %q, want no output", stdout, stderr)
	}
	assertDownRuntimeAbsent(t, runtimeDir)
}

func TestDownMalformedManifestWithoutDaemonIsNoop(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	canonicalRoot := projectDirCanonical(t, projectRoot)
	runtimeDir := t.TempDir()
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, projectRoot, "version: [\n")

	stdout, stderr, err := stopShutdownRun(t, "down")
	if err != nil {
		t.Fatalf("down with malformed manifest and no daemon: %v", err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if want := fmt.Sprintf("Nothing is running in %s. Use hum list --all to see every scope.\n", canonicalRoot); stdout != want {
		t.Fatalf("down with malformed manifest and no daemon output = %q, want %q", stdout, want)
	}
	assertDownRuntimeAbsent(t, runtimeDir)
}

func TestDownAvailableNoopSuccess(t *testing.T) {
	canonicalRoot := projectDirCanonical(t, stopShutdownTestProject(t))
	server, runtimeDir := stopShutdownTestServer(t, 500*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	stdout, stderr, err := stopShutdownRun(t, "down")
	if err != nil {
		t.Fatalf("down with no records: %v", err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if want := fmt.Sprintf("Nothing is running in %s. Use hum list --all to see every scope.\n", canonicalRoot); stdout != want {
		t.Fatalf("down with no records output = %q, want %q", stdout, want)
	}
	if _, err := os.Stat(server.Paths().Socket); err != nil {
		t.Fatalf("daemon socket after no-op down: %v", err)
	}

	stdout, stderr, err = stopShutdownRun(t, "down", "--json")
	if err != nil {
		t.Fatalf("down --json with no records: %v", err)
	}
	if stderr != "" || stdout != "" {
		t.Fatalf("down --json with no records output = %q stderr = %q, want no output", stdout, stderr)
	}
}

func TestDownReportsRealStopFailure(t *testing.T) {
	child := newDownTestChild(8001, 0, errors.New("graceful stop failed"))
	supervisor := downTestSupervisor(t, map[string]*downTestChild{"broken": child})
	server, runtimeDir := downTestServer(t, supervisor)
	projectRoot := stopShutdownTestProject(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	downStartProcess(t, server, projectRoot, "broken", "ad_hoc", []string{"broken"})

	stdout, stderr, err := stopShutdownRun(t, "down")
	if err == nil {
		t.Fatal("down unexpectedly succeeded after a real stop failure")
	}
	if !strings.Contains(err.Error(), "graceful stop failed") {
		t.Fatalf("down error = %v, want graceful stop failure", err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if want := "broken error: graceful stop failed\n"; stdout != want {
		t.Fatalf("down failure output = %q, want %q", stdout, want)
	}
	if got := child.termCalls.Load(); got != 1 {
		t.Fatalf("TERM calls = %d, want one", got)
	}
	if got := child.killCalls.Load(); got != 0 {
		t.Fatalf("KILL calls = %d, want none after immediate failure", got)
	}
}

func TestDownStopTransportFailureIsError(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	runtimeDir, err := os.MkdirTemp("/tmp", "h-dn-")
	if err != nil {
		t.Fatalf("create runtime directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	paths := daemon.NewRuntimePaths(runtimeDir)
	listener, err := net.Listen("unix", paths.Socket)
	if err != nil {
		t.Fatalf("listen for refusing down daemon: %v", err)
	}
	served := make(chan error, 1)
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case serveErr := <-served:
			if serveErr != nil {
				t.Errorf("refusing down daemon: %v", serveErr)
			}
		case <-time.After(time.Second):
			t.Error("refusing down daemon did not finish")
		}
	})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			served <- acceptErr
			return
		}
		defer conn.Close()
		decoder := protocol.NewDecoder(conn)
		encoder := protocol.NewEncoder(conn)
		if _, decodeErr := decoder.DecodeRequest(); decodeErr != nil {
			served <- decodeErr
			return
		}
		if encodeErr := encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}); encodeErr != nil {
			served <- encodeErr
			return
		}
		if _, decodeErr := decoder.DecodeRequest(); decodeErr != nil {
			served <- decodeErr
			return
		}
		if closeErr := listener.Close(); closeErr != nil {
			served <- closeErr
			return
		}
		processes := []protocol.Process{{
			Name:  "broken",
			Root:  projectRoot,
			Cwd:   projectRoot,
			State: string(app.StateRunning),
		}}
		served <- encoder.EncodeResponse(protocol.NewListResponse(processes))
	}()

	stdout, stderr, err := stopShutdownRun(t, "down", "--json")
	if err == nil {
		t.Fatal("down unexpectedly succeeded after a refused stop connection")
	}
	if !daemonUnavailable(err) {
		t.Fatalf("down error = %v, want unavailable transport error", err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	results := stopShutdownDecodeResults(t, stdout)
	if len(results) != 1 {
		t.Fatalf("down result count = %d, want one error result: %q", len(results), stdout)
	}
	if got := results[0]; got.Name != "broken" || got.Status != "error" {
		t.Fatalf("down result = %+v, want broken error", got)
	}
}

func TestDownStopsDependentsFirst(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	concurrency := &downTestConcurrency{}
	events := make(chan downLifecycleEvent, 16)
	children := map[string]*downTestChild{
		"db":     newDownTestChild(8201, 120*time.Millisecond, nil),
		"api":    newDownTestChild(8202, 120*time.Millisecond, nil),
		"web":    newDownTestChild(8203, 120*time.Millisecond, nil),
		"worker": newDownTestChild(8204, 120*time.Millisecond, nil),
		"adhoc":  newDownTestChild(8205, 120*time.Millisecond, nil),
	}
	for name, child := range children {
		child.concurrency = concurrency
		child.onTerm = func(name string) func() {
			return func() { events <- downLifecycleEvent{name: name, kind: "request"} }
		}(name)
		child.onDone = func(name string) func() {
			return func() { events <- downLifecycleEvent{name: name, kind: "complete"} }
		}(name)
	}
	supervisor := downTestSupervisor(t, children)
	server, runtimeDir := downTestServer(t, supervisor)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, projectRoot, `version: 1
processes:
  db:
    argv: [db]
    ready: {match: "ready"}
  api:
    argv: [api]
    after: [db]
    ready: {match: "ready"}
  web:
    argv: [web]
    after: [api]
    ready: {match: "ready"}
  idle:
    argv: [idle]
    after: [db]
  worker:
    argv: [worker]
`)
	for _, name := range []string{"db", "api", "web", "worker"} {
		downStartProcess(t, server, projectRoot, name, "manifest", []string{name})
	}
	downStartProcess(t, server, projectRoot, "adhoc", "ad_hoc", []string{"adhoc"})

	stdout, stderr, err := stopShutdownRun(t, "down", "--json")
	if err != nil {
		t.Fatalf("down --json: %v", err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	results := stopShutdownDecodeResults(t, stdout)
	wantResults := []stopShutdownJSONResult{
		{Name: "adhoc", Status: "stopped"},
		{Name: "api", Status: "stopped"},
		{Name: "db", Status: "stopped"},
		{Name: "idle", Status: "not_running"},
		{Name: "web", Status: "stopped"},
		{Name: "worker", Status: "stopped"},
	}
	if len(results) != len(wantResults) {
		t.Fatalf("down results = %#v, want lexical %#v", results, wantResults)
	}
	for index := range wantResults {
		if results[index] != wantResults[index] {
			t.Errorf("down result %d = %#v, want %#v", index, results[index], wantResults[index])
		}
	}

	gotEvents := make([]downLifecycleEvent, 0, len(children)*2)
	for range len(children) * 2 {
		select {
		case event := <-events:
			gotEvents = append(gotEvents, event)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out collecting stop events: %#v", gotEvents)
		}
	}
	positions := make(map[string]map[string]int, len(children))
	for index, event := range gotEvents {
		if positions[event.name] == nil {
			positions[event.name] = make(map[string]int)
		}
		positions[event.name][event.kind] = index
	}
	apiRequest, apiRequested := positions["api"]["request"]
	apiComplete, apiCompleted := positions["api"]["complete"]
	dbRequest, dbRequested := positions["db"]["request"]
	if !apiRequested || !apiCompleted || !dbRequested {
		t.Fatalf("dependency stop events missing: %#v", gotEvents)
	}
	for _, name := range []string{"adhoc", "web", "worker"} {
		_, requested := positions[name]["request"]
		complete, completed := positions[name]["complete"]
		if !requested || !completed {
			t.Fatalf("first-wave stop events missing for %q: %#v", name, gotEvents)
		}
		if apiRequest <= complete {
			t.Errorf("api stop request at %d preceded %s completion at %d: %#v", apiRequest, name, complete, gotEvents)
		}
	}
	if dbRequest <= apiComplete {
		t.Errorf("db stop request at %d preceded api completion at %d: %#v", dbRequest, apiComplete, gotEvents)
	}
	if got := concurrency.maximum.Load(); got < 3 {
		t.Errorf("maximum concurrent stop signals = %d, want at least three in first wave", got)
	}
	if _, stopped := positions["idle"]; stopped {
		t.Errorf("inactive declaration was stopped: %#v", gotEvents)
	}
}

type downLifecycleEvent struct {
	name string
	kind string
}

func TestDownStopsProcessesConcurrentlyWithIndependentConnections(t *testing.T) {
	const delay = 300 * time.Millisecond
	concurrency := &downTestConcurrency{}
	children := map[string]*downTestChild{
		"alpha": newDownTestChild(8101, delay, nil),
		"beta":  newDownTestChild(8102, delay, nil),
	}
	for _, child := range children {
		child.concurrency = concurrency
	}
	supervisor := downTestSupervisor(t, children)
	server, runtimeDir := downTestServer(t, supervisor)
	projectRoot := stopShutdownTestProject(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	for _, name := range []string{"alpha", "beta"} {
		downStartProcess(t, server, projectRoot, name, "ad_hoc", []string{name})
	}

	stdout, stderr, err := stopShutdownRun(t, "down")
	if err != nil {
		t.Fatalf("down: %v", err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if want := "alpha stopped\nbeta stopped\n"; stdout != want {
		t.Fatalf("down output = %q, want %q", stdout, want)
	}
	if got := concurrency.maximum.Load(); got != int32(len(children)) {
		t.Fatalf("maximum concurrent stop signals = %d, want %d", got, len(children))
	}
	for name, child := range children {
		if got := child.termCalls.Load(); got != 1 {
			t.Errorf("%s TERM calls = %d, want one", name, got)
		}
		if got := child.killCalls.Load(); got != 0 {
			t.Errorf("%s KILL calls = %d, want none after graceful stop", name, got)
		}
	}
}

type downTestConcurrency struct {
	current atomic.Int32
	maximum atomic.Int32
}

func (c *downTestConcurrency) enter() func() {
	current := c.current.Add(1)
	for maximum := c.maximum.Load(); current > maximum && !c.maximum.CompareAndSwap(maximum, current); maximum = c.maximum.Load() {
	}
	return func() { c.current.Add(-1) }
}

type downTestChild struct {
	pid         int
	pgid        int
	delay       time.Duration
	stopErr     error
	done        chan struct{}
	once        sync.Once
	onTerm      func()
	onDone      func()
	termCalls   atomic.Int32
	killCalls   atomic.Int32
	concurrency *downTestConcurrency
}

func newDownTestChild(pid int, delay time.Duration, stopErr error) *downTestChild {
	return &downTestChild{pid: pid, pgid: pid, delay: delay, stopErr: stopErr, done: make(chan struct{})}
}

func (c *downTestChild) PID() int              { return c.pid }
func (c *downTestChild) PGID() int             { return c.pgid }
func (c *downTestChild) Done() <-chan struct{} { return c.done }
func (c *downTestChild) Wait() managedprocess.Result {
	<-c.done
	return managedprocess.Result{ExitCode: 0, ExitedAt: time.Now()}
}

func (c *downTestChild) Signal(sig os.Signal) error {
	switch sig {
	case syscall.SIGTERM:
		c.termCalls.Add(1)
		if c.onTerm != nil {
			c.onTerm()
		}
		if c.concurrency != nil {
			defer c.concurrency.enter()()
		}
		if c.delay != 0 {
			time.Sleep(c.delay)
		}
		c.once.Do(func() {
			close(c.done)
			if c.onDone != nil {
				c.onDone()
			}
		})
		return c.stopErr
	case syscall.SIGKILL:
		c.killCalls.Add(1)
		c.once.Do(func() { close(c.done) })
		return nil
	default:
		c.once.Do(func() { close(c.done) })
		return nil
	}
}

func downTestSupervisor(t *testing.T, children map[string]*downTestChild) *app.Supervisor {
	t.Helper()
	supervisor, err := app.New(app.Options{
		StopGrace: 2 * time.Second,
		StartProcess: func(spec managedprocess.Spec) (app.Child, error) {
			if len(spec.Argv) == 0 {
				return nil, errors.New("missing test process name")
			}
			child := children[spec.Argv[0]]
			if child == nil {
				return nil, fmt.Errorf("unknown test process %q", spec.Argv[0])
			}
			return child, nil
		},
	})
	if err != nil {
		t.Fatalf("create test supervisor: %v", err)
	}
	return supervisor
}

func downTestServer(t *testing.T, supervisor *app.Supervisor) (*daemon.Server, string) {
	t.Helper()
	runtimeDir, err := os.MkdirTemp("/tmp", "h-down-runtime-")
	if err != nil {
		t.Fatalf("create runtime directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	server, err := daemon.NewServer(daemon.Config{RuntimeDir: runtimeDir, Supervisor: supervisor})
	if err != nil {
		t.Fatalf("create test daemon: %v", err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	readyCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.WaitReady(readyCtx); err != nil {
		_ = server.Close()
		t.Fatalf("wait for test daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Close()
		select {
		case <-serveDone:
		case <-time.After(3 * time.Second):
			t.Errorf("test daemon did not exit during cleanup")
		}
	})
	return server, runtimeDir
}

func downStartProcess(t *testing.T, server *daemon.Server, projectRoot, name, source string, argv []string) app.Process {
	t.Helper()
	client, err := daemon.Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatalf("dial daemon for %q: %v", name, err)
	}
	defer client.Close()
	root := ""
	if source != "ad_hoc" {
		root = projectRoot
	}
	process, err := client.Start(context.Background(), daemon.StartRequest{
		Name:   name,
		Source: source,
		Root:   root,
		Cwd:    projectRoot,
		Argv:   append([]string(nil), argv...),
		Env:    os.Environ(),
	})
	if err != nil {
		t.Fatalf("start %q: %v", name, err)
	}
	return process
}

func assertDownRuntimeAbsent(t *testing.T, runtimeDir string) {
	t.Helper()
	paths := daemon.NewRuntimePaths(runtimeDir)
	for _, path := range []string{paths.Socket, paths.PID, paths.Lock, paths.Ready, paths.Log} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("runtime artifact %q stat error = %v, want not exists", path, err)
		}
	}
}
