package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/output"
	"hum/internal/project"
	"hum/internal/protocol"
)

func writeManifestCLITestFile(t *testing.T, root, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatalf("write hum.yaml: %v", err)
	}
}

func manifestCLILaunchResults(t *testing.T, output string) []manifestLaunchResult {
	t.Helper()
	var results []manifestLaunchResult
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var result manifestLaunchResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatalf("decode launch result %q: %v", line, err)
		}
		results = append(results, result)
	}
	return results
}

func manifestCLIExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	return -1
}

func manifestCLIRecoveryStubDaemon(t *testing.T, processes map[string]protocol.Process) (string, <-chan protocol.Operation, <-chan struct{}) {
	t.Helper()
	runtimeDir := t.TempDir()
	listener, err := net.Listen("unix", daemon.NewRuntimePaths(runtimeDir).Socket)
	if err != nil {
		t.Fatalf("listen for recovery stub daemon: %v", err)
	}
	operations := make(chan protocol.Operation, 16)
	done := make(chan struct{})
	var connMu sync.Mutex
	var conn net.Conn
	t.Cleanup(func() {
		_ = listener.Close()
		connMu.Lock()
		active := conn
		connMu.Unlock()
		if active != nil {
			_ = active.Close()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("recovery stub daemon did not finish")
		}
	})
	go func() {
		defer close(done)
		active, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		connMu.Lock()
		conn = active
		connMu.Unlock()
		defer active.Close()

		decoder := protocol.NewDecoder(active)
		encoder := protocol.NewEncoder(active)
		request, decodeErr := decoder.DecodeRequest()
		if decodeErr != nil || request.Op != protocol.OpHello {
			return
		}
		if encodeErr := encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}); encodeErr != nil {
			return
		}
		for {
			request, decodeErr = decoder.DecodeRequest()
			if decodeErr != nil {
				return
			}
			operations <- request.Op
			switch request.Op {
			case protocol.OpGet:
				if request.Get == nil {
					return
				}
				process, ok := processes[request.Get.Name]
				if !ok {
					_ = encoder.EncodeResponse(protocol.NewErrorResponse(protocol.OpGet, protocol.NewWireError(protocol.ErrorNotFound, "not found", nil)))
					continue
				}
				if encodeErr := encoder.EncodeResponse(protocol.NewGetResponse(process)); encodeErr != nil {
					return
				}
			case protocol.OpWait:
				if encodeErr := encoder.EncodeResponse(protocol.NewWaitResponse(protocol.WaitExited, 0, nil)); encodeErr != nil {
					return
				}
			case protocol.OpStart:
				if encodeErr := encoder.EncodeResponse(protocol.NewErrorResponse(protocol.OpStart, protocol.NewWireError(protocol.ErrorInternal, "unexpected start request", nil))); encodeErr != nil {
					return
				}
			default:
				if encodeErr := encoder.EncodeResponse(protocol.NewErrorResponse(request.Op, protocol.NewWireError(protocol.ErrorInternal, "unexpected request", nil))); encodeErr != nil {
					return
				}
			}
		}
	}()
	return runtimeDir, operations, done
}

