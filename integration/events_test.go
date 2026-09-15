package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"hum/internal/testutil"
)

func TestEventsJSON(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	runtime := lifecycleNewRuntime(t)
	t.Cleanup(func() {
		_ = testutil.Run(t, hum, runtime.cwd, runtime.env, "shutdown", "--stop-processes")
	})

	first := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "event-one", "--detach", "--", "/bin/sh", "-c", "sleep 30")
	if first.Code != 0 || first.Err != nil {
		t.Fatalf("first run: code=%d err=%v stdout=%q stderr=%q", first.Code, first.Err, first.Stdout, first.Stderr)
	}
	beforeEvents := readEvents(t, testutil.Run(t, hum, runtime.cwd, runtime.env, "events", "--json"))
	before := eventCursors(beforeEvents)
	if len(before) < 2 {
		t.Fatalf("first history cursors = %v", before)
	}
	operationID := ""
	launchID := ""
	for _, event := range beforeEvents {
		if event.Kind == "operation" && event.Event == "run" && event.Origin == "cli" {
			operationID = event.OperationID
		}
		if event.Kind == "lifecycle" && event.Event == "launch" {
			launchID = event.OperationID
		}
	}
	if operationID == "" || launchID != operationID {
		t.Fatalf("run attribution operation_id=%q launch_id=%q events=%+v", operationID, launchID, beforeEvents)
	}
	shutdown := testutil.Run(t, hum, runtime.cwd, runtime.env, "shutdown", "--stop-processes")
	if shutdown.Code != 0 || shutdown.Err != nil {
		t.Fatalf("shutdown: code=%d err=%v stdout=%q stderr=%q", shutdown.Code, shutdown.Err, shutdown.Stdout, shutdown.Stderr)
	}
	offline := eventCursors(readEvents(t, testutil.Run(t, hum, runtime.cwd, runtime.env, "events", "--json")))
	if len(offline) < len(before) || offline[len(offline)-1] < before[len(before)-1] {
		t.Fatalf("offline history cursors = %v, before = %v", offline, before)
	}

	second := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", "event-two", "--detach", "--", "/bin/sh", "-c", "sleep 30")
	if second.Code != 0 || second.Err != nil {
		t.Fatalf("second run: code=%d err=%v stdout=%q stderr=%q", second.Code, second.Err, second.Stdout, second.Stderr)
	}
	after := eventCursors(readEvents(t, testutil.Run(t, hum, runtime.cwd, runtime.env, "events", "--json")))
	if len(after) <= len(offline) || after[len(after)-1] <= offline[len(offline)-1] {
		t.Fatalf("replacement history cursors = %v, offline = %v", after, offline)
	}
}

type eventJSON struct {
	Cursor      uint64 `json:"cursor"`
	Type        string `json:"type"`
	Kind        string `json:"kind"`
	Event       string `json:"event"`
	Origin      string `json:"origin"`
	OperationID string `json:"operation_id"`
}

func eventCursors(events []eventJSON) []uint64 {
	result := make([]uint64, 0, len(events))
	for _, event := range events {
		result = append(result, event.Cursor)
	}
	return result
}

func readEvents(t *testing.T, result testutil.Result) []eventJSON {
	t.Helper()
	if result.Code != 0 || result.Err != nil {
		t.Fatalf("events: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
	}
	var events []eventJSON
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		var value eventJSON
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatalf("decode event line %q: %v", line, err)
		}
		if value.Type == "event" {
			events = append(events, value)
		}
	}
	return events
}
