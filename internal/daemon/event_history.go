package daemon

// Durable service event history. This file deliberately has no dependency on
// the supervisor: history remains readable when no daemon is running.

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"hum/internal/protocol"
)

const (
	maxHistoryEvents   = 2000
	maxHistoryBytes    = 1 << 20
	maxHistoryEvent    = 16 << 10
	historyCursorBlock = 64
)

var (
	ErrHistoryUnavailable  = errors.New("event history unavailable")
	ErrHistoryCursorFuture = errors.New("event history cursor is beyond the high-water mark")
)

// HistoryPage is an immutable read result.
type HistoryPage struct {
	Events     []protocol.HistoryEvent
	NextCursor protocol.Cursor
	Truncated  bool
	HasMore    bool
}

type historyCursor struct {
	Cursor uint64 `json:"cursor"`
}

// EventHistory owns one scope's durable history. A History is safe for
// concurrent appends and reads; separate daemon replacements serialize through
// the atomic files and should not append concurrently during replacement.
type EventHistory struct {
	dir, eventPath, cursorPath string
	diagnostic                 func(error)
	mu                         *sync.Mutex
	loaded                     bool
	events                     []protocol.HistoryEvent
	eventSizes                 []int
	retainedBytes              int
	diskEvents, diskBytes      int
	maxEvents, maxBytes        int
	highwater, reserved        protocol.Cursor
	fullRewrites, markWrites   int
	malformed                  bool
	needsRewrite               bool
	unavailable                error
	diagnosed                  bool
}

var historyLocks sync.Map

func historyLock(path string) *sync.Mutex {
	value, _ := historyLocks.LoadOrStore(path, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// NewEventHistory opens (but does not create) a history for scope and root.
// Global histories use an empty root. The CLI and MCP read history without a
// daemon, so loading verifies the runtime directory as daemon startup does.
func NewEventHistory(runtimeDir, scope, root string) *EventHistory {
	key := scope + "\x00" + root
	digest := sha256.Sum256([]byte(key))
	name := fmt.Sprintf("events-%x", digest[:12])
	eventPath := filepath.Join(runtimeDir, name+".ndjson")
	return &EventHistory{dir: runtimeDir, eventPath: eventPath, cursorPath: filepath.Join(runtimeDir, name+".cursor"), mu: historyLock(eventPath), maxEvents: maxHistoryEvents, maxBytes: maxHistoryBytes}
}

// NewHistory is a concise compatibility alias.
func NewHistory(runtimeDir, scope, root string) *EventHistory {
	return NewEventHistory(runtimeDir, scope, root)
}

// SetDiagnostic receives one bounded diagnostic per malformed scope per
// EventHistory lifetime. It is never called while the history mutex is held.
func (h *EventHistory) SetDiagnostic(fn func(error)) {
	if h != nil {
		h.mu.Lock()
		h.diagnostic = fn
		h.mu.Unlock()
	}
}
func (h *EventHistory) EventPath() string {
	if h == nil {
		return ""
	}
	return h.eventPath
}
func (h *EventHistory) CursorPath() string {
	if h == nil {
		return ""
	}
	return h.cursorPath
}

func (h *EventHistory) diagnose(err error) {
	if h.diagnosed || h.diagnostic == nil {
		return
	}
	h.diagnosed = true
	fn := h.diagnostic
	fn(err)
}

func (h *EventHistory) loadLocked() error {
	if h.loaded {
		return h.unavailable
	}
	h.loaded = true
	if err := checkPrivateDir(h.dir); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		h.unavailable = fmt.Errorf("%w: %w", ErrHistoryUnavailable, err)
		return h.unavailable
	}
	data, err := os.ReadFile(h.cursorPath)
	cursorMissing := os.IsNotExist(err)
	if err == nil {
		var mark historyCursor
		if json.Unmarshal(data, &mark) != nil {
			h.unavailable = ErrHistoryUnavailable
			return h.unavailable
		}
		h.highwater = protocol.Cursor(mark.Cursor)
		h.reserved = h.highwater
	} else if !cursorMissing {
		h.unavailable = ErrHistoryUnavailable
		return h.unavailable
	}
	data, err = os.ReadFile(h.eventPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		h.unavailable = ErrHistoryUnavailable
		return h.unavailable
	}
	if cursorMissing && len(data) != 0 {
		h.unavailable = ErrHistoryUnavailable
		return h.unavailable
	}
	// A final non-newline fragment is a torn append and is intentionally ignored.
	// The next append rewrites the complete prefix before adding new data.
	if len(data) != 0 && data[len(data)-1] != '\n' {
		h.needsRewrite = true
		if cut := strings.LastIndexByte(string(data), '\n'); cut >= 0 {
			data = data[:cut+1]
		} else {
			data = nil
		}
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1024), maxHistoryEvent+1)
	var previous protocol.Cursor
	for scanner.Scan() {
		line := scanner.Bytes()
		var event protocol.HistoryEvent
		if len(line)+1 > maxHistoryEvent || json.Unmarshal(line, &event) != nil || event.Cursor == 0 || event.Cursor <= previous || event.Cursor > h.highwater || event.Kind != protocol.EventLifecycle && event.Kind != protocol.EventOperation || event.Name == "" || event.Event == "" {
			h.malformed = true
			h.events, h.eventSizes, h.retainedBytes = nil, nil, 0
			h.diagnose(errors.New("malformed event history payload"))
			return nil
		}
		previous = event.Cursor
		if event.Time.IsZero() {
			h.malformed = true
			h.events, h.eventSizes, h.retainedBytes = nil, nil, 0
			h.diagnose(errors.New("malformed event history timestamp"))
			return nil
		}
		h.diskEvents++
		h.diskBytes += len(line) + 1
		h.events = append(h.events, event)
		h.eventSizes = append(h.eventSizes, len(line)+1)
		h.retainedBytes += len(line) + 1
		h.trimLocked()
	}
	if scanner.Err() != nil {
		h.unavailable = ErrHistoryUnavailable
		return h.unavailable
	}
	return nil
}

