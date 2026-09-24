---
id: HUM-141
title: Run independent internal/daemon tests in parallel
status: To Do
assignee: []
created_date: '2026-09-24 22:52'
updated_date: '2026-09-24 22:52'
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

Isolation today: tests build servers with testServer and a per-test runtime directory from shortRuntimeDir or t.TempDir. The only process-wide state is t.Setenv, at internal/daemon/daemon_test.go:90-103 (HUM_RUNTIME_DIR, XDG_RUNTIME_DIR, TMPDIR) and internal/daemon/runtime_windows_test.go:32-33 (LOCALAPPDATA, APPDATA). Go panics if t.Parallel is called in a test that uses t.Setenv, so those tests stay serial.

Procedure:
1. Record the baseline `go test ./internal/daemon -count=1` wall time.
2. Add `t.Parallel()` as the first statement of each top-level Test in internal/daemon that does not call t.Setenv, directly or in a subtest, and does not mutate package-level variables. Check package-level hooks with `rg -n "^\s+[a-zA-Z]+ = " internal/daemon/*_test.go` and leave any test that assigns one serial.
3. Serial tests get a `// Not parallel: <reason>` comment and a line in Implementation Notes.
4. Tests that depend on elapsed time, such as TestCloseCompletesWithStalledFollower (daemon_test.go:2135, which sleeps 1s to fill a socket), stay correct under load because their sleeps create a condition rather than race one. If one flakes, fix its synchronization rather than making it serial.

Non-goals: parallelizing internal/app or internal/cli; changing assertions or production code.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/daemon -count=1` exits 0 with package time at most 50% of the baseline recorded in Implementation Notes at the start of this task, after HUM-137 has merged (35.6s before HUM-137 on 2026-09-24).
- [ ] #2 AC2 — `go test ./internal/daemon -count=5 -shuffle=on` exits 0.
- [ ] #3 AC3 — `go test -race ./internal/daemon -count=2` exits 0.
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