func TestUpPreservesCrashRecovery(t *testing.T) {
	root := stopShutdownTestProject(t)
	next := time.Date(2026, time.September, 6, 5, 0, 1, 0, time.UTC)
	pendingNextCursor := protocol.Cursor(21)
	exhaustedNextCursor := protocol.Cursor(29)
	processes := map[string]protocol.Process{
		"pending": {
			Name: "pending", Source: "manifest", Root: root, Cwd: root, Argv: []string{"pending"},
			State: "exited", LaunchCursor: 11, NextCursor: &pendingNextCursor, Restart: protocol.RestartOnFailure, Relaunches: 2, NextLaunchAt: &next,
		},
		"exhausted": {
			Name: "exhausted", Source: "manifest", Root: root, Cwd: root, Argv: []string{"exhausted"},
			State: "exited", LaunchCursor: 19, NextCursor: &exhaustedNextCursor, Restart: protocol.RestartOnFailure, Relaunches: 5,
		},
	}
	runtimeDir, operations, done := manifestCLIRecoveryStubDaemon(t, processes)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  pending:
    argv: [pending]
    ready:
      match: ready
      timeout: 1s
    restart: on-failure
  exhausted:
    argv: [exhausted]
    ready:
      match: ready
      timeout: 1s
    restart: on-failure
`)

	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err == nil || manifestCLIExitCode(err) != 3 || stderr != "" {
		t.Fatalf("up = code %d err=%v stdout=%q stderr=%q, want exit code 3 without stderr", manifestCLIExitCode(err), err, stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 2 {
		t.Fatalf("up returned %d results, want two: %s", len(results), stdout)
	}
	if results[0].Name != "exhausted" || results[1].Name != "pending" {
		t.Fatalf("up order = %#v, want lexical order", results)
	}
	for _, result := range results {
		if result.Source != "manifest" || result.State != "exited" || result.Restart != protocol.RestartOnFailure || result.Readiness != "" {
			t.Fatalf("recovery result = %#v, want exited manifest on-failure without readiness", result)
		}
		if result.LaunchCursor == nil {
			t.Fatalf("recovery result omitted launch cursor: %#v", result)
		}
		switch result.Name {
		case "pending":
			if result.Outcome != "recovery_pending" || *result.LaunchCursor != 11 || result.Relaunches != 2 || result.NextLaunchAt == nil || !result.NextLaunchAt.Equal(next) {
				t.Fatalf("pending recovery result = %#v", result)
			}
		case "exhausted":
			if result.Outcome != "recovery_exhausted" || *result.LaunchCursor != 19 || result.Relaunches != 5 || result.NextLaunchAt != nil {
				t.Fatalf("exhausted recovery result = %#v", result)
			}
		default:
			t.Fatalf("unexpected recovery result = %#v", result)
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recovery stub daemon did not observe the CLI connection close")
	}
	var got []protocol.Operation
	for {
		select {
		case operation := <-operations:
			got = append(got, operation)
		default:
			if len(got) != 2 || got[0] != protocol.OpGet || got[1] != protocol.OpGet {
				t.Fatalf("CLI up daemon operations = %v, want two get requests only", got)
			}
			return
		}
	}
}

func TestManifestStart(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "printf ready; sleep 30"]
    ready:
      match: ready
      timeout: 2s
  worker:
    argv: [/bin/sh, -c, "sleep 30"]
`)

	stdout, stderr, err := stopShutdownRun(t, "start", "--json", "web", "worker")
	if err != nil {
		t.Fatalf("start manifest processes: %v (stderr: %s)", err, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 2 {
		t.Fatalf("start returned %d results, want 2: %s", len(results), stdout)
	}
	if results[0].Name != "web" || results[0].Outcome != "started" || results[0].Readiness != "ready" {
		t.Fatalf("web start result = %+v", results[0])
	}
	if results[1].Name != "worker" || results[1].Outcome != "running_unverified" {
		t.Fatalf("worker start result = %+v", results[1])
	}
	if results[0].Source != "manifest" || !reflect.DeepEqual(results[0].Argv, []string{"/bin/sh", "-c", "printf ready; sleep 30"}) {
		t.Fatalf("web launch identity = %+v", results[0])
	}

	stdout, stderr, err = stopShutdownRun(t, "start", "--json", "web", "worker")
	if err != nil {
		t.Fatalf("idempotent manifest start: %v (stderr: %s)", err, stderr)
	}
	results = manifestCLILaunchResults(t, stdout)
	if len(results) != 2 || results[0].Outcome != "already_running" || results[1].Outcome != "already_running" {
		t.Fatalf("duplicate start results = %+v", results)
	}
}

func TestManifestReadinessSurvivesExitAfterMatch(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	client := daemon.NewClient(clientConn)
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverConn.Close()
	})

	serverDone := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		decoder := protocol.NewDecoder(serverConn)
		encoder := protocol.NewEncoder(serverConn)
		request, err := decoder.DecodeRequest()
		if err != nil {
			serverDone <- err
			return
		}
		if request.Op != protocol.OpHello || request.Hello == nil || request.Hello.Version != protocol.Version {
			serverDone <- fmt.Errorf("hello request = %#v", request)
			return
		}
		if err := encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}); err != nil {
			serverDone <- err
			return
		}

		next := protocol.Cursor(1)
		process := protocol.Process{
			Name: "web", Source: "manifest", Root: "/tmp/project", PID: 123,
			Cwd: "/tmp/project", Argv: []string{"fixture"}, LaunchCursor: 0,
			NextCursor: &next, State: string(app.StateRunning),
		}
		gets := 0
		for {
			request, err = decoder.DecodeRequest()
			if err != nil {
				if errors.Is(err, io.EOF) {
					serverDone <- nil
				} else {
					serverDone <- err
				}
				return
			}
			switch request.Op {
			case protocol.OpGet:
				gets++
				process.State = string(app.StateExited)
				process.Readiness = nil
				if err := encoder.EncodeResponse(protocol.NewGetResponse(process)); err != nil {
					serverDone <- err
					return
				}
				if gets > 1 {
					serverDone <- nil
					return
				}
			case protocol.OpWait:
				if request.Wait == nil {
					serverDone <- errors.New("wait request omitted payload")
					return
				}
				if request.Wait.After != nil {
					serverDone <- fmt.Errorf("wait after cursor = %d, want nil", *request.Wait.After)
					return
				}
				if err := encoder.EncodeResponse(protocol.NewWaitResponse(protocol.WaitMatched, 0, nil)); err != nil {
					serverDone <- err
					return
				}
			default:
				serverDone <- fmt.Errorf("unexpected request %q", request.Op)
				return
			}
		}
	}()

	if err := client.Hello(context.Background()); err != nil {
		t.Fatalf("hello: %v", err)
	}
	definition := project.Definition{
		Name: "web", Source: "manifest", Argv: []string{"fixture"},
		Ready: &project.ReadyDefinition{Match: "ready"},
	}
	process := app.Process{
		Name: "web", Source: "manifest", Root: "/tmp/project",
		PID: 123, Cwd: "/tmp/project", Argv: []string{"fixture"},
		State: app.StateRunning, LaunchCursor: 0,
		Readiness: &app.Readiness{State: app.ReadinessStarting, Match: "ready"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := manifestReadinessResult(client, ctx, "/tmp/project", definition, process, "started", time.Second)
	if err != nil {
		t.Fatalf("manifest readiness: %v", err)
	}
	if result.Outcome != "started" || result.Readiness != app.ReadinessReady || result.ReadyCursor == nil || *result.ReadyCursor != 0 {
		t.Fatalf("manifest readiness result = %#v, want started and ready at cursor 0", result)
	}

	_ = client.Close()
	if err := <-serverDone; err != nil {
		t.Fatalf("fake daemon: %v", err)
	}
}

func TestManifestStartTimeout(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "sleep 30"]
    ready:
      match: never-seen
`)

	stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--timeout", "10ms", "web")
	if err == nil || manifestCLIExitCode(err) != 2 {
		t.Fatalf("readiness timeout = %v (code %d, stderr: %s), want exit code 2", err, manifestCLIExitCode(err), stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 1 || results[0].Outcome != "timed_out" {
		t.Fatalf("timeout result = %+v", results)
	}
}

func TestManifestStartConcurrent(t *testing.T) {
	root := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "sleep 30"]
`)

	const callers = 8
	var wg sync.WaitGroup
	outputs := make([]string, callers)
	errs := make([]error, callers)
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func(index int) {
			defer wg.Done()
			var stdout, stderr bytes.Buffer
			errs[index] = NewRootCommand("test", "test", &stdout, &stderr).Run(context.Background(), []string{"hum", "start", "--json", "--no-wait", "web"})
			outputs[index] = stdout.String() + stderr.String()
		}(i)
	}
	wg.Wait()

	launched := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent start %d failed: %v (%s)", i, err, outputs[i])
		}
		results := manifestCLILaunchResults(t, outputs[i])
		if len(results) != 1 {
			t.Fatalf("concurrent start %d returned %d results: %s", i, len(results), outputs[i])
		}
		if results[0].Outcome == "running_unverified" {
			launched++
		} else if results[0].Outcome != "already_running" {
			t.Fatalf("concurrent start %d result = %+v", i, results[0])
		}
	}
	if launched != 1 {
		t.Fatalf("concurrent starts launched %d children, want exactly 1", launched)
	}
	active := stopShutdownListActive(t, server, root)
	if len(active) != 1 || active[0].Name != "web" {
		t.Fatalf("active processes after concurrent start = %+v", active)
	}
}

