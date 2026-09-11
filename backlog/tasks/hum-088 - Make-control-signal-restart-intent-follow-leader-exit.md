---
id: HUM-088
title: Make control-signal restart intent follow leader exit
status: To Do
assignee: []
created_date: '2026-09-11 01:40'
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
- [ ] #1 The reproduction exits 0 and control intent expiry uses an observable leader-lifecycle handshake.
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
