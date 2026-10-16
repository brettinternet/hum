//go:build !windows

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	urfavecli "github.com/urfave/cli/v3"
	"hum/internal/daemon"
	"hum/internal/protocol"
)

func TestWaitCLIRequestOptions(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantAfter   *protocol.Cursor
		wantMatch   string
		wantTimeout int64
	}{
		{
			name:        "defaults",
			args:        []string{"wait", "api"},
			wantTimeout: 30_000,
		},
		{
			name:        "explicit options including zero cursor",
			args:        []string{"wait", "api", "--after-cursor", "0", "--match", "ready", "--timeout", "1500ms", "--json"},
			wantAfter:   waitCLIProtocolCursor(0),
			wantMatch:   "ready",
			wantTimeout: 1_500,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := protocol.WaitResponse{Op: protocol.OpWait, OK: true, Outcome: protocol.WaitMatched, Cursor: 7}
			runtimeDir, requests := waitCLIStubDaemon(t, response)
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

			stdout, stderr, err := waitCLIRun(t, test.args...)
			if err != nil {
				t.Fatalf("wait command: %v (stdout=%q stderr=%q)", err, stdout, stderr)
			}
			if stderr != "" {
				t.Fatalf("wait command stderr = %q, want empty", stderr)
			}
			var request protocol.WaitRequest
			select {
			case request = <-requests:
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for captured wait request")
			}
			if request.Name != "api" {
				t.Errorf("wait name = %q, want api", request.Name)
			}
			if request.Cwd == "" {
				t.Error("wait cwd is empty")
			}
			if test.wantAfter == nil {
				if request.After != nil {
					t.Fatalf("omitted after cursor = %d, want nil", *request.After)
				}
			} else if request.After == nil || *request.After != *test.wantAfter {
				if request.After == nil {
					t.Fatalf("explicit after cursor = nil, want %d", *test.wantAfter)
				}
				t.Fatalf("explicit after cursor = %d, want %d", *request.After, *test.wantAfter)
			}
			if request.Match != test.wantMatch {
				t.Errorf("wait match = %q, want %q", request.Match, test.wantMatch)
			}
			if request.TimeoutMS != test.wantTimeout {
				t.Errorf("wait timeout_ms = %d, want %d", request.TimeoutMS, test.wantTimeout)
			}
		})
	}
}

func TestWaitCLIOutputsAndExitCodes(t *testing.T) {
	tests := []struct {
		name       string
		outcome    protocol.WaitOutcome
		cursor     protocol.Cursor
		exit       *protocol.Exit
		match      bool
		jsonOutput bool
		wantCode   int
		wantHuman  string
	}{
		{
			name:      "matched human",
			outcome:   protocol.WaitMatched,
			cursor:    4,
			match:     true,
			wantHuman: "outcome: matched\ncursor: 4\n",
		},
		{
			name:       "matched JSON",
			outcome:    protocol.WaitMatched,
			cursor:     4,
			match:      true,
			jsonOutput: true,
		},
		{
			name:      "exited before match human",
			outcome:   protocol.WaitExited,
			cursor:    5,
			exit:      &protocol.Exit{Code: 23},
			match:     true,
			wantCode:  3,
			wantHuman: "outcome: exited\ncursor: 5\nexit_code: 23\n",
		},
		{
			name:       "exited before match JSON",
			outcome:    protocol.WaitExited,
			cursor:     5,
			exit:       &protocol.Exit{Code: 23},
			match:      true,
			jsonOutput: true,
			wantCode:   3,
		},
		{
			name:      "exit without match human",
			outcome:   protocol.WaitExited,
			cursor:    6,
			exit:      &protocol.Exit{Code: 0},
			wantHuman: "outcome: exited\ncursor: 6\nexit_code: 0\n",
		},
		{
			name:       "timed out JSON",
			outcome:    protocol.WaitTimedOut,
			cursor:     9,
			jsonOutput: true,
			wantCode:   2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := protocol.WaitResponse{
				Op:      protocol.OpWait,
				OK:      true,
				Outcome: test.outcome,
				Cursor:  test.cursor,
				Exit:    test.exit,
			}
			runtimeDir, _ := waitCLIStubDaemon(t, response)
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

			args := []string{"wait", "api", "--timeout", "1s"}
			if test.match {
				args = append(args, "--match", "ready")
			}
			if test.jsonOutput {
				args = append(args, "--json")
			}
			stdout, stderr, err := waitCLIRun(t, args...)
			if got := waitCLIExitCode(err); got != test.wantCode {
				t.Fatalf("wait exit code = %d (err=%v), want %d; stdout=%q stderr=%q", got, err, test.wantCode, stdout, stderr)
			}
			if stderr != "" {
				t.Fatalf("wait stderr = %q, want empty", stderr)
			}
			if test.jsonOutput {
				var got protocol.WaitResponse
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("decode wait JSON %q: %v", stdout, err)
				}
				if got.Op != protocol.OpWait || !got.OK || got.Outcome != test.outcome || got.Cursor != test.cursor {
					t.Fatalf("wait JSON response = %#v, want outcome %q cursor %d", got, test.outcome, test.cursor)
				}
				if test.exit == nil {
					if got.Exit != nil {
						t.Fatalf("wait JSON exit = %#v, want nil", got.Exit)
					}
				} else if got.Exit == nil || got.Exit.Code != test.exit.Code {
					t.Fatalf("wait JSON exit = %#v, want code %d", got.Exit, test.exit.Code)
				}
				return
			}
			if stdout != test.wantHuman {
				t.Errorf("wait human output = %q, want %q", stdout, test.wantHuman)
			}
		})
	}
}