func TestManifestUp(t *testing.T) {
	root := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  zeta:
    argv: [/bin/sh, -c, "sleep 30"]
  alpha:
    argv: [/bin/sh, -c, "printf ready; sleep 30"]
    ready:
      match: ready
      timeout: 2s
  beta:
    argv: [/definitely/not/a/real/hum-command]
`)

	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err == nil || manifestCLIExitCode(err) != 1 {
		t.Fatalf("up error precedence = %v (code %d, stderr: %s), want exit code 1", err, manifestCLIExitCode(err), stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 3 {
		t.Fatalf("up returned %d results, want 3: %s", len(results), stdout)
	}
	if got := []string{results[0].Name, results[1].Name, results[2].Name}; !reflect.DeepEqual(got, []string{"alpha", "beta", "zeta"}) {
		t.Fatalf("up order = %v, want lexical order", got)
	}
	if results[0].Outcome != "started" || results[1].Outcome != "error" || results[2].Outcome != "running_unverified" {
		t.Fatalf("up outcomes = %+v", results)
	}
	if results[1].Error == "" {
		t.Fatalf("failed beta result has no error: %+v", results[1])
	}
	active := stopShutdownListActive(t, server, root)
	activeNames := make([]string, 0, len(active))
	for _, process := range active {
		activeNames = append(activeNames, process.Name)
	}
	if !reflect.DeepEqual(activeNames, []string{"alpha", "zeta"}) && !reflect.DeepEqual(activeNames, []string{"zeta", "alpha"}) {
		t.Fatalf("active processes after partial up = %v", activeNames)
	}
}

func TestUpOrdersByAfter(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  db:
    argv: [/bin/sh, -c, "printf db-ready; sleep 30"]
    ready: {match: db-ready}
  api:
    argv: [/bin/sh, -c, "printf api-ready; sleep 30"]
    after: [db]
    ready: {match: api-ready}
  web:
    argv: [/bin/sh, -c, "printf web-ready; sleep 30"]
    after: [api]
    ready: {match: web-ready}
  root:
    argv: [/bin/sh, -c, "printf root-ready; sleep 30"]
    ready: {match: root-ready}
`)
	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err != nil {
		t.Fatalf("ordered up: %v (stdout=%s stderr=%s)", err, stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if got := []string{results[0].Name, results[1].Name, results[2].Name, results[3].Name}; !reflect.DeepEqual(got, []string{"api", "db", "root", "web"}) {
		t.Fatalf("ordered up names = %v", got)
	}
	for _, result := range results {
		if result.Outcome != "started" || result.Readiness != app.ReadinessReady {
			t.Fatalf("ordered up result = %+v", result)
		}
	}
	stdout, stderr, err = stopShutdownRun(t, "up", "--json")
	if err != nil {
		t.Fatalf("idempotent ordered up: %v (stdout=%s stderr=%s)", err, stdout, stderr)
	}
	results = manifestCLILaunchResults(t, stdout)
	for _, result := range results {
		if result.Outcome != "already_running" || result.Readiness != app.ReadinessReady {
			t.Fatalf("idempotent ordered up result = %+v", result)
		}
	}
}

func TestUpReportsBlockedExistingState(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  db:
    argv: [/bin/sh, -c, "exit 4"]
    ready: {match: db-ready}
  api:
    argv: [/bin/sh, -c, "sleep 30"]
    after: [db]
    ready: {match: api-ready}
  web:
    argv: [/bin/sh, -c, "sleep 30"]
    after: [api]
    ready: {match: web-ready}
`)

	startedOut, startedErr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "api")
	if err != nil || startedErr != "" {
		t.Fatalf("seed running api: %v (stdout=%s stderr=%s)", err, startedOut, startedErr)
	}
	started := manifestCLILaunchResults(t, startedOut)[0]

	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err == nil || manifestCLIExitCode(err) != 3 || stderr != "" {
		t.Fatalf("up with running blocked record: %v (stdout=%s stderr=%s)", err, stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if results[0].Name != "api" || results[0].Outcome != "skipped" || results[0].ExistingState != "running" || results[0].PID == nil || started.PID == nil || *results[0].PID != *started.PID {
		t.Fatalf("running blocked api = %+v, seeded %+v", results[0], started)
	}
	if !reflect.DeepEqual(results[0].BlockedBy, []string{"db"}) || results[2].Name != "web" || results[2].Outcome != "skipped" || results[2].ExistingState != "" || results[2].PID != nil {
		t.Fatalf("running blocked results = %+v", results)
	}

	if _, stopErr, stopRunErr := stopShutdownRun(t, "stop", "api"); stopRunErr != nil || stopErr != "" {
		t.Fatalf("stop seeded api: %v (stderr=%s)", stopRunErr, stopErr)
	}
	stdout, stderr, err = stopShutdownRun(t, "up", "--json")
	if err == nil || manifestCLIExitCode(err) != 3 || stderr != "" {
		t.Fatalf("up with exited blocked record: %v (stdout=%s stderr=%s)", err, stdout, stderr)
	}
	results = manifestCLILaunchResults(t, stdout)
	if results[0].Outcome != "skipped" || results[0].ExistingState != "exited" || results[0].LaunchCursor == nil || started.LaunchCursor == nil || *results[0].LaunchCursor != *started.LaunchCursor {
		t.Fatalf("exited blocked api = %+v, seeded %+v", results[0], started)
	}
	if results[2].Outcome != "skipped" || results[2].ExistingState != "" {
		t.Fatalf("absent blocked web = %+v", results[2])
	}

	human, humanErr, humanRunErr := stopShutdownRun(t, "up")
	if humanRunErr == nil || manifestCLIExitCode(humanRunErr) != 3 || humanErr != "" {
		t.Fatalf("human blocked up: %v (stdout=%s stderr=%s)", humanRunErr, human, humanErr)
	}
	for _, phrase := range []string{"api: skipped (blocked by db); existing process exited", "web: skipped (blocked by api); not launched"} {
		if !strings.Contains(human, phrase) {
			t.Fatalf("human blocked output missing %q: %s", phrase, human)
		}
	}
}

func TestManifestList(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "echo hello world"]
  api:
    argv: [go, run, ./api, --port=8080]
`)

	stdout, stderr, err := stopShutdownRun(t, "list", "--json")
	if err != nil {
		t.Fatalf("manifest list without daemon: %v (stderr: %s)", err, stderr)
	}
	var result listJSON
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode manifest list: %v (%s)", err, stdout)
	}
	if len(result.Processes) != 2 {
		t.Fatalf("manifest list returned %d processes, want 2", len(result.Processes))
	}
	if result.Processes[0].Name != "api" || result.Processes[0].Source != "manifest" || result.Processes[0].Root != root {
		t.Fatalf("first manifest list process = %+v", result.Processes[0])
	}
	if !reflect.DeepEqual(result.Processes[0].Argv, []string{"go", "run", "./api", "--port=8080"}) {
		t.Fatalf("api argv = %v", result.Processes[0].Argv)
	}
	if result.Processes[0].State != "stopped" || result.Processes[1].State != "stopped" {
		t.Fatalf("manifest list states = %q, %q", result.Processes[0].State, result.Processes[1].State)
	}
	if _, err := os.Stat(runtimeDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only manifest list touched runtime directory: %v", err)
	}

	stdout, stderr, err = stopShutdownRun(t, "list")
	if err != nil {
		t.Fatalf("human manifest list: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "source=manifest") || !strings.Contains(stdout, "argv=go run ./api --port=8080") {
		t.Fatalf("human manifest list omitted source/argv: %s", stdout)
	}
}

