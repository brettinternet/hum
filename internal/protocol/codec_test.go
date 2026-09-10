package protocol

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// TestProtocolRoundTripAllFields exercises every request and response
// operation with every optional DTO field populated. This is intentionally a
// protocol-package test: daemon code must use these DTOs directly rather than
// reconstructing a flattened transport model.
func TestProtocolRoundTripAllFields(t *testing.T) {
	stamp := time.Date(2026, time.September, 10, 12, 34, 56, 123, time.UTC)
	cursor, next := Cursor(7), Cursor(8)
	ready := &ReadinessConfig{Match: "ready", Timeout: 3 * time.Second}
	tty := &TTYSize{Columns: 120, Rows: 40}
	requests := []any{
		Hello{Op: OpHello, Version: 19},
		StartRequest{Op: OpStart, Scope: ScopeProject, Name: "start", Argv: []string{"tool", "--flag"}, Cwd: "/work", Root: "/project", Env: []string{"A=B"}, Source: "manifest", Ready: ready, TTY: true, TTYSize: tty, Restart: RestartOnFailure, Attached: true},
		ListRequest{Op: OpList, Scope: ScopeGlobal, Cwd: "/work", All: true, IncludeCompleted: true},
		GetRequest{Op: OpGet, Scope: ScopeProject, Name: "get", Cwd: "/work"},
		OutputRequest{Op: OpOutput, Scope: ScopeProject, Name: "output", Cwd: "/work", After: &cursor, SinceMS: 11, Tail: 12, Stream: StreamStdout, Match: "needle", MaxEntries: 13, MaxBytes: 14},
		OutputRequest{Op: OpOutput, Scope: ScopeProject, Name: "output-absolute", Cwd: "/work", SinceUnixNano: stamp.UnixNano()},
		FollowRequest{Op: OpFollow, Scope: ScopeProject, Name: "follow", Cwd: "/work", After: &cursor, UntilExit: true, SinceUnixNano: stamp.UnixNano(), Tail: 15, Stream: StreamStderr, Match: "follow", MaxEntries: 16, MaxBytes: 17},
		FollowRequest{Op: OpFollow, Scope: ScopeProject, Name: "follow-relative", Cwd: "/work", SinceMS: 19},
		WaitRequest{Op: OpWait, Scope: ScopeProject, Name: "wait", Cwd: "/work", After: &cursor, Match: "wait", TimeoutMS: 18},
		SignalRequest{Op: OpSignal, Scope: ScopeGlobal, Name: "signal", Cwd: "/work", Signal: "SIGTERM", Control: true},
		StopRequest{Op: OpStop, Scope: ScopeProject, Name: "stop", Cwd: "/work"},
		RestartRequest{Op: OpRestart, Scope: ScopeProject, Name: "restart", Cwd: "/work", Root: "/project", Update: true, Argv: []string{"new"}, Env: []string{"C=D"}, Source: "manifest", Ready: ready, TTY: true, TTYSize: tty, Restart: RestartOnFailure},
		RemoveRequest{Op: OpRemove, Scope: ScopeGlobal, Name: "remove", Cwd: "/work"},
		ShutdownRequest{Op: OpShutdown, Force: true},
		InputAttachRequest{Op: OpInputAttach, Scope: ScopeProject, Name: "input", Cwd: "/work", Root: "/project", TTY: true, Argv: []string{"shell"}, Source: "manifest", Ready: ready, Columns: 100, Rows: 30},
		InputReleaseRequest{Op: OpInputRelease},
		InputWriteRequest{Op: OpInputWrite, LaunchCursor: cursor, Data: "aGVsbG8="},
		InputResizeRequest{Op: OpInputResize, LaunchCursor: cursor, Columns: 100, Rows: 30},
	}
	for _, want := range requests {
		t.Run(requestOperationName(want), func(t *testing.T) {
			var wire bytes.Buffer
			if err := NewEncoder(&wire).EncodeRequest(want); err != nil {
				t.Fatal(err)
			}
			got, err := NewDecoder(bytes.NewReader(wire.Bytes())).DecodeRequest()
			if err != nil {
				t.Fatal(err)
			}
			var actual any
			switch got.Op {
			case OpHello:
				actual = *got.Hello
			case OpStart:
				actual = *got.Start
			case OpList:
				actual = *got.List
			case OpGet:
				actual = *got.Get
			case OpOutput:
				actual = *got.Output
			case OpFollow:
				actual = *got.Follow
			case OpWait:
				actual = *got.Wait
			case OpSignal:
				actual = *got.Signal
			case OpStop:
				actual = *got.Stop
			case OpRestart:
				actual = *got.Restart
			case OpRemove:
				actual = *got.Remove
			case OpShutdown:
				actual = *got.Shutdown
			case OpInputAttach:
				actual = *got.InputAttach
			case OpInputRelease:
				actual = *got.InputRelease
			case OpInputWrite:
				actual = *got.InputWrite
			case OpInputResize:
				actual = *got.InputResize
			}
			if !reflect.DeepEqual(want, actual) {
				t.Fatalf("request round trip = %#v, want %#v", actual, want)
			}
		})
	}
	requireEveryFieldPopulated(t, requests, nil)

	process := Process{Name: "process", Source: "manifest", Scope: ScopeProject, Root: "/project", TTY: true, PID: 41, PGID: 42, Cwd: "/work", Argv: []string{"tool"}, Start: stamp, LaunchCursor: cursor, NextCursor: &next, State: StateExited, Exit: &Exit{Code: -1, Time: stamp, Error: "failed", Signal: &SignalInfo{Name: "SIGTERM", Number: 15}}, ExitCode: -1, ExitedAt: stamp, RestartCount: 2, Followers: 3, Restart: RestartOnFailure, Relaunches: 4, NextLaunchAt: &stamp, Readiness: &Readiness{State: ReadinessReady, Cursor: &cursor, Time: stamp, Match: "ready"}}
	entries := []OutputEntry{{Cursor: cursor, Stream: StreamStdout, Time: stamp, Text: "output"}}
	wireError := NewWireError(ErrorInvalidRequest, "bad", map[string]any{"client": 18, "daemon": 19})
	responses := []any{
		HelloResponse{Op: OpHello, Version: 19, Warnings: []StartupWarning{{Project: "/project", Name: "x", Outcome: "reclaimed", Message: "ok"}}},
		StartResponse{Op: OpStart, OK: true, Process: &process, Warnings: []StartupWarning{{Project: "/project", Name: "x", Outcome: "reclaimed", Message: "ok"}}},
		ListResponse{Op: OpList, OK: true, Processes: []Process{process}, Warnings: []StartupWarning{{Project: "/project", Name: "x", Outcome: "unresolved", Message: "hold"}}},
		GetResponse{Op: OpGet, OK: true, Process: &process, Warnings: []StartupWarning{{Project: "/project", Name: "x", Outcome: "reclaimed", Message: "ok"}}},
		WaitResponse{Op: OpWait, OK: true, Outcome: WaitExited, Cursor: cursor, Exit: process.Exit, Message: "done"},
		WaitResponse{Op: OpWait, OK: true, Outcome: WaitTimedOut, Cursor: cursor, ProcessObserved: true, Message: "timed out"},
		OutputResponse{Op: OpOutput, OK: true, Entries: entries, Next: &next, Oldest: &cursor, Latest: &next, EvictedThrough: &cursor, Truncated: true, More: true},
		SignalResponse{Op: OpSignal, OK: true, Name: "signal", Signal: &SignalInfo{Name: "SIGTERM", Number: 15}, Status: "sent"},
		StopResponse{Op: OpStop, OK: true, Process: &process}, RestartResponse{Op: OpRestart, OK: true, Process: &process},
		RemoveResponse{Op: OpRemove, OK: true}, ShutdownResponse{Op: OpShutdown, OK: true, Processes: []Process{process}},
		StreamEvent{Op: OpEvent, Type: EventOutput, Name: "event", Entries: entries, Next: &next, Oldest: &cursor, Latest: &next, EvictedThrough: &cursor, Truncated: true, More: true, Cursor: &cursor, Ready: true, Warnings: []StartupWarning{{Project: "/project", Name: "x", Outcome: "reclaimed", Message: "ok"}}, Time: stamp, Exit: process.Exit},
		InputAttachResponse{Op: OpInputAttach, OK: true}, InputAckResponse{Op: OpInputWrite, OK: true, LaunchCursor: cursor, Written: 5}, InputAckResponse{Op: OpInputResize, OK: true, LaunchCursor: cursor}, InputAckResponse{Op: OpInputRelease, OK: true}, InputStateEvent{Op: OpInputState, State: StateRunning, LaunchCursor: cursor, TTY: true},
		ErrorResponse{Op: OpGet, OK: false, Error: wireError},
	}
	for _, want := range responses {
		t.Run("response/"+responseOperationName(want), func(t *testing.T) {
			var wire bytes.Buffer
			if err := NewEncoder(&wire).EncodeResponse(want); err != nil {
				t.Fatal(err)
			}
			got, err := NewDecoder(bytes.NewReader(wire.Bytes())).DecodeResponse()
			if err != nil {
				t.Fatal(err)
			}
			if got.Op != responseOperation(want) {
				t.Fatalf("response op=%q, want %q", got.Op, responseOperation(want))
			}
			actual := responseValue(got)
			wantJSON, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			actualJSON, err := json.Marshal(actual)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(wantJSON, actualJSON) {
				t.Fatalf("response round trip = %s, want %s", actualJSON, wantJSON)
			}
			if warnings := reflect.ValueOf(want).FieldByName("Warnings"); warnings.IsValid() && !reflect.DeepEqual(got.Warnings, warnings.Interface()) {
				t.Fatalf("envelope warnings = %#v, want %#v", got.Warnings, warnings.Interface())
			}
			if expected, ok := want.(WaitResponse); ok && expected.Outcome == WaitTimedOut {
				decoded := actual.(WaitResponse)
				if !decoded.ProcessObserved {
					t.Fatal("wait timeout lost nonzero process_observed")
				}
			}
		})
	}
	// Typed responses carry Error only on failure, which ErrorResponse covers.
	requireEveryFieldPopulated(t, responses, map[string]bool{"Error": true, "ErrorResponse.OK": true})
	requireEveryFieldPopulated(t, []any{process, *process.Exit, *process.Exit.Signal, *process.Readiness, entries[0], *wireError}, nil)
}