func TestWaitTimeoutExplainsNeverObserved(t *testing.T) {
	guidance := `no process named "api" was observed during the wait; check the name or start it first.`
	for _, test := range []struct {
		name   string
		json   bool
		observ bool
	}{
		{name: "human false"},
		{name: "json false", json: true},
		{name: "json true", json: true, observ: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := protocol.WaitResponse{Op: protocol.OpWait, OK: true, Outcome: protocol.WaitTimedOut, Cursor: 9, ProcessObserved: test.observ}
			runtimeDir, _ := waitCLIStubDaemon(t, response)
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			args := []string{"wait", "api", "--timeout", "1s"}
			if test.json {
				args = append(args, "--json")
			}
			stdout, stderr, err := waitCLIRun(t, args...)
			if got := waitCLIExitCode(err); got != 2 {
				t.Fatalf("wait exit code = %d (err=%v), want 2; stdout=%q stderr=%q", got, err, stdout, stderr)
			}
			if stderr != "" {
				t.Fatalf("wait stderr = %q, want empty", stderr)
			}
			if !test.json {
				want := "outcome: timed_out\ncursor: 9\n" + guidance + "\n"
				if stdout != want {
					t.Fatalf("wait human output = %q, want %q", stdout, want)
				}
				return
			}
			var got protocol.WaitResponse
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("decode wait JSON %q: %v", stdout, err)
			}
			if got.ProcessObserved != test.observ {
				t.Fatalf("JSON process_observed = %v, want %v", got.ProcessObserved, test.observ)
			}
			if !strings.Contains(stdout, `"process_observed":`) {
				t.Fatalf("wait JSON omitted process_observed: %q", stdout)
			}
		})
	}
}

func TestWaitCLIValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing name", args: []string{"wait"}, want: "wait requires a process name"},
		{name: "too many names", args: []string{"wait", "one", "two"}, want: "wait accepts exactly one process name"},
		{name: "invalid cursor", args: []string{"wait", "api", "--after-cursor", "-1"}, want: "after-cursor"},
		{name: "invalid regex", args: []string{"wait", "api", "--match", "["}, want: "regular expression"},
		{name: "empty regex", args: []string{"wait", "api", "--match", ""}, want: "match must not be empty"},
		{name: "invalid duration", args: []string{"wait", "api", "--timeout", "later"}, want: "valid duration"},
		{name: "non-positive duration", args: []string{"wait", "api", "--timeout", "0s"}, want: "positive"},
		{name: "sub-millisecond duration", args: []string{"wait", "api", "--timeout", "1ns"}, want: "at least 1ms"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeDir := filepath.Join(t.TempDir(), "runtime")
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

			stdout, stderr, err := waitCLIRun(t, test.args...)
			if err == nil {
				t.Fatalf("wait validation unexpectedly succeeded (stdout=%q stderr=%q)", stdout, stderr)
			}
			if got := waitCLIExitCode(err); got != 1 {
				t.Fatalf("wait validation exit code = %d (err=%v), want 1; stdout=%q stderr=%q", got, err, stdout, stderr)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Errorf("wait validation error = %q, want substring %q", err, test.want)
			}
			// Flag parse errors are reported once through the returned error, with
			// no help dump on stdout and no duplicate line on stderr.
			if stdout != "" || stderr != "" {
				t.Errorf("wait validation output = stdout %q stderr %q, want empty", stdout, stderr)
			}
			if _, statErr := os.Stat(runtimeDir); !os.IsNotExist(statErr) {
				t.Fatalf("wait validation runtime state stat error = %v, want not-exist", statErr)
			}
		})
	}
}

