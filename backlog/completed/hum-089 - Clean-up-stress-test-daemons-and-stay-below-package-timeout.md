---
id: HUM-089
title: Clean up stress-test daemons and stay below package timeout
status: Done
assignee: []
created_date: '2026-09-11 01:40'
updated_date: '2026-09-11 05:36'
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
- [x] #1 Three consecutive task stress invocations exit 0 and the temporary-process filter reports zero survivors after each run.
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
Implementation started in isolated worktree; claimed with worklease after selection by task backlog:next.

Implementation commit 8cc1dff merged to main.
AC#1 PASS — three consecutive task stress invocations exited 0; the slowest package completed in 260.590s, below Go’s 10-minute package timeout, and the PPID-1 temporary-process filter reported zero go-build cli.test serve, Test*/hum serve, or hum-fixture survivors after each run.
task ci PASS on final merged commit 8cc1dff.
Independent verifier PASS — repeated all three serial task stress invocations in 4m10s, 4m13s, and 4m17s with zero survivors after every run; confirmed only integration/ and internal/cli/ test files changed and no tests were deleted, skipped, or weakened.
Review findings fixed — daemon startup cancellation is registered before runtime cleanup; transport-loss cleanup uses identity-verified stale-group reclamation instead of signaling a bare PID; forced cleanup budgets two default stop-grace windows plus reap slack.
Modified-file contract satisfied: integration/run_reconnect_test.go and internal/cli/{daemon_start_test.go,list_logs_test.go,serve_run_test.go} only. No protected gate files changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Stress tests now shut down auto-started CLI daemons before removing runtime state, cancel and await in-flight daemon startup during cleanup, and recover transport-loss fixtures through identity-verified stale-group reclamation. Three local and three independent stress runs passed with zero survivors; task ci passed; commit 8cc1dff is merged to main.
<!-- SECTION:FINAL_SUMMARY:END -->
