//go:build !windows

package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/process"
	"hum/internal/project"
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

func TestEventHistoryLogCursorRoundTrip(t *testing.T) {
	root := t.TempDir()
	server := &Server{eventOps: make(map[string]eventOperation), eventQueue: make(chan queuedHistoryEvent, 3)}
	history := NewEventHistory(t.TempDir(), protocol.ScopeProject, root)
	zero, last := output.Cursor(0), output.Cursor(812)
	for _, event := range []app.LifecycleEvent{
		{Scope: protocol.ScopeProject, Root: root, Cwd: root, Name: "api", Event: "launch", LogCursor: &zero},
		{Scope: protocol.ScopeProject, Root: root, Cwd: root, Name: "api", Event: "exit", LogCursor: &last},
		{Scope: protocol.ScopeProject, Root: root, Cwd: root, Name: "api", Event: "exit"},
	} {
		server.recordLifecycle(event)
		queued := <-server.eventQueue
		if _, err := history.Append(queued.event); err != nil {
			t.Fatal(err)
		}
	}
	loaded := NewEventHistory(history.dir, protocol.ScopeProject, root)
	page, err := loaded.Read(nil, time.Time{}, nil, false, nil, 10, nil, 0)
	if err != nil || len(page.Events) != 3 {
		t.Fatalf("reloaded history = %+v, err=%v", page, err)
	}
	if page.Events[0].LogCursor == nil || *page.Events[0].LogCursor != 0 || page.Events[1].LogCursor == nil || *page.Events[1].LogCursor != 812 || page.Events[2].LogCursor != nil {
		t.Fatalf("reloaded log cursors = %+v", page.Events)
	}
}

func TestEventHistoryRetentionAndNameReuse(t *testing.T) {
	history := NewEventHistory(t.TempDir(), protocol.ScopeProject, "/project")
	if history.maxEvents != 2000 || history.maxBytes != 1<<20 {
		t.Fatalf("production limits = %d events, %d bytes", history.maxEvents, history.maxBytes)
	}
	history.maxEvents = 20
	for i := 0; i < history.maxEvents+3; i++ {
		if _, err := history.Append(protocol.HistoryEvent{Name: "same", Kind: protocol.EventLifecycle, Event: "exit", Detail: strings.Repeat("x", 20)}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := history.Read(nil, time.Time{}, nil, false, nil, history.maxEvents, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != history.maxEvents || page.Events[0].Cursor != 4 || page.Events[len(page.Events)-1].Cursor != protocol.Cursor(history.maxEvents+3) {
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

func TestEventHistoryAppendCost(t *testing.T) {
	history := NewEventHistory(t.TempDir(), protocol.ScopeProject, "/project")
	history.maxEvents, history.maxBytes = 20, 1<<20
	for i := 0; i < 1000; i++ {
		if _, err := history.Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "ready"}); err != nil {
			t.Fatal(err)
		}
	}
	if history.fullRewrites > 51 || history.markWrites > 32 {
		t.Fatalf("1000 appends: %d rewrites, %d marks", history.fullRewrites, history.markWrites)
	}
	if history.fullRewrites == 0 || history.markWrites == 0 {
		t.Fatalf("counters not exercised: %d rewrites, %d marks", history.fullRewrites, history.markWrites)
	}
	page, err := history.Read(nil, time.Time{}, nil, false, nil, 20, nil, 0)
	if err != nil || len(page.Events) != 20 || page.Events[0].Cursor != 981 {
		t.Fatalf("live retention = %#v, err=%v", page, err)
	}
	loaded := NewEventHistory(history.dir, protocol.ScopeProject, "/project")
	loaded.maxEvents, loaded.maxBytes = 20, 1<<20
	page, err = loaded.Read(nil, time.Time{}, nil, false, nil, 20, nil, 0)
	if err != nil || len(page.Events) != 20 || page.Events[0].Cursor != 981 || loaded.diskEvents > 40 {
		t.Fatalf("loaded retention = %#v, disk events=%d, err=%v", page, loaded.diskEvents, err)
	}
	if _, err := loaded.Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "ready"}); err != nil {
		t.Fatal(err)
	}
	if loaded.fullRewrites != 0 {
		t.Fatalf("reload rewrote before reaching slack: %d", loaded.fullRewrites)
	}
}

func TestEventHistoryByteCompactionAndReload(t *testing.T) {
	dir := t.TempDir()
	history := NewEventHistory(dir, protocol.ScopeGlobal, "")
	history.maxEvents, history.maxBytes = 20, 1000
	for i := 0; i < 12; i++ {
		if _, err := history.Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "ready", Detail: strings.Repeat("x", 250)}); err != nil {
			t.Fatal(err)
		}
		loaded := NewEventHistory(dir, protocol.ScopeGlobal, "")
		loaded.maxEvents, loaded.maxBytes = history.maxEvents, history.maxBytes
		page, err := loaded.Read(nil, time.Time{}, nil, false, nil, 20, nil, 0)
		if err != nil || len(page.Events) == 0 || page.Events[len(page.Events)-1].Cursor != protocol.Cursor(i+1) || loaded.retainedBytes > history.maxBytes {
			t.Fatalf("reload after append %d: page=%#v bytes=%d err=%v", i, page, loaded.retainedBytes, err)
		}
	}
	if history.fullRewrites == 0 || history.fullRewrites >= 12 {
		t.Fatalf("byte compaction rewrites=%d", history.fullRewrites)
	}
	data, err := os.ReadFile(history.EventPath())
	if err != nil || len(data) > 2*history.maxBytes {
		t.Fatalf("disk bytes=%d err=%v", len(data), err)
	}
}

