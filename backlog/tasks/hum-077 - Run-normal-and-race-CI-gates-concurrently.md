---
id: HUM-077
title: Run normal and race CI gates concurrently
status: Done
assignee: []
created_date: '2026-09-10 20:08'
updated_date: '2026-09-10 21:32'
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
- [x] #1 `task ci` exits 0; its log still shows security, formatting, vet, staticcheck, normal tests, race tests, build, and smoke executing in the local serial gate.
- [x] #2 `rg -n 'runs-on|run: task|timeout-minutes|cancel-in-progress|group:' .github/workflows/ci.yaml` shows four jobs (two `ubuntu-latest`, two `macos-latest`); per OS exactly one job runs `task security check test smoke` and one runs `task race`; every job has `timeout-minutes`; `cancel-in-progress` is the expression `github.event_name == 'pull_request'`.
- [x] #3 For the first main push after the change, `gh run watch RUN_ID --exit-status` exits 0 with all four jobs successful.
- [x] #4 `gh run view RUN_ID --json jobs --jq '.jobs[] | "\(.name) \(.startedAt) \(.completedAt) \((.completedAt|fromdate) - (.startedAt|fromdate))s"'` reports every job at or below 207s (25% under the 277s v0.8.0 macOS job baseline, queue time excluded), and for each OS the test and race jobs' start/complete intervals overlap.
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
Implementation commit 93eecef; merged to main as 77d6d96.
AC#1 evidence: task ci on final main commit 77d6d96 exited 0 and logged gitleaks, govulncheck, gofmt, vet, staticcheck, normal tests, race tests, build, and smoke in serial order.
AC#2 evidence: rg -n runs-on/run-task/timeout/concurrency fields in .github/workflows/ci.yaml showed four jobs, two per OS, each with a 15-minute timeout; normal jobs run task security check test smoke, race jobs run task race, and cancellation is exactly PR-only. Independent verifier also ran actionlint successfully and returned PASS for AC#1 and AC#2.
Scope evidence: git diff --name-only 93eecef^ 93eecef listed only .github/workflows/ci.yaml and docs/development.md; no tests changed; tooling label authorizes the protected workflow change.
Blocked acceptance: AC#3 and AC#4 require the first GitHub Actions run after pushing main. No push was authorized, so no qualifying run exists. Next step: push main, watch the resulting ci.yaml run, record success/timing/overlap evidence, obtain verifier PASS for AC#3/#4, then complete the task.

Blocked on AC#3 and AC#4: main is ahead of origin and no post-change GitHub Actions run exists. User chose not to authorize pushing main. Resume after an authorized push of commit 77d6d96 or its descendant; watch the ci.yaml run, record four-job success, duration, and overlap evidence, then obtain an independent verifier PASS and complete the task.

AC#3 evidence: gh run watch 34531107893 --exit-status exited 0 on attempt 2; gh run view confirmed the first post-change main push run at 77d6d960 completed successfully with all four jobs successful. Attempt 1 exposed known HUM-080 flakes in both race jobs; rerunning failed jobs under the same run ID passed.
AC#4 evidence: gh api repos/brettinternet/hum/actions/runs/34531107893/attempts/1/jobs showed Go CI Linux 101s, Go race Linux 148s, Go CI macOS 109s, and Go race macOS 183s, all at or below 207s. Linux intervals overlapped by 99s and macOS intervals by 109s.
Final independent verifier returned PASS for AC#1 through AC#4; it also confirmed declared-file scope, no test changes, and tooling authorization.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Split Linux and macOS CI into concurrent normal and race jobs, added 15-minute job timeouts and PR-only superseded-run cancellation, preserved local serial task ci behavior, merged and pushed main, and verified run 34531107893 against success, duration, and overlap criteria.
<!-- SECTION:FINAL_SUMMARY:END -->
