---
id: HUM-003
title: Implement bounded stdout and stderr buffers
status: Done
assignee: []
created_date: '2026-09-02 17:05'
updated_date: '2026-09-03 01:07'
labels:
  - output
milestone: m-0
dependencies:
  - HUM-001
modified_files:
  - internal/output/
priority: high
type: feature
ordinal: 300
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the core appends line-oriented stdout and stderr entries into one ordered per-process sequence, answers deterministic bounded reads using monotonically increasing entry cursors, and notifies concurrent followers without coupling output storage to CLI presentation.

Scope: one entry sequence per process where each entry carries stream (stdout, stderr, or system), timestamp, and raw line text; the cursor is the entry sequence number, starting at 0 and never reused; line splitting on newline with partial lines flushed after a short idle interval or at the configured maximum line length; bounded retention by total bytes per process with oldest-first eviction; reads with after-cursor, tail (entry count), stream filter, regex match, and byte limit; conservative 100-entry and 16 KiB defaults; next cursor plus truncation/eviction metadata when the cursor predates retained data; exact-boundary and future-cursor handling; multiple simultaneous append/exit subscribers; cancellation-safe subscriptions; bounded per-read delivery so a slow follower cannot cause unbounded memory growth; system entries (for example a restart marker) appended through the same API. Avoid avoidable copies and allocations on append/read paths.

Non-goals: daemon networking, process execution, disk persistence, CLI rendering, or PTY semantics.

Modified-file contract: internal/output/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/output -run TestRingEviction` exits 0 and proves retained bytes stay at or below the configured bound while cursors only increase and are never reused.
- [x] #2 `go test ./internal/output -run 'TestCursorTruncation|TestFollowerEviction'` exits 0 and proves stale, exact-boundary, future, and evicted follower cursors return documented data or explicit errors/metadata.
- [x] #3 `go test ./internal/output -run 'TestReadFilters|TestMultipleFollowers'` exits 0 and covers stdout/stderr/both filtering over one cursor space, tail, byte limit, regex matches, next cursor, multiple simultaneous followers, cancellation, and bounded slow-client delivery.
- [x] #4 `go test ./internal/output -run TestLineSplitting` exits 0 and proves newline splitting, idle flush of a partial line, and splitting of a line longer than the maximum length, each preserving raw bytes.
- [x] #5 `go test ./internal/output -bench . -benchmem` completes with stable bounded storage; implementation notes record allocations for append, bounded read, and follower notification.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented as commit acbf8fa and merged to main as e1ff663.

AC#1: `go test ./internal/output -run TestRingEviction` exited 0. TestRingEviction proves retained payload bytes never exceed the configured bound, successful cursors start at 0 and increase monotonically, rejected appends consume no cursor, and evicted slots release payload references.

AC#2: `go test ./internal/output -run 'TestCursorTruncation|TestFollowerEviction'` exited 0. Coverage includes stale cursors with Truncated/EvictedThrough metadata, exact retained and latest boundaries, future-cursor errors, and a lagging follower resuming from retained data.

AC#3: `go test ./internal/output -run 'TestReadFilters|TestMultipleFollowers'` exited 0. Coverage includes stdout/stderr/all filtering over one cursor space, tail, entry and byte limits, regex, Next/More, simultaneous followers, prompt blocked-read cancellation, nonblocking slow followers, and explicit eviction metadata.

AC#4: `go test ./internal/output -run TestLineSplitting` exited 0. Coverage includes LF and CRLF preservation, NUL/invalid UTF-8 bytes, idle partial flush and retry, maximum-length chunks, close/flush, callback-failure retries, and preservation of prior chunk boundaries.

AC#5: `go test ./internal/output -bench . -benchmem` exited 0. Apple M1 Max results: BenchmarkAppend 9.241 ns/op, 0 B/op, 0 allocs/op; BenchmarkRead 1281 ns/op, 6168 B/op, 4 allocs/op; BenchmarkReadTailLarge 56052 ns/op, 114712 B/op, 4 allocs/op; BenchmarkFollowerNotification 155.8 ns/op, 272 B/op, 7 allocs/op. Append and follower benchmarks assert retained bytes, entries, and backing storage remain bounded while cursors advance.

Final merged-main verification: `task ci` exited 0, running gofmt verification, go vet, staticcheck, and all Go tests; `go test -race ./internal/output` exited 0. Independent verifier re-read the final package, named test coverage, benchmark bounds, and these notes and returned PASS for AC#1 through AC#5. Adversarial reviews found and resolved exit watermark ordering, bounded exit notification history, line-retry/idle boundaries, timer cleanup, bounded-read allocation, and sparse-tail allocation defects. Final splitter and bounded-exit reviews reported no validated findings.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-02 23:08
---
Claimed for implementation in branch hum-003.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented and merged bounded per-process stdout/stderr/system output storage with monotonic cursors, oldest-first byte eviction, deterministic filtered/tail reads, raw-byte line splitting with idle retry, and bounded cancellation-safe followers. Added focused race-tested coverage and allocation benchmarks. Implementation: acbf8fa; merged to main: e1ff663.
<!-- SECTION:FINAL_SUMMARY:END -->
