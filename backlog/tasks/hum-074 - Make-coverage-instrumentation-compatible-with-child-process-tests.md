---
id: HUM-074
title: Make coverage instrumentation compatible with child-process tests
status: Done
assignee: []
created_date: '2026-09-10 01:55'
updated_date: '2026-09-10 17:27'
labels:
  - tooling
dependencies:
  - HUM-069
modified_files:
  - internal/process/process_test.go
  - internal/testutil/harness.go
  - Taskfile.dist.yaml
  - .taskfiles/cli.yaml
  - docs/development.md
priority: medium
type: chore
ordinal: 50700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Repository-wide Go coverage collection passes without helper-process coverage warnings contaminating supervised stderr, and the command is available as a documented task target. Evidence: `go test ./... -coverprofile=...` currently fails seven `internal/process` assertions because instrumented helper children emit `warning: GOCOVERDIR not set, no coverage data emitted`; normal and race suites do not expose it. Scope: give instrumented helper children an isolated valid coverage directory or explicitly disable child instrumentation in test setup, add a deterministic coverage task, and preserve exact output assertions. Non-goals: do not filter the warning from production capture, weaken stream assertions, or impose an arbitrary coverage percentage gate.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `mise exec go -- go test ./... -coverprofile=/tmp/hum-coverage.out` exits 0 with no `GOCOVERDIR` warning.
- [x] #2 `task coverage` exits 0, prints a function coverage report, and `git status --short` shows no generated coverage artifact in the repository.
- [x] #3 `mise exec go -- go test -race ./internal/process -count=1` exits 0 with all exact output assertions intact.
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
AC#1 PASS — `mise exec go -- go test ./... -coverprofile=/tmp/hum-coverage.out` exited 0 with no `GOCOVERDIR` warning.
AC#2 PASS — `task coverage` exited 0, printed the per-function report (total 75.3%), and `git status --short` showed no generated repository coverage artifact.
AC#3 PASS — `mise exec go -- go test -race ./internal/process -count=1` exited 0 with exact output assertions intact.
Gate PASS — `task ci` exited 0 on the final working tree.
Independent verifier PASS — reran AC#1–AC#3, confirmed no warning or repository artifact, and found no deleted, skipped, or weakened tests. The implementation diff is limited to declared paths; the provider-owned backlog task file is the only additional changed path.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: reproduced with `mise exec go -- go test ./... -count=1 -coverprofile=/tmp/hum-coverage.out` (exit 1): seven internal/process failures (TestStartCapturesLiteralArguments, TestStartedCallbackPrecedesOutputCapture, TestStartProvidesEOFStdin, TestStartResolvesExecutableFromSuppliedPath, TestStartResolvesRelativeAndEmptyPathComponentsFromSpecDirectory, TestCaptureSeparatesStreamsAndFlushesTailsBeforeExit, TestCaptureDrainsFastExitOutput) each with `warning: GOCOVERDIR not set`. Labelled tooling (Taskfile.dist.yaml); depends on HUM-069 because both edit internal/testutil/harness.go and Taskfile.dist.yaml.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Configured instrumented internal/process helper binaries and descendants with an isolated temporary GOCOVERDIR that is removed after the package suite. Added and documented `task coverage`, which writes its profile outside the repository and prints function coverage. All acceptance commands, `task ci`, and independent verification passed.
<!-- SECTION:FINAL_SUMMARY:END -->
