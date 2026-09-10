---
id: HUM-072
title: Centralize supervisor lifecycle transitions
status: To Do
assignee: []
created_date: '2026-09-10 01:52'
updated_date: '2026-09-10 01:59'
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
- [ ] #1 `mise exec go -- go test -race ./internal/app -count=1` exits 0.
- [ ] #2 `mise exec go -- go test ./internal/app -run TestLifecycleTransitionInvariants -count=1` exits 0 after covering start, explicit restart, automatic relaunch, persistence failure, stop, remove, and TTY preparation legal state combinations.
- [ ] #3 `rg -n "doneClosed" internal/app` exits 1, and `mise exec go -- go test ./internal/app -run TestRunningTransitionSharedByStartAndRestart -count=1` exits 0.
- [ ] #4 `task ci` exits 0.
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
