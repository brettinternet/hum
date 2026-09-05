package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/protocol"
)

func TestStatusRunningJSON(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	name := "api"
	argv := []string{"/bin/sh", "-c", "printf 'status-output\\n'; sleep 30"}
	started := stopShutdownStartProcess(t, server, projectRoot, name, argv)
	t.Cleanup(func() { _, _, _ = stopShutdownRun(t, "stop", name) })

	before := statusWaitForCursor(t, server, projectRoot, name, 1)
	if before.State != app.StateRunning {
		t.Fatalf("process state before status = %q, want running", before.State)
	}

	stdout, stderr, err := stopShutdownRun(t, "status", name, "--json")
	if err != nil {
		t.Fatalf("status --json: %v (stderr=%q)", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("status --json stderr = %q", stderr)
	}
	got := statusDecodeJSON(t, stdout)
	statusAssertExactFields(t, stdout)

	if got.Name != name {
		t.Errorf("status name = %q, want %q", got.Name, name)
	}
	if got.ProjectRoot != projectRoot {
		t.Errorf("status project_root = %q, want %q", got.ProjectRoot, projectRoot)
	}
	if got.PID != started.PID || got.PID <= 0 {
		t.Errorf("status pid = %d, want started pid %d", got.PID, started.PID)
	}
	if got.PGID != started.PGID || got.PGID <= 0 {
		t.Errorf("status pgid = %d, want started pgid %d", got.PGID, started.PGID)
	}
	if got.Cwd != projectRoot {
		t.Errorf("status cwd = %q, want %q", got.Cwd, projectRoot)
	}
	if !reflect.DeepEqual(got.Argv, argv) {
		t.Errorf("status argv = %#v, want %#v", got.Argv, argv)
	}
	parsedStart, err := time.Parse(time.RFC3339, got.StartedAt)
	if err != nil {
		t.Fatalf("parse status started_at %q: %v", got.StartedAt, err)
	}
	if got.StartedAt != parsedStart.Format(time.RFC3339Nano) {
		t.Errorf("status started_at = %q, want RFC3339 formatting", got.StartedAt)
	}
	if got.StartedAt != started.Start.Format(time.RFC3339Nano) {
		t.Errorf("status started_at = %q, want process start %q", got.StartedAt, started.Start.Format(time.RFC3339Nano))
	}
	if got.State != string(app.StateRunning) {
		t.Errorf("status state = %q, want %q", got.State, app.StateRunning)
	}
	if got.ExitStatus != nil {
		t.Errorf("running status exit_status = %d, want null", *got.ExitStatus)
	}
	if got.RestartCount != 0 {
		t.Errorf("running status restart_count = %d, want zero", got.RestartCount)
	}
	if got.NextCursor != protocol.Cursor(before.NextCursor) || got.NextCursor == 0 {
		t.Errorf("status next_cursor = %d, want stable positive cursor %d", got.NextCursor, before.NextCursor)
	}

	beforeAgain := statusReadProcess(t, server, projectRoot, name)
	stdoutAgain, stderr, err := stopShutdownRun(t, "status", name, "--json")
	if err != nil {
		t.Fatalf("repeated status --json: %v (stderr=%q)", err, stderr)
	}
	if stdoutAgain != stdout {
		t.Errorf("repeated status JSON changed:\nfirst:  %ssecond: %s", stdout, stdoutAgain)
	}
	after := statusReadProcess(t, server, projectRoot, name)
	if !reflect.DeepEqual(beforeAgain, after) {
		t.Errorf("status changed process snapshot:\nbefore: %#v\nafter:  %#v", beforeAgain, after)
	}
	if after.NextCursor != before.NextCursor {
		t.Errorf("status changed next cursor from %d to %d", before.NextCursor, after.NextCursor)
	}
}

func TestStatusListFollowers(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	name := "watched"
	stopShutdownStartProcess(t, server, projectRoot, name, []string{"/bin/sh", "-c", "sleep 30"})
	t.Cleanup(func() { _, _, _ = stopShutdownRun(t, "stop", name) })

	plainBefore, stderr, err := stopShutdownRun(t, "list", "--all")
	if err != nil || stderr != "" {
		t.Fatalf("list --all before follow = %q, stderr %q, err %v", plainBefore, stderr, err)
	}
	if strings.Contains(plainBefore, "followers=") {
		t.Fatalf("plain list without followers changed: %q", plainBefore)
	}

	client, err := daemon.Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	follower, err := client.Follow(context.Background(), protocol.NewFollowRequest(name, projectRoot))
	if err != nil {
		t.Fatal(err)
	}

	human, stderr, err := stopShutdownRun(t, "status", name)
	if err != nil || stderr != "" || !strings.Contains(human, "followers: 1\n") {
		t.Fatalf("followed status = %q, stderr %q, err %v", human, stderr, err)
	}
	jsonStatus, stderr, err := stopShutdownRun(t, "status", name, "--json")
	if err != nil || stderr != "" || statusDecodeJSON(t, jsonStatus).Followers != 1 {
		t.Fatalf("followed status JSON = %q, stderr %q, err %v", jsonStatus, stderr, err)
	}
	listHuman, stderr, err := stopShutdownRun(t, "list", "--all")
	if err != nil || stderr != "" || !strings.Contains(listHuman, "followers=1") {
		t.Fatalf("followed list --all = %q, stderr %q, err %v", listHuman, stderr, err)
	}
	listOutput, stderr, err := stopShutdownRun(t, "list", "--all", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("followed list --all JSON = %q, stderr %q, err %v", listOutput, stderr, err)
	}
	var listed listJSON
	if err := json.Unmarshal([]byte(listOutput), &listed); err != nil || len(listed.Processes) != 1 || listed.Processes[0].Followers != 1 {
		t.Fatalf("followed list JSON = %#v, decode err %v", listed, err)
	}

	if err := follower.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if statusReadProcess(t, server, projectRoot, name).Followers == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	plainAfter, stderr, err := stopShutdownRun(t, "list", "--all")
	if err != nil || stderr != "" || plainAfter != plainBefore {
		t.Fatalf("plain list after detach = %q, want unchanged %q, stderr %q, err %v", plainAfter, plainBefore, stderr, err)
	}
}

func TestStatusExitedAndSignaled(t *testing.T) {
	t.Run("exited JSON and human output include terminal status", func(t *testing.T) {
		projectRoot := stopShutdownTestProject(t)
		server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

		name := "finished"
		stopShutdownStartProcess(t, server, projectRoot, name, []string{"/bin/sh", "-c", "exit 23"})
		exited := hum006ListLogsWaitForExit(t, runtimeDir, projectRoot, name)
		if exited.ExitCode != 23 {
			t.Fatalf("managed exit code = %d, want 23", exited.ExitCode)
		}

		stdout, stderr, err := stopShutdownRun(t, "status", name, "--json")
		if err != nil {
			t.Fatalf("exited status --json: %v (stderr=%q)", err, stderr)
		}
		got := statusDecodeJSON(t, stdout)
		if got.State != string(app.StateExited) {
			t.Errorf("exited status state = %q, want %q", got.State, app.StateExited)
		}
		if got.ExitStatus == nil || *got.ExitStatus != 23 {
			t.Errorf("exited status exit_status = %v, want 23", got.ExitStatus)
		}

		human, stderr, err := stopShutdownRun(t, "status", name)
		if err != nil {
			t.Fatalf("exited human status: %v (stderr=%q)", err, stderr)
		}
		for _, want := range []string{"name: finished", "state: exited", "exit_status: 23"} {
			if !strings.Contains(strings.ToLower(human), want) {
				t.Errorf("exited human status = %q, missing %q", human, want)
			}
		}
	})

	t.Run("signaled JSON and human output include signal exit status", func(t *testing.T) {
		projectRoot := stopShutdownTestProject(t)
		server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

		name := "signaled"
		stopShutdownStartProcess(t, server, projectRoot, name, []string{"/bin/sh", "-c", "kill -TERM $$"})
		exited := hum006ListLogsWaitForExit(t, runtimeDir, projectRoot, name)
		if exited.ExitCode != -1 {
			t.Fatalf("managed signal exit code = %d, want -1", exited.ExitCode)
		}

		stdout, stderr, err := stopShutdownRun(t, "status", name, "--json")
		if err != nil {
			t.Fatalf("signaled status --json: %v (stderr=%q)", err, stderr)
		}
		got := statusDecodeJSON(t, stdout)
		if got.State != string(app.StateExited) {
			t.Errorf("signaled status state = %q, want %q", got.State, app.StateExited)
		}
		if got.ExitStatus == nil || *got.ExitStatus != -1 {
			t.Errorf("signaled status exit_status = %v, want -1", got.ExitStatus)
		}

		human, stderr, err := stopShutdownRun(t, "status", name)
		if err != nil {
			t.Fatalf("signaled human status: %v (stderr=%q)", err, stderr)
		}
		for _, want := range []string{"name: signaled", "state: exited", "exit_status: -1"} {
			if !strings.Contains(strings.ToLower(human), want) {
				t.Errorf("signaled human status = %q, missing %q", human, want)
			}
		}
	})
}

func TestStatusErrors(t *testing.T) {
	t.Run("requires exactly one name", func(t *testing.T) {
		for _, test := range []struct {
			args []string
			want string
		}{
			{args: []string{"status"}, want: "status requires a process name"},
			{args: []string{"status", "one", "two"}, want: "status accepts exactly one process name"},
		} {
			_, _, err := stopShutdownRun(t, test.args...)
			if err == nil || err.Error() != test.want {
				t.Errorf("hum %s error = %v, want %q", strings.Join(test.args, " "), err, test.want)
			}
		}
	})

	t.Run("typed invalid and missing process errors propagate", func(t *testing.T) {
		stopShutdownTestProject(t)
		_, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

		_, _, err := stopShutdownRun(t, "status", "bad/name")
		var invalid *daemon.WireError
		if !errors.As(err, &invalid) || invalid.Code != protocol.ErrorInvalidRequest {
			t.Errorf("invalid status error = %v, want typed invalid-request wire error", err)
		}

		_, _, err = stopShutdownRun(t, "status", "missing")
		var missing *daemon.WireError
		if !errors.As(err, &missing) || missing.Code != protocol.ErrorNotFound {
			t.Errorf("missing status error = %v, want typed not-found wire error", err)
		}
	})
}

func TestStatusUnavailableDoesNotStartDaemon(t *testing.T) {
	stopShutdownTestProject(t)
	runtimeDir := hum006ListLogsTempDir(t, "status-runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	stdout, stderr, err := stopShutdownRun(t, "status", "api")
	if err == nil || err.Error() != logsUnavailableMessage {
		t.Fatalf("unavailable status error = %v, want exact %q", err, logsUnavailableMessage)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("unavailable status output = stdout %q stderr %q, want no command output", stdout, stderr)
	}
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		t.Fatalf("read empty runtime directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("status populated runtime directory %q: %#v", runtimeDir, entries)
	}

}

func TestStatusVersionMismatchClosesConnectionWithoutReplacement(t *testing.T) {
	stopShutdownTestProject(t)
	runtimeDir := hum006ListLogsTempDir(t, "status-mismatch")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	paths := daemon.NewRuntimePaths(runtimeDir)
	socket := paths.Socket
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen for mismatched status daemon: %v", err)
	}
	const attempts = 8
	served := make(chan error, attempts)
	serveDone := make(chan struct{})
	var connMu sync.Mutex
	var activeConn net.Conn
	stopping := false

	t.Cleanup(func() {
		connMu.Lock()
		stopping = true
		conn := activeConn
		connMu.Unlock()

		_ = listener.Close()
		if conn != nil {
			_ = conn.Close()
		}

		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Error("status mismatch server goroutine did not finish")
		}
	})

	go func() {
		defer close(serveDone)
		for range attempts {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				served <- acceptErr
				return
			}
			connMu.Lock()
			if stopping {
				connMu.Unlock()
				_ = conn.Close()
				return
			}
			activeConn = conn
			connMu.Unlock()
			decoder := protocol.NewDecoder(conn)
			encoder := protocol.NewEncoder(conn)
			request, requestErr := decoder.DecodeRequest()
			if requestErr == nil && (request.Op != protocol.OpHello || request.Hello == nil || request.Hello.Version != protocol.Version) {
				requestErr = fmt.Errorf("first request = %q version %d, want hello version %d", request.Op, request.Version, protocol.Version)
			}
			if requestErr == nil {
				requestErr = encoder.EncodeResponse(protocol.NewErrorResponse(
					protocol.OpHello,
					protocol.NewWireError(
						protocol.ErrorVersionMismatch,
						fmt.Sprintf("daemon protocol version mismatch: client %d, daemon 999", protocol.Version),
						protocol.VersionMismatchDetails{Client: protocol.Version, Daemon: 999},
					),
				))
			}
			if requestErr == nil {
				_, requestErr = decoder.DecodeRequest()
				if requestErr == nil {
					requestErr = errors.New("status sent a request after version mismatch")
				} else if !errors.Is(requestErr, io.EOF) {
					requestErr = fmt.Errorf("wait for status connection close: %w", requestErr)
				} else {
					requestErr = nil
				}
			}
			_ = conn.Close()
			connMu.Lock()
			activeConn = nil
			connMu.Unlock()
			served <- requestErr
		}
	}()

	for attempt := range attempts {
		stdout, stderr, err := stopShutdownRun(t, "status", "api")
		if stdout != "" || stderr != "" {
			t.Fatalf("status mismatch output = stdout %q stderr %q", stdout, stderr)
		}
		var mismatch *daemon.VersionMismatchError
		if !errors.As(err, &mismatch) || mismatch == nil {
			t.Fatalf("status mismatch error = %v, want version mismatch", err)
		}
		if mismatch.DaemonVersion != 999 {
			t.Fatalf("status mismatch daemon version = %d, want 999", mismatch.DaemonVersion)
		}
		select {
		case serveErr := <-served:
			if serveErr != nil {
				t.Fatalf("mismatched status connection %d: %v", attempt+1, serveErr)
			}
		case <-time.After(time.Second):
			t.Fatalf("status mismatch connection %d remained open", attempt+1)
		}
	}
	if _, err := os.Stat(paths.PID); err == nil {
		t.Fatalf("status created daemon pid %q", paths.PID)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspect daemon pid %q: %v", paths.PID, err)
	}
}