// requireEveryFieldPopulated fails when an exported, JSON-visible field of a
// fixture type is zero in every fixture of that type. A comparison against a
// hand-written literal cannot notice a new DTO field that both sides leave at
// its zero value, so this forces every field to take part in the round trip.
func requireEveryFieldPopulated(t *testing.T, fixtures []any, allowZero map[string]bool) {
	t.Helper()
	populated := map[reflect.Type]map[string]bool{}
	for _, fixture := range fixtures {
		value := reflect.ValueOf(fixture)
		if populated[value.Type()] == nil {
			populated[value.Type()] = map[string]bool{}
		}
		for index := 0; index < value.NumField(); index++ {
			if !value.Field(index).IsZero() {
				populated[value.Type()][value.Type().Field(index).Name] = true
			}
		}
	}
	for typ, fields := range populated {
		for index := 0; index < typ.NumField(); index++ {
			field := typ.Field(index)
			if !field.IsExported() || field.Tag.Get("json") == "-" || fields[field.Name] || allowZero[field.Name] || allowZero[typ.Name()+"."+field.Name] {
				continue
			}
			t.Errorf("%s.%s is zero in every fixture; populate it so the round trip proves it survives", typ.Name(), field.Name)
		}
	}
}

func responseValue(value ResponseEnvelope) any {
	switch value.Op {
	case OpHello:
		return *value.Hello
	case OpStart:
		return *value.Start
	case OpList:
		return *value.List
	case OpGet:
		return *value.Get
	case OpWait:
		return *value.Wait
	case OpOutput:
		return *value.Output
	case OpSignal:
		return *value.Signal
	case OpStop:
		return *value.Stop
	case OpRestart:
		return *value.Restart
	case OpRemove:
		return *value.Remove
	case OpShutdown:
		return *value.Shutdown
	case OpEvent:
		return *value.Event
	case OpInputAttach:
		return *value.InputAttach
	case OpInputWrite, OpInputResize, OpInputRelease:
		return *value.InputAck
	case OpInputState:
		return *value.InputState
	default:
		return *value.Generic
	}
}

