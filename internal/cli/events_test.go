package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hum/internal/daemon"
	"hum/internal/protocol"
)

func eventTestHistory(t *testing.T) *daemon.EventHistory {
	t.Helper()
	history := daemon.NewEventHistory(t.TempDir(), protocol.ScopeProject, "/project")
	for _, event := range []protocol.HistoryEvent{{Name: "api", Kind: protocol.EventOperation, Event: "start", Outcome: "success", OperationID: "op-1"}, {Name: "api", Kind: protocol.EventLifecycle, Event: "launch"}, {Name: "web", Kind: protocol.EventLifecycle, Event: "startup_failure"}} {
		if _, err := history.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	return history
}

func TestEventsNoArgs(t *testing.T) {
	history := eventTestHistory(t)
	page, err := history.Read(nil, time.Time{}, nil, false, nil, 50, nil, 0)
	if err != nil || len(page.Events) != 3 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}
func TestEventsEmptyState(t *testing.T) {
	var out bytes.Buffer
	root := t.TempDir()
	command := NewRootCommand("test", "test", &out, &out)
	if err := command.Run(context.Background(), []string{"hum", "--project", root, "events", "--runtime-dir", t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No service events retained") {
		t.Fatalf("output=%q", out.String())
	}
}
func TestEventsNames(t *testing.T) {
	history := eventTestHistory(t)
	page, err := history.Read([]string{"api"}, time.Time{}, nil, false, nil, 50, nil, 0)
	if err != nil || len(page.Events) != 2 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}
func TestEventsFilters(t *testing.T) {
	history := eventTestHistory(t)
	page, err := history.Read(nil, time.Time{}, []protocol.EventKind{protocol.EventLifecycle}, true, nil, 50, nil, 0)
	if err != nil || len(page.Events) != 1 || page.Events[0].Event != "startup_failure" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}
func TestEventsPaging(t *testing.T) {
	history := eventTestHistory(t)
	cursor := protocol.Cursor(1)
	page, err := history.Read(nil, time.Time{}, nil, false, nil, 1, &cursor, 0)
	if err != nil || len(page.Events) != 1 || page.NextCursor != 2 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}
func TestEventsHelp(t *testing.T) {
	var out bytes.Buffer
	if err := NewRootCommand("test", "test", &out, &out).Run(context.Background(), []string{"hum", "events", "--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "hum events") {
		t.Fatalf("help=%q", out.String())
	}
}
func TestEventsCompletion(t *testing.T) {
	var out bytes.Buffer
	if err := NewRootCommand("test", "test", &out, &out).Run(context.Background(), []string{"hum", "completion", "bash"}); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 {
		t.Fatal("empty completion")
	}
}
func TestEventsWidth(t *testing.T) {
	if got := elideEventLine(strings.Repeat("x", 100), 80); len([]rune(got)) != 80 {
		t.Fatalf("width=%d", len([]rune(got)))
	}
}
func TestEventsColor(t *testing.T) {
	var out bytes.Buffer
	if err := writeEventsHuman(&out, []protocol.HistoryEvent{{Name: "api", Kind: protocol.EventLifecycle, Event: "launch"}}, false, colorPolicy{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("unexpected escape: %q", out.String())
	}
}
func TestEventsJSON(t *testing.T) {
	var out bytes.Buffer
	if err := writeEventsJSON(&out, []protocol.HistoryEvent{{Cursor: 1, Time: time.Now(), Kind: protocol.EventOperation, Name: "api", Event: "start", OperationID: "op-1"}}, 1, false, false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var metadata map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &metadata); err != nil || metadata["type"] != "metadata" {
		t.Fatalf("metadata=%v err=%v", metadata, err)
	}
}
