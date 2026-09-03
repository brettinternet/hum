package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHelloAndShutdownFrozenShapes(t *testing.T) {
	hello, err := json.Marshal(NewHello())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(hello), `{"op":"hello","version":5}`; got != want {
		t.Fatalf("hello JSON = %s, want %s", got, want)
	}
	var decodedHello Hello
	if err := json.Unmarshal(hello, &decodedHello); err != nil {
		t.Fatal(err)
	}
	if decodedHello != NewHello() {
		t.Fatalf("decoded hello = %#v, want %#v", decodedHello, NewHello())
	}

	shutdown, err := json.Marshal(NewShutdownRequest(true))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(shutdown), `{"op":"shutdown","force":true}`; got != want {
		t.Fatalf("shutdown JSON = %s, want %s", got, want)
	}
	var decodedShutdown ShutdownRequest
	if err := json.Unmarshal(shutdown, &decodedShutdown); err != nil {
		t.Fatal(err)
	}
	if decodedShutdown.Op != OpShutdown || !decodedShutdown.Force {
		t.Fatalf("decoded shutdown = %#v", decodedShutdown)
	}
}

func TestOperationRequestsRoundTripThroughDecoder(t *testing.T) {
	after := Cursor(17)
	cases := []struct {
		name  string
		value any
		op    Operation
		check func(t *testing.T, req Request)
	}{
		{name: "hello", value: NewHello(), op: OpHello, check: func(t *testing.T, req Request) {
			if req.Hello == nil || req.Version != Version {
				t.Fatalf("hello request = %#v", req)
			}
		}},
		{name: "start", value: NewStartRequest("api", []string{"go", "run", "."}, "/tmp/project", []string{"PORT=8080"}), op: OpStart, check: func(t *testing.T, req Request) {
			if req.Start == nil || req.Start.Name != "api" || !reflect.DeepEqual(req.Start.Env, []string{"PORT=8080"}) {
				t.Fatalf("start request = %#v", req.Start)
			}
		}},
		{name: "list", value: NewListRequest("/tmp/project", true, true), op: OpList, check: func(t *testing.T, req Request) {
			if req.List == nil || !req.List.All || !req.List.IncludeCompleted {
				t.Fatalf("list request = %#v", req.List)
			}
		}},
		{name: "get", value: NewGetRequest("api", "/tmp/project"), op: OpGet, check: func(t *testing.T, req Request) {
			if req.Get == nil || req.Get.Name != "api" {
				t.Fatalf("get request = %#v", req.Get)
			}
		}},
		{name: "output", value: func() OutputRequest {
			r := NewOutputRequest("api", "/tmp/project")
			r.After, r.Tail, r.Stream, r.MaxEntries, r.MaxBytes = &after, 3, StreamBoth, 4, 256
			return r
		}(), op: OpOutput, check: func(t *testing.T, req Request) {
			if req.Output == nil || req.Output.After == nil || *req.Output.After != after || req.Output.Stream != StreamBoth {
				t.Fatalf("output request = %#v", req.Output)
			}
		}},
		{name: "follow", value: func() FollowRequest {
			r := NewFollowRequest("api", "/tmp/project")
			r.After, r.Stream = &after, StreamStdout
			return r
		}(), op: OpFollow, check: func(t *testing.T, req Request) {
			if req.Follow == nil || req.Follow.After == nil || *req.Follow.After != after {
				t.Fatalf("follow request = %#v", req.Follow)
			}
		}},
		{name: "wait", value: func() WaitRequest {
			r := NewWaitRequest("api", "/tmp/project", 1500)
			r.After, r.Match = &after, "ready"
			return r
		}(), op: OpWait, check: func(t *testing.T, req Request) {
			if req.Wait == nil || req.Wait.After == nil || *req.Wait.After != after ||
				req.Wait.Match != "ready" || req.Wait.TimeoutMS != 1500 {
				t.Fatalf("wait request = %#v", req.Wait)
			}
		}},
		{name: "signal", value: NewSignalRequest("api", "/tmp/project", "SIGINT"), op: OpSignal, check: func(t *testing.T, req Request) {
			if req.Signal == nil || req.Signal.Signal != "SIGINT" {
				t.Fatalf("signal request = %#v", req.Signal)
			}
		}},
		{name: "stop", value: NewStopRequest("api", "/tmp/project"), op: OpStop, check: func(t *testing.T, req Request) {
			if req.Stop == nil || req.Stop.Name != "api" {
				t.Fatalf("stop request = %#v", req.Stop)
			}
		}},
		{name: "restart", value: func() RestartRequest {
			r := NewRestartRequest("api", "/tmp/project")
			r.Update = true
			r.Argv = []string{"./server", "--port", "8080"}
			r.Env = []string{"TOKEN=updated"}
			r.Source = "manifest"
			r.Ready = &ReadinessConfig{Match: "ready", Timeout: time.Second}
			return r
		}(), op: OpRestart, check: func(t *testing.T, req Request) {
			if req.Restart == nil || !req.Restart.Update || !reflect.DeepEqual(req.Restart.Argv, []string{"./server", "--port", "8080"}) ||
				!reflect.DeepEqual(req.Restart.Env, []string{"TOKEN=updated"}) || req.Restart.Source != "manifest" ||
				req.Restart.Ready == nil || req.Restart.Ready.Match != "ready" {
				t.Fatalf("restart request = %#v", req.Restart)
			}
		}},
		{name: "shutdown", value: NewShutdownRequest(false), op: OpShutdown, check: func(t *testing.T, req Request) {
			if req.Shutdown == nil || req.Shutdown.Force {
				t.Fatalf("shutdown request = %#v", req.Shutdown)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line, err := MarshalLine(tc.value, 4096)
			if err != nil {
				t.Fatal(err)
			}
			var header map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(line), &header); err != nil {
				t.Fatal(err)
			}
			if header["op"] != string(tc.op) {
				t.Fatalf("op = %#v, want %q", header["op"], tc.op)
			}
			req, err := NewDecoder(bytes.NewReader(line), 4096).DecodeRequest()
			if err != nil {
				t.Fatal(err)
			}
			if req.Op != tc.op || len(req.Raw) == 0 || len(req.Payload) == 0 {
				t.Fatalf("request envelope = %#v", req)
			}
			tc.check(t, req)
		})
	}
}

