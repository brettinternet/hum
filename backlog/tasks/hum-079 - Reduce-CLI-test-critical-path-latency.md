---
id: HUM-079
title: Reduce CLI test critical-path latency
status: To Do
assignee: []
created_date: '2026-09-10 20:09'
updated_date: '2026-09-10 20:35'
labels:
  - tooling
dependencies:
  - HUM-080
references:
  - internal/cli/serve_run_test.go
  - internal/cli/list_logs_test.go
  - internal/cli/daemon_start_test.go
modified_files:
  - internal/cli/*_test.go
  - internal/testutil/
priority: medium
type: enhancement
ordinal: 55700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
In GitHub Actions CI run 34522958965, `hum/internal/cli` dominated the race suite at 103.627s on Linux and 121.699s on macOS, against race phases of 127s and 146s: it is the CI critical path, and it stays so after HUM-077 splits the race job out. Its normal run took 52.756s and 62.908s. A local uncached profile found 208 top-level tests and no `t.Parallel()` calls. Large contributors included `TestAttachedRunInterruptLifecycle` (6.78s), `TestEnsureDaemonWaitsForSequentialRecovery` (6.08s), `TestLogsSince` (4.98s), `TestLogsFollow` (4.78s), and `TestUpHumanProgress` (3.62s).

Constraint in the current code: the CLI tests call `t.Setenv` 128 times (serve_run_test.go 34, manifest_test.go 30, discovery_test.go 11, status_test.go 10, restart_test.go 10, stop_shutdown_test.go 8, down_test.go 8, flag_alias_lifecycle_parity_test.go 7, wait_test.go 5, others fewer). Go panics when a test calls both `t.Setenv` and `t.Parallel`, so the first step is to pass HUM_* runtime configuration into the command under test explicitly (an env slice or config injection through the existing test helpers) instead of mutating the process environment. Only after that can `t.Parallel()` apply to tests without process-global dependencies. The package also has 25 `time.Sleep` calls to convert to condition polling where they stand in for synchronization.

Outcome: reduce CLI-package normal and race elapsed time while preserving subprocess, signal, output-following, recovery, cleanup, and per-test isolation coverage.

Scope: CLI test orchestration, synchronization, fixtures, and test-only helpers. Measure base and candidate revisions with identical commands on the same machine and record all raw wall times plus medians in Implementation Notes. BASE_SHA is the commit the change branches from (`git merge-base main HEAD` in a worktree, or `HEAD~1` for a single commit on main).

Depends on HUM-080: parallelizing tests changes timing and will expose latent races; stabilize first.

Non-goals: deleting or skipping tests; weakening assertions; shortening correctness deadlines without replacement synchronization; changing production behavior; sharing mutable runtime state; or relying on test order.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 At both `BASE_SHA` and the candidate revision on the same runner, `for run_number in 1 2 3; do /usr/bin/time -p mise exec go -- go test -count=1 ./internal/cli; done` exits 0 for all six runs; the recorded candidate median `real` time is no more than 70% of the base median.
- [ ] #2 At both `BASE_SHA` and the candidate revision on the same runner, `/usr/bin/time -p mise exec go -- go test -race -count=1 ./internal/cli` exits 0; the recorded candidate `real` time is no more than 75% of the base time.
- [ ] #3 `mise exec go -- go test -shuffle=on -count=3 ./internal/cli` exits 0, demonstrating that the optimization introduced no ordering dependency.
- [ ] #4 `task ci` exits 0 with no CLI test deleted, skipped, or assertion weakened.
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
