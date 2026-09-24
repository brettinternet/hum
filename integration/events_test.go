package integration

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"hum/internal/testutil"
)

func TestEventsLogCursor(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	runtime := lifecycleNewRuntime(t)
	t.Cleanup(func() { _ = testutil.Run(t, hum, runtime.cwd, runtime.env, "shutdown", "--stop-processes") })
	name := "cursor-api"
	run := testutil.Run(t, hum, runtime.cwd, runtime.env, "run", name, "--detach", "--", "/bin/sh", "-c", "echo one; echo two; exit 3")
	if run.Code != 0 || run.Err != nil {
		t.Fatalf("run: %+v", run)
	}
	getEvents := func() []eventJSON {
		return readEvents(t, testutil.Run(t, hum, runtime.cwd, runtime.env, "events", name, "--json"))
	}
	awaitExits := func(want int) []eventJSON {
		t.Helper()
		var events []eventJSON
		if !lifecycleWaitCondition(5*time.Second, func() bool {
			events = getEvents()
			count := 0
			for _, event := range events {
				if event.Kind == "lifecycle" && event.Event == "exit" {
					count++
				}
			}
			return count >= want
		}) {
			t.Fatalf("wanted %d exits, events=%+v", want, events)
		}
		return events
	}
	checkLogs := func(after *uint64) []logsitEntry {
		t.Helper()
		args := []string{"logs", name, "--json"}
		if after != nil {
			args = append(args, "--after-cursor", strconv.FormatUint(*after, 10))
		}
		result := testutil.Run(t, hum, runtime.cwd, runtime.env, args...)
		if result.Code != 0 || result.Err != nil {
			t.Fatalf("logs: %+v", result)
		}
		lines := logsitDecodeJSONLines(t, result.Stdout)
		if len(lines) != 1 {
			t.Fatalf("logs JSON: %q", result.Stdout)
		}
		return lines[0].Event.Entries
	}
	first := awaitExits(1)
	var launches, exits []eventJSON
	for _, event := range first {
		if event.Kind == "lifecycle" && event.Event == "launch" {
			launches = append(launches, event)
		}
		if event.Kind == "lifecycle" && event.Event == "exit" {
			exits = append(exits, event)
		}
	}
	if len(launches) != 1 || launches[0].LogCursor != nil || len(exits) != 1 {
		t.Fatalf("first incarnation events: %+v", first)
	}
	entries := checkLogs(nil)
	if len(entries) != 2 || entries[0].Text != "one\n" || entries[1].Text != "two\n" || exits[0].LogCursor == nil || *exits[0].LogCursor != entries[1].Cursor {
		t.Fatalf("first exit=%+v logs=%+v", exits[0], entries)
	}
	start := testutil.Run(t, hum, runtime.cwd, runtime.env, "start", name)
	if start.Code != 0 || start.Err != nil {
		t.Fatalf("start: %+v", start)
	}
	all := awaitExits(2)
	launches, exits = nil, nil
	for _, event := range all {
		if event.Kind == "lifecycle" && event.Event == "launch" {
			launches = append(launches, event)
		}
		if event.Kind == "lifecycle" && event.Event == "exit" {
			exits = append(exits, event)
		}
	}
	if len(launches) != 2 || launches[1].LogCursor == nil || len(exits) != 2 {
		t.Fatalf("second incarnation events: %+v", all)
	}
	second := checkLogs(launches[1].LogCursor)
	if len(second) != 2 || second[0].Text != "one\n" || second[1].Text != "two\n" || exits[1].LogCursor == nil || *exits[1].LogCursor != second[1].Cursor {
		t.Fatalf("second exit=%+v logs=%+v", exits[1], second)
	}
}

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
	SchemaVersion int     `json:"schema_version"`
	Cursor        uint64  `json:"cursor"`
	Type          string  `json:"type"`
	Kind          string  `json:"kind"`
	Event         string  `json:"event"`
	Origin        string  `json:"origin"`
	OperationID   string  `json:"operation_id"`
	NextCursor    uint64  `json:"next_cursor"`
	LogCursor     *uint64 `json:"log_cursor"`
	Truncated     bool    `json:"truncated"`
	HasMore       bool    `json:"has_more"`
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
