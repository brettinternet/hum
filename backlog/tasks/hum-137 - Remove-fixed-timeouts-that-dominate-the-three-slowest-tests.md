---
id: HUM-137
title: Remove fixed timeouts that dominate the three slowest tests
status: Done
assignee: []
created_date: '2026-09-24 22:51'
updated_date: '2026-09-24 23:12'
labels:
  - integration
  - daemon
dependencies: []
modified_files:
  - integration/lifecycle_test.go
  - internal/daemon/event_history_test.go
  - internal/daemon/runtime_test.go
  - internal/app/app.go
priority: high
type: task
ordinal: 21000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the three slowest top-level tests stop waiting on timeouts that are not the behavior under test. Measured on 2026-09-24 at commit 5f0ed4c with `go test -count=1 -json ./...` (macOS, Apple Silicon):

| Test | Time | Cause |
|---|---|---|
| integration TestSignalledLeaderWithSurvivingDescendant (integration/lifecycle_test.go:166) | 10.24s | The grandchild ignores TERM (`tree-child ... ignore-term`), so the final `hum stop descendant-state` waits the full default stop grace, 10s (internal/config/config.go:45 DefaultStopGrace). The test asserts the `descendants` state and that stop leaves it, not the grace length. |
| internal/daemon TestEventHistoryAppendCost (internal/daemon/event_history_test.go:107) | 8.71s | 1,000 appends, each fsyncing the history file (internal/daemon/event_history.go:284,297). The assertions are counter bounds, not elapsed time. |
| internal/daemon TestLaunchPersistenceFailureStopsChild/unconfirmed_cleanup_blocks_duplicate (internal/daemon/runtime_test.go:217) | 5.07s | Waits the full `persistenceCleanupTimeout = 5 * time.Second` constant (internal/app/app.go:1439, used at :2214 and :2515) before the expected context.DeadlineExceeded. The timeout outcome is the behavior; its length is not. |

Procedure:
1. Record each baseline with `go test <pkg> -run <exact ^name$> -count=1 -v` in Implementation Notes.
2. Signalled leader: build the runtime env with `testutil.RuntimeEnv(runtime.dir, "HUM_STOP_GRACE=500ms")` for this test only (pattern: integration/stop_shutdown_test.go:69, integration/down_test.go:58). Keep every existing assertion: PID 0, PGID equals leader, leader not alive, one list record in `descendants`, stop returns `"status":"stopped"`, state leaves `descendants`.
3. Event history: lower the append count (for example to 200) and scale the rewrite and mark upper bounds from the compaction algorithm in internal/daemon/event_history.go, not by trial. With maxEvents 20 the current bound is 51 rewrites per 1,000 appends. Derive the new bound and write the derivation as a one-line comment. Keep every invariant: rewrites and marks both nonzero, both bounded, live Read returns the newest 20 events with the correct first cursor (981 today; recompute for the new count), reload returns the same 20 with diskEvents at most 40, and the first append after reload does no full rewrite.
4. Persistence cleanup: add a `PersistenceCleanupTimeout time.Duration` field to app.Options; zero means the current 5s default. Replace the package constant uses with the resolved field. In the daemon test set it to about 100ms. Keep the assertions that the start error names runtime state and wraps context.DeadlineExceeded, the record is StateUnresolved, and a duplicate start fails. Do not change the confirmed-cleanup subtest.

Owner exception (2026-09-24): step 3 may reduce the iteration count and step 2 may shorten the stop grace. Neither counts as weakening as long as every assertion listed above remains.

Non-goals: other slow tests (for example internal/cli TestAttachedRunInterruptLifecycle and TestEnsureDaemonWaitsForSequentialRecovery, whose waits are semantic); parallelizing packages (separate tasks); changing production defaults.

Stop rules: each of the three changes is independent. If one cannot meet its AC without changing an assertion, land the other two, leave that test unchanged, and report why.

Unrelated failures: if `task ci` or a package run fails in a test this task did not touch, rerun that package once. If the rerun passes, record both runs in Implementation Notes and continue; if it fails again, stop and report it. Do not fix unrelated tests in this task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `go test ./integration -run "^TestSignalledLeaderWithSurvivingDescendant$" -count=1 -v` exits 0 and reports the test at 3.0s or less (10.24s baseline).
- [x] #2 AC2 — `go test ./internal/daemon -run "^TestEventHistoryAppendCost$" -count=1 -v` exits 0 and reports the test at 3.0s or less (8.71s baseline); the test appends at most 200 events.
- [x] #3 AC3 — `go test ./internal/daemon -run "^TestLaunchPersistenceFailureStopsChild$" -count=1 -v` exits 0 and reports the test at 1.5s or less (5.16s baseline).
- [x] #4 AC4 — `go test ./internal/app ./internal/daemon ./integration -count=1` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted or skipped; the only reductions are the owner-excepted iteration count and stop grace, and every assertion listed in the description remains
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Configure only the descendant integration test with 500ms stop grace while retaining its state assertions.
2. Reduce append-cost iterations to 200 and derive compaction bounds from the existing algorithm; retain live/reload/counter assertions.
3. Resolve an optional persistence cleanup timeout in app.Options (default 5s) and set 100ms only in the unconfirmed-cleanup daemon subtest.
4. Run focused acceptance, independent verification, task ci, commit, merge to main, and clean up the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Baseline on main 4502f23 (macOS): go test ./integration -run ^TestSignalledLeaderWithSurvivingDescendant$ -count=1 -v PASS 10.49s; go test ./internal/daemon -run ^TestEventHistoryAppendCost$ -count=1 -v PASS 3.77s; go test ./internal/daemon -run ^TestLaunchPersistenceFailureStopsChild$ -count=1 -v PASS 5.05s (unconfirmed 5.02s). Worktree hum-137-timeouts created by Worktrunk from main.

Focused worktree checks: AC#1 go test ./integration -run ^TestSignalledLeaderWithSurvivingDescendant$ -count=1 -v PASS 0.98s (from 10.49s). AC#2 go test ./internal/daemon -run ^TestEventHistoryAppendCost$ -count=1 -v PASS 0.79s (200 appends, bound 8 rewrites/4 marks; from 3.77s). AC#3 go test ./internal/daemon -run ^TestLaunchPersistenceFailureStopsChild$ -count=1 -v PASS 0.17s (unconfirmed 0.12s; from 5.05s). AC#4 go test ./internal/app ./internal/daemon ./integration -count=1 PASS (10.616s, 19.919s, 38.451s). Independent verifier in progress.

Independent verifier PASS for AC#1–AC#4: repeat observed 0.88s, 0.78s, 0.15s, and all three packages pass (11.007s, 19.833s, 38.479s); no defects. Reviewed default timeout 5s at both cleanup paths, 200-append compaction bounds, and all original assertions. Commit 968f44f4425be55ac5cc0b5cf4e0d419f5b8954b passed task check:staged and task ci (security, checks, all Go and Python tests, race, smoke). Fast-forward merged to main. Diff contains only the four declared code paths, no test deletion/skip, no protected gate changes; provider task metadata is the required out-of-contract state update. No remaining blocker; next step: HUM-138 or HUM-141, both unblocked.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Shortened nonsemantic waits in three tests without changing assertions or production defaults. AC#1–#4 and task ci passed on 968f44f; independent verifier PASS; merged to main.
<!-- SECTION:FINAL_SUMMARY:END -->