func waitCLIStubDaemon(t *testing.T, response protocol.WaitResponse) (string, <-chan protocol.WaitRequest) {
	t.Helper()
	runtimeDir, err := os.MkdirTemp("/tmp", "h-")
	if err != nil {
		t.Fatalf("create short wait runtime directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	paths := daemon.NewRuntimePaths(runtimeDir)
	listener, err := net.Listen("unix", paths.Socket)
	if err != nil {
		t.Fatalf("listen for wait stub daemon: %v", err)
	}
	requests := make(chan protocol.WaitRequest, 1)
	serveDone := make(chan struct{})
	var connMu sync.Mutex
	var controlConn, waitConn net.Conn
	stopping := false

	t.Cleanup(func() {
		connMu.Lock()
		stopping = true
		control := controlConn
		wait := waitConn
		connMu.Unlock()

		_ = listener.Close()
		if control != nil {
			_ = control.Close()
		}
		if wait != nil {
			_ = wait.Close()
		}
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Error("wait stub daemon goroutine did not finish")
		}
	})

	go func() {
		defer close(serveDone)

		control, err := listener.Accept()
		if err != nil {
			return
		}
		connMu.Lock()
		if stopping {
			connMu.Unlock()
			_ = control.Close()
			return
		}
		controlConn = control
		connMu.Unlock()
		defer control.Close()

		controlDecoder := protocol.NewDecoder(control)
		controlEncoder := protocol.NewEncoder(control)
		request, err := controlDecoder.DecodeRequest()
		if err != nil || request.Op != protocol.OpHello {
			return
		}
		if err := controlEncoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}); err != nil {
			return
		}

		wait, err := listener.Accept()
		if err != nil {
			return
		}
		connMu.Lock()
		if stopping {
			connMu.Unlock()
			_ = wait.Close()
			return
		}
		waitConn = wait
		connMu.Unlock()
		defer wait.Close()

		waitDecoder := protocol.NewDecoder(wait)
		waitEncoder := protocol.NewEncoder(wait)
		request, err = waitDecoder.DecodeRequest()
		if err != nil || request.Op != protocol.OpHello {
			return
		}
		if err := waitEncoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}); err != nil {
			return
		}
		request, err = waitDecoder.DecodeRequest()
		if err != nil || request.Op != protocol.OpWait || request.Wait == nil {
			return
		}
		requests <- *request.Wait
		_ = waitEncoder.EncodeResponse(response)
	}()
	return runtimeDir, requests
}