func requestOperationName(value any) string { return string(requestOperation(value)) }
func requestOperation(value any) Operation {
	switch v := value.(type) {
	case Hello:
		return v.Op
	case StartRequest:
		return v.Op
	case ListRequest:
		return v.Op
	case GetRequest:
		return v.Op
	case OutputRequest:
		return v.Op
	case FollowRequest:
		return v.Op
	case WaitRequest:
		return v.Op
	case SignalRequest:
		return v.Op
	case StopRequest:
		return v.Op
	case RestartRequest:
		return v.Op
	case RemoveRequest:
		return v.Op
	case ShutdownRequest:
		return v.Op
	case InputAttachRequest:
		return v.Op
	case InputReleaseRequest:
		return v.Op
	case InputWriteRequest:
		return v.Op
	case InputResizeRequest:
		return v.Op
	default:
		return ""
	}
}
func responseOperationName(value any) string { return string(responseOperation(value)) }
func responseOperation(value any) Operation {
	switch v := value.(type) {
	case HelloResponse:
		return v.Op
	case StartResponse:
		return v.Op
	case ListResponse:
		return v.Op
	case GetResponse:
		return v.Op
	case WaitResponse:
		return v.Op
	case OutputResponse:
		return v.Op
	case SignalResponse:
		return v.Op
	case StopResponse:
		return v.Op
	case RestartResponse:
		return v.Op
	case RemoveResponse:
		return v.Op
	case ShutdownResponse:
		return v.Op
	case StreamEvent:
		return v.Op
	case InputAttachResponse:
		return v.Op
	case InputAckResponse:
		return v.Op
	case InputStateEvent:
		return v.Op
	case ErrorResponse:
		return v.Op
	default:
		return ""
	}
}
