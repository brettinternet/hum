---
id: HUM-074
title: Make coverage instrumentation compatible with child-process tests
status: To Do
assignee: []
created_date: '2026-09-10 01:55'
updated_date: '2026-09-10 06:01'
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
- [ ] #1 `mise exec go -- go test ./... -coverprofile=/tmp/hum-coverage.out` exits 0 with no `GOCOVERDIR` warning.
- [ ] #2 `task coverage` exits 0, prints a function coverage report, and `git status --short` shows no generated coverage artifact in the repository.
- [ ] #3 `mise exec go -- go test -race ./internal/process -count=1` exits 0 with all exact output assertions intact.
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

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: reproduced with `mise exec go -- go test ./... -count=1 -coverprofile=/tmp/hum-coverage.out` (exit 1): seven internal/process failures (TestStartCapturesLiteralArguments, TestStartedCallbackPrecedesOutputCapture, TestStartProvidesEOFStdin, TestStartResolvesExecutableFromSuppliedPath, TestStartResolvesRelativeAndEmptyPathComponentsFromSpecDirectory, TestCaptureSeparatesStreamsAndFlushesTailsBeforeExit, TestCaptureDrainsFastExitOutput) each with `warning: GOCOVERDIR not set`. Labelled tooling (Taskfile.dist.yaml); depends on HUM-069 because both edit internal/testutil/harness.go and Taskfile.dist.yaml.
---
<!-- COMMENTS:END -->
