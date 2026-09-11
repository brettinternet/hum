---
id: HUM-087
title: Replace remaining test readiness timing assumptions
status: To Do
assignee: []
created_date: '2026-09-11 01:40'
labels: []
dependencies: []
ordinal: 61800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: stress-sensitive tests wait for observable completed state rather than file existence or fixed cancellation windows. Scope: TestCLIDiscoveryCancellationReapsCommand, TestMCPDiscoveryCancellationReapsCommand, TestLogsStripTerminalControl, TestOneShotInputAnswersPrompt, and TestDaemonCrashReclaimsOrphans. Reproduction: task stress; observed failures included 'CLI discovery command did not start', raw attached run context deadline exceeded, marker .input existing with empty contents, orphan.started not appearing within 8s, and managed child not surviving a crash. Suspected causes: narrow wall-clock windows, file existence observed before contents are complete, and daemon crash initiated before managed output/readiness is confirmed. Non-goals: blanket timeout widening or assertion retries. Modified files: integration/, internal/cli/, internal/testutil/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 mise exec go -- go test -race -count=100 -run 'TestCLIDiscoveryCancellationReapsCommand|TestMCPDiscoveryCancellationReapsCommand|TestLogsStripTerminalControl' ./internal/cli and mise exec go -- go test -race -count=100 -run 'TestOneShotInputAnswersPrompt|TestDaemonCrashReclaimsOrphans' ./integration exit 0 using observable handshakes.
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