func TestWaitRequestAndResponseShapes(t *testing.T) {
	request := NewWaitRequest("api", "/work/project", 1500)
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"op":"wait","name":"api","cwd":"/work/project","timeout_ms":1500}`; got != want {
		t.Fatalf("wait request JSON = %s, want %s", got, want)
	}
	var decodedRequest WaitRequest
	if err := json.Unmarshal(encoded, &decodedRequest); err != nil {
		t.Fatal(err)
	}
	if decodedRequest.Op != OpWait || decodedRequest.Name != request.Name ||
		decodedRequest.Cwd != request.Cwd || decodedRequest.TimeoutMS != request.TimeoutMS ||
		decodedRequest.After != nil || decodedRequest.Match != "" {
		t.Fatalf("decoded wait request = %#v", decodedRequest)
	}

	zero := Cursor(0)
	request.After, request.Match = &zero, "ready"
	encoded, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"op":"wait","name":"api","cwd":"/work/project","after":0,"match":"ready","timeout_ms":1500}`; got != want {
		t.Fatalf("wait request with filters JSON = %s, want %s", got, want)
	}
	var decodedFiltered WaitRequest
	if err := json.Unmarshal(encoded, &decodedFiltered); err != nil {
		t.Fatal(err)
	}
	if decodedFiltered.After == nil || *decodedFiltered.After != 0 || decodedFiltered.Match != "ready" {
		t.Fatalf("decoded filtered wait request = %#v", decodedFiltered)
	}

	var union Request
	if err := json.Unmarshal(encoded, &union); err != nil {
		t.Fatal(err)
	}
	if union.Op != OpWait || union.Wait == nil || union.Wait.After == nil || *union.Wait.After != 0 {
		t.Fatalf("wait request union = %#v", union)
	}
	if err := json.Unmarshal([]byte(`{"op":"start","name":"api","argv":["true"],"cwd":"/work/project","env":[]}`), &union); err != nil {
		t.Fatal(err)
	}
	if union.Wait != nil || union.Start == nil {
		t.Fatalf("request union retained stale wait = %#v", union)
	}

	at := time.Date(2026, time.September, 3, 11, 22, 33, 0, time.UTC)
	responseCases := []struct {
		name  string
		value WaitResponse
		want  string
	}{
		{
			name:  "matched",
			value: NewWaitResponse(WaitMatched, 7, nil),
			want:  `{"op":"wait","ok":true,"outcome":"matched","cursor":7}`,
		},
		{
			name:  "exited",
			value: NewWaitResponse(WaitExited, 9, &Exit{Code: 17, Time: at}),
			want:  `{"op":"wait","ok":true,"outcome":"exited","cursor":9,"exit":{"code":17,"time":"2026-09-03T11:22:33Z"}}`,
		},
		{
			name:  "timed out",
			value: NewWaitResponse(WaitTimedOut, 11, nil),
			want:  `{"op":"wait","ok":true,"outcome":"timed_out","cursor":11}`,
		},
	}
	for _, tc := range responseCases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(encoded); got != tc.want {
				t.Fatalf("wait response JSON = %s, want %s", got, tc.want)
			}
			var decoded WaitResponse
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Op != OpWait || !decoded.OK || decoded.Outcome != tc.value.Outcome ||
				decoded.Cursor != tc.value.Cursor {
				t.Fatalf("decoded wait response = %#v, want %#v", decoded, tc.value)
			}
			if !reflect.DeepEqual(decoded.Exit, tc.value.Exit) {
				t.Fatalf("decoded wait exit = %#v, want %#v", decoded.Exit, tc.value.Exit)
			}
		})
	}

	errorResponse := WaitResponse{
		Op:     OpWait,
		OK:     false,
		Cursor: 12,
		Error:  NewWireError(ErrorInternal, "wait failed", nil),
	}
	encoded, err = json.Marshal(errorResponse)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"op":"wait","ok":false,"cursor":12,"error":{"code":"internal","message":"wait failed"}}`; got != want {
		t.Fatalf("wait error JSON = %s, want %s", got, want)
	}
	var decodedError WaitResponse
	if err := json.Unmarshal(encoded, &decodedError); err != nil {
		t.Fatal(err)
	}
	if decodedError.OK || decodedError.Cursor != errorResponse.Cursor || decodedError.Error == nil ||
		decodedError.Error.Code != ErrorInternal {
		t.Fatalf("decoded wait error = %#v", decodedError)
	}

	var invalidRequest WaitRequest
	if err := json.Unmarshal([]byte(`{"op":"get","name":"api","cwd":"/work/project","timeout_ms":1500}`), &invalidRequest); err == nil {
		t.Fatal("wait request with another operation unexpectedly decoded")
	} else {
		var unknown *UnknownOperationError
		if !errors.As(err, &unknown) || unknown.Operation != OpGet {
			t.Fatalf("wait request rejection = %v", err)
		}
	}
	var invalidResponse WaitResponse
	if err := json.Unmarshal([]byte(`{"op":"get","ok":true,"cursor":1}`), &invalidResponse); err == nil {
		t.Fatal("wait response with another operation unexpectedly decoded")
	} else {
		var unknown *UnknownOperationError
		if !errors.As(err, &unknown) || unknown.Operation != OpGet {
			t.Fatalf("wait response rejection = %v", err)
		}
	}
}

func TestStatusGetRequestResponseRoundTrip(t *testing.T) {
	request := NewGetRequest("api", "/work/project")
	requestLine, err := MarshalLine(request, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(requestLine), `{"op":"get","name":"api","cwd":"/work/project"}`+"\n"; got != want {
		t.Fatalf("get request JSON = %s, want %s", got, want)
	}
	decodedRequest, err := NewDecoder(bytes.NewReader(requestLine), 4096).DecodeRequest()
	if err != nil {
		t.Fatal(err)
	}
	if decodedRequest.Op != OpGet || decodedRequest.Get == nil || *decodedRequest.Get != request {
		t.Fatalf("decoded get request = %#v, want %#v", decodedRequest.Get, request)
	}

	startedAt := time.Date(2026, time.September, 3, 11, 22, 33, 0, time.UTC)
	nextCursor := Cursor(19)
	process := Process{
		Name:         "api",
		Root:         "/work/project",
		PID:          4321,
		PGID:         4321,
		Cwd:          "/work/project",
		Argv:         []string{"tool", "--message", "hello world", ""},
		Start:        startedAt,
		LaunchCursor: 7,
		NextCursor:   &nextCursor,
		State:        "running",
		RestartCount: 2,
	}
	responseLine, err := MarshalLine(NewGetResponse(process), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(responseLine), `{"op":"get","ok":true,"process":{"name":"api","root":"/work/project","pid":4321,"pgid":4321,"cwd":"/work/project","argv":["tool","--message","hello world",""],"start":"2026-09-03T11:22:33Z","launch_cursor":7,"next_cursor":19,"state":"running","exited_at":"0001-01-01T00:00:00Z","restart_count":2}}`+"\n"; got != want {
		t.Fatalf("get response JSON = %s, want %s", got, want)
	}

	var decodedResponse GetResponse
	if err := NewDecoder(bytes.NewReader(responseLine), 4096).Decode(&decodedResponse); err != nil {
		t.Fatal(err)
	}
	if decodedResponse.Op != OpGet || !decodedResponse.OK || decodedResponse.Process == nil {
		t.Fatalf("decoded get response = %#v", decodedResponse)
	}
	if got := decodedResponse.Process; got.Name != process.Name || got.Root != process.Root ||
		got.PID != process.PID || got.PGID != process.PGID || got.Cwd != process.Cwd ||
		!reflect.DeepEqual(got.Argv, process.Argv) || !got.Start.Equal(process.Start) ||
		got.LaunchCursor != process.LaunchCursor || got.NextCursor == nil ||
		*got.NextCursor != *process.NextCursor || got.State != process.State ||
		got.RestartCount != process.RestartCount {
		t.Fatalf("decoded process = %#v, want %#v", got, process)
	}
	if decodedResponse.Process.Exit != nil {
		t.Fatalf("running process exit = %#v, want nil", decodedResponse.Process.Exit)
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(responseLine), &envelope); err != nil {
		t.Fatal(err)
	}
	var processFields map[string]json.RawMessage
	if err := json.Unmarshal(envelope["process"], &processFields); err != nil {
		t.Fatal(err)
	}
	if got, want := string(processFields["start"]), `"2026-09-03T11:22:33Z"`; got != want {
		t.Fatalf("start JSON = %s, want RFC3339 %s", got, want)
	}
	if got, want := string(processFields["next_cursor"]), "19"; got != want {
		t.Fatalf("next_cursor JSON = %s, want %s", got, want)
	}
	for _, name := range []string{"env", "environment"} {
		if _, ok := processFields[name]; ok {
			t.Fatalf("status response leaked %q: %s", name, responseLine)
		}
	}
}

func TestStatusGetResponseNullableExit(t *testing.T) {
	wire := []byte(`{"op":"get","ok":true,"process":{"name":"api","root":"/work/project","pid":4321,"pgid":4321,"cwd":"/work/project","argv":["tool"],"start":"2026-09-03T11:22:33Z","launch_cursor":7,"next_cursor":19,"state":"running","exit":null}}`)
	var response GetResponse
	if err := json.Unmarshal(wire, &response); err != nil {
		t.Fatal(err)
	}
	if response.Process == nil {
		t.Fatal("nullable-exit response omitted process")
	}
	if response.Process.Exit != nil {
		t.Fatalf("nullable exit decoded as %#v, want nil", response.Process.Exit)
	}
}

func TestStableFieldNamesAndResponseEnvironmentIsolation(t *testing.T) {
	request := NewStartRequest("api", []string{"sleep", "1"}, "/work", []string{"SECRET=do-not-echo"})
	line, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"op", "name", "argv", "cwd", "env"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("start field %q missing from %s", name, line)
		}
	}
	for _, name := range []string{"operation", "command", "environment"} {
		if _, ok := fields[name]; ok {
			t.Fatalf("unstable start field %q present in %s", name, line)
		}
	}

	process := Process{Name: "api", Root: "/work", PID: 42, PGID: 42, Cwd: "/work", Argv: []string{"sleep", "1"}, State: "running"}
	response, err := json.Marshal(NewStartResponse(process))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(response, []byte(`"env"`)) || bytes.Contains(response, []byte("SECRET=do-not-echo")) {
		t.Fatalf("start response leaked environment: %s", response)
	}
	var decoded StartResponse
	if err := json.Unmarshal(response, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Process == nil || decoded.Process.Name != process.Name {
		t.Fatalf("decoded start response = %#v", decoded)
	}
}

func TestOutputAndStreamingEventRoundTrips(t *testing.T) {
	at := time.Unix(123, 456).UTC()
	next, oldest, latest, evicted := Cursor(4), Cursor(1), Cursor(9), Cursor(0)
	result := OutputResult{
		Entries: []OutputEntry{{Cursor: 2, Stream: StreamStderr, Time: at, Text: "warning\n"}},
		Next:    &next, Oldest: &oldest, Latest: &latest, EvictedThrough: &evicted,
		Truncated: true, More: true,
	}
	response := NewOutputResponse(result)
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"entries", "next", "oldest", "latest", "evicted_through", "truncated", "more"} {
		if !bytes.Contains(encoded, []byte(`"`+field+`"`)) {
			t.Fatalf("output response missing %q: %s", field, encoded)
		}
	}
	var decoded OutputResponse
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Result == nil || !decoded.Truncated || len(decoded.Entries) != 1 || decoded.Entries[0].Text != "warning\n" {
		t.Fatalf("decoded output response = %#v", decoded)
	}

	events := []StreamEvent{
		NewOutputEvent(result),
		NewCursorEvent(10),
		NewEvictionEvent(3),
		NewExitEvent(Exit{Code: 17, Time: at}),
		NewReadyEvent(&next),
		NewErrorEvent(NewWireError(ErrorInvalidRequest, "bad follow", map[string]any{"field": "match"})),
	}
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(encoded, []byte(`{"op":"event"`)) {
			t.Fatalf("event operation = %s", encoded)
		}
		var decoded StreamEvent
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Op != eventOperation || decoded.Type != event.Type {
			t.Fatalf("decoded event = %#v, want %#v", decoded, event)
		}
	}
}

func TestTypedErrorsAndBoundedNDJSON(t *testing.T) {
	wireErr := NewWireError(ErrorVersionMismatch, "protocol version mismatch", VersionMismatchDetails{Client: 2, Daemon: Version})
	encoded, err := json.Marshal(wireErr)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"code":"version_mismatch","message":"protocol version mismatch","details":{"client":2,"daemon":5}}`; got != want {
		t.Fatalf("wire error JSON = %s, want %s", got, want)
	}
	var decoded WireError
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Code != ErrorVersionMismatch || decoded.Message == "" {
		t.Fatalf("decoded wire error = %#v", decoded)
	}

	malformed := NewDecoder(strings.NewReader(`{"op":"get"`+"\n"), 128)
	if _, err := malformed.DecodeRequest(); err == nil || !errors.Is(err, ErrMalformed) {
		t.Fatalf("malformed error = %v", err)
	}
	unknown := NewDecoder(strings.NewReader(`{"op":"wat"}`+"\n"), 128)
	if _, err := unknown.DecodeRequest(); err == nil || !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("unknown operation error = %v", err)
	}
	missing := NewDecoder(strings.NewReader(`{"name":"api"}`+"\n"), 128)
	if _, err := missing.DecodeRequest(); err == nil || !errors.Is(err, ErrMissingOperation) {
		t.Fatalf("missing operation error = %v", err)
	}

	var stream bytes.Buffer
	stream.WriteString(strings.Repeat("x", 33))
	stream.WriteByte('\n')
	stream.WriteString(`{"op":"hello","version":1}`)
	stream.WriteByte('\n')
	decoder := NewDecoder(&stream, 32)
	if _, err := decoder.DecodeRequest(); err == nil || !errors.Is(err, ErrOversized) {
		t.Fatalf("oversized error = %v", err)
	}
	if request, err := decoder.DecodeRequest(); err != nil || request.Op != OpHello {
		t.Fatalf("decoder recovery request = %#v, err %v", request, err)
	}
}

