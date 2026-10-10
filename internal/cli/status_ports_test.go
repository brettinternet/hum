//go:build !windows

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"hum/internal/protocol"
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