func TestEventHistoryCrashNeverReusesCursor(t *testing.T) {
	dir := t.TempDir()
	newHistory := func() *EventHistory { return NewEventHistory(dir, protocol.ScopeProject, "/project") }
	event := protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "launch"}
	first := newHistory()
	var highest protocol.Cursor
	for i := 0; i < 3; i++ {
		appended, err := first.Append(event)
		if err != nil || appended.Cursor <= highest {
			t.Fatalf("first block append = %#v, %v; previous=%d", appended, err, highest)
		}
		highest = appended.Cursor
	}
	if first.markWrites != 1 {
		t.Fatalf("mark writes=%d; expected one block", first.markWrites)
	}
	if cursor, err := first.HighWater(); err != nil || cursor != highest {
		t.Fatalf("live highwater=%d, err=%v; want %d", cursor, err, highest)
	}
	future := highest + 1
	if _, err := first.Read(nil, time.Time{}, nil, false, nil, 50, &future, 0); !errors.Is(err, ErrHistoryCursorFuture) {
		t.Fatalf("cursor inside reserved block was accepted: %v", err)
	}
	second := newHistory()
	appended, err := second.Append(event)
	if err != nil || appended.Cursor <= highest {
		t.Fatalf("restart reused cursor: %#v, %v; previous=%d", appended, err, highest)
	}
	highest = appended.Cursor
	file, err := os.OpenFile(second.EventPath(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString(`{"cursor":`); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	third := newHistory()
	appended, err = third.Append(event)
	if err != nil || appended.Cursor <= highest {
		t.Fatalf("torn-tail restart reused cursor: %#v, %v; previous=%d", appended, err, highest)
	}
	page, err := newHistory().Read(nil, time.Time{}, nil, false, nil, 50, nil, 0)
	if err != nil || len(page.Events) != 5 || page.Events[len(page.Events)-1].Cursor != appended.Cursor {
		t.Fatalf("recovered page=%#v, err=%v", page, err)
	}
}

func TestEventHistoryReaderIgnoresLiveReservation(t *testing.T) {
	dir := t.TempDir()
	event := protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "launch"}
	writer := NewEventHistory(dir, protocol.ScopeProject, "/project")
	for i := 0; i < 3; i++ {
		if _, err := writer.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	reader := NewEventHistory(dir, protocol.ScopeProject, "/project")
	page, err := reader.Read(nil, time.Time{}, nil, false, nil, 50, nil, 0)
	if err != nil || page.NextCursor != 3 {
		t.Fatalf("daemonless reader next cursor=%d, err=%v; want newest durable 3", page.NextCursor, err)
	}
	appended, err := writer.Append(event)
	if err != nil || appended.Cursor != 4 {
		t.Fatalf("writer append = %#v, %v", appended, err)
	}
	after := page.NextCursor
	page, err = NewEventHistory(dir, protocol.ScopeProject, "/project").Read(nil, time.Time{}, nil, false, nil, 50, &after, 0)
	if err != nil || page.Truncated || len(page.Events) != 1 || page.Events[0].Cursor != 4 {
		t.Fatalf("reader after live append page=%#v, err=%v", page, err)
	}
}

func TestEventHistoryServerReusesHistoryAndReleasesReservation(t *testing.T) {
	root, err := project.CanonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := shortRuntimeDir(t)
	supervisor, err := app.New(app.Options{})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Config{RuntimeDir: runtimeDir, Supervisor: supervisor})
	if err != nil {
		t.Fatal(err)
	}
	event := protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "launch"}
	var history *EventHistory
	for i := 0; i < 3; i++ {
		if history, err = server.history(protocol.ScopeProject, root, root); err != nil {
			t.Fatal(err)
		}
		appended, appendErr := history.Append(event)
		if appendErr != nil || appended.Cursor != protocol.Cursor(i+1) {
			t.Fatalf("server append %d = %#v, %v", i, appended, appendErr)
		}
	}
	if history.markWrites != 1 {
		t.Fatalf("server history mark writes=%d; want one reservation", history.markWrites)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	// A request racing shutdown must not reserve cursors after the release.
	if _, err := history.Append(event); !errors.Is(err, ErrHistoryUnavailable) {
		t.Fatalf("append after shutdown = %v, want unavailable", err)
	}
	restarted := NewEventHistory(runtimeDir, protocol.ScopeProject, root)
	appended, err := restarted.Append(event)
	if err != nil || appended.Cursor != 4 {
		t.Fatalf("append after clean shutdown = %#v, %v; want cursor 4", appended, err)
	}
	after := protocol.Cursor(3)
	page, err := restarted.Read(nil, time.Time{}, nil, false, nil, 50, &after, 0)
	if err != nil || page.Truncated || len(page.Events) != 1 {
		t.Fatalf("page after clean restart=%#v, err=%v", page, err)
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
	if page, err = persisted.Read(nil, time.Time{}, nil, false, nil, 50, nil, 0); err != nil || len(page.Events) != 2 || page.Events[1].Cursor <= page.Events[0].Cursor {
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
	zero := protocol.Cursor(0)
	if page, err = recovered.Read(nil, time.Time{}, nil, false, nil, 50, &zero, 0); err != nil || !page.Truncated {
		t.Fatalf("discarded payload page=%#v err=%v, want truncated", page, err)
	}
	if calls != 1 {
		t.Fatalf("diagnostics=%d, want one", calls)
	}
	appended, err := recovered.Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "ready"})
	if err != nil || appended.Cursor <= 1 {
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

// TestEventHistoryRefusesForeignRuntimeDirectory covers the daemonless CLI and
// MCP read path: events planted by another user who owns the runtime directory
// must not be reported.
func TestEventHistoryRefusesForeignRuntimeDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewEventHistory(dir, protocol.ScopeGlobal, "").Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "launch"}); err != nil {
		t.Fatal(err)
	}
	missing, err := NewEventHistory(filepath.Join(dir, "missing"), protocol.ScopeGlobal, "").Read(nil, time.Time{}, nil, false, nil, 50, nil, 0)
	if err != nil || len(missing.Events) != 0 {
		t.Fatalf("missing runtime directory page=%#v err=%v, want empty", missing, err)
	}
	simulateForeignRuntimeUser(t)
	page, err := NewEventHistory(dir, protocol.ScopeGlobal, "").Read(nil, time.Time{}, nil, false, nil, 50, nil, 0)
	if !errors.Is(err, ErrHistoryUnavailable) || !strings.Contains(err.Error(), "not owned by the current user") || len(page.Events) != 0 {
		t.Fatalf("foreign history page=%#v err=%v, want ownership refusal", page, err)
	}
}

