---
id: HUM-090
title: Stabilize recorded-group reclaim after KILL
status: To Do
assignee: []
created_date: '2026-09-11 01:40'
labels: []
dependencies: []
ordinal: 64800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: startup recovery reports a recorded group reclaimed once the KILLed group is observably gone. Reproduction: task stress; observed TestStartupReclaimsRecordedGroups/kill_after_grace warning outcome unresolved with 'process group remained alive after KILL'. Suspected cause: process-group disappearance and wait/reap publication are not synchronized at the recovery boundary. Non-goals: widening grace or accepting unresolved state. Modified files: internal/daemon/, internal/process/, internal/testutil/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 mise exec go -- go test -race -count=100 -run 'TestStartupReclaimsRecordedGroups/kill_after_grace' ./internal/daemon exits 0 with reclaimed outcome.
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