func TestMaxLineUnknownOperationResponseIsBounded(t *testing.T) {
	const maxLineBytes = DefaultMaxLineBytes
	const requestPrefix = `{"op":"`
	const requestSuffix = `"}`

	operation := strings.Repeat("x", maxLineBytes-len(requestPrefix)-len(requestSuffix))
	requestLine := requestPrefix + operation + requestSuffix
	if got, want := len(requestLine), maxLineBytes; got != want {
		t.Fatalf("unknown-operation request size = %d, want %d", got, want)
	}

	decoder := NewDecoder(strings.NewReader(requestLine+"\n"), maxLineBytes)
	_, decodeErr := decoder.DecodeRequest()
	if decodeErr == nil {
		t.Fatal("largest accepted unknown-operation request unexpectedly succeeded")
	}
	if errors.Is(decodeErr, io.EOF) {
		t.Fatalf("largest accepted unknown-operation request returned EOF: %v", decodeErr)
	}
	if errors.Is(decodeErr, ErrOversized) {
		t.Fatalf("largest accepted unknown-operation request was oversized: %v", decodeErr)
	}
	if !errors.Is(decodeErr, ErrUnknownOperation) {
		t.Fatalf("unknown-operation error = %v", decodeErr)
	}
	var typedDecodeErr *DecodeError
	if !errors.As(decodeErr, &typedDecodeErr) || typedDecodeErr.Kind != DecodeUnknownOperation {
		t.Fatalf("unknown-operation decode error type = %#v", decodeErr)
	}
	var unknownErr *UnknownOperationError
	if !errors.As(decodeErr, &unknownErr) || unknownErr.Operation != Operation(operation) {
		t.Fatalf("unknown-operation detail = %#v", decodeErr)
	}

	wireErr := WireErrorForDecode(decodeErr)
	if wireErr == nil || wireErr.Code != ErrorUnknownOperation {
		t.Fatalf("unknown-operation wire error = %#v", wireErr)
	}

	var output bytes.Buffer
	encoder := NewEncoder(&output, maxLineBytes)
	if err := encoder.EncodeResponse(NewErrorResponse("", wireErr)); err != nil {
		t.Fatalf("bounded unknown-operation response: %v", err)
	}
	responseLine := output.Bytes()
	if len(responseLine) == 0 || responseLine[len(responseLine)-1] != '\n' {
		t.Fatalf("unknown-operation response is not newline-terminated: %q", responseLine)
	}
	if got := len(responseLine) - 1; got > maxLineBytes {
		t.Fatalf("unknown-operation response size = %d, want at most %d", got, maxLineBytes)
	}
	if strings.Contains(string(responseLine), operation) {
		t.Fatal("unknown-operation response echoed the giant operation string")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(responseLine, &fields); err != nil {
		t.Fatalf("decode unknown-operation response: %v", err)
	}
	for _, field := range []string{"op", "ok", "error"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("unknown-operation response missing %q field: %s", field, responseLine)
		}
	}
	var responseOp Operation
	if err := json.Unmarshal(fields["op"], &responseOp); err != nil {
		t.Fatalf("decode unknown-operation response op: %v", err)
	}
	if responseOp != "" {
		t.Fatalf("unknown-operation response op = %q, want empty", responseOp)
	}
	var responseError WireError
	if err := json.Unmarshal(fields["error"], &responseError); err != nil {
		t.Fatalf("decode unknown-operation response error: %v", err)
	}
	if responseError.Code != ErrorUnknownOperation {
		t.Fatalf("unknown-operation response code = %q, want %q", responseError.Code, ErrorUnknownOperation)
	}
	if strings.Contains(responseError.Message, operation) {
		t.Fatal("unknown-operation response error echoed the giant operation string")
	}
}

