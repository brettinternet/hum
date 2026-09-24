---
id: HUM-135
title: >-
  Stabilize TTY, attached-run, and readiness-relaunch tests that flake on CI
  runners
status: Done
assignee: []
created_date: '2026-09-24 12:39'
updated_date: '2026-09-24 22:34'
labels:
  - cli
  - testing
  - reviewed
dependencies: []
priority: medium
type: bug
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: CI stops failing intermittently in three tests that pass locally (including under -race with -count=5) but failed on GitHub runners during the HUM-117..125 review (run 35999701233, commit 86b26b2, whose Unix-effective change was orchestrate-only): TestTTYCLI (internal/cli/tty_test.go:90, macOS race: replacement TTY run printed only the attach banner), TestAttachedRunForegroundLifecycle/SIGHUP_detaches_and_retained_logs_remain_readable (macOS race, run_reconnect_test.go:238: retained logs empty, next cursor 0), and TestExecutableReadinessAutomaticRelaunchIncarnationIsolation (internal/app/app_test.go:1229, Linux: probe pid file had fewer than 2 pids within 2s). HUM-122 notes also record a transient macOS TestTTYCLI race failure.

Scope: for each test, find out whether it is a fixed wall-clock wait racing runner load or a real ordering bug; make waits progress-based or event-driven, and fix product code if the failure is real.

Non-goals: TestAttachStreamsBurstWithoutAborting (HUM-134); weakening, skipping, or deleting assertions.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 go test -race ./internal/cli -run '^(TestTTYCLI|TestAttachedRunForegroundLifecycle)$' -count=20 exits 0
- [x] #2 go test ./internal/app -run '^TestExecutableReadinessAutomaticRelaunchIncarnationIsolation$' -count=50 -cpu 1,2 exits 0
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
1. Trace the three failures against lifecycle ordering and establish observable progress.
2. Keep macOS PTY slave alive until pending output is captured, wait for foreground output before HUP, and synchronize readiness relaunch against probe and timer events.
3. Run repeated focused checks, independent verification, task ci, then commit and merge.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Root causes: macOS PTY discards unread output when the last slave closes before capture is scheduled (reproduced in direct process test); the fixture .started marker precedes its stdout; the 100ms child and two-second wait raced executable probe startup plus one-second relaunch backoff. Implemented bounded PTY capture retention and progress-driven test synchronization. Process short-lived TTY regression and targeted integration checks pass; full gates pending.

Implementation commit baf4e68. AC#1: go test -race ./internal/cli -run '^(TestTTYCLI|TestAttachedRunForegroundLifecycle)$' -count=20 exited 0; AttachedRunForegroundLifecycle lives in integration, so go test -race ./integration -run '^TestAttachedRunForegroundLifecycle/SIGHUP_detaches_and_retained_logs_remain_readable$' -count=20 also exited 0. AC#2: go test ./internal/app -run '^TestExecutableReadinessAutomaticRelaunchIncarnationIsolation$' -count=50 -cpu 1,2 exited 0. task ci passed on baf4e68 (earlier attempts hit intermittent unrelated CLI/daemon timeouts; focused repetitions of those tests passed 10 times each). Independent verifier PASS for AC#1 and AC#2; follow-up reviewed PTY correction and returned PASS. Seven changed paths are the three affected tests, process implementation/regression test, and Darwin/Linux ioctl helpers; the task declared no modified-file list, so these are the justified scope deviation. No tests deleted, skipped, or weakened; no protected gate files changed. Next: integrate baf4e68 into main and clean up Worktrunk checkout.

Integrated baf4e68 by fast-forward into local main. No remote push requested. Next: remove verified session-owned worktree and branch; no implementation blocker.

Worktrunk post-remove removed the verified fix/HUM-135 checkout and branch; implementation is on main at baf4e68. All gates recorded above have completed successfully; earlier pending note is superseded. Claim released by Done status; no next implementation step.

Review (2026-09-24): PTY slave retention is bounded by captureHardTimeout (1s) including the pending-bytes wait; ttyPendingBytes covers the only supported Unix targets (darwin, linux). Re-ran TestTTYCLI -race count=10, TestAttachedRunForegroundLifecycle -race count=5, relaunch isolation count=50 -cpu 1,2, TestTTYShortLivedOutput: all PASS. No follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Fixed lost short-lived macOS TTY output by retaining the PTY slave through pending capture; synchronized HUP and readiness-relaunch tests with observable progress. AC#1, AC#2, independent verification, and task ci passed on baf4e68; fast-forward merged locally.
<!-- SECTION:FINAL_SUMMARY:END -->
