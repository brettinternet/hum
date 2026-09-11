---
id: HUM-091
title: Qualify the scheduled stress gate end to end
status: To Do
assignee: []
created_date: '2026-09-11 01:51'
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
- [ ] #1 task stress exits 0 on three consecutive invocations at the candidate revision and all three wall times are recorded in Implementation Notes.
- [ ] #2 With task stress running concurrently, mise exec go -- go test -race -count=20 -run 'TestLogsSince|TestManifestWorkflow|TestNDJSONFollow|TestControlSignalDaemonRoundTripSuppressesRestart|TestControlSignalSurvivorResumesOnFailureRestart|TestInputCommand|TestShutdown|TestFlagAliasParityReadCommands' ./integration ./internal/cli ./internal/daemon ./internal/app ./internal/mcp exits 0.
- [ ] #3 gh workflow run stress.yaml followed by gh run watch for the dispatched run exits 0 with successful Linux and macOS jobs.
- [ ] #4 gh pr comment 1 --body '@dependabot rebase' is followed through completion and PR #1 CI succeeds; the run URL and result are recorded in Implementation Notes.
- [ ] #5 rg -n 'stress' Taskfile.dist.yaml .github/workflows/ci.yaml matches only Taskfile.dist.yaml and task ci exits 0.
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