func TestEventHistoryLifecycleOperationAttributionAndReadIsolation(t *testing.T) {
	root := t.TempDir()
	server := &Server{
		paths:      NewRuntimePaths(t.TempDir()),
		maxLine:    defaultWireMaxLine,
		eventOps:   make(map[string]eventOperation),
		histories:  make(map[string]*EventHistory),
		eventQueue: make(chan queuedHistoryEvent, 32),
	}

	for _, lifecycle := range []string{"launch", "ready", "startup_failure"} {
		operationID := "op-" + lifecycle
		finish := server.beginEventOperation(protocol.ScopeProject, root, root, "api", operationID, "mcp")
		server.recordLifecycle(app.LifecycleEvent{Scope: protocol.ScopeProject, Root: root, Cwd: root, Name: "api", Event: lifecycle, Time: time.Now().UTC()})
		finish()
		queued := <-server.eventQueue
		if queued.event.Kind != protocol.EventLifecycle || queued.event.Event != lifecycle || queued.event.OperationID != operationID {
			t.Fatalf("attributed %s event=%#v", lifecycle, queued.event)
		}
	}
	for _, lifecycle := range []string{"exit", "relaunch_scheduled", "relaunch_attempt", "relaunch_failure", "relaunch_exhausted"} {
		server.recordLifecycle(app.LifecycleEvent{Scope: protocol.ScopeProject, Root: root, Cwd: root, Name: "api", Event: lifecycle, Time: time.Now().UTC()})
		queued := <-server.eventQueue
		if queued.event.Event != lifecycle || queued.event.OperationID != "" {
			t.Fatalf("automatic %s event=%#v", lifecycle, queued.event)
		}
	}
	sharedID := "bulk-operation"
	server.appendHistory(protocol.ScopeProject, root, root, sharedID, "mcp", "down", "api", "success", "operator_stop")
	server.appendHistory(protocol.ScopeProject, root, root, sharedID, "mcp", "down", "web", "failure", "")
	firstOperation, firstLifecycle, secondOperation := <-server.eventQueue, <-server.eventQueue, <-server.eventQueue
	if firstOperation.event.OperationID != sharedID || firstOperation.event.Origin != "mcp" || firstOperation.event.Outcome != "success" || firstLifecycle.event.Event != "operator_stop" || firstLifecycle.event.OperationID != sharedID || secondOperation.event.OperationID != sharedID || secondOperation.event.Outcome != "failure" {
		t.Fatalf("bulk attribution: first=%#v lifecycle=%#v second=%#v", firstOperation.event, firstLifecycle.event, secondOperation.event)
	}
	server.appendHistory(protocol.ScopeProject, root, root, "remove-id", "cli", "remove", "api", "success", "removal")
	removeOperation, removal := <-server.eventQueue, <-server.eventQueue
	if removeOperation.event.Event != "remove" || removal.event.Event != "removal" || removal.event.OperationID != "remove-id" {
		t.Fatalf("removal events=%#v %#v", removeOperation.event, removal.event)
	}
	server.appendHistory(protocol.ScopeProject, root, root, "restart-id", "cli", "restart", "api", "success", "explicit_restart")
	_, restart := <-server.eventQueue, <-server.eventQueue
	if restart.event.Event != "explicit_restart" || restart.event.OperationID != "restart-id" {
		t.Fatalf("restart event=%#v", restart.event)
	}

	// Events is read-only even when validation fails: dispatch must not append an
	// operation failure for a malformed history request.
	response, _ := server.dispatch(&protocol.Request{Op: protocol.OpEvents, Events: &protocol.EventsRequest{Scope: protocol.ScopeProject, Root: root, Tail: maxHistoryEvents + 1}})
	if responseError(response) == nil || len(server.eventQueue) != 0 {
		t.Fatalf("read-only events response=%#v queued=%d", response, len(server.eventQueue))
	}
}

