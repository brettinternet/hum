---
id: HUM-077
title: Run normal and race CI gates concurrently
status: To Do
assignee: []
created_date: '2026-09-10 20:08'
updated_date: '2026-09-10 20:35'
labels:
  - tooling
dependencies: []
references:
  - .github/workflows/ci.yaml
modified_files:
  - .github/workflows/ci.yaml
  - docs/development.md
priority: high
type: enhancement
ordinal: 53700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
GitHub Actions CI run 34522958965 for v0.8.0 completed in 4m43s wall-clock. Excluding queue time, its Linux job took 3m52s and its macOS job took 4m37s. Each job ran every `task ci` phase serially. Linux phase durations from the run log: security 4s, check (gofmt, vet, staticcheck) 20s, normal tests 68s, race tests 127s, build plus smoke 2s. The race phase is 55% of the job, and the normal and race gates share no state.

Outcome: pull-request and main-branch CI keep identical Linux and macOS coverage while the normal and race gates run as separate jobs per OS, so the critical path becomes the race job (about 140s Linux, 160s macOS) instead of the serial sum.

Design (ci.yaml only; no Taskfile change):
- Jobs `test-linux` and `test-macos` run `task security check test smoke`; jobs `race-linux` and `race-macos` run `task race`. Taskfile v3 accepts several targets in one invocation, so no new task entry points are needed and local `task ci` is untouched.
- Every job gets `timeout-minutes: 15`. The suite supervises real processes; a hung test today runs until the 6-hour default.
- `concurrency: { group: ci-${{ github.ref }}, cancel-in-progress: ${{ github.event_name == 'pull_request' }} }` cancels superseded pull-request pushes but lets main-branch runs finish. HUM-076 selects the newest CI run for the tagged commit and fails closed on a cancelled run, so main runs must never be cancelled.
- Keep the workflow filename ci.yaml and stable job names; HUM-076 filters runs by workflow file.
- Keep security and check on both operating systems: 13 files carry `//go:build` constraints, so vet and staticcheck results are OS-dependent.

Non-goals: dropping an OS or any phase, optimizing individual tests (HUM-078, HUM-079), caching (HUM-081), changing release verification (HUM-076), adding workflow-lint tooling, or weakening local `task ci`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task ci` exits 0; its log still shows security, formatting, vet, staticcheck, normal tests, race tests, build, and smoke executing in the local serial gate.
- [ ] #2 `rg -n 'runs-on|run: task|timeout-minutes|cancel-in-progress|group:' .github/workflows/ci.yaml` shows four jobs (two `ubuntu-latest`, two `macos-latest`); per OS exactly one job runs `task security check test smoke` and one runs `task race`; every job has `timeout-minutes`; `cancel-in-progress` is the expression `github.event_name == 'pull_request'`.
- [ ] #3 For the first main push after the change, `gh run watch RUN_ID --exit-status` exits 0 with all four jobs successful.
- [ ] #4 `gh run view RUN_ID --json jobs --jq '.jobs[] | "\(.name) \(.startedAt) \(.completedAt) \((.completedAt|fromdate) - (.startedAt|fromdate))s"'` reports every job at or below 207s (25% under the 277s v0.8.0 macOS job baseline, queue time excluded), and for each OS the test and race jobs' start/complete intervals overlap.
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
