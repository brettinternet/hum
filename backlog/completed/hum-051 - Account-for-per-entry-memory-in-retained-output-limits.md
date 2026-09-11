---
id: HUM-051
title: Bound retained output by text and entry overhead
status: Done
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-07 03:37'
labels:
  - output
  - daemon
milestone: m-4
dependencies: []
modified_files:
  - internal/output/types.go
  - internal/output/ring.go
  - internal/output/ring_test.go
  - internal/output/store_test.go
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/cli/root.go
  - internal/cli/surface_test.go
  - internal/cli/list_logs_test.go
  - docs/design.md
priority: medium
type: bug
ordinal: 28700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the per-process retained-output budget (`--output-bytes`, default 4 MiB) bounds both retained text and entry cardinality. Each stored entry is charged `len(text) + 128` bytes, where 128 is the documented conservative fixed overhead for entry metadata, string/slice storage, and allocator slack. The charged size is used for oversize rejection, eviction, initial/growing ring capacity, and the invariant that accounted retained bytes never exceed the configured budget. Read byte limits remain text-byte limits, preserving the wire and CLI contract.

Scope: change the in-memory output ring accounting and capacity rules, expose the fixed charge within the output package for exact tests, clear evicted slots so text becomes unreachable, and document the distinction between retention charge and read bytes. Removing a process must release references so ordinary Go garbage collection can reclaim them; the daemon must not force global GC or promise immediate RSS return.

Why now (measured 2026-09-06): 300k short lines totaling 2.0 MB of text raised daemon RSS from 11 MB to 66 MB because text-only accounting allows hundreds of thousands of entry records under a 4 MiB budget. A chatty development server can therefore grow the shared daemon far beyond the configured bound.

Non-goals: exact RSS bounding, forcing memory back to the OS, calling `runtime.GC` or `debug.FreeOSMemory` from request paths, compressing output, changing wire fields, changing read-limit semantics, or persistent storage.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/output -run '^TestRetentionChargesEntryOverhead$' -count=1 -v` exits 0 and prints PASS, proving each entry is charged exactly `len(text)+128`, an entry whose charged size exceeds the budget is rejected without mutation, and accounted retained bytes never exceed the limit across append and eviction.
- [x] #2 `go test ./internal/output -run '^TestRetentionBoundsShortEntryCardinality$' -count=1 -v` exits 0 and prints PASS, proving 300k ten-byte appends under a 4 MiB limit retain at most `floor(4MiB/138)` entries, preserve chronological reads/cursors, bound backing capacity to the charged entry limit, and clear evicted slots.
- [x] #3 `go test ./internal/app -run '^TestRemoveReleasesOutputReferences$' -count=1 -v` exits 0 and prints PASS, proving remove makes the process output store and its entries unreachable without invoking forced GC; `rg -n 'FreeOSMemory|runtime\.GC' internal/app internal/daemon` prints no matches.
- [x] #4 `go test ./internal/cli -run '^TestOutputByteDocs$' -count=1 -v` exits 0 and prints PASS, proving help and docs/design.md distinguish charged retention bytes from text-only read limits without claiming an exact RSS cap.
- [x] #5 `task ci` exits 0.
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
1. Define the 128-byte fixed retention charge and apply charged-size arithmetic to append, oversize, eviction, and capacity bounds.
2. Clear evicted storage and prove removed stores hold no reachable entries without forcing garbage collection.
3. Add accounting/cardinality/regression tests and document retention-charge versus read-byte semantics.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed for implementation in an isolated worktree.

AC#1 PASS — go test ./internal/output -run "^TestRetentionChargesEntryOverhead$" -count=1 -v exited 0 and printed PASS.
AC#2 PASS — go test ./internal/output -run "^TestRetentionBoundsShortEntryCardinality$" -count=1 -v exited 0 and printed PASS.
AC#3 PASS — go test ./internal/app -run "^TestRemoveReleasesOutputReferences$" -count=1 -v exited 0 and printed PASS; rg -n "FreeOSMemory|runtime\.GC" internal/app internal/daemon found no matches.
AC#4 PASS — go test ./internal/cli -run "^TestOutputByteDocs$" -count=1 -v exited 0 and printed PASS.
AC#5 PASS — task ci exited 0.
Independent verifier passed AC#1–AC#5. Scope expanded to internal/output/store_test.go and internal/cli/list_logs_test.go to update directly affected legacy tiny-budget fixtures, and internal/cli/root.go because AC#4 explicitly requires help text to distinguish charged retention from text-only reads.

Final commit 8344bd8. After rebasing onto current main, task ci exited 0 on the final commit.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented charged retained-output accounting with a fixed 128-byte per-entry overhead, bounded ring capacity/cardinality, slot clearing, output-reference release on removal, updated help/design documentation, and regression coverage. Merged commit 8344bd8 to main.
<!-- SECTION:FINAL_SUMMARY:END -->
