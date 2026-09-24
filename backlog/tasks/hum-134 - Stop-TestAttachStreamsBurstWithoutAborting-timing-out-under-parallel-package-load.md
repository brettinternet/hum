---
id: HUM-134
title: >-
  Stop TestAttachStreamsBurstWithoutAborting timing out under parallel package
  load
status: Done
assignee: []
created_date: '2026-09-24 06:53'
updated_date: '2026-09-24 20:51'
labels:
  - cli
  - testing
dependencies: []
modified_files:
  - internal/cli/serve_run_test.go
priority: medium
type: bug
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the default-parallel task ci gate no longer fails intermittently in TestAttachStreamsBurstWithoutAborting (internal/cli/serve_run_test.go). Evidence: the test streamed only ~10-11k of 12000 flood lines before cliServeRunWaitForCondition timed out during default-parallel go test ./... runs while reviewing HUM-123/128/129/130/131 (twice), and the same flake is recorded in the notes of HUM-118, HUM-123, HUM-124, HUM-125, HUM-128, HUM-131, and HUM-132. Focused and serial (GOFLAGS=-p=1) runs pass, so this looks like a fixed wall-clock wait racing CPU contention rather than a dropped-output bug, but confirm that before changing the wait.

Scope: determine whether attach is slow or losing lines under load (for example, check whether the line count keeps advancing at timeout); then make the wait progress-based or otherwise load-tolerant while still failing if attach aborts, exits, or stops making progress.

Non-goals: changing attach backpressure behavior unless the investigation shows lines are actually lost; weakening the whole-burst assertion; lowering floodLines to hide the problem.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 go test ./internal/cli -run '^TestAttachStreamsBurstWithoutAborting$' -count=20 exits 0
- [x] #2 task ci exits 0 on three consecutive runs with default package parallelism, with no TestAttachStreamsBurstWithoutAborting failure
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
1. Inspect flood fixture, attach capture, and timeout behavior; reproduce under package load and observe line-count progress.
2. Change only the burst wait to tolerate continued progress while bounding stalls and aborts; preserve all 12000-line and stderr/exit assertions.
3. Run the focused count=20 check and three default-parallel task ci gates, obtain independent verifier evidence, commit, merge to main, and finalize provider state.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation commit 0ca5029 changes only internal/cli/serve_run_test.go. Prior CI notes (HUM-131) recorded 9475 of 12000 lines at five-second timeout; the flood fixture emits all 12000 then stays alive, so partial output at deadline is compatible with slow delivery, not evidence of loss. The test now resets a five-second no-progress window as observed line count rises, fails promptly on attach exit or stall, and still asserts the exact whole burst. Focused count=20 PASS; task ci runs 1 and 2 PASS on 0ca5029. Third task ci failed in unrelated integration smoke TestShutdown due to an empty grandchild PID marker; no attach failure. Next: rerun full gates and independent acceptance verification.

AC#1 go test ./internal/cli -run '^TestAttachStreamsBurstWithoutAborting$' -count=20: PASS on 0ca5029; independent verifier reran the exact command and got PASS (3.865s).
AC#2 task ci: PASS on 0ca5029 for three consecutive default-parallel runs after the unrelated TestShutdown smoke flake; no attach failures. Independent verifier also ran three consecutive task ci gates (one initial, two sequential) and reported PASS for AC2; follow-up runs used cached Go test results.
Review: one general pass identified a pre-existing hypothetical duplicate-plus-omission gap in the original line-count assertion; no observed trigger, no change to this assertion in this item. The original last-line, exact-count, stderr, and exit assertions remain, and the changed wait fails after five seconds of no progress or immediately on exit. Only declared test file changed; no tests deleted/skipped or protected gate files changed. Branch 0ca5029 fast-forward merged into main; task ci passed on that commit. Next: finalize task state, commit provider record, clean up the owned worktree.

Finalization: provider completion commit 59d3e41 on main; task ci PASS on 59d3e41 (default-parallel go test ./..., race, smoke, security). Worktrunk removed the merged hum-134 worktree and branch and its associated Herdr workspace. No remaining blocker or resumable step; task Done. The earlier Next note was superseded by these completed actions.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
HUM-134: flood attach wait now tracks output progress rather than a fixed total deadline while retaining whole-burst assertions. Committed 0ca5029 and fast-forward merged to main; focused count=20 and three consecutive task ci runs passed, independently verified.
<!-- SECTION:FINAL_SUMMARY:END -->
