---
id: HUM-063
title: Bound daemon auto-start by the full recovery budget
status: To Do
assignee: []
created_date: '2026-09-10 01:49'
labels: []
dependencies: []
modified_files:
  - internal/daemon/client.go
  - internal/daemon/client_test.go
  - internal/cli/daemon_start.go
  - internal/cli/daemon_start_test.go
  - docs/design.md
priority: high
type: bug
ordinal: 39700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Client auto-start waits long enough for every bounded stale-runtime reconciliation group, and timeout cancellation terminates the spawned daemon cleanly rather than creating a second stale recovery cycle. Evidence: daemon recovery allows five seconds per stale process group while `ensureDaemon` uses one fixed five-second startup context; two or more stale groups can exhaust that context before the daemon publishes its socket. Scope: derive one documented startup budget from the maximum reconciliation work, preserve caller cancellation, reap failed children, and cover multi-group recovery deterministically. Non-goals: do not increase normal healthy-daemon dial latency, weaken identity verification, or make recovery unbounded.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise exec go -- go test -race ./internal/daemon ./internal/cli` exits 0.
- [ ] #2 A focused daemon/client regression command with at least two sequential stale-group reconciliation waits exits 0 and proves auto-start reaches a ready daemon without killing the recovering child at five seconds.
- [ ] #3 A focused cancellation regression command exits 0 and proves caller cancellation still returns promptly and leaves no spawned daemon child or stale startup artifacts.
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
