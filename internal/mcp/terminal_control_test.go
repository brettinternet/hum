package mcp

import (
	"context"
	"reflect"
	"testing"
	"time"

	"hum/internal/protocol"
)

func TestLogsStripTerminalControl(t *testing.T) {
	root := t.TempDir()
	next, oldest, latest, evicted := protocol.Cursor(8), protocol.Cursor(2), protocol.Cursor(11), protocol.Cursor(1)
	at := time.Unix(71, 9)
	client := &fakeClient{output: protocol.OutputResult{
		Entries: []protocol.OutputEntry{
			{Cursor: 7, Stream: protocol.StreamStdout, Time: at, Text: "red\n"},
			{Cursor: 8, Stream: protocol.StreamSystem, Time: at, Text: "system\n"},
		},
		Next: &next, Oldest: &oldest, Latest: &latest, EvictedThrough: &evicted,
		Truncated: true, More: true,
	}}
	s, _, _ := newTestServer(t, nil, client)

	value, err := s.callTool(context.Background(), "logs", args(root, "name", "controls", "tail", 2, "max_entries", 3, "max_bytes", 100))
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(protocol.OutputResult)
	if !ok {
		t.Fatalf("logs result type = %T, want protocol.OutputResult", value)
	}
	if !reflect.DeepEqual(result, client.output) {
		t.Fatalf("MCP logs result = %#v, want daemon result %#v", result, client.output)
	}
	if len(result.Entries) != 2 || result.Entries[0].Text != "red\n" || result.Entries[1].Text != "system\n" {
		t.Fatalf("MCP logs entries = %#v, want supplied text unchanged", result.Entries)
	}
	if result.Next == nil || *result.Next != next || result.Oldest == nil || *result.Oldest != oldest || result.Latest == nil || *result.Latest != latest || result.EvictedThrough == nil || *result.EvictedThrough != evicted {
		t.Fatalf("MCP logs cursor metadata = %#v, want unchanged daemon metadata", result)
	}
	if len(client.outputs) != 1 || client.outputs[0].Tail != 2 || client.outputs[0].MaxEntries != 3 || client.outputs[0].MaxBytes != 100 {
		t.Fatalf("MCP logs request = %#v, want bounded fields forwarded", client.outputs)
	}

	properties := s.toolDefinitions()[5].OutputSchema["properties"].(map[string]any)
	entries := properties["entries"].(map[string]any)
	if entries["type"] != "array" {
		t.Fatalf("MCP logs schema entries = %#v, want array", entries)
	}
}
