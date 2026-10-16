//go:build !windows

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"hum/internal/daemon"
	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
)

func TestStatusPorts(t *testing.T) {
	listeners := []protocol.PortEndpoint{
		{Transport: "tcp", Address: "127.0.0.1", Port: 5173, PIDs: []int{4182}},
		{Transport: "tcp", Address: "::", Port: 9229, PIDs: []int{4182, 4190}},
	}
	for _, tc := range []struct {
		name       string
		inspection *protocol.PortInspection
		want       string
	}{
		{"available", &protocol.PortInspection{State: "available", Listeners: listeners}, "127.0.0.1:5173 (pid 4182), [::]:9229 (pids 4182,4190)"},
		{"empty", &protocol.PortInspection{State: "available", Listeners: []protocol.PortEndpoint{}}, "none"},
		{"partial", &protocol.PortInspection{State: "partial", Listeners: listeners, Diagnostic: "one member unreadable"}, "127.0.0.1:5173 (pid 4182), [::]:9229 (pids 4182,4190) (partial: one member unreadable)"},
		{"partial empty", &protocol.PortInspection{State: "partial", Listeners: []protocol.PortEndpoint{}, Diagnostic: "one member unreadable"}, "(partial: one member unreadable)"},
		{"denied", &protocol.PortInspection{State: "denied", Listeners: []protocol.PortEndpoint{}, Diagnostic: "permission denied"}, "denied (permission denied)"},
		{"unavailable", &protocol.PortInspection{State: "unavailable", Listeners: []protocol.PortEndpoint{}, Diagnostic: "lsof not found"}, "unavailable (lsof not found)"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/human", true: "/json"}[jsonOutput], func(t *testing.T) {
				requests := make(chan protocol.GetRequest, 1)
				runtimeDir, _, _ := manifestCLIRecoveryStubDaemon(t, map[string]protocol.Process{
					"api": {Name: "api", State: "running", PID: 4182, NextCursor: waitCLIProtocolCursor(0), Ports: tc.inspection},
				}, func(req protocol.GetRequest) { requests <- req })
				t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
				args := []string{"status", "api"}
				if jsonOutput {
					args = append(args, "--json")
				}
				stdout, stderr, err := stopShutdownRun(t, args...)
				if err != nil || stderr != "" {
					t.Fatalf("status: err=%v stderr=%q output=%q", err, stderr, stdout)
				}
				if req := <-requests; !req.Ports || req.Name != "api" {
					t.Fatalf("named status request = %+v", req)
				}
				if jsonOutput {
					got := statusDecodeJSON(t, stdout)
					want, _ := json.Marshal(tc.inspection)
					actual, _ := json.Marshal(got.Ports)
					if got.PID != 4182 || got.State != "running" || string(actual) != string(want) {
						t.Fatalf("status = %+v; ports = %s, want %s", got, actual, want)
					}
				} else if !strings.Contains(stdout, "listening: "+tc.want+"\n") || strings.Count(stdout, "listening:") != 1 {
					t.Fatalf("human status = %q, want single listening line %q", stdout, tc.want)
				}
			})
		}
	}
}