func TestDeclaredNameCollision(t *testing.T) {
	root := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	stdout, stderr, err := stopShutdownRun(t, "run", "web", "--detach", "--", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("start ad-hoc process: %v (stdout: %s, stderr: %s)", err, stdout, stderr)
	}
	writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "sleep 30"]
`)

	stdout, stderr, err = stopShutdownRun(t, "start", "--json", "web")
	if err == nil || manifestCLIExitCode(err) != 1 {
		t.Fatalf("declared/ad-hoc collision = %v (code %d, stdout: %s, stderr: %s)", err, manifestCLIExitCode(err), stdout, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 1 || results[0].Outcome != "error" || !strings.Contains(results[0].Error, "ad-hoc") {
		t.Fatalf("collision result = %+v", results)
	}
	active := stopShutdownListActive(t, server, root)
	if len(active) != 1 || active[0].Source != "ad_hoc" {
		t.Fatalf("collision changed active process = %+v", active)
	}

	_, stderr, err = stopShutdownRun(t, "run", "web", "--detach", "--", "/bin/sh", "-c", "sleep 30")
	if err == nil || !strings.Contains(err.Error(), "declared") {
		t.Fatalf("raw run for declared name error = %v (stderr: %s)", err, stderr)
	}
}

func TestManifestRestart(t *testing.T) {
	root := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	marker := filepath.Join(root, "restart-marker")
	command := fmt.Sprintf(`printf "$HUM_MANIFEST_TEST_VALUE" > %s; sleep 30`, marker)
	writeManifestCLITestFile(t, root, fmt.Sprintf(`version: 1
