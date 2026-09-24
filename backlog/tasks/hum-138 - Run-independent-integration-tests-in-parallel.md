---
id: HUM-138
title: Run independent integration tests in parallel
status: To Do
assignee: []
created_date: '2026-09-24 22:51'
updated_date: '2026-09-24 22:58'
labels:
  - integration
dependencies:
  - HUM-137
modified_files:
  - integration/*_test.go
priority: high
type: task
ordinal: 22000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `go test ./integration` stops being the critical path of every test run. Today no integration test calls t.Parallel(). On 2026-09-24 at commit 5f0ed4c the package took 58.8s wall while its top-level tests summed to 56.8s, so it runs fully serially. `go test ./...` took 65s wall, and every other package finished within 36s, so integration alone sets the wall time for every local loop and CI job.

The tests are already isolated from one another. Each test creates its own runtime directory and cwd with lifecycleNewRuntime (integration/lifecycle_test.go:637), testutil.RuntimeDir, or t.TempDir. Each one passes env explicitly to subprocesses through testutil.RuntimeEnv, and no integration file calls t.Setenv, os.Setenv, or os.Chdir. TestMain (integration/main_test.go:17) builds the hum and hum-fixture binaries once, and tests only read those paths afterward. HTTP readiness uses port 0 (integration/manifest_test.go:26).

Procedure:
1. Record the baseline `go test ./integration -count=1` wall time in Implementation Notes.
2. Add `t.Parallel()` as the first statement of every top-level Test function in integration/ (not TestMain). It must run after any platform skip helper such as lifecycleRequireUnix, or be placed so a skip still works. Add it to subtests only when they already build independent runtimes. TestWait subtests each call lifecycleNewRuntime, for example.
3. If a test cannot run in parallel, leave it serial and add a `// Not parallel: <reason>` comment above it. List it in Implementation Notes. Acceptable reasons are real shared state, such as a fixed path or a process-wide scan. Speculative contention is not a reason.
4. Timeouts: integration waits are condition polls with 5s to 8s deadlines (lifecycleTimeout integration/lifecycle_test.go:24, runitWaitTimeout integration/run_reconnect_test.go:27). If a test flakes under parallel load, first fix a fixed sleep or a missing readiness wait in that test. Raise a deadline only when the test is already condition-based, and record why.
5. The Windows CI job (`task windows:test`) runs this package too. Keep any Windows-only test parallel-safe on the same terms. It cannot be run locally, so note that it is unverified locally.

Non-goals: parallelizing other packages (internal/daemon is a separate task); consolidating helpers (separate task); changing assertions.

Stop rules: give each test that flakes under parallel load at most two fix attempts (a missing readiness wait or a fixed sleep). If it still flakes, leave it serial with `// Not parallel: flakes under parallel load: <symptom>` and move on. Do not raise shared timeout constants. If more than three tests end up serial, or AC1 is still missed after that, stop and report which tests set the critical path, their durations, and the core count of the machine, instead of iterating further.

Unrelated failures: if `task ci` or a package run fails in a test this task did not touch, rerun that package once. If the rerun passes, record both runs in Implementation Notes and continue; if it fails again, stop and report it. Do not fix unrelated tests in this task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./integration -count=1` exits 0 and its package time is at most 25s, or at most 45% of the baseline recorded in Implementation Notes (58.8s on 2026-09-24).
- [ ] #2 AC2 — `go test ./integration -count=5` exits 0.
- [ ] #3 AC3 — `go test -race ./integration -count=2` exits 0.
- [ ] #4 AC4 — `task smoke` exits 0.
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
