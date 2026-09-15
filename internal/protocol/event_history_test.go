package protocol

import (
	"encoding/json"
	"testing"
)

func TestEventHistoryProtocolRoundTrip(t *testing.T) {
	request := EventsRequest{Scope: ScopeProject, Root: "/project", Names: []string{"api"}, Kinds: []EventKind{EventLifecycle}, Tail: 7}
	line, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Request
	if err := json.Unmarshal(line, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Op != OpEvents || decoded.Events == nil || decoded.Events.Tail != 7 {
		t.Fatalf("decoded request = %#v", decoded)
	}
	event := HistoryEvent{Cursor: 1, Kind: EventLifecycle, Name: "api", Event: "launch", OperationID: "op-1"}
	response := NewEventsResponse([]HistoryEvent{event}, 1, false, false)
	responseLine, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(responseLine) == 0 {
		t.Fatal("empty response")
	}
}