processes:
  web:
    argv: [/bin/sh, -c, %q]
`, command))

	t.Setenv("HUM_MANIFEST_TEST_VALUE", "before")
	stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "web")
	if err != nil {
		t.Fatalf("start before restart: %v (stderr: %s)", err, stderr)
	}
	started := manifestCLILaunchResults(t, stdout)
	if len(started) != 1 || started[0].LaunchCursor == nil {
		t.Fatalf("start result = %+v", started)
	}
	if err := waitForManifestMarker(marker, "before"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HUM_MANIFEST_TEST_VALUE", "after")
	stdout, stderr, err = stopShutdownRun(t, "restart", "--json", "web")
	if err != nil {
		t.Fatalf("restart manifest process: %v (stderr: %s)", err, stderr)
	}
	var restarted restartResult
	if err := json.Unmarshal([]byte(stdout), &restarted); err != nil {
		t.Fatalf("decode restart result: %v (%s)", err, stdout)
	}
	if restarted.Source != "manifest" || !reflect.DeepEqual(restarted.Argv, []string{"/bin/sh", "-c", command}) {
		t.Fatalf("restart identity = %+v", restarted)
	}
	store, err := server.Supervisor().Output(root, "web")
	if err != nil {
		t.Fatalf("restart output store: %v", err)
	}
	read, err := store.Read(output.ReadOptions{})
	if err != nil {
		t.Fatalf("read restart output: %v", err)
	}
	var (
		markerCursor output.Cursor
		foundMarker  bool
	)
	for _, entry := range read.Entries {
		if entry.Stream == output.System && entry.Text == "web restarted\n" {
			markerCursor = entry.Cursor
			foundMarker = true
			break
		}
	}
	if !foundMarker {
		t.Fatalf("restart marker missing from %#v", read.Entries)
	}
	if got := output.Cursor(restarted.LaunchCursor); got != markerCursor {
		t.Fatalf("restart launch cursor = %d, marker cursor = %d", got, markerCursor)
	}
	current, err := server.Supervisor().Get(root, "web")
	if err != nil {
		t.Fatalf("get restarted process: %v", err)
	}
	if current.LaunchCursor != markerCursor || current.NextCursor <= markerCursor {
		t.Fatalf("restarted process cursors = launch %d, next %d; marker %d", current.LaunchCursor, current.NextCursor, markerCursor)
	}
	if err := waitForManifestMarker(marker, "after"); err != nil {
		t.Fatal(err)
	}
}

func TestReadinessFields(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, root, `version: 1
processes:
  waiting:
    argv: [/bin/sh, -c, "sleep 30"]
    ready:
      match: never-seen
      timeout: 1s
  plain:
    argv: [/bin/sh, -c, "sleep 30"]
