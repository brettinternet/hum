---
id: HUM-141
title: Run independent internal/daemon tests in parallel
status: Done
assignee: []
created_date: '2026-09-24 22:52'
updated_date: '2026-09-25 00:20'
labels:
  - daemon
dependencies:
  - HUM-137
modified_files:
  - internal/daemon/*_test.go
priority: medium
type: task
ordinal: 25000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: internal/daemon stops being the next serial long pole once integration runs in parallel (HUM-138). On 2026-09-24 at commit 5f0ed4c the package took 35.6s wall and its top-level tests summed to 35.4s. No daemon test calls t.Parallel(). HUM-137 removes about 11s of fixed waits first, and this task depends on it so the two do not edit the same files at once.

Isolation today: tests build servers with testServer and a per-test runtime directory from shortRuntimeDir or t.TempDir. Process-wide state includes t.Setenv (HUM_RUNTIME_DIR, XDG_RUNTIME_DIR, TMPDIR, and on Windows LOCALAPPDATA/APPDATA), runtimeUserOverride/runtimeSIDOverride (including simulateForeignRuntimeUser), and TestDaemonSignal sending SIGTERM/SIGINT to this process (which shuts down every live server). Go panics if t.Parallel is called in a test that uses t.Setenv; tests changing these overrides also stay serial.

Procedure:
1. Record the baseline `go test ./internal/daemon -count=1` wall time.
2. Add `t.Parallel()` as the first statement of each top-level Test in internal/daemon that does not call t.Setenv, directly or in a subtest, and does not mutate package-level variables. Check package-level hooks with `rg -n "^\s+[a-zA-Z]+ = " internal/daemon/*_test.go` and leave any test that assigns one serial.
3. Serial tests get a `// Not parallel: <reason>` comment and a line in Implementation Notes.
4. Tests that depend on elapsed time, such as TestCloseCompletesWithStalledFollower (daemon_test.go:2135, which sleeps 1s to fill a socket), stay correct under load because their sleeps create a condition rather than race one. If one flakes, fix its synchronization rather than making it serial.

Non-goals: parallelizing internal/app or internal/cli; changing assertions or production code.

Stop rules: give each test that flakes under parallel load at most two fix attempts (a missing readiness wait or a fixed sleep). If it still flakes, leave it serial with `// Not parallel: flakes under parallel load: <symptom>` and move on. Do not raise shared timeout constants. If more than five tests end up serial on a single platform, or AC1 is still missed after that, stop and report which tests set the critical path, their durations, and the core count of the machine, instead of iterating further.

Unrelated failures: if `task ci` or a package run fails in a test this task did not touch, rerun that package once. If the rerun passes, record both runs in Implementation Notes and continue; if it fails again, stop and report it. Do not fix unrelated tests in this task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `go test ./internal/daemon -count=1` exits 0 with package time at most 50% of the baseline recorded in Implementation Notes at the start of this task, after HUM-137 has merged (35.6s before HUM-137 on 2026-09-24).
- [x] #2 AC2 — `go test ./internal/daemon -count=5 -shuffle=on` exits 0.
- [x] #3 AC3 — `go test -race ./internal/daemon -count=2` exits 0.
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
1. Measure post-HUM-137 daemon baseline and inventory global state plus test isolation.
2. Parallelize eligible top-level tests, document serial exceptions, and fix any bounded synchronization flakes.
3. Run AC1/AC2/AC3, independent verification and task ci, then commit, integrate and clean up.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Baseline (post-HUM-137, main 18dfc83, macOS): go test ./internal/daemon -count=1 => ok hum/internal/daemon 19.451s; AC1 threshold 9.7255s (50%). Machine: 10 logical cores. Serial exceptions identified: TestPrivateSocket uses t.Setenv; TestForeignRuntimeDirectoryIsNeverTrusted modifies runtimeUserOverride; Windows TestWindowsDefaultRuntimeDirIsAbsoluteWithoutAppData uses t.Setenv and TestWindowsForeignRuntimeAndPipeRefused modifies runtimeSIDOverride. The latter must not overlap other tests using the process-wide override.

Stop rule reached: four macOS tests must remain serial, not three: TestPrivateSocket (t.Setenv; 0.03s), TestForeignRuntimeDirectoryIsNeverTrusted (runtimeUserOverride; 0.22s), TestEventHistoryRefusesForeignRuntimeDirectory (same override; 0.02s), TestDaemonConnectionsRequireTheCurrentUser (same override; 0.02s). The latter two were not identified by the original task inventory; parallelizing them makes other tests see a foreign runtime and fail. Windows also needs two serial tests for t.Setenv/runtimeSIDOverride. A diagnostic go test -json ./internal/daemon -count=1 after marking four serial failed in 5.582s (TestRemoveAndShutdown/refuses_active_processes and TestMultipleFollowers); an earlier parallel run with three serial failed in 5.349s (TestHelloVersion, TestMultipleFollowers, TestSignalCanonicalRoundTrip). Critical-path top-level durations in the diagnostic run: TestCloseCompletesWithStalledFollower 3.23s, TestEventHistoryAppendCost 2.89s, TestStartupReclaimsRecordedGroups 2.79s, TestWaitDaemonBridge 2.28s (10 logical cores). A serial-only go test -json -run filter for the four tests passed in 0.411s. Stop per explicit >3-serial rule: AC1/AC2/AC3 unverified; no fix attempts on flaky tests, no commit/merge. Next: owner decision whether to relax the serial-test cap for process-wide safety and permit up to two synchronization fixes per flaky test, or change the task scope/target. Changes remain uncommitted in session-owned .worktrees/hum-141-parallel-daemon-tests pending decision.

Owner approved raising the serial cap to four to cover the discovered process-wide override users; keep the original two-fix-attempt limit per flaky test.

Further root cause: TestDaemonSignal sends SIGTERM/SIGINT to os.Getpid() (daemon_test.go), and all concurrently running servers use signal.NotifyContext; this shuts down unrelated parallel servers, explaining random dial/no-socket, stopped-before-readiness, and supervisor-shut-down failures. It must remain serial, bringing macOS to five mandatory serial tests (above the owner-approved four). Stopped again; pending owner permission for five, not treating these global-signal failures as timing flakes.

Owner approved raising the serial cap to five for TestDaemonSignal; it must remain serial because it signals the shared process.

Post-commit gate run on 8f46b3d: task ci failed in untouched internal/cli/TestShutdown/--stop-processes_waits_for_graceful_process-tree_termination (timed out waiting for term marker at 3.04s); per task unrelated-failure rule reran go test ./internal/cli -count=1 once, passed in 27.114s. Retrying task ci once for final-commit evidence.

Commit 8f46b3d test(daemon): parallelize independent tests (91 added lines in internal/daemon/*_test.go only; no production code, assertions, deletions, skips, or protected gate changes). AC#1 — go test ./internal/daemon -count=1 => PASS 5.597s, 28.8% of post-HUM-137 baseline 19.451s (threshold 9.7255s). AC#2 — go test ./internal/daemon -count=5 -shuffle=on => PASS 26.899s. AC#3 — go test -race ./internal/daemon -count=2 => PASS 14.722s. Independent verifier PASS for all three ACs (repeated AC1 5.801s, AC2 31.881s, AC3 12.425s); reviewed all changed tests, serial exceptions, no weakening. task check:staged PASS. task ci on final commit 8f46b3d PASS on retry (security, check, test, race, smoke); first failed in unrelated internal/cli TestShutdown, package rerun passed and both runs recorded above. Windows serial behavior statically reviewed but not run natively.

Delivery: worktree commit 8f46b3d fast-forward merged into main at same SHA using wt merge --no-commit --no-rebase; Worktrunk removed the session-owned worktree and branch. No remaining blocker; next step: none for HUM-141.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Parallelized independent daemon tests; kept five process-wide-state tests serial on macOS and two on Windows. Package 5.597s vs 19.451s baseline; shuffle x5, race x2, independent verifier and task ci passed. Merged 8f46b3d to main and cleaned up the worktree.
<!-- SECTION:FINAL_SUMMARY:END -->
