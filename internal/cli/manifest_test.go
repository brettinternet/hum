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

func TestManifestReadinessUsesInitialCursorBoundary(t *testing.T) {
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
				readiness := &protocol.Readiness{State: protocol.ReadinessStarting, Match: "ready"}
				if gets > 1 {
					zero := protocol.Cursor(0)
					readiness = &protocol.Readiness{State: protocol.ReadinessReady, Cursor: &zero, Match: "ready"}
				}
				process.Readiness = readiness
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
	if result.Readiness != app.ReadinessReady || result.ReadyCursor == nil || *result.ReadyCursor != 0 {
		t.Fatalf("manifest readiness result = %#v, want ready at cursor 0", result)
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
