package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestEventHistoryProtocolRoundTrip(t *testing.T) {
	after := Cursor(41)
	request := EventsRequest{Op: OpEvents, Scope: ScopeProject, Root: "/project", Cwd: "/project/subdir", Names: []string{"api", "web"}, SinceUnixNano: time.Unix(10, 20).UnixNano(), Kinds: []EventKind{EventLifecycle, EventOperation}, Failed: true, Match: "api|boom", Tail: 7, AfterCursor: &after, MaxBytes: 4096}
	line, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Request
	if err := json.Unmarshal(line, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Op != OpEvents || decoded.Events == nil || !reflect.DeepEqual(*decoded.Events, request) {
		t.Fatalf("decoded request = %#v, want %#v", decoded, request)
	}
	exitCode := 9
	events := []HistoryEvent{
		{Cursor: 42, Time: time.Unix(11, 0).UTC(), Kind: EventOperation, Name: "api", Event: "start", Detail: "failed safely", OperationID: "op-1", Origin: "mcp", Outcome: "failure"},
		{Cursor: 43, Time: time.Unix(12, 0).UTC(), Kind: EventLifecycle, Name: "api", Event: "exit", OperationID: "op-1", ExitCode: &exitCode, Signal: "TERM"},
	}
	response := NewEventsResponse(events, 43, true, true)
	responseLine, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var decodedResponse EventsResponse
	if err := json.Unmarshal(responseLine, &decodedResponse); err != nil {
		t.Fatal(err)
	}
	if decodedResponse.Op != OpEvents || !decodedResponse.OK || decodedResponse.NextCursor != 43 || !decodedResponse.Truncated || !decodedResponse.HasMore || !reflect.DeepEqual(decodedResponse.Events, events) {
		t.Fatalf("decoded response = %#v, want %#v", decodedResponse, response)
	}
}
