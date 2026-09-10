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

func TestSystemStreamSelection(t *testing.T) {
	store, err := output.NewStore(output.Limits{RetainedBytes: 4096, DefaultReadEntries: 100, DefaultReadBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	entries := []struct {
		stream output.Stream
		text   string
	}{
		{output.Stdout, "child-out\n"},
		{output.System, "api launched\n"},
		{output.Stderr, "child-err\n"},
		{output.System, "api restarted\n"},
		{output.Stdout, "child-later\n"},
	}
	for index, entry := range entries {
		if _, appendErr := store.Append(entry.stream, at.Add(time.Duration(index)*time.Second), entry.text); appendErr != nil {
			t.Fatal(appendErr)
		}
	}

	for _, test := range []struct {
		stream protocol.Stream
		mask   output.StreamMask
	}{{protocol.StreamStdout, output.StdoutMask}, {protocol.StreamStderr, output.StderrMask}, {protocol.StreamSystem, output.SystemMask}, {protocol.StreamBoth, output.AllStreams}} {
		options, optionsErr := readOptionsFromProtocol(protocol.OutputRequest{Stream: test.stream})
		if optionsErr != nil || options.Streams != test.mask {
			t.Fatalf("stream %q options = %#v, err=%v; want mask %v", test.stream, options, optionsErr, test.mask)
		}
		follow, followErr := readOptionsFromFollow(protocol.FollowRequest{Stream: test.stream})
		if followErr != nil || follow.Streams != test.mask {
			t.Fatalf("follow stream %q options = %#v, err=%v; want mask %v", test.stream, follow, followErr, test.mask)
		}
	}

	after := protocol.Cursor(0)
	options, err := readOptionsFromProtocol(protocol.OutputRequest{
		After: &after, SinceUnixNano: at.Add(time.Second).UnixNano(), Tail: 1,
		Stream: protocol.StreamSystem, Match: "launched|restarted", MaxEntries: 1, MaxBytes: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.Read(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Stream != output.System || result.Entries[0].Text != "api restarted\n" {
		t.Fatalf("system result = %#v, want only newest matching supervision entry", result)
	}
	if result.Next == nil || result.Oldest == nil || result.Latest == nil {
		t.Fatalf("system result metadata = %#v, want unchanged cursor bounds", result)
	}

	bothOptions, err := readOptionsFromProtocol(protocol.OutputRequest{Stream: protocol.StreamBoth})
	if err != nil {
		t.Fatal(err)
	}
	both, err := store.Read(bothOptions)
	if err != nil {
		t.Fatal(err)
	}
	if len(both.Entries) != len(entries) {
		t.Fatalf("both entries = %#v, want all stdout, stderr, and system entries", both.Entries)
	}
}

func TestMatchContextWireOptions(t *testing.T) {
	opts, err := readOptionsFromProtocol(protocol.OutputRequest{Stream: protocol.StreamSystem, Match: "ERROR", Context: 2})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Streams != output.SystemMask || opts.Match == nil || !opts.Match.MatchString("ERROR") || opts.Context != 2 {
		t.Fatalf("match context options = %#v", opts)
	}
	for _, request := range []protocol.OutputRequest{{Context: -1, Match: "ERROR"}, {Context: 1}} {
		if _, err := readOptionsFromProtocol(request); err == nil || !errors.Is(err, app.ErrInvalidRequest) {
			t.Fatalf("invalid match context request %#v error = %v, want invalid request", request, err)
		}
	}
	follow, err := readOptionsFromFollow(protocol.FollowRequest{Match: "ERROR"})
	if err != nil || follow.Context != 0 {
		t.Fatalf("follow context = %d, err = %v", follow.Context, err)
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