// trimLocked keeps the visible window bounded while the disk can use its slack.
func (h *EventHistory) trimLocked() {
	for len(h.events) > h.maxEvents || h.retainedBytes > h.maxBytes {
		h.retainedBytes -= h.eventSizes[0]
		h.events[0] = protocol.HistoryEvent{}
		h.events = h.events[1:]
		h.eventSizes = h.eventSizes[1:]
	}
}

func (h *EventHistory) ensureDir() error {
	if err := os.MkdirAll(h.dir, 0700); err != nil {
		return err
	}
	return nil
}

func historyWriteAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".hum-history-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err == nil && runtime.GOOS != "windows" {
		// Windows denies Sync on directory handles. The file itself was
		// already flushed before the rename; retain the directory flush on
		// Unix where it is supported.
		if dir, openErr := os.Open(filepath.Dir(path)); openErr == nil {
			err = dir.Sync()
			_ = dir.Close()
		}
	}
	return err
}

func boundedEvent(event protocol.HistoryEvent) (protocol.HistoryEvent, []byte, error) {
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if event.Kind != protocol.EventLifecycle && event.Kind != protocol.EventOperation {
		return event, nil, errors.New("unknown event kind")
	}
	if event.Name == "" || event.Event == "" {
		return event, nil, errors.New("event name and event are required")
	}
	if len(event.Detail) > maxHistoryEvent {
		event.Detail = event.Detail[:maxHistoryEvent]
	}
	encode := func() ([]byte, error) {
		data, err := json.Marshal(event)
		return append(data, '\n'), err
	}
	data, err := encode()
	if err != nil {
		return event, nil, err
	}
	for len(data) > maxHistoryEvent && event.Detail != "" {
		event.Detail = event.Detail[:len(event.Detail)/2]
		data, err = encode()
		if err != nil {
			return event, nil, err
		}
	}
	if len(data) > maxHistoryEvent {
		return event, nil, errors.New("event exceeds 16 KiB bound")
	}
	return event, data, nil
}