func TestBoundedEncoderAndResponseEnvironmentRejection(t *testing.T) {
	var output bytes.Buffer
	encoder := NewEncoder(&output, 64)
	if err := encoder.EncodeResponse(struct {
		Op  Operation `json:"op"`
		Env []string  `json:"env"`
	}{Op: OpStart, Env: []string{"SECRET=value"}}); !errors.Is(err, ErrResponseEnvironment) {
		t.Fatalf("environment response error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("environment response wrote %d bytes", output.Len())
	}
	if err := encoder.EncodeResponse(NewSignalResponse()); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != `{"op":"signal","ok":true}`+"\n" {
		t.Fatalf("encoded signal = %q", got)
	}
	if err := encoder.Encode(NewStartRequest("api", []string{strings.Repeat("x", 128)}, "/work", nil)); err == nil || !errors.Is(err, ErrOversized) {
		t.Fatalf("oversized encode error = %v", err)
	}
	if line, err := MarshalLine(NewHello(), 4); err == nil || !errors.Is(err, ErrOversized) || line != nil {
		t.Fatalf("oversized marshal = %q, err %v", line, err)
	}
}

func TestRestartProtocolRoundTrip(t *testing.T) {
	request := NewRestartRequest("api", "/project")
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"op":"restart","name":"api","cwd":"/project"}`; got != want {
		t.Fatalf("restart request = %s, want %s", got, want)
	}
	decoded, err := NewDecoder(bytes.NewReader(append(raw, '\n'))).DecodeRequest()
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Restart == nil || !reflect.DeepEqual(*decoded.Restart, request) {
		t.Fatalf("decoded restart = %#v", decoded.Restart)
	}

	process := &Process{Name: "api", PID: 42, LaunchCursor: 7, RestartCount: 2}
	response := NewRestartResponse(process)
	raw, err = json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var decodedResponse RestartResponse
	if err := json.Unmarshal(raw, &decodedResponse); err != nil {
		t.Fatal(err)
	}
	if decodedResponse.Op != OpRestart || !decodedResponse.OK || decodedResponse.Process == nil ||
		decodedResponse.Process.PID != 42 || decodedResponse.Process.LaunchCursor != 7 || decodedResponse.Process.RestartCount != 2 {
		t.Fatalf("decoded response = %#v", decodedResponse)
	}
}

func TestReadinessFieldProtocolRoundTrip(t *testing.T) {
	at := time.Unix(231, 456).UTC()
	matchingCursor := Cursor(12)
	request := StartRequest{
		Op: OpStart, Name: "api", Source: "manifest:hum.yaml",
		Argv: []string{"server", "--port", "8080"}, Cwd: "/project",
		Env:   []string{"TOKEN=do-not-echo"},
		Ready: &ReadinessConfig{Match: `listening`, Timeout: 1500 * time.Millisecond},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decodedRequest StartRequest
	if err := json.Unmarshal(raw, &decodedRequest); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodedRequest, request) {
		t.Fatalf("decoded readiness request = %#v, want %#v", decodedRequest, request)
	}
	if !bytes.Contains(raw, []byte(`"source":"manifest:hum.yaml"`)) ||
		!bytes.Contains(raw, []byte(`"ready":{"match":"listening"`)) {
		t.Fatalf("readiness request fields missing from %s", raw)
	}

	process := Process{
		Name: "api", Source: "manifest:hum.yaml", Root: "/project",
		PID: 42, PGID: 42, Cwd: "/project", Argv: request.Argv,
		Start: at, LaunchCursor: 7, State: "running",
		Readiness: &Readiness{
			State: ReadinessReady, Cursor: &matchingCursor, Time: at,
			Match: `listening`,
		},
	}
	response, err := json.Marshal(NewStartResponse(process))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(response, []byte(`"env"`)) || bytes.Contains(response, []byte("do-not-echo")) {
		t.Fatalf("readiness response leaked environment: %s", response)
	}
	var decodedResponse StartResponse
	if err := json.Unmarshal(response, &decodedResponse); err != nil {
		t.Fatal(err)
	}
	if decodedResponse.Process == nil || decodedResponse.Process.Source != process.Source ||
		decodedResponse.Process.Readiness == nil ||
		decodedResponse.Process.Readiness.State != ReadinessReady ||
		decodedResponse.Process.Readiness.Cursor == nil ||
		*decodedResponse.Process.Readiness.Cursor != matchingCursor ||
		decodedResponse.Process.Readiness.Match != `listening` {
		t.Fatalf("decoded readiness response = %#v", decodedResponse)
	}
}

func TestExplicitRootRequestRoundTrip(t *testing.T) {
	start := StartRequest{
		Op: OpStart, Name: "api", Root: "/outer/project", Cwd: "/outer/project/tool",
		Argv: []string{"server"}, Env: []string{"TOKEN=secret"},
	}
	raw, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"root":"/outer/project"`)) {
		t.Fatalf("start request omitted explicit root: %s", raw)
	}
	var decodedStart StartRequest
	if err := json.Unmarshal(raw, &decodedStart); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodedStart, start) {
		t.Fatalf("decoded start = %#v, want %#v", decodedStart, start)
	}

	restart := RestartRequest{
		Op: OpRestart, Name: "api", Root: "/outer/project", Cwd: "/outer/project/tool",
		Update: true, Argv: []string{"server", "--reload"}, Source: "manifest",
		Ready: &ReadinessConfig{Match: "ready", Timeout: time.Second},
	}
	raw, err = json.Marshal(restart)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"root":"/outer/project"`)) {
		t.Fatalf("restart request omitted explicit root: %s", raw)
	}
	var decodedRestart RestartRequest
	if err := json.Unmarshal(raw, &decodedRestart); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodedRestart, restart) {
		t.Fatalf("decoded restart = %#v, want %#v", decodedRestart, restart)
	}
}
