package daemon

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/process"
	"hum/internal/protocol"
)

func TestGlobalScopeWireValidation(t *testing.T) {
	legacy := protocol.Request{Op: protocol.OpGet, Get: &protocol.GetRequest{Op: protocol.OpGet, Name: "proxy", Cwd: "/project"}}
	if err := normalizeProtocolScope(&legacy); err != nil || legacy.Get.Scope != app.ScopeProject {
		t.Fatalf("legacy scope=%q err=%v", legacy.Get.Scope, err)
	}
	cases := []protocol.Request{{Op: protocol.OpGet, Get: &protocol.GetRequest{Op: protocol.OpGet, Scope: "machine"}}, {Op: protocol.OpStart, Start: &protocol.StartRequest{Op: protocol.OpStart, Scope: app.ScopeGlobal, Root: "/project"}}, {Op: protocol.OpList, List: &protocol.ListRequest{Op: protocol.OpList, Scope: app.ScopeGlobal, All: true}}}
	for _, request := range cases {
		if err := normalizeProtocolScope(&request); err == nil {
			t.Fatalf("invalid global request accepted: %#v", request)
		}
	}
	server := testServer(t, Config{RuntimeDir: shortRuntimeDir(t)})
	root := t.TempDir()
	for _, request := range []app.StartRequest{{Name: "proxy", Cwd: root, Argv: []string{"/bin/sh", "-c", "sleep 30"}, Env: []string{"PATH=/usr/bin:/bin"}}, {Scope: app.ScopeGlobal, Name: "proxy", Cwd: t.TempDir(), Argv: []string{"/bin/sh", "-c", "sleep 30"}, Env: []string{"PATH=/usr/bin:/bin"}}} {
		if _, err := server.supervisor.Start(request); err != nil {
			t.Fatal(err)
		}
	}
	items, err := server.listProcessesScoped(root, app.ScopeProject, true, true)
	if err != nil || len(items) != 2 || items[0].Scope == items[1].Scope {
		t.Fatalf("all-scope list=%+v err=%v", items, err)
	}
}

func TestSignalExitWireStreamRoundTrip(t *testing.T) {
	exitedAt := time.Date(2026, time.September, 6, 12, 34, 56, 0, time.UTC)
	event := protocolStreamEventFromOutput("signal", output.Event{Exit: &output.Exit{
		Code: -1, Time: exitedAt, SignalName: "SIGTERM", SignalNumber: 15,
	}})
	if event.Exit == nil || event.Exit.Code != -1 || event.Exit.Signal == nil || event.Exit.Signal.Name != "SIGTERM" || event.Exit.Signal.Number != 15 {
		t.Fatalf("signal stream event = %#v, want SIGTERM exit", event)
	}
}

func TestTerminalStateWireRoundTrip(t *testing.T) {
	exitedAt := time.Date(2026, time.September, 6, 12, 34, 56, 0, time.UTC)
	cases := []struct {
		name string
		want app.Process
	}{
		{
			name: "stopped",
			want: app.Process{Name: "operator", Root: "/work/project", Cwd: "/work/project", Argv: []string{"sleep", "30"}, State: app.StateStopped},
		},
		{
			name: "exited zero",
			want: app.Process{Name: "zero", Root: "/work/project", Cwd: "/work/project", Argv: []string{"true"}, State: app.StateExited, Exit: &process.Result{ExitCode: 0, ExitedAt: exitedAt}},
		},
		{
			name: "exited signal",
			want: app.Process{Name: "signal", Root: "/work/project", Cwd: "/work/project", Argv: []string{"kill"}, State: app.StateExited, Exit: &process.Result{ExitCode: -1, ExitedAt: exitedAt, Signal: &process.SignalInfo{Name: "SIGTERM", Number: 15}, Err: errors.New("terminated by signal")}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := protocolProcessFromApp(tc.want)
			encoded, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			var decoded protocol.Process
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			got := decoded
			if got.State != string(tc.want.State) {
				t.Fatalf("state = %q, want %q", got.State, tc.want.State)
			}
			if tc.want.Exit == nil {
				if got.Exit != nil || got.ExitCode != 0 || !got.ExitedAt.IsZero() {
					t.Fatalf("stopped exit details = %#v, code=%d, at=%v", got.Exit, got.ExitCode, got.ExitedAt)
				}
				return
			}
			wantError := ""
			if tc.want.Exit.Err != nil {
				wantError = tc.want.Exit.Err.Error()
			}
			if got.Exit == nil || got.Exit.Code != tc.want.Exit.ExitCode || !got.Exit.Time.Equal(tc.want.Exit.ExitedAt) || got.Exit.Error != wantError {
				t.Fatalf("exit = %#v, want code=%d time=%v error=%q", got.Exit, tc.want.Exit.ExitCode, tc.want.Exit.ExitedAt, wantError)
			}
			if tc.want.Exit.Signal == nil {
				if got.Exit.Signal != nil {
					t.Fatalf("exit signal = %#v, want omitted", got.Exit.Signal)
				}
			} else if got.Exit.Signal == nil || got.Exit.Signal.Name != tc.want.Exit.Signal.Name || got.Exit.Signal.Number != tc.want.Exit.Signal.Number {
				t.Fatalf("exit signal = %#v, want %#v", got.Exit.Signal, tc.want.Exit.Signal)
			}
		})
	}
}
