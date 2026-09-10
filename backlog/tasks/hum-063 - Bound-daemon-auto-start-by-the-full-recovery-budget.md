---
id: HUM-063
title: Bound daemon auto-start by the full recovery budget
status: To Do
assignee: []
created_date: '2026-09-10 01:49'
updated_date: '2026-09-10 06:01'
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
Outcome: Client auto-start waits long enough for every bounded stale-runtime reconciliation group, and timeout cancellation terminates the spawned daemon cleanly rather than creating a second stale recovery cycle. Evidence: `reconcileStartup` (internal/daemon/runtime.go:611) reclaims each recorded stale group with `reclaimRuntimeGroup`, which waits up to the stop grace after TERM and again after KILL, so one unresponsive group can take 2x grace (20s at the 10s default) and N groups run sequentially. `waitForDaemon` (internal/cli/daemon_start.go:118) uses one fixed `daemonStartupTimeout` of 5s and on expiry calls `terminateDetachedChild`, killing the daemon mid-reconciliation and leaving the runtime stale for the next start. Scope: derive one documented startup budget from the maximum reconciliation work (groups x 2 x grace plus dial slack), preserve caller cancellation, reap failed children, and cover multi-group recovery deterministically. Non-goals: do not increase normal healthy-daemon dial latency, weaken identity verification, or make recovery unbounded.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise exec go -- go test -race ./internal/daemon && mise exec go -- go test ./internal/cli` exits 0.
- [ ] #2 `mise exec go -- go test ./internal/cli -run TestEnsureDaemonWaitsForSequentialRecovery -count=1` exits 0 after proving a daemon that needs TERM-then-KILL escalation on two sequential stale groups is not killed at five seconds and publishes its socket.
- [ ] #3 `mise exec go -- go test ./internal/cli -run TestEnsureDaemonCancellationReapsChild -count=1` exits 0 after proving caller cancellation returns promptly and leaves no daemon child or stale startup artifacts.
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

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: confirmed with corrected numbers. The original evidence said five seconds per stale group; the code uses the configured stop grace (default 10s, zero falls back to 10s at runtime.go:618) and waits it twice per group (TERM then KILL). The 5s client budget is exceeded by a single unresponsive group.
---
<!-- COMMENTS:END -->
