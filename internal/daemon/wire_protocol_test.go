package daemon

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/process"
)

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
			wire := wireProcessFromApp(tc.want)
			encoded, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			var decoded wireProcess
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			got := protocolProcessFromWire(decoded)
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