func statusDecodeJSON(t *testing.T, text string) statusJSON {
	t.Helper()
	var result statusJSON
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("decode status JSON %q: %v", text, err)
	}
	return result
}

func statusAssertExactFields(t *testing.T, text string) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		t.Fatalf("decode status JSON fields %q: %v", text, err)
	}
	want := map[string]struct{}{
		"name": {}, "project_root": {}, "tty": {}, "pid": {}, "pgid": {}, "cwd": {},
		"argv": {}, "started_at": {}, "state": {}, "exit_status": {},
		"restart_count": {}, "followers": {}, "next_cursor": {},
	}
	if len(fields) != len(want) {
		t.Fatalf("status JSON fields = %#v, want exactly %#v", fields, want)
	}
	for field := range want {
		if _, ok := fields[field]; !ok {
			t.Errorf("status JSON missing field %q", field)
		}
	}
}

func statusReadProcess(t *testing.T, server *daemon.Server, cwd, name string) app.Process {
	t.Helper()
	client, err := daemon.Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatalf("dial daemon for status snapshot: %v", err)
	}
	defer client.Close()
	process, err := client.Get(context.Background(), daemon.GetRequest{Name: name, Cwd: cwd})
	if err != nil {
		t.Fatalf("read process status snapshot: %v", err)
	}
	return process
}

func statusWaitForCursor(t *testing.T, server *daemon.Server, cwd, name string, want uint64) app.Process {
	t.Helper()
	client, err := daemon.Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatalf("dial daemon waiting for status cursor: %v", err)
	}
	defer client.Close()

	deadline := time.Now().Add(3 * time.Second)
	var last app.Process
	var lastErr error
	for time.Now().Before(deadline) {
		requestContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		last, lastErr = client.Get(requestContext, daemon.GetRequest{Name: name, Cwd: cwd})
		cancel()
		if lastErr == nil && uint64(last.NextCursor) >= want {
			return last
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("process %q did not reach cursor %d: last=%#v err=%v", name, want, last, lastErr)
	return last
}
