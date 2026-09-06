---
id: HUM-051
title: Account for per-entry memory in retained output limits
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - output
  - daemon
milestone: m-4
dependencies: []
modified_files:
  - internal/output/ring.go
  - internal/output/types.go
  - internal/output/ring_test.go
  - internal/app/app.go
  - docs/design.md
priority: medium
type: bug
ordinal: 28700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the retained-output budget (`--output-bytes`, default 4 MiB) bounds daemon memory, not just text bytes: each entry is charged its text length plus a fixed per-entry overhead (roughly the Entry struct plus string header, about 100 bytes), and eviction or `remove` releases memory to the OS (debug.FreeOSMemory after large evictions or on a slow timer).

Why now (measured 2026-09-06): 300k short lines totalling 2.0 MB of text raised daemon RSS from 11 MB to 66 MB (~188 B per entry, ~27x amplification) and `hum remove` on every record returned none of it after 120 s. A chatty dev server left running all day grows the shared daemon monotonically.

Non-goals: compressing output, changing the wire protocol, persistent storage.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/output -run '^TestRetentionChargesEntryOverhead$' -count=1 -v` exits 0 and prints PASS: 300k ten-byte entries under a 4 MiB limit retain far fewer than 300k entries and the ring's accounted size never exceeds the limit.
- [ ] #2 `go test ./internal/daemon -run '^TestRemoveReleasesMemory$' -count=1 -v` exits 0 and prints PASS: runtime.MemStats HeapInuse after remove and FreeOSMemory is below 25% of its peak.
- [ ] #3 `task ci` exits 0.
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
