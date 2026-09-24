---
id: HUM-137
title: Remove fixed timeouts that dominate the three slowest tests
status: To Do
assignee: []
created_date: '2026-09-24 22:51'
updated_date: '2026-09-24 22:59'
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
- [ ] #1 AC1 — `go test ./integration -run "^TestSignalledLeaderWithSurvivingDescendant$" -count=1 -v` exits 0 and reports the test at 3.0s or less (10.24s baseline).
- [ ] #2 AC2 — `go test ./internal/daemon -run "^TestEventHistoryAppendCost$" -count=1 -v` exits 0 and reports the test at 3.0s or less (8.71s baseline); the test appends at most 200 events.
- [ ] #3 AC3 — `go test ./internal/daemon -run "^TestLaunchPersistenceFailureStopsChild$" -count=1 -v` exits 0 and reports the test at 1.5s or less (5.16s baseline).
- [ ] #4 AC4 — `go test ./internal/app ./internal/daemon ./integration -count=1` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted or skipped; the only reductions are the owner-excepted iteration count and stop grace, and every assertion listed in the description remains
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->
