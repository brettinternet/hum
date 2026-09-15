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
	SchemaVersion int    `json:"schema_version"`
	Cursor        uint64 `json:"cursor"`
	Type          string `json:"type"`
	Kind          string `json:"kind"`
	Event         string `json:"event"`
	Origin        string `json:"origin"`
	OperationID   string `json:"operation_id"`
	NextCursor    uint64 `json:"next_cursor"`
	Truncated     bool   `json:"truncated"`
	HasMore       bool   `json:"has_more"`
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
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	metadataRecords := 0
	var previous uint64
	for index, line := range lines {
		var value eventJSON
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatalf("decode event line %q: %v", line, err)
		}
		if value.SchemaVersion != 1 {
			t.Fatalf("record %d schema_version=%d", index, value.SchemaVersion)
		}
		switch value.Type {
		case "event":
			if metadataRecords != 0 || value.Cursor <= previous || value.Kind == "" || value.Event == "" {
				t.Fatalf("invalid ordered event record %d: %#v", index, value)
			}
			previous = value.Cursor
			events = append(events, value)
		case "metadata":
			metadataRecords++
			if index != len(lines)-1 || value.NextCursor < previous {
				t.Fatalf("metadata must trail events and advance cursor: %#v", value)
			}
		default:
			t.Fatalf("record %d has unknown type %q", index, value.Type)
		}
	}
	if metadataRecords != 1 {
		t.Fatalf("metadata records=%d stdout=%q", metadataRecords, result.Stdout)
	}
	return events
}