// Append assigns the next cursor and durably records event. On a write error,
// the operation is returned as failed; assigned cursors are never reused.
func (h *EventHistory) Append(event protocol.HistoryEvent) (protocol.HistoryEvent, error) {
	if h == nil {
		return event, ErrHistoryUnavailable
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.loadLocked(); err != nil {
		return event, err
	}
	if err := h.ensureDir(); err != nil {
		return event, err
	}
	event.Cursor = h.highwater + 1
	if event.Cursor == 0 {
		return event, ErrHistoryUnavailable
	}
	bounded, line, err := boundedEvent(event)
	if err != nil {
		return event, err
	}
	// Reserve ahead of the payload. A restart begins at the persisted mark,
	// skipping unused cursors in the block rather than reusing them.
	if event.Cursor > h.reserved {
		reserved := event.Cursor + historyCursorBlock - 1
		if reserved < event.Cursor {
			reserved = ^protocol.Cursor(0)
		}
		mark, marshalErr := json.Marshal(historyCursor{Cursor: uint64(reserved)})
		if marshalErr != nil {
			return event, marshalErr
		}
		if err = historyWriteAtomic(h.cursorPath, mark); err != nil {
			return event, err
		}
		h.reserved = reserved
		h.markWrites++
	}
	h.highwater = event.Cursor

	// Only publish the new event and trim the window after the payload is durable.
	// Compute the compacted window without mutating the live one on failure.
	retained := append(append([]protocol.HistoryEvent(nil), h.events...), bounded)
	sizes := append(append([]int(nil), h.eventSizes...), len(line))
	bytes := h.retainedBytes + len(line)
	for len(retained) > h.maxEvents || bytes > h.maxBytes {
		bytes -= sizes[0]
		retained = retained[1:]
		sizes = sizes[1:]
	}
	rewrite := h.malformed || h.needsRewrite || h.diskEvents+1 > 2*h.maxEvents || h.diskBytes+len(line) > 2*h.maxBytes
	if rewrite {
		payload := make([]byte, 0, bytes)
		for _, item := range retained {
			encoded, marshalErr := json.Marshal(item)
			if marshalErr != nil {
				return event, marshalErr
			}
			payload = append(payload, encoded...)
			payload = append(payload, '\n')
		}
		err = historyWriteAtomic(h.eventPath, payload)
		if err == nil {
			h.fullRewrites++
			h.diskEvents, h.diskBytes = len(retained), len(payload)
			h.malformed, h.needsRewrite = false, false
		}
	} else {
		file, openErr := os.OpenFile(h.eventPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if openErr == nil {
			_, openErr = file.Write(line)
			if openErr == nil {
				openErr = file.Sync()
			}
			closeErr := file.Close()
			if openErr == nil {
				openErr = closeErr
			}
			err = openErr
		} else {
			err = openErr
		}
		if err == nil {
			h.diskEvents++
			h.diskBytes += len(line)
		}
	}
	if err != nil {
		// The cursor was reserved even if the payload failed. A retry must
		// replace any partial line before writing another event.
		h.needsRewrite = true
		return event, err
	}
	h.events, h.eventSizes, h.retainedBytes = retained, sizes, bytes
	return bounded, nil
}

// Read returns one immutable filtered page. A nil Match means no regex.
func (h *EventHistory) Read(names []string, since time.Time, kinds []protocol.EventKind, failed bool, match *regexp.Regexp, tail int, after *protocol.Cursor, maxBytes int) (HistoryPage, error) {
	if h == nil {
		return HistoryPage{}, ErrHistoryUnavailable
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.loadLocked(); err != nil {
		return HistoryPage{}, err
	}
	if tail <= 0 {
		tail = 50
	}
	if tail > h.maxEvents {
		tail = h.maxEvents
	}
	nameSet := make(map[string]bool)
	for _, name := range names {
		nameSet[name] = true
	}
	kindSet := make(map[protocol.EventKind]bool)
	for _, kind := range kinds {
		kindSet[kind] = true
	}
	matches := make([]protocol.HistoryEvent, 0)
	for _, event := range h.events {
		if len(nameSet) != 0 && !nameSet[event.Name] || !since.IsZero() && event.Time.Before(since) || len(kindSet) != 0 && !kindSet[event.Kind] || failed && !eventFailed(event) || match != nil && !match.MatchString(event.Name) && !match.MatchString(event.Detail) {
			continue
		}
		matches = append(matches, event)
	}
	page := HistoryPage{NextCursor: h.highwater}
	if after == nil {
		if len(matches) > tail {
			matches = matches[len(matches)-tail:]
		}
		if maxBytes > 0 {
			used := 0
			start := len(matches)
			for start > 0 {
				data, _ := json.Marshal(matches[start-1])
				if used+len(data)+1 > maxBytes {
					break
				}
				used += len(data) + 1
				start--
			}
			matches = matches[start:]
		}
		page.Events = append([]protocol.HistoryEvent(nil), matches...)
		page.HasMore = false
		return page, nil
	}
	if *after > h.highwater {
		return page, fmt.Errorf("%w: after cursor %d is beyond history cursor %d", ErrHistoryCursorFuture, *after, h.highwater)
	}
	expected := *after + 1
	for _, event := range h.events {
		if event.Cursor <= *after {
			continue
		}
		if event.Cursor > expected {
			page.Truncated = true
			break
		}
		expected = event.Cursor + 1
	}
	if !page.Truncated && expected <= h.highwater {
		page.Truncated = true
	}
	for _, event := range matches {
		if event.Cursor > *after {
			page.Events = append(page.Events, event)
		}
	}
	if len(page.Events) > tail {
		page.Events = page.Events[:tail]
		page.HasMore = true
	}
	if len(page.Events) != 0 {
		page.NextCursor = page.Events[len(page.Events)-1].Cursor
	}
	if len(page.Events) == 0 {
		page.NextCursor = h.highwater
	}
	if !page.HasMore {
		for _, event := range matches {
			if event.Cursor > page.NextCursor {
				page.HasMore = true
				break
			}
		}
	}
	if maxBytes > 0 {
		hadEvents := len(page.Events) != 0
		used := 0
		keep := page.Events[:0]
		for _, event := range page.Events {
			value := event
			data, _ := json.Marshal(value)
			if used+len(data)+1 > maxBytes {
				page.HasMore = true
				break
			}
			keep = append(keep, event)
			used += len(data) + 1
		}
		page.Events = keep
		if len(keep) != 0 {
			page.NextCursor = keep[len(keep)-1].Cursor
		} else if hadEvents {
			page.NextCursor = *after
			page.HasMore = true
		}
	}
	return page, nil
}

func eventFailed(event protocol.HistoryEvent) bool {
	if event.Kind == protocol.EventOperation {
		return event.Outcome != "" && event.Outcome != "success"
	}
	if event.Event == "startup_failure" || event.Event == "relaunch_failure" || event.Event == "relaunch_exhausted" {
		return true
	}
	return event.Event == "exit" && (event.ExitCode == nil || *event.ExitCode != 0 || event.Signal != "")
}

// NewOperationID generates an opaque per-request identifier.
func NewOperationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		binary.LittleEndian.PutUint64(b[:8], uint64(time.Now().UnixNano()))
		binary.LittleEndian.PutUint64(b[8:], uint64(os.Getpid()))
	}
	return fmt.Sprintf("%x", b[:])
}

func (h *EventHistory) HighWater() (protocol.Cursor, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.loadLocked(); err != nil {
		return 0, err
	}
	return h.highwater, nil
}
