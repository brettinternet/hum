---
id: HUM-133
title: Stop rewriting the whole event history on every append once it is full
status: Done
assignee: []
created_date: '2026-09-23 21:53'
updated_date: '2026-09-24 20:22'
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
ordinal: 17000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: appending a daemon event stays cheap once history reaches its retention limit, and the retention tests run in about a second instead of 30. Evidence (macOS, 2026-09-23): `go test ./internal/daemon -run TestEventHistory -count=1 -v` spends 30.7s in TestEventHistoryRetentionAndNameReuse (internal/daemon/event_history_test.go:48). The test appends 2,003 events, at about 15ms each. `task stress` runs internal/daemon five times under -race, so this one test costs minutes there.

Cause, in EventHistory.Append (internal/daemon/event_history.go:272):
- Every append first rewrites the cursor mark file through historyWriteAtomic (:204), which writes a temp file, fsyncs it, renames it, and fsyncs the directory. Append then writes the event line and fsyncs again. On macOS, Go's File.Sync issues F_FULLFSYNC.
- Once history holds more than maxHistoryEvents (2000) or maxHistoryBytes (1 MiB), every append sets needsRewrite. Each append then rewrites the whole file atomically, and the trim loop calls historyBytes, which re-marshals every event, once for each dropped event.
- One goroutine, Server.writeHistoryEvents (internal/daemon/server.go:692), performs appends from eventQueue (capacity 1024, server.go:158). queueHistoryEvent (:708) blocks its caller when the queue is full. During bursts such as a restart loop, slow appends can therefore stall lifecycle and request paths.

Scope:
1. Compaction with slack: keep appending while the file holds at most twice the retention limits (4,000 events or 2 MiB), then rewrite it to the newest retained events. Trim the in-memory events to the retention limits after every append so Read results do not change. loadLocked (:117) trims loaded events to the limits too. Track retained bytes incrementally instead of calling historyBytes in a loop.
2. Cursor reservation: persist the cursor mark in blocks, for example highwater+64, and rewrite it only when the block is used up. The mark is still written before any event whose cursor it covers. A crash may skip at most one block of cursors, but must never reuse one. The code already allows gaps ("A crash can leave a gap, but can never cause cursor reuse"). Keep a separate in-memory last-assigned cursor so that ErrHistoryCursorFuture checks, NextCursor, and HighWater (:508) use the real last cursor. After a restart, start from the persisted mark, as today.
3. Turn the limits into EventHistory fields that NewEventHistory (:73) sets from the constants, so retention tests can use small limits such as 20 events. Add unexported counters for full rewrites and mark writes that tests can read. Keep one test asserting that the production defaults are 2000 events and 1 MiB.
4. Update docs/design.md:963-968 so it says a crash can skip cursors but never reuses them.

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
- [x] #1 AC1 — `go test ./internal/daemon -run "^TestEventHistory" -count=1 -v` exits 0, and TestEventHistoryRetentionAndNameReuse reports under 2s on macOS (30.7s on 2026-09-23).
- [x] #2 AC2 — `go test ./internal/daemon -run "^TestEventHistoryAppendCost$" -count=1 -v` exits 0; with limits of 20 events, 1,000 appends perform at most 51 full rewrites and at most 32 cursor-mark writes, read from the test counters.
- [x] #3 AC3 — `go test ./internal/daemon -run "^TestEventHistoryCrashNeverReusesCursor$" -count=1 -v` exits 0; it reopens the directory with a new EventHistory after appends inside a reserved block and after a torn final line, and proves every new cursor is greater than every cursor returned before.
- [x] #4 AC4 — `go test ./internal/daemon -count=1` exits 0 and `go test -race ./internal/daemon -run "^TestEventHistory" -count=3` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Track retained and on-disk counts/bytes, compact only past doubled limits or corruption, and reserve cursor blocks ahead of payloads. 2. Add small-limit cost, restart/crash, reload/slack, and default-limit tests; update design documentation. 3. Run focused tests, independent verification, final-commit task ci; merge to main and remove the verified worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commits 3d5d1fb and eb944e9 fast-forwarded into main; verified worktree hum-133-event-history removed with Worktrunk. Review: one independent verifier PASS for AC1–AC4, no concrete defects. Initial task ci failed because CLI event tests expected exact pre-reservation cursor values and two intermittent load-sensitive tests (TestAttachStreamsBurstWithoutAborting, TestWaitDaemonBridge); adapted CLI assertions to reservation semantics, focused CLI events passed, then task ci passed on final commit eb944e9 (default Go tests, race, security, staticcheck, installer, smoke). Scope deviation: internal/cli/events_test.go needed to reflect reserved high-water on a freshly opened daemonless reader; no protected gate files changed. Existing checks were adapted to the new contract (20-event fixture retains newest 20; disk <= doubled slack; cursor sequence monotonic across restarts), not skipped or deleted. Next step: none.

AC#1 — go test ./internal/daemon -run "^TestEventHistory" -count=1 -v: PASS; TestEventHistoryRetentionAndNameReuse 0.13s locally, independent verifier 0.30s macOS (<2s).

AC#2 — go test ./internal/daemon -run "^TestEventHistoryAppendCost$" -count=1 -v: PASS; 1000 appends with 20-event limit satisfy <=51 rewrites and <=32 mark writes, asserted from counters.

AC#3 — go test ./internal/daemon -run "^TestEventHistoryCrashNeverReusesCursor$" -count=1 -v: PASS; restart in reservation block and after torn tail both yield increasing cursors.

AC#4 — go test ./internal/daemon -count=1: PASS; go test -race ./internal/daemon -run "^TestEventHistory" -count=3: PASS.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Compacted event history with slack and reserved cursor blocks; verified four acceptance criteria, independent verifier PASS, and task ci on eb944e9. Merged to main and removed worktree.
<!-- SECTION:FINAL_SUMMARY:END -->