func TestListPortLookup(t *testing.T) {
	root := t.TempDir()
	holder := protocol.Process{
		Name: "web", Scope: protocol.ScopeProject, Root: root, Cwd: root, State: protocol.StateRunning, PID: 4182,
		Ports: &protocol.PortInspection{State: "available", Listeners: []protocol.PortEndpoint{{Transport: "tcp", Address: "127.0.0.1", Port: 3000, PIDs: []int{4190}}}},
	}
	response := protocol.PortLookupResponse{
		Op: protocol.OpPortLookup, OK: true, Processes: []protocol.Process{holder},
		Incomplete: []protocol.PortLookupIncomplete{{Name: "uncertain", Scope: protocol.ScopeGlobal, State: "denied", Diagnostic: "permission denied"}},
	}
	runtimeDir, operations, failures := portLookupCLITestDaemon(t, response)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	human, stderr, err := stopShutdownRun(t, "--project", root, "list", "--all", "--port", "3000")
	if err != nil || stderr != "" {
		t.Fatalf("human port lookup: err=%v stderr=%q stdout=%q", err, stderr, human)
	}
	if !strings.Contains(human, "Project: "+root+" (hum --project "+root+")") || !strings.Contains(human, "NAME  STATE    PID   LISTENING") || !strings.Contains(human, "127.0.0.1:3000 (pid 4190)") || !strings.Contains(human, "Incomplete: uncertain (hum --global) denied: permission denied") {
		t.Fatalf("human port lookup output = %q", human)
	}
	if op := <-operations; op != protocol.OpPortLookup {
		t.Fatalf("port lookup operation = %s", op)
	}
	if failures := drainPortLookupFailures(failures); len(failures) != 0 {
		t.Fatalf("port lookup stub failures: %v", failures)
	}

	jsonOutput, stderr, err := stopShutdownRun(t, "--project", root, "list", "--port", "3000", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("JSON port lookup: err=%v stderr=%q stdout=%q", err, stderr, jsonOutput)
	}
	var jsonResult listJSON
	if err := json.Unmarshal([]byte(jsonOutput), &jsonResult); err != nil {
		t.Fatalf("decode JSON port lookup: %v, output=%q", err, jsonOutput)
	}
	if len(jsonResult.Processes) != 1 || jsonResult.Processes[0].Name != "web" || jsonResult.Processes[0].Ports == nil || len(jsonResult.Processes[0].Ports.Listeners) != 1 || jsonResult.Processes[0].Ports.Listeners[0].Port != 3000 {
		t.Fatalf("JSON port lookup holders = %#v", jsonResult.Processes)
	}
	if len(jsonResult.Incomplete) != 1 || jsonResult.Incomplete[0].Name != "uncertain" || jsonResult.Incomplete[0].State != "denied" {
		t.Fatalf("JSON port lookup incomplete = %#v", jsonResult.Incomplete)
	}
	if op := <-operations; op != protocol.OpPortLookup {
		t.Fatalf("JSON lookup operation = %s", op)
	}
	if failures := drainPortLookupFailures(failures); len(failures) != 0 {
		t.Fatalf("JSON port lookup stub failures: %v", failures)
	}

	response.Processes = []protocol.Process{}
	response.Incomplete = []protocol.PortLookupIncomplete{}
	noHolderRuntime, noHolderOperations, noHolderFailures := portLookupCLITestDaemon(t, response)
	t.Setenv("HUM_RUNTIME_DIR", noHolderRuntime)
	noHolder, _, err := stopShutdownRun(t, "list", "--port", "3000")
	if err != nil || !strings.Contains(noHolder, "No Hum process is listening on TCP port 3000.") {
		t.Fatalf("empty port lookup: err=%v stdout=%q", err, noHolder)
	}
	if op := <-noHolderOperations; op != protocol.OpPortLookup {
		t.Fatalf("empty lookup operation = %s", op)
	}
	if failures := drainPortLookupFailures(noHolderFailures); len(failures) != 0 {
		t.Fatalf("port lookup stub failures: %v", failures)
	}

	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "zero", args: []string{"list", "--port", "0"}, want: "between 1 and 65535"},
		{name: "too large", args: []string{"list", "--port", "65536"}, want: "between 1 and 65535"},
		{name: "global", args: []string{"--global", "list", "--port", "3000"}, want: "does not accept --global"},
	} {
		t.Run("rejects "+test.name+" before daemon contact", func(t *testing.T) {
			runtime, rejectedOperations, rejectedFailures := portLookupCLITestDaemon(t, response)
			t.Setenv("HUM_RUNTIME_DIR", runtime)
			stdout, stderr, err := invokePortLookupCLI(t, test.args...)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("rejected list args %v: error=%v stdout=%q stderr=%q", test.args, err, stdout, stderr)
			}
			if len(rejectedOperations) != 0 || len(drainPortLookupFailures(rejectedFailures)) != 0 {
				t.Fatalf("invalid request contacted daemon: operations=%v", rejectedOperations)
			}
		})
	}

	plainRuntime, plainOperations, plainFailures := portLookupCLITestDaemon(t, response)
	t.Setenv("HUM_RUNTIME_DIR", plainRuntime)
	for _, args := range [][]string{
		{"--project", root, "list", "--json"},
		{"--project", root, "list", "--all", "--json"},
		{"--project", root, "status", "--json"},
	} {
		if _, _, err := stopShutdownRun(t, args...); err != nil {
			t.Fatalf("ordinary inspection-free command %v: %v", args, err)
		}
	}
	for index := 0; index < 3; index++ {
		if op := <-plainOperations; op != protocol.OpList {
			t.Fatalf("ordinary list/status operation = %s, want list", op)
		}
	}
	if failures := drainPortLookupFailures(plainFailures); len(failures) != 0 {
		t.Fatalf("ordinary list/status stub failures: %v", failures)
	}
}

func invokePortLookupCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	command := NewRootCommand("test", "test", &stdout, &stderr)
	command.ExitErrHandler = func(context.Context, *urfavecli.Command, error) {}
	argv := append([]string{"hum"}, args...)
	SetInvocationArgs(command, argv)
	err := command.Run(context.Background(), argv)
	return stdout.String(), stderr.String(), err
}

func portLookupCLITestDaemon(t *testing.T, response protocol.PortLookupResponse) (string, <-chan protocol.Operation, <-chan error) {
	t.Helper()
	runtimeDir, err := os.MkdirTemp("/tmp", "h-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	listener, err := net.Listen("unix", daemon.NewRuntimePaths(runtimeDir).Socket)
	if err != nil {
		t.Fatal(err)
	}
	operations := make(chan protocol.Operation, 16)
	failures := make(chan error, 16)
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("port lookup daemon did not stop")
		}
	})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				decoder := protocol.NewDecoder(conn)
				encoder := protocol.NewEncoder(conn)
				request, err := decoder.DecodeRequest()
				if err != nil || request.Op != protocol.OpHello {
					failures <- err
					return
				}
				if err := encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}); err != nil {
					failures <- err
					return
				}
				for {
					request, err = decoder.DecodeRequest()
					if err != nil {
						return
					}
					operations <- request.Op
					switch request.Op {
					case protocol.OpPortLookup:
						value := response
						value.Op, value.OK = protocol.OpPortLookup, true
						if err := encoder.EncodeResponse(value); err != nil {
							failures <- err
							return
						}
					case protocol.OpList:
						if err := encoder.EncodeResponse(protocol.ListResponse{Op: protocol.OpList, OK: true, Processes: []protocol.Process{}}); err != nil {
							failures <- err
							return
						}
					default:
						failures <- errors.New("unexpected operation: " + string(request.Op))
						_ = encoder.EncodeResponse(protocol.NewErrorResponse(request.Op, protocol.NewWireError(protocol.ErrorInternal, "unexpected request", nil)))
						return
					}
				}
			}()
		}
	}()
	return runtimeDir, operations, failures
}

func drainPortLookupFailures(failures <-chan error) []error {
	var result []error
	for {
		select {
		case err := <-failures:
			if err != nil {
				result = append(result, err)
			}
		default:
			return result
		}
	}
}

func TestStatusPortsAggregateAndTerminal(t *testing.T) {
	for _, name := range []string{"aggregate", "stopped", "exited", "missing"} {
		t.Run(name, func(t *testing.T) {
			requests := make(chan protocol.GetRequest, 1)
			processes := map[string]protocol.Process{}
			if name != "missing" {
				state := name
				if name == "aggregate" {
					state = "running"
				}
				processes["api"] = protocol.Process{Name: "api", State: state, NextCursor: waitCLIProtocolCursor(0)}
			}
			runtimeDir, operations, done := manifestCLIRecoveryStubDaemon(t, processes, func(req protocol.GetRequest) { requests <- req })
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			args := []string{"status"}
			if name != "aggregate" {
				args = append(args, "api")
			}
			args = append(args, "--json")
			stdout, _, err := stopShutdownRun(t, args...)
			<-done
			if name == "missing" {
				if err == nil {
					t.Fatal("missing status unexpectedly succeeded")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(stdout, "\"ports\"") {
				t.Fatalf("status exposed ports: %s", stdout)
			}
			if name == "aggregate" {
				if len(requests) != 0 {
					t.Fatal("aggregate status requested individual inspection")
				}
				if op := <-operations; op != protocol.OpList {
					t.Fatalf("aggregate operation = %s", op)
				}
			} else if name != "missing" {
				if got := statusDecodeJSON(t, stdout); got.State != name {
					t.Fatalf("status state = %s, want %s", got.State, name)
				}
			}
		})
	}
}
