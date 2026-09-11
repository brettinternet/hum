---
id: HUM-087
title: Replace remaining test readiness timing assumptions
status: Done
assignee:
  - '@pi'
created_date: '2026-09-11 01:40'
updated_date: '2026-09-11 13:32'
labels: []
dependencies: []
references:
  - internal/app/app_test.go
  - 'https://github.com/brettinternet/hum/actions/runs/34569438207'
ordinal: 61800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: stress-sensitive tests wait for observable completed state rather than file existence or fixed cancellation windows. Scope: TestCLIDiscoveryCancellationReapsCommand, TestMCPDiscoveryCancellationReapsCommand, TestLogsStripTerminalControl, TestOneShotInputAnswersPrompt, and TestDaemonCrashReclaimsOrphans. Reproduction: task stress; observed failures included 'CLI discovery command did not start', raw attached run context deadline exceeded, marker .input existing with empty contents, orphan.started not appearing within 8s, and managed child not surviving a crash. Suspected causes: narrow wall-clock windows, file existence observed before contents are complete, and daemon crash initiated before managed output/readiness is confirmed. Non-goals: blanket timeout widening or assertion retries. Modified files: integration/, internal/cli/, internal/testutil/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 mise exec go -- go test -race -count=100 -run 'TestCLIDiscoveryCancellationReapsCommand|TestMCPDiscoveryCancellationReapsCommand|TestLogsStripTerminalControl' ./internal/cli and mise exec go -- go test -race -count=100 -run 'TestOneShotInputAnswersPrompt|TestDaemonCrashReclaimsOrphans' ./integration exit 0 using observable handshakes.
- [x] #2 mise exec go -- go test -race -count=100 -run TestExecutableReadinessProbeSchedulingAndEnvironment ./internal/app exits 0 using observable probe-start synchronization rather than a wall-clock immediate-start assertion.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Replace PID and marker existence checks with content-bearing readiness handshakes.
2. Drive raw follow and attached-output tests with explicit output/gate synchronization instead of cancellation sleeps.
3. Confirm daemon-managed output is published before simulating a daemon crash.
4. Run the focused race reproductions, task ci, and an independent verifier pass.

Replace TestExecutableReadinessProbeSchedulingAndEnvironment's elapsed-time immediate-start assertion with an observable probe-start handshake, then rerun its focused race reproduction and task ci.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented and merged to main in commit dcf8ef7.
AC#1 evidence — mise exec go -- go test -race -count=100 for TestCLIDiscoveryCancellationReapsCommand, TestMCPDiscoveryCancellationReapsCommand, and TestLogsStripTerminalControl in ./internal/cli exited 0 in 124.448s; the required count=100 command for TestOneShotInputAnswersPrompt and TestDaemonCrashReclaimsOrphans in ./integration exited 0 in 138.528s. PID and marker checks now require completed file content, follow output is released through a gate after the follower observes replay, attached output completes naturally, and daemon crash waits for retained managed output.
DoD#1 evidence — task ci exited 0 on the final implementation commit.
Review evidence — independent verifier returned PASS for AC#1, task ci, modified-path compliance, and no deleted, skipped, or weakened tests. The diff touches only integration/ and internal/cli/, within the declared modified-file contract.

Qualification regression (2026-09-11): remote main stress run https://github.com/brettinternet/hum/actions/runs/34569438207 failed on Linux at TestExecutableReadinessProbeSchedulingAndEnvironment with shuffle seed 1789107597555930002: first probe did not begin immediately (elapsed=251.306117ms, events=[]); macOS passed. HUM-091 returned this stress-sensitive readiness timing assumption here. Next action: replace the wall-clock immediate-start assertion in internal/app/app_test.go with an observable probe-start handshake, then run the new AC#2 command and task ci.

AC#2 evidence — mise exec go -- go test -race -count=100 -run TestExecutableReadinessProbeSchedulingAndEnvironment ./internal/app exited 0 in 60.967s on commit ec2ce69; independent verifier reran it and exited 0 in 60.746s. The test now waits for the probe start event to be published instead of asserting that startup beat a wall-clock interval.

DoD evidence — task ci exited 0 on final implementation commit ec2ce69. Independent verifier passed AC#1 and AC#2, confirmed no test was deleted, skipped, or weakened, and found no protected gate changes. Modified-path deviation: internal/app/app_test.go is outside the original list but is required by AC#2, named in References, and explicitly selected by the qualification-regression next action.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Replaced the five scoped timing assumptions with content, output, gate, and process-completion handshakes. Both required race count=100 reproductions, task ci, and independent verification passed; implementation merged to main as dcf8ef7.

Completed AC#2 in ec2ce69 and fast-forwarded it to main. Executable readiness scheduling now synchronizes on the probe-published start event instead of an elapsed-time threshold; the race count=100 reproduction, task ci, and independent verification passed.
<!-- SECTION:FINAL_SUMMARY:END -->
