---
id: HUM-134
title: >-
  Stop TestAttachStreamsBurstWithoutAborting timing out under parallel package
  load
status: To Do
assignee: []
created_date: '2026-09-24 06:53'
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
- [ ] #1 go test ./internal/cli -run '^TestAttachStreamsBurstWithoutAborting$' -count=20 exits 0
- [ ] #2 task ci exits 0 on three consecutive runs with default package parallelism, with no TestAttachStreamsBurstWithoutAborting failure
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
