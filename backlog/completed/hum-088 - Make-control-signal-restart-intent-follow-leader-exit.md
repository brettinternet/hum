---
id: HUM-088
title: Make control-signal restart intent follow leader exit
status: Done
assignee: []
created_date: '2026-09-11 01:40'
updated_date: '2026-09-11 03:54'
labels: []
dependencies: []
ordinal: 62800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a control signal that terminates a process-group leader suppresses that incarnation's on-failure restart even when descendant/output cleanup exceeds StopGrace. Reproduction: mise exec go -- go test -race -count=100 -run TestControlSignalSuppressesOnFailureRestart ./internal/app; observed terminal exit 17 replaced by a running successor under load. Suspected cause: expireControlIntent clears intent on a grace timer before Child.Done closes, although the leader has already exited. Non-goals: increasing StopGrace or disabling restart policy. Modified files: internal/app/, internal/process/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The reproduction exits 0 and control intent expiry uses an observable leader-lifecycle handshake.
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
Implementation merged to main in commit 6a01e6d.
AC#1 evidence — mise exec go -- go test -race -count=100 -run TestControlSignalSuppressesOnFailureRestart ./internal/app exited 0 in 113.029s. process.Child.LeaderDone now publishes the leader wait boundary before descendant/output cleanup, and control-intent expiry selects that observable handshake with a legacy Done fallback.
DoD#1 evidence — task ci exited 0 on final main commit 6a01e6d, including normal tests, race tests, vet, staticcheck, security scans, build, and smoke test.
Review evidence — independent verifier returned PASS for AC#1, task ci, modified-path compliance, and test integrity; focused leader-exit, survivor, and process race tests passed.
Modified paths — internal/app/app.go, internal/app/app_test.go, internal/process/process.go only. No test was deleted, skipped, or weakened.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Control-signal restart intent now follows the process-group leader exit rather than delayed descendant/output cleanup, preventing an on-failure successor after a signal-caused leader exit.
<!-- SECTION:FINAL_SUMMARY:END -->
