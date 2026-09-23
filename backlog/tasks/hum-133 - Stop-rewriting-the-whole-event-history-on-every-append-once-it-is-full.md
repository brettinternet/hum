---
id: HUM-133
title: Stop rewriting the whole event history on every append once it is full
status: To Do
assignee: []
created_date: '2026-09-23 21:53'
labels:
  - daemon
  - events
milestone: m-4
dependencies: []
modified_files:
  - internal/daemon/event_history.go
  - internal/daemon/event_history_test.go
  - docs/design.md
priority: medium
type: enhancement
ordinal: 16000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: appending a daemon event stays cheap once history reaches its retention limit, and the retention tests run in about a second instead of 30. Evidence (macOS, 2026-09-23): `go test ./internal/daemon -run TestEventHistory -count=1 -v` spends 30.7s in TestEventHistoryRetentionAndNameReuse (internal/daemon/event_history_test.go:47). The test appends 2,003 events, at about 15ms each. `task stress` runs internal/daemon five times under -race, so this one test costs minutes there.

Cause, in EventHistory.Append (internal/daemon/event_history.go:266):
- Every append first rewrites the cursor mark file through historyWriteAtomic (:198), which writes a temp file, fsyncs it, renames it, and fsyncs the directory. Append then writes the event line and fsyncs again. On macOS, Go's File.Sync issues F_FULLFSYNC.
- Once history holds more than maxHistoryEvents (2000) or maxHistoryBytes (1 MiB), every append sets needsRewrite. Each append then rewrites the whole file atomically, and the trim loop calls historyBytes, which re-marshals every event, once for each dropped event.
- One goroutine, Server.writeHistoryEvents (internal/daemon/server.go:687), performs appends from eventQueue (capacity 1024, server.go:158). queueHistoryEvent (:704) blocks its caller when the queue is full. During bursts such as a restart loop, slow appends can therefore stall lifecycle and request paths.

Scope:
1. Compaction with slack: keep appending while the file holds at most twice the retention limits (4,000 events or 2 MiB), then rewrite it to the newest retained events. Trim the in-memory events to the retention limits after every append so Read results do not change. loadLocked (:117) trims loaded events to the limits too. Track retained bytes incrementally instead of calling historyBytes in a loop.
2. Cursor reservation: persist the cursor mark in blocks, for example highwater+64, and rewrite it only when the block is used up. The mark is still written before any event whose cursor it covers. A crash may skip at most one block of cursors, but must never reuse one. The code already allows gaps ("A crash can leave a gap, but can never cause cursor reuse"). Keep a separate in-memory last-assigned cursor so that ErrHistoryCursorFuture checks, NextCursor, and HighWater (:502) use the real last cursor. After a restart, start from the persisted mark, as today.
3. Turn the limits into EventHistory fields that NewEventHistory (:73) sets from the constants, so retention tests can use small limits such as 20 events. Add unexported counters for full rewrites and mark writes that tests can read. Keep one test asserting that the production defaults are 2000 events and 1 MiB.
4. Update docs/design.md:957-962 so it says a crash can skip cursors but never reuses them.

Invariants that must still hold (existing tests cover most of them):
- No cursor is reused after a crash at any point in Append.
- loadLocked ignores a torn final line and rewrites it.
- A malformed payload is diagnosed once.
- An event ahead of the mark is distrusted.
- Files stay mode 0600.
- Each event stays within the 16 KiB bound.
- Read returns the newest 2000 events within 1 MiB.

Non-goals: dropping the per-event payload fsync or weakening durability in any other way; batching several events into one append; changing Read semantics or the event JSON shape.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/daemon -run "^TestEventHistory" -count=1 -v` exits 0, and TestEventHistoryRetentionAndNameReuse reports under 2s on macOS (30.7s on 2026-09-23).
- [ ] #2 AC2 — `go test ./internal/daemon -run "^TestEventHistoryAppendCost$" -count=1 -v` exits 0; with limits of 20 events, 1,000 appends perform at most 51 full rewrites and at most 32 cursor-mark writes, read from the test counters.
- [ ] #3 AC3 — `go test ./internal/daemon -run "^TestEventHistoryCrashNeverReusesCursor$" -count=1 -v` exits 0; it reopens the directory with a new EventHistory after appends inside a reserved block and after a torn final line, and proves every new cursor is greater than every cursor returned before.
- [ ] #4 AC4 — `go test ./internal/daemon -count=1` exits 0 and `go test -race ./internal/daemon -run "^TestEventHistory" -count=3` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->
