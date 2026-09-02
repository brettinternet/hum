---
id: DEVPROC-003
title: Implement bounded stdout and stderr buffers
status: To Do
assignee: []
created_date: '2026-09-02 17:05'
updated_date: '2026-09-02 20:05'
labels:
  - output
milestone: m-0
dependencies:
  - DEVPROC-001
modified_files:
  - internal/output/
priority: high
type: feature
ordinal: 300
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the core can append timestamped stdout and stderr entries, answer deterministic bounded reads using monotonically increasing byte cursors, and notify concurrent followers without coupling output storage to CLI presentation.

Scope: separate streams; bounded in-memory ring eviction; cursor-before-retained-data truncation and explicit eviction metadata; line-tail and byte limits; stream filtering; regex matching; conservative 100-line and 16 KiB defaults; next cursor and truncation metadata; multiple simultaneous append/exit subscribers; cancellation-safe subscriptions; bounded per-read delivery so a slow follower cannot cause unbounded memory growth. Avoid avoidable copies and allocations on append/read paths.

Non-goals: daemon networking, process execution, disk persistence, CLI rendering, or PTY semantics.

Modified-file contract: internal/output/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/output -run TestRingEviction` exits 0 and proves retained bytes stay bounded while absolute cursors only increase.
- [ ] #2 `go test ./internal/output -run 'TestCursorTruncation|TestFollowerEviction'` exits 0 and proves stale, exact-boundary, future, and evicted follower cursors return documented data or explicit errors/metadata.
- [ ] #3 `go test ./internal/output -run 'TestReadFilters|TestMultipleFollowers'` exits 0 and covers stdout/stderr/both, tail, byte limit, regex matches, next cursor, multiple simultaneous followers, cancellation, and bounded slow-client delivery.
- [ ] #4 `go test ./internal/output -bench . -benchmem` completes with stable bounded storage; implementation notes record allocations for append, bounded read, and follower notification.
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
