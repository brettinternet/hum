---
id: HUM-142
title: Replace per-file polling helpers with shared testutil helpers
status: Done
assignee: []
created_date: '2026-09-24 22:53'
updated_date: '2026-09-25 00:53'
labels:
  - integration
  - cli
dependencies:
  - HUM-138
  - HUM-139
modified_files:
  - internal/testutil/harness.go
  - integration/*_test.go
  - internal/cli/*_test.go
priority: medium
type: task
ordinal: 26000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: generic test waits live once in internal/testutil, and integration and CLI test files stop carrying private copies. Test files were written one task at a time, each with its own name prefix to avoid collisions (logsit*, runit*, stopit*, lifecycle*, durable*, hum006ListLogs*, stopShutdown*, cliServeRun*). The same polling loops have been copied many times, and each new test file adds another copy.

internal/testutil/harness.go already exports WaitForFile (:335), WaitForText (:347), WaitForProcessGone (:361), and WaitForProcessGroupGone (:373), and has an unexported waitForCondition (:384).

Copies at commit 5f0ed4c:
| Helper kind | Copies |
|---|---|
| process stdout or stderr contains text before a deadline | integration: durableWaitText (durable_session_test.go:24), lifecycleWaitForStderr (lifecycle_test.go:644), runitWaitForOutput (run_reconnect_test.go:622), logsitWaitFollowerText (logs_test.go:689), terminalWaitForProcessText (terminal_control_test.go:117), manifestIntegrationWaitForText (manifest_test.go:1172); internal/cli: cliServeRunWaitForTextIn (serve_run_test.go:2322) |
| poll a condition until a deadline | integration: lifecycleWaitCondition (lifecycle_test.go:760), stopitWaitForCondition (stop_shutdown_test.go:485); internal/cli: cliServeRunWaitForCondition (serve_run_test.go:2328) |
| path exists | internal/cli: stopShutdownWaitForFile (stop_shutdown_test.go:454 and test_helpers_windows_test.go:211), waitCLIPath (daemon_start_test.go:190), cliServeRunWaitForFile (serve_run_test.go:2308); all duplicate testutil.WaitForFile |
| path gone | integration: lifecycleWaitPathGone (lifecycle_test.go:749), stopitWaitForPathGone (stop_shutdown_test.go:473); internal/cli: stopShutdownWaitForPathGone (stop_shutdown_test.go:468 and test_helpers_windows_test.go:225), waitCLIPathAbsent (daemon_start_test.go:178), waitWindowsPathGone (lifecycle_windows_test.go:358) |
| process group gone | internal/cli stopShutdownWaitForProcessGroupGone (stop_shutdown_test.go:~491), which duplicates testutil.WaitForProcessGroupGone |
| process-list JSON DTO | integration runitProcessRecord (run_reconnect_test.go:51) and logsitProcess (logs_test.go:59), while lifecycle_test.go:143 already decodes into protocol.Process |

Procedure:
1. In internal/testutil/harness.go, export the condition poll (for example `WaitUntil(timeout time.Duration, condition func() bool) bool`). Add `WaitForOutput(t testing.TB, p *Process, stderr bool, text string, timeout time.Duration)` and `WaitForPathGone(t testing.TB, path string, timeout time.Duration)`. Match the existing helper style: t.Helper, a fatal message that includes the last observed value, and a 10ms poll.
2. Replace every copy in the table with the testutil helper. Keep each call site timeout and message intent. A copy that returns an error instead of failing the test (the cliServeRun* helpers return error) may keep a thin local wrapper only if a caller needs the error. Otherwise switch to the fatal form.
3. Replace runitProcessRecord and logsitProcess with protocol.Process (plus a local wrapper struct for the `processes` array if needed) when protocol.Process has every field the assertions read. If a field is missing, keep that DTO and say why.
4. Leave domain-specific waits alone, such as waiting for a status state, log cursor, eviction, or launch count (relaunchIntegrationWait*, manifestWaitForLogText, manifestWaitForEviction, hum006ListLogsWaitFor*). They encode a test predicate, not a generic poll.
5. Do not rename test functions or change assertions.

Non-goals: renaming existing prefixes on non-generic helpers; splitting large test files; changing timeouts.

Stop rules: this is a mechanical refactor. If a copy differs in behavior from the shared helper (a different poll interval that the test relies on, or a non-fatal return used from a goroutine), keep it and list it in Implementation Notes. Do not change the shared helper to fit one caller.

Unrelated failures: if `task ci` or a package run fails in a test this task did not touch, rerun that package once. If the rerun passes, record both runs in Implementation Notes and continue; if it fails again, stop and report it. Do not fix unrelated tests in this task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `rg -n "^func (durableWaitText|lifecycleWaitForStderr|runitWaitForOutput|logsitWaitFollowerText|terminalWaitForProcessText|manifestIntegrationWaitForText|lifecycleWaitCondition|stopitWaitForCondition|lifecycleWaitPathGone|stopitWaitForPathGone|stopShutdownWaitForFile|stopShutdownWaitForPathGone|stopShutdownWaitForProcessGroupGone|waitCLIPath|waitCLIPathAbsent|cliServeRunWaitForFile|cliServeRunWaitForCondition)\(" integration internal/cli` exits 1 (no matches), or each remaining match is justified in Implementation Notes.
- [x] #2 AC2 — `go vet ./integration ./internal/cli ./internal/testutil && GOOS=windows go vet ./integration ./internal/cli ./internal/testutil && go test ./integration ./internal/cli ./internal/testutil -count=1` exits 0.
- [x] #3 AC3 — `git diff --numstat main -- integration internal/cli internal/testutil` shows a combined net reduction of at least 100 lines.
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
1. Consolidate generic output, condition, and path waits in internal/testutil and migrate integration/CLI call sites without changing assertions or deadlines.
2. Replace duplicate process-list DTOs with protocol.Process where fields match; preserve any behaviorally distinct waits and explain exceptions.
3. Run focused vet/test and net-line checks, independent verification, full CI, commit, merge to main, and record final evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation: generic waits consolidated in internal/testutil; integration and CLI call sites retain their timeout arguments and assertions. Exceptions: cliServeRunWaitForFile/WaitForCondition/WaitForTextIn remain thin error-returning wrappers because CLI tests propagate errors from goroutines; their polling is now delegated to testutil.WaitUntil. Kept logsitProcess and runitProcessRecord DTOs: hum list --json emits readiness as a string, whereas protocol.Process.UnmarshalJSON expects a Readiness object; direct decoding fails the integration tests, and filtering/re-encoding every record would add substantial complexity for a mechanical test refactor. Original DTOs ignore presentation-only readiness. Former stopShutdown waits poll at 5ms; no test asserts or relies on that poll interval (timeouts and gate assertions are unchanged); shared waits use 10ms.

AC#1 — rg -n "^func (durableWaitText|lifecycleWaitForStderr|runitWaitForOutput|logsitWaitFollowerText|terminalWaitForProcessText|manifestIntegrationWaitForText|lifecycleWaitCondition|stopitWaitForCondition|lifecycleWaitPathGone|stopitWaitForPathGone|stopShutdownWaitForFile|stopShutdownWaitForPathGone|stopShutdownWaitForProcessGroupGone|waitCLIPath|waitCLIPathAbsent|cliServeRunWaitForFile|cliServeRunWaitForCondition)\\(" integration internal/cli found only cliServeRunWaitForFile and cliServeRunWaitForCondition, justified error-returning wrappers delegating to testutil.WaitUntil; verifier PASS.
AC#2 — go vet ./integration ./internal/cli ./internal/testutil && GOOS=windows go vet ./integration ./internal/cli ./internal/testutil && go test ./integration ./internal/cli ./internal/testutil -count=1: exit 0, verifier independently PASS.
AC#3 — git diff --numstat main -- integration internal/cli internal/testutil in worktree before merge: 139 additions, 386 deletions, net reduction 247; verifier PASS.
Delivery: commit 23ccd5e8a7ff01a0b97c40cee17dac687e0872f9 fast-forward merged to main. task check:staged passed. task ci passed on final commit (vet, staticcheck, tests, race, security, smoke); first precommit task ci failed in untouched internal/daemon/TestFollowAcrossOrdinaryStartReplacement with context deadline exceeded, go test ./internal/daemon -count=1 passed on one rerun, final committed task ci passed. Independent verifier PASS AC1–AC3, no item-scoped defects. Diff contains only declared test and testutil paths; no tests deleted, skipped, or weakened; no protected gate files changed. Next step: finalize task status and commit provider record on main, then remove owned worktree.

Final state: Done releases the provider claim; no blockers or resumable implementation steps remain. Worktrunk removed the session-owned hum-142-shared-test-waits worktree and branch after confirming it matched main. Provider record will be committed on main.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Shared generic test waits across CLI and integration; net 247 lines removed. Verified both-platform vet, focused tests, independent AC verification, staged check, and task ci; merged 23ccd5e into main.
<!-- SECTION:FINAL_SUMMARY:END -->
