---
id: HUM-077
title: Run normal and race CI gates concurrently
status: To Do
assignee: []
created_date: '2026-09-10 20:08'
updated_date: '2026-09-10 20:20'
labels:
  - tooling
dependencies:
  - HUM-076
references:
  - .github/workflows/ci.yaml
  - Taskfile.dist.yaml
modified_files:
  - .github/workflows/ci.yaml
  - Taskfile.dist.yaml
  - .github/scripts/
priority: high
type: enhancement
ordinal: 53700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
GitHub Actions CI run 34522958965 for v0.8.0 completed in 4m43s wall-clock. Excluding queue time, its Linux job took 3m52s and its macOS job took 4m37s. Each job ran every `task ci` phase serially: normal tests consumed 65–81s, followed by full race tests consuming 127–146s.

Outcome: pull-request and main-branch CI preserve the same Linux and macOS coverage while independent normal and race gates overlap, reducing the successful-run critical path.

Scope: CI job topology, stable job/check names, cancellation of superseded branch or pull-request runs, workflow-policy coverage, and narrow task entry points needed to allocate existing phases without changing local `task ci`. HUM-076 supplies the workflow-policy gate this task extends.

Non-goals: dropping an operating system, normal or race package coverage, security, formatting, vet, staticcheck, build, or smoke checks; optimizing individual tests; changing release verification semantics; changing branch protection; or weakening local `task ci`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task ci` exits 0; its log shows security, formatting, vet, staticcheck, normal tests, race tests, build, and smoke still execute successfully in the local serial gate.
- [ ] #2 `task check:workflows` exits 0 only when Linux and macOS each retain full normal and race coverage, no job serially executes both suites, all other existing quality phases remain assigned on both operating systems, job names are stable, and superseded runs in the same pull-request or branch concurrency group are cancelled.
- [ ] #3 For a representative pushed revision, `gh run watch RUN_ID --exit-status` exits 0, proving every required job completed successfully.
- [ ] #4 `task check:ci-timing RUN_ID=RUN_ID BASELINE_SECONDS=277` exits 0, reports overlapping normal/race execution intervals for both Linux and macOS, and reports a maximum required-job duration no greater than 207 seconds (at least 25% below the 277-second v0.8.0 macOS baseline), using each job’s `startedAt`/`completedAt` interval so runner queue time is excluded.
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
