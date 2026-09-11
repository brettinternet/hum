---
id: HUM-091
title: Qualify the scheduled stress gate end to end
status: To Do
assignee: []
created_date: '2026-09-11 01:51'
updated_date: '2026-09-11 06:28'
labels:
  - tooling
dependencies:
  - HUM-080
  - HUM-086
  - HUM-087
  - HUM-088
  - HUM-089
  - HUM-090
references:
  - Taskfile.dist.yaml
  - .github/workflows/stress.yaml
  - .github/workflows/ci.yaml
priority: high
type: task
ordinal: 65800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: after the known stress defects are fixed, the candidate revision passes local loaded-machine qualification, the remote Linux/macOS stress workflow, and the Dependabot regression check.

Scope:
- Run task stress three consecutive times and record wall times.
- Run the original flaky-test selection at race count 20 while task stress runs concurrently.
- Dispatch and watch the stress workflow on remote main.
- Rebase Dependabot PR #1 and report its CI result.
- Run the final task ci gate.

Dependencies: HUM-080 and HUM-086 through HUM-090. A failure is returned to the prerequisite that owns its test or synchronization seam rather than fixed in this qualification item.

Non-goals: unrelated production changes, timeout widening, assertion retries, deleting or weakening tests, or adding stress to the push CI gate.

Modified files: none expected; provider evidence only.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 task stress exits 0 on three consecutive invocations at the candidate revision and all three wall times are recorded in Implementation Notes.
- [x] #2 With task stress running concurrently, mise exec go -- go test -race -count=20 -run 'TestLogsSince|TestManifestWorkflow|TestNDJSONFollow|TestControlSignalDaemonRoundTripSuppressesRestart|TestControlSignalSurvivorResumesOnFailureRestart|TestInputCommand|TestShutdown|TestFlagAliasParityReadCommands' ./integration ./internal/cli ./internal/daemon ./internal/app ./internal/mcp exits 0.
- [ ] #3 gh workflow run stress.yaml followed by gh run watch for the dispatched run exits 0 with successful Linux and macOS jobs.
- [x] #4 gh pr comment 1 --body '@dependabot rebase' is followed through completion and PR #1 CI succeeds; the run URL and result are recorded in Implementation Notes.
- [x] #5 rg -n 'stress' Taskfile.dist.yaml .github/workflows/ci.yaml matches only Taskfile.dist.yaml and task ci exits 0.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1 — PASS: `task stress` exited 0 three consecutive times on candidate efcacb1; wall times were 264.94s, 268.30s, and 259.15s.
AC#2 — PASS: while a fourth `task stress` invocation ran concurrently and exited 0 in 251.11s, `mise exec go -- go test -race -count=20 -run 'TestLogsSince|TestManifestWorkflow|TestNDJSONFollow|TestControlSignalDaemonRoundTripSuppressesRestart|TestControlSignalSurvivorResumesOnFailureRestart|TestInputCommand|TestShutdown|TestFlagAliasParityReadCommands' ./integration ./internal/cli ./internal/daemon ./internal/app ./internal/mcp` exited 0 in 163.46s.
AC#3 — BLOCKED/FAIL: after authorized push of candidate efcacb1 to remote main, `gh workflow run stress.yaml` dispatched https://github.com/brettinternet/hum/actions/runs/34569438207 and `gh run watch 34569438207 --exit-status` exited 1. macOS passed; Linux failed `TestExecutableReadinessProbeSchedulingAndEnvironment` because the first probe had not begun within the wall-clock window (`elapsed=251.306117ms events=[]`, shuffle seed 1789107597555930002). The readiness timing assumption was returned to reopened prerequisite HUM-087 as AC#2; HUM-091 remains incomplete until that prerequisite is fixed and the remote workflow passes.
AC#4 — PASS: `gh pr comment 1 --body '@dependabot rebase'` produced comment https://github.com/brettinternet/hum/pull/1#issuecomment-5630156781; rebased head 2265796f completed CI run https://github.com/brettinternet/hum/actions/runs/34568004514 successfully on all four Linux/macOS CI and race jobs.
AC#5 — PASS: `rg -n 'stress' Taskfile.dist.yaml .github/workflows/ci.yaml` matched only `Taskfile.dist.yaml:73`; `task ci` exited 0 in 118.05s on candidate efcacb1.
Delivery state — no production or test files changed. HUM-087 was reopened with the exact failure and next action; rerun AC#3 and final `task ci` after HUM-087 is complete.
<!-- SECTION:NOTES:END -->
