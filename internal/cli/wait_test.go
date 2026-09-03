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

func TestWaitCLIValidation(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		want           string
		frameworkParse bool
	}{
		{name: "missing name", args: []string{"wait"}, want: "wait requires a process name"},
		{name: "too many names", args: []string{"wait", "one", "two"}, want: "wait accepts exactly one process name"},
		{name: "invalid cursor", args: []string{"wait", "api", "--after-cursor", "-1"}, want: "after-cursor", frameworkParse: true},
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
			if test.frameworkParse {
				if !strings.Contains(strings.ToLower(stdout), "usage:") {
					t.Errorf("wait flag-parse stdout = %q, want usage", stdout)
				}
				if !strings.Contains(strings.ToLower(stderr), "incorrect usage") ||
					!strings.Contains(strings.ToLower(stderr), strings.ToLower(test.want)) {
					t.Errorf("wait flag-parse stderr = %q, want incorrect usage mentioning %q", stderr, test.want)
				}
			} else if stdout != "" || stderr != "" {
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

func TestWaitCLIUnavailableDoesNotCreateRuntimeState(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	stdout, stderr, err := waitCLIRun(t, "wait", "api")
	if err == nil || err.Error() != logsUnavailableMessage {
		t.Fatalf("unavailable wait error = %v, want exact %q", err, logsUnavailableMessage)
	}
	if got := waitCLIExitCode(err); got != 1 {
		t.Fatalf("unavailable wait exit code = %d, want 1", got)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("unavailable wait output = stdout %q stderr %q, want empty", stdout, stderr)
	}
	if _, statErr := os.Stat(runtimeDir); !os.IsNotExist(statErr) {
		t.Fatalf("unavailable wait runtime state stat error = %v, want not-exist", statErr)
	}
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
