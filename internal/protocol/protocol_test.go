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
	if got, want := string(hello), `{"op":"hello","version":1}`; got != want {
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
	if got, want := string(encoded), `{"code":"version_mismatch","message":"protocol version mismatch","details":{"client":2,"daemon":1}}`; got != want {
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
