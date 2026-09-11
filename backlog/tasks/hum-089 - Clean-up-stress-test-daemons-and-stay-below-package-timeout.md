---
id: HUM-089
title: Clean up stress-test daemons and stay below package timeout
status: To Do
assignee: []
created_date: '2026-09-11 01:40'
labels: []
dependencies: []
ordinal: 63800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: repeated stress invocations leave no orphaned test daemons or fixtures and do not hit Go's 10-minute package timeout. Reproduction: task stress three times, then ps -axo ppid,command filtered for temporary go-build cli.test serve and Test*/hum serve or hum-fixture processes; observed dozens of PPID 1 daemons per run and package timeout panics while TestSignalledLeaderWithSurvivingDescendant, TestLogsFollowMultipleProcesses, TestRestartWaitsForReadiness, or TestAttachedRunInterruptLifecycle happened to be active. Suspected cause: runtime-directory cleanup removes state without shutting down auto-started daemons; transport-loss tests intentionally kill daemon ownership without terminating managed fixtures. Non-goals: increasing go test timeout before cleanup is fixed. Modified files: integration/, internal/cli/, internal/testutil/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Three consecutive task stress invocations exit 0 and the temporary-process filter reports zero survivors after each run.
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