func TestWaitCLIPreLaunchSessionTimesOut(t *testing.T) {
	_, runtimeDir := stopShutdownTestServer(t, time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	stdout, stderr, err := waitCLIRun(t, "wait", "api", "--timeout", "20ms")
	if got := waitCLIExitCode(err); got != 2 {
		t.Fatalf("pre-launch wait exit code = %d, want 2 (stdout=%q stderr=%q err=%v)", got, stdout, stderr, err)
	}
	if !strings.Contains(stdout, "timed_out") || stderr != "" {
		t.Fatalf("pre-launch wait output = stdout %q stderr %q", stdout, stderr)
	}
}

func TestCursorSessionWaitFlagForwardingAndJSON(t *testing.T) {
	const sessionID = "session-42"
	response := protocol.WaitResponse{Op: protocol.OpWait, OK: true, Outcome: protocol.WaitMatched, Cursor: 4, SessionID: sessionID}
	runtimeDir, requests := waitCLIStubDaemon(t, response)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	stdout, stderr, err := waitCLIRun(t, "wait", "api", "--after-cursor", "0", "--session-id", sessionID, "--json")
	if err != nil || stderr != "" {
		t.Fatalf("wait command: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	var result protocol.WaitResponse
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode wait JSON %q: %v", stdout, err)
	}
	if result.SessionID != sessionID {
		t.Fatalf("wait session_id = %q, want %q", result.SessionID, sessionID)
	}
	select {
	case request := <-requests:
		if request.SessionID != sessionID || request.After == nil || *request.After != 0 {
			t.Fatalf("wait request = %+v, want session %q and cursor 0", request, sessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for captured wait request")
	}
}

func TestCursorSessionFlagUsageBeforeDaemonContact(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	for _, args := range [][]string{
		{"logs", "api", "--session-id", "old"},
		{"logs", "api", "web", "--after-cursor", "2", "--session-id", "old"},
		{"wait", "api", "--session-id", "old"},
	} {
		stdout, stderr, err := waitCLIRun(t, args...)
		if err == nil || waitCLIExitCode(err) != 1 {
			t.Fatalf("%v error = %v, want usage error (stdout=%q stderr=%q)", args, err, stdout, stderr)
		}
	}
	paths := daemon.NewRuntimePaths(runtimeDir)
	if _, err := os.Stat(paths.Socket); !os.IsNotExist(err) {
		t.Fatalf("invalid session cursor contacted daemon; socket stat error = %v", err)
	}
	if _, err := os.Stat(paths.PID); !os.IsNotExist(err) {
		t.Fatalf("invalid session cursor started daemon; pid stat error = %v", err)
	}
}

func TestCursorSessionLogsAndFollowForwardFlag(t *testing.T) {
	const sessionID = "session-logs"
	for _, follow := range []bool{false, true} {
		t.Run(map[bool]string{false: "bounded", true: "follow"}[follow], func(t *testing.T) {
			runtimeDir, requests := cursorSessionCLIStubDaemon(t, sessionID)
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			args := []string{"logs", "api", "--after-cursor", "3", "--session-id", sessionID, "--json"}
			if follow {
				args = append(args, "--follow")
			}
			stdout, stderr, err := waitCLIRun(t, args...)
			if err != nil || stderr != "" {
				t.Fatalf("logs command: err=%v stdout=%q stderr=%q", err, stdout, stderr)
			}
			select {
			case request := <-requests:
				if request.Op == protocol.OpOutput {
					if request.Output == nil || request.Output.SessionID != sessionID || request.Output.After == nil || *request.Output.After != 3 {
						t.Fatalf("bounded output request = %+v, want session %q and cursor 3", request.Output, sessionID)
					}
				} else if request.Op == protocol.OpFollow {
					if request.Follow == nil || request.Follow.SessionID != sessionID || request.Follow.After == nil || *request.Follow.After != 3 {
						t.Fatalf("follow request = %+v, want session %q and cursor 3", request.Follow, sessionID)
					}
				} else {
					t.Fatalf("captured request op = %q, want output or follow", request.Op)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for captured logs request")
			}
		})
	}
}

func cursorSessionCLIStubDaemon(t *testing.T, sessionID string) (string, <-chan protocol.Request) {
	t.Helper()
	runtimeDir, err := os.MkdirTemp("/tmp", "h-")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", daemon.NewRuntimePaths(runtimeDir).Socket)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan protocol.Request, 4)
	serveDone := make(chan struct{})
	var handlers sync.WaitGroup
	go func() {
		defer close(serveDone)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			handlers.Add(1)
			go func(conn net.Conn) {
				defer handlers.Done()
				defer conn.Close()
				decoder, encoder := protocol.NewDecoder(conn), protocol.NewEncoder(conn)
				request, decodeErr := decoder.DecodeRequest()
				if decodeErr != nil || request.Op != protocol.OpHello {
					return
				}
				if encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}) != nil {
					return
				}
				for {
					request, decodeErr = decoder.DecodeRequest()
					if decodeErr != nil {
						return
					}
					requests <- request
					switch request.Op {
					case protocol.OpOutput:
						cursor := protocol.Cursor(3)
						_ = encoder.EncodeResponse(protocol.NewOutputResponse(protocol.OutputResult{SessionID: sessionID, Entries: []protocol.OutputEntry{}, Next: &cursor}))
						return
					case protocol.OpFollow:
						_ = encoder.EncodeResponse(protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventReady, Name: "api", SessionID: sessionID, Ready: true})
						return
					case protocol.OpGet:
						_ = encoder.EncodeResponse(protocol.NewGetResponse(protocol.Process{Name: "api", SessionID: sessionID, State: protocol.StateRunning}))
					default:
						_ = encoder.EncodeResponse(protocol.NewWireError(protocol.ErrorInvalidRequest, "unexpected request", nil))
						return
					}
				}
			}(conn)
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-serveDone
		handlers.Wait()
		_ = os.RemoveAll(runtimeDir)
	})
	return runtimeDir, requests
}

func waitCLIProtocolCursor(value protocol.Cursor) *protocol.Cursor {
	return &value
}

func waitCLIRun(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr strings.Builder
	command := NewRootCommand("test", "test", &stdout, &stderr)
	command.ExitErrHandler = func(context.Context, *urfavecli.Command, error) {}
	argv := append([]string{"hum"}, args...)
	err := command.Run(context.Background(), argv)
	return stdout.String(), stderr.String(), err
}

func waitCLIExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	return 1
}
