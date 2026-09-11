---
id: HUM-086
title: Preserve attached-run handoffs under fast exit and early signals
status: Done
assignee: []
created_date: '2026-09-11 01:40'
updated_date: '2026-09-11 03:34'
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
- [x] #1 Both reproduction commands exit 0 and the implementation uses explicit handoff synchronization rather than retries or wider timeouts.
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
AC#1 — PASS: `mise exec go -- go test -race -count=500 -run TestAttachedRunOneIncarnation/already-exited ./internal/cli` exited 0; `mise exec go -- go test -race -count=50 -run TestDetachedAndObserverLifecycleUnchanged/attach_and_logs_signals_detach_observers_only ./internal/cli` exited 0. The app idle callback now honors the existing `starting` reservation under `Supervisor.mu`, and attach registers signal delivery before daemon lookup or retained/live output; no retries or timeout changes were added.
Delivery — commit `195bbcc` merged fast-forward to `main`; `task ci` passed on that final commit.
Verification — independent verifier PASS for AC#1 after rerunning both exact commands; diff limited to `internal/app/` and `internal/cli/`; no tests deleted, skipped, or weakened; no protected gate files changed. Review found no item-scoped defects.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Preserved pre-launch sessions through in-flight start publication and registered attach signal handling before any output can become observable. Added a deterministic launch-handoff regression test and merged commit `195bbcc` to `main`.
<!-- SECTION:FINAL_SUMMARY:END -->