`)

	if _, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "waiting", "plain"); err != nil {
		t.Fatalf("start readiness fixture: %v (stderr: %s)", err, stderr)
	}
	if _, stderr, err := stopShutdownRun(t, "run", "adhoc", "--detach", "--", "/bin/sh", "-c", "sleep 30"); err != nil {
		t.Fatalf("start ad-hoc readiness fixture: %v (stderr: %s)", err, stderr)
	}

	stdout, stderr, err := stopShutdownRun(t, "status", "--json", "waiting")
	if err != nil {
		t.Fatalf("status waiting: %v (stderr: %s)", err, stderr)
	}
	var waiting statusJSON
	if err := json.Unmarshal([]byte(stdout), &waiting); err != nil {
		t.Fatalf("decode waiting status: %v", err)
	}
	if waiting.Source != "manifest" || waiting.Readiness != "starting" {
		t.Fatalf("waiting status = %+v", waiting)
	}

	stdout, stderr, err = stopShutdownRun(t, "status", "--json", "plain")
	if err != nil {
		t.Fatalf("status plain: %v (stderr: %s)", err, stderr)
	}
	var plain statusJSON
	if err := json.Unmarshal([]byte(stdout), &plain); err != nil {
		t.Fatalf("decode plain status: %v", err)
	}
	if plain.Source != "manifest" || plain.Readiness != "running_unverified" {
		t.Fatalf("plain status = %+v", plain)
	}

	stdout, stderr, err = stopShutdownRun(t, "status", "--json", "adhoc")
	if err != nil {
		t.Fatalf("status ad-hoc: %v (stderr: %s)", err, stderr)
	}
	var adhoc map[string]any
	if err := json.Unmarshal([]byte(stdout), &adhoc); err != nil {
		t.Fatalf("decode ad-hoc status: %v", err)
	}
	if _, ok := adhoc["readiness"]; ok {
		t.Fatalf("ad-hoc status unexpectedly contains readiness: %s", stdout)
	}
}

func TestManifestStartRetainsRecordedReadinessAfterManifestEdit(t *testing.T) {
	tests := []struct {
		name   string
		edited string
	}{
		{
			name: "remove ready",
			edited: `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "echo new; sleep 0.3; echo old; sleep 30"]