func TestEventHistorySanitizesFailedControlRequest(t *testing.T) {
	root, err := project.CanonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := shortRuntimeDir(t)
	supervisor, err := app.New(app.Options{StartProcess: func(process.Spec) (app.Child, error) {
		return nil, errors.New("raw-error-secret")
	}})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Config{RuntimeDir: runtimeDir, Supervisor: supervisor})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	request := &protocol.Request{Op: protocol.OpStart, ID: "operation-1", EventOperation: "run", EventOrigin: "cli", Start: &protocol.StartRequest{Name: "api", Scope: protocol.ScopeProject, Root: root, Cwd: root, Argv: []string{"argv-secret"}, Env: []string{"TOKEN=environment-secret"}, Origin: "cli"}}
	response, _ := server.dispatch(request)
	if responseError(response) == nil {
		t.Fatal("failed start unexpectedly succeeded")
	}
	server.flushHistoryEvents()
	history, err := server.history(protocol.ScopeProject, root, root)
	if err != nil {
		t.Fatal(err)
	}
	page, err := history.Read(nil, time.Time{}, nil, false, nil, 50, nil, 0)
	if err != nil || len(page.Events) != 2 {
		t.Fatalf("events=%#v err=%v", page.Events, err)
	}
	encoded, err := json.Marshal(page.Events)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"raw-error-secret", "argv-secret", "environment-secret", "TOKEN="} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("history retained %q: %s", secret, encoded)
		}
	}
	for _, event := range page.Events {
		if event.OperationID != "operation-1" {
			t.Fatalf("operation attribution=%#v", page.Events)
		}
	}
	if page.Events[0].Event != "startup_failure" || page.Events[1].Event != "run" || page.Events[1].Origin != "cli" || page.Events[1].Outcome != "failure" {
		t.Fatalf("failed operation events=%#v", page.Events)
	}
}

func TestEventHistoryByteRetentionAndNeverTruncatedZero(t *testing.T) {
	history := NewEventHistory(t.TempDir(), protocol.ScopeProject, "/project")
	zero := protocol.Cursor(0)
	if page, err := history.Read(nil, time.Time{}, nil, false, nil, 50, &zero, 0); err != nil || page.Truncated || page.NextCursor != 0 {
		t.Fatalf("never-used zero page=%#v err=%v", page, err)
	}
	for i := 0; i < 80; i++ {
		if _, err := history.Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "exit", Detail: strings.Repeat("é", 8000)}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := history.Read(nil, time.Time{}, nil, false, nil, maxHistoryEvents, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) >= 80 || page.Events[0].Cursor == 1 {
		t.Fatalf("byte retention did not evict oldest events: count=%d first=%d", len(page.Events), page.Events[0].Cursor)
	}
	data, err := os.ReadFile(history.EventPath())
	if err != nil || len(data) > 2*maxHistoryBytes {
		t.Fatalf("stored bytes=%d err=%v", len(data), err)
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
		if event.Cursor == 0 || index > 0 && event.Cursor <= page.Events[index-1].Cursor {
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
