---
id: HUM-078
title: Reduce integration test critical-path latency
status: In Progress
assignee: []
created_date: '2026-09-10 20:08'
updated_date: '2026-09-11 02:56'
labels:
  - tooling
dependencies:
  - HUM-080
  - HUM-087
references:
  - integration/lifecycle_test.go
  - integration/run_reconnect_test.go
  - integration/relaunch_test.go
modified_files:
  - integration/
  - internal/testutil/
priority: medium
type: enhancement
ordinal: 54700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
In GitHub Actions CI run 34522958965, `hum/integration` dominated the normal suite at 65.818s on Linux and 80.925s on macOS; its race run took 68.961s and 88.198s respectively. A local uncached profile found 41 top-level tests and no `t.Parallel()` calls. The largest contributors included `TestAttachedRunForegroundLifecycle` (13.46s), `TestSignalledLeaderWithSurvivingDescendant` (11.52s), `TestUpOrderedStack` (5.16s), `TestShutdown` (4.48s), and `TestRelaunchAfterCrash` (4.17s).

Levers present in the current code, in order of expected payoff:
1. `testutil.BuildHum` and `testutil.BuildFixture` run `go build` inside the calling test. The integration package calls them 49 times per run, so every test pays a go tool invocation even with a warm build cache. Build both binaries once per package (TestMain or `sync.Once`) into a package-scoped temp dir and hand tests the paths.
2. No integration test calls `t.Setenv`; runtime isolation already flows through `testutil.RuntimeDir` and `testutil.RuntimeEnv`, so `t.Parallel()` is viable for tests that do not depend on process-global state (signals to the test process's own group, PTY ownership, cwd). Keep tests that do depend on such state serial and say which in the notes.
3. Fixed waits: `time.Sleep` appears 20 times in integration tests; replace the ones that stand in for synchronization with condition polling on observable state.

Outcome: reduce integration-package elapsed time while preserving lifecycle coverage, process cleanup, and per-test isolation.

Scope: integration-test orchestration, synchronization, fixtures, and test-only helpers. Measure base and candidate revisions with identical commands on the same machine and record all raw wall times plus the median in Implementation Notes. BASE_SHA is the commit the change branches from (`git merge-base main HEAD` in a worktree, or `HEAD~1` for a single commit on main).

Depends on HUM-080: parallelizing tests changes timing and will expose latent races; stabilize first.

Non-goals: deleting or skipping tests; weakening assertions; shortening correctness deadlines without replacement synchronization; changing production behavior; sharing mutable runtime state; or relying on test order.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 At both `BASE_SHA` and the candidate revision on the same runner, `for run_number in 1 2 3; do /usr/bin/time -p mise exec go -- go test -count=1 ./integration; done` exits 0 for all six runs; the recorded candidate median `real` time is no more than 70% of the base median.
- [x] #2 `mise exec go -- go test -race -count=1 ./integration` exits 0 at the candidate revision.
- [x] #3 `mise exec go -- go test -shuffle=on -count=3 ./integration` exits 0, demonstrating that the optimization introduced no ordering dependency.
- [x] #4 `task ci` exits 0 with no integration test deleted, skipped, or assertion weakened.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add integration package setup that builds hum and hum-fixture once into a package-lifetime temporary directory, then route existing integration helpers through those immutable binaries.
2. Keep test execution serial: the first candidate run fell from about 100 seconds to about 50 seconds from build sharing alone, exceeding the latency target without adding signal, PTY, or cleanup concurrency risk.
3. Measure three candidate normal runs and retain the three base measurements from the unchanged main checkout, then run race, shuffled, and full CI gates without weakening tests.
4. Review the diff independently, fix valid findings, commit the implementation, merge it to main, update provider evidence, and release the worklease/worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Progress — package-scoped immutable binary sharing reduced the first candidate normal run to real 50.29s from the approximately 100s base runs. Kept all integration tests serial because the build optimization alone exceeds the 30% target and avoids introducing signal, PTY, or process-cleanup concurrency risk. Base attempts so far: real 98.98s PASS, 99.88s FAIL in pre-existing TestOneShotInputAnswersPrompt readiness race tracked by HUM-087, and 100.22s PASS.

Implementation merged to main in commit 88648a7.
AC#1 evidence — final unchanged-base loop passed three runs at real 93.83s, 95.85s, and 93.20s (median 93.83s). Candidate loop passed three runs at real 48.84s, 48.41s, and 49.05s (median 48.84s, 52.05% of base). An independent verifier reproduced candidate median 48.51s, but its unchanged-base loop failed twice in pre-existing TestOneShotInputAnswersPrompt with empty fixture input; that readiness race is owned by HUM-087. AC#1 and independent-verifier DOD remain unchecked until HUM-087 is complete and a fresh base loop passes.
AC#2 evidence — mise exec go -- go test -race -count=1 ./integration exited 0; independent verifier also passed AC#2.
AC#3 evidence — mise exec go -- go test -shuffle=on -count=3 ./integration exited 0; independent verifier also passed AC#3.
AC#4 evidence — task ci exited 0 locally and independently; review confirmed only integration/ changed and no test was deleted, skipped, or weakened.
Review evidence — independent verifier found no implementation defect: package TestMain builds immutable hum and fixture binaries once, retains per-test runtime and temp-directory isolation, and cleans package binaries after the run.
Blocked on HUM-087: its known TestOneShotInputAnswersPrompt file-content readiness race prevents AC#1 from reliably producing three successful unchanged-base runs and therefore prevents a verifier PASS for every criterion. Resume after HUM-087, rerun both three-run loops on the same runner, and obtain a fresh independent PASS.
<!-- SECTION:NOTES:END -->