`,
		},
		{
			name: "change ready",
			edited: `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "echo new; sleep 0.3; echo old; sleep 30"]
    ready:
      match: new
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := stopShutdownTestProject(t)
			_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			writeManifestCLITestFile(t, root, `version: 1
processes:
  web:
    argv: [/bin/sh, -c, "echo new; sleep 0.3; echo old; sleep 30"]
    ready:
      match: old
`)

			stdout, stderr, err := stopShutdownRun(t, "start", "--json", "--no-wait", "web")
			if err != nil {
				t.Fatalf("initial start: %v (stderr: %s)", err, stderr)
			}
			started := manifestCLILaunchResults(t, stdout)
			if len(started) != 1 || started[0].Readiness != app.ReadinessStarting {
				t.Fatalf("initial start result = %+v, want starting readiness", started)
			}

			writeManifestCLITestFile(t, root, test.edited)
			stdout, stderr, err = stopShutdownRun(t, "start", "--json", "--timeout", "1s", "web")
			if err != nil {
				t.Fatalf("start after manifest edit: %v (stderr: %s)", err, stderr)
			}
			results := manifestCLILaunchResults(t, stdout)
			if len(results) != 1 {
				t.Fatalf("start after manifest edit returned %d results: %s", len(results), stdout)
			}
			if results[0].Outcome != "already_running" || results[0].Readiness != app.ReadinessReady {
				t.Fatalf("start after %s result = %+v, want already_running/ready", test.name, results[0])
			}
			if results[0].Source != "manifest" || !reflect.DeepEqual(results[0].Argv, []string{"/bin/sh", "-c", "echo new; sleep 0.3; echo old; sleep 30"}) {
				t.Fatalf("start after %s identity = %+v", test.name, results[0])
			}
		})
	}
}

func waitForManifestMarker(filename, want string) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(filename)
		if err == nil && string(contents) == want {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	contents, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read restart marker %q: %w", filename, err)
	}
	return fmt.Errorf("restart marker = %q, want %q", string(contents), want)
}
