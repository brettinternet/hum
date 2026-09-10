---
id: HUM-072
title: Centralize supervisor lifecycle transitions
status: Done
assignee: []
created_date: '2026-09-10 01:52'
updated_date: '2026-09-10 14:15'
labels: []
dependencies: []
modified_files:
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/app/relaunch_test.go
  - internal/app/tty_test.go
  - docs/design.md
priority: medium
type: chore
ordinal: 48700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Start, restart, automatic relaunch, exit publication, and failure recovery update lifecycle state through a small set of locked transition helpers with explicit invariants. Evidence: `record` has overlapping state flags, running-state initialization is duplicated between Start and Restart, exit/reset logic is distributed across several blocks, and `doneClosed` is assigned in six places but never read. This makes stale-field drift compile silently in the core supervisor. Scope: remove dead lifecycle fields, consolidate existing transition blocks without introducing a framework, and add invariant-focused tests across every launch and terminal path. Non-goals: do not redesign the supervisor, change externally observed states, alter relaunch policy, or add abstraction beyond the repeated transitions.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `mise exec go -- go test -race ./internal/app -count=1` exits 0.
- [x] #2 `mise exec go -- go test ./internal/app -run TestLifecycleTransitionInvariants -count=1` exits 0 after covering start, explicit restart, automatic relaunch, persistence failure, stop, remove, and TTY preparation legal state combinations.
- [x] #3 `rg -n "doneClosed" internal/app` exits 1, and `mise exec go -- go test ./internal/app -run TestRunningTransitionSharedByStartAndRestart -count=1` exits 0.
- [x] #4 `task ci` exits 0.
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
Implementation Notes (2026-09-10):
- AC#1: `mise exec go -- go test -race ./internal/app -count=1` exited 0.
- AC#2: `mise exec go -- go test ./internal/app -run TestLifecycleTransitionInvariants -count=1` exited 0; subtests cover start, explicit restart, automatic relaunch, exit-persistence failure, stop, remove, live unresolved recovery, and TTY preparation.
- AC#3: `rg -n "doneClosed" internal/app` exited 1 and `mise exec go -- go test ./internal/app -run TestRunningTransitionSharedByStartAndRestart -count=1` exited 0.
- AC#4: `task ci` exited 0 on the final implementation. An earlier independent run hit the pre-existing timing threshold in `TestDownStopsProcessesConcurrentlyWithIndependentConnections`; its focused race rerun and the complete final gate both passed.
- Review: independent lifecycle/concurrency review findings were fixed by asserting unresolved channel legality, stopped/removed child termination, and restart incarnation channel replacement.
- Modified-file contract: implementation touches only `internal/app/app.go` and `internal/app/app_test.go`; the provider-owned HUM-072 task file changed only for the required claim, evidence, and completion metadata. No tests were deleted, skipped, or weakened; no protected gate file changed.

Independent verification (2026-09-10): PASS for AC#1 through AC#4. The verifier reran both focused tests, confirmed the no-match search, and completed `task ci` successfully; it also confirmed the modified-file justification, no weakened tests, and no protected gate changes.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: confirmed. `doneClosed` is declared at internal/app/app.go:837 and assigned at :1472, :1639, :1966, :2107, :2402 with no reads. Running-state initialization duplicated between Start and Restart as described. Keep scope tight per non-goals; app.go is 3573 lines and the supervisor is the riskiest package to refactor, so AC#2 invariant tests must exist before consolidation.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Centralized dormant, running, terminal, and unresolved record transitions; removed the unread `doneClosed` flag; and added lifecycle invariant coverage for launch, restart, automatic recovery, persistence failure, operator controls, removal, and TTY preparation.
<!-- SECTION:FINAL_SUMMARY:END -->
