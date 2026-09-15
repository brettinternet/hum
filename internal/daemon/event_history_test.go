package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"hum/internal/protocol"
)

func TestEventHistoryAppendAndPaging(t *testing.T) {
	history := NewEventHistory(t.TempDir(), protocol.ScopeProject, "/project")
	for _, item := range []struct{ name, kind, event string }{
		{"api", string(protocol.EventOperation), "start"},
		{"api", string(protocol.EventLifecycle), "launch"},
		{"web", string(protocol.EventOperation), "stop"},
	} {
		if _, err := history.Append(protocol.HistoryEvent{Name: item.name, Kind: protocol.EventKind(item.kind), Event: item.event}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := history.Read([]string{"api"}, time.Time{}, nil, false, regexp.MustCompile("api"), 50, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.Events[0].Cursor != 1 || page.Events[1].Cursor != 2 {
		t.Fatalf("page = %#v", page.Events)
	}
	cursor := protocol.Cursor(1)
	page, err = history.Read(nil, time.Time{}, nil, false, nil, 1, &cursor, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Cursor != 2 || !page.HasMore || page.NextCursor != 2 {
		t.Fatalf("cursor page = %#v, metadata=%#v", page.Events, page)
	}
}

func TestEventHistoryRetentionAndNameReuse(t *testing.T) {
	history := NewEventHistory(t.TempDir(), protocol.ScopeProject, "/project")
	for i := 0; i < maxHistoryEvents+3; i++ {
		if _, err := history.Append(protocol.HistoryEvent{Name: "same", Kind: protocol.EventLifecycle, Event: "exit", Detail: strings.Repeat("x", 20)}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := history.Read(nil, time.Time{}, nil, false, nil, maxHistoryEvents, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != maxHistoryEvents || page.Events[0].Cursor != 4 || page.Events[len(page.Events)-1].Cursor != maxHistoryEvents+3 {
		t.Fatalf("retention = %d, first=%d", len(page.Events), page.Events[0].Cursor)
	}
	zero := protocol.Cursor(0)
	if truncated, readErr := history.Read(nil, time.Time{}, nil, false, nil, 1, &zero, 0); readErr != nil || !truncated.Truncated || truncated.Events[0].Cursor != 4 {
		t.Fatalf("truncated page=%#v err=%v", truncated, readErr)
	}
	if _, err := history.Append(protocol.HistoryEvent{Name: "same", Kind: protocol.EventLifecycle, Event: "removal"}); err != nil {
		t.Fatal(err)
	}
	if page, err = history.Read([]string{"same"}, time.Time{}, nil, false, nil, 1, nil, 0); err != nil || len(page.Events) != 1 || page.Events[0].Event != "removal" {
		t.Fatalf("reuse page=%#v err=%v", page, err)
	}
}

func TestEventHistoryRecoveryAndCursorContinuation(t *testing.T) {
	dir := t.TempDir()
	history := NewEventHistory(dir, protocol.ScopeProject, "/project")
	if _, err := history.Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "launch"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(history.EventPath(), []byte(`{"cursor":1,"time":"2026-09-15T00:00:00Z","kind":"lifecycle","name":"api","event":"launch"}
{"cursor":2`), 0600); err != nil {
		t.Fatal(err)
	}
	recovered := NewEventHistory(dir, protocol.ScopeProject, "/project")
	page, err := recovered.Read(nil, time.Time{}, nil, false, nil, 50, nil, 0)
	if err != nil || len(page.Events) != 1 {
		t.Fatalf("torn recovery page=%#v err=%v", page, err)
	}
	if _, err := recovered.Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "ready"}); err != nil {
		t.Fatal(err)
	}
	persisted := NewEventHistory(dir, protocol.ScopeProject, "/project")
	if page, err = persisted.Read(nil, time.Time{}, nil, false, nil, 50, nil, 0); err != nil || len(page.Events) != 2 || page.Events[1].Cursor != 2 {
		t.Fatalf("rewritten recovery page=%#v err=%v", page, err)
	}
}

func TestEventHistoryMalformedPayloadAndCursorUnavailable(t *testing.T) {
	dir := t.TempDir()
	history := NewEventHistory(dir, protocol.ScopeProject, "/project")
	if _, err := history.Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "launch"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(history.EventPath(), []byte("not-json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	recovered := NewEventHistory(dir, protocol.ScopeProject, "/project")
	recovered.SetDiagnostic(func(error) { calls++ })
	page, err := recovered.Read(nil, time.Time{}, nil, false, nil, 50, nil, 0)
	if err != nil || len(page.Events) != 0 {
		t.Fatalf("malformed page=%#v err=%v", page, err)
	}
	_, _ = recovered.Read(nil, time.Time{}, nil, false, nil, 50, nil, 0)
	if calls != 1 {
		t.Fatalf("diagnostics=%d, want one", calls)
	}
	appended, err := recovered.Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "ready"})
	if err != nil || appended.Cursor != 2 {
		t.Fatalf("append after malformed payload = %#v, %v", appended, err)
	}
	if err := os.WriteFile(history.CursorPath(), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	unavailable := NewEventHistory(dir, protocol.ScopeProject, "/project")
	if _, err := unavailable.Read(nil, time.Time{}, nil, false, nil, 50, nil, 0); !errors.Is(err, ErrHistoryUnavailable) {
		t.Fatalf("cursor error=%v", err)
	}
}

func TestEventHistoryConcurrentScopesBoundsAndWriteFailure(t *testing.T) {
	dir := t.TempDir()
	const count = 24
	var wait sync.WaitGroup
	errorsCh := make(chan error, count)
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := NewEventHistory(dir, protocol.ScopeProject, "/project").Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventOperation, Event: "start", Outcome: "success"})
			errorsCh <- err
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	history := NewEventHistory(dir, protocol.ScopeProject, "/project")
	page, err := history.Read(nil, time.Time{}, nil, false, nil, count, nil, 0)
	if err != nil || len(page.Events) != count {
		t.Fatalf("concurrent page=%#v err=%v", page, err)
	}
	for index, event := range page.Events {
		if event.Cursor != protocol.Cursor(index+1) {
			t.Fatalf("cursor[%d]=%d", index, event.Cursor)
		}
	}
	global := NewEventHistory(dir, protocol.ScopeGlobal, "")
	if globalPage, readErr := global.Read(nil, time.Time{}, nil, false, nil, 50, nil, 0); readErr != nil || len(globalPage.Events) != 0 {
		t.Fatalf("global isolation page=%#v err=%v", globalPage, readErr)
	}

	large, err := history.Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "ready", Detail: strings.Repeat("é", maxHistoryEvent)})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(large)
	if err != nil || len(encoded)+1 > maxHistoryEvent {
		t.Fatalf("bounded event bytes=%d err=%v", len(encoded)+1, err)
	}

	if err := os.Remove(history.EventPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(history.EventPath(), 0700); err != nil {
		t.Fatal(err)
	}
	failed, err := history.Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "exit"})
	if err == nil {
		t.Fatalf("write unexpectedly succeeded: %#v", failed)
	}
	if page, readErr := history.Read(nil, time.Time{}, nil, false, nil, maxHistoryEvents, nil, 0); readErr != nil || len(page.Events) != count+1 {
		t.Fatalf("failed write leaked into page=%#v err=%v", page, readErr)
	}
}
