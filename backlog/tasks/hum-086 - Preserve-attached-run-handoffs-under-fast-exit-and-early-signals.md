---
id: HUM-086
title: Preserve attached-run handoffs under fast exit and early signals
status: To Do
assignee: []
created_date: '2026-09-11 01:40'
labels: []
dependencies: []
ordinal: 60800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: attached CLI observers cannot lose the pre-launch session or receive a default OS signal after output becomes observable. Scope: internal/app session eviction and internal/cli attach signal registration. Non-goals: timeout increases or retries. Reproductions: (1) mise exec go -- go test -race -count=500 -run 'TestAttachedRunOneIncarnation/already-exited' ./internal/cli; observed intermittent 'supervised process not found' instead of exit 37; suspected cause: the final follower closes after the child exit notification while StartProcess is still publishing, and the idle callback evicts the incarnation-0 session. (2) mise exec go -- go test -race -count=50 -run 'TestDetachedAndObserverLifecycleUnchanged/attach_and_logs_signals_detach_observers_only' ./internal/cli; observed attach exit by signal interrupt; suspected cause: retained output becomes observable before attach registers signal.Notify. Modified files: internal/app/, internal/cli/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Both reproduction commands exit 0 and the implementation uses explicit handoff synchronization rather than retries or wider timeouts.
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
