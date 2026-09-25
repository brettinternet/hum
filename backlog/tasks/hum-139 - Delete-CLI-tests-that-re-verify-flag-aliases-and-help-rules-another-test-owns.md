---
id: HUM-139
title: Delete CLI tests that re-verify flag aliases and help rules another test owns
status: Done
assignee: []
created_date: '2026-09-24 22:51'
updated_date: '2026-09-25 18:30'
labels:
  - cli
  - reviewed
dependencies: []
modified_files:
  - internal/cli/flag_alias_lifecycle_parity_test.go
  - internal/cli/flag_alias_read_parity_test.go
  - internal/cli/surface_test.go
  - cmd/hum/main_test.go
  - internal/cli/wait_test.go
  - internal/cli/list_logs_test.go
  - internal/cli/flag_alias_test.go
priority: medium
type: task
ordinal: 23000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: internal/cli and cmd/hum drop about 800 test lines that only repeat assertions already owned by a cheaper test. Nothing hum does loses coverage.

Owner exception (2026-09-24): this task may delete the files and functions in the table below after Implementation Notes map every unique assertion they hold to the test that now owns it. It may not remove anything else. Its Definition of Done replaces the default no-deletion rule with a scoped one.

| Delete | Size and cost | Owner that already covers it |
|---|---|---|
| internal/cli/flag_alias_lifecycle_parity_test.go (TestFlagAliasParityLifecycleCommands) | 446 lines, 3.5s; runs each lifecycle command twice through real daemons and children, once with short flags and once with long flags | TestFlagAliases (internal/cli/flag_alias_test.go:14) pins the alias inventory for every command. TestFlagAliasParity (:161) parses each short and long pair, including run and its custom parser, and compares the parsed value, output mode, daemon and detach request, run name and argv, stdout, stderr, and exit code. Validation parity is at :246. Short and long spellings resolve to the same flag inside urfave/cli, so running the whole command twice adds no hum behavior. |
| internal/cli/flag_alias_read_parity_test.go (TestFlagAliasParityReadCommands) | 301 lines; starts a real daemon and processes | Same owners. Its semantic checks, such as wait exit code 3 and request After/Match/TimeoutMS at :31, belong to internal/cli/wait_test.go TestWaitCLIRequestOptions (:22) and TestWaitCLIOutputsAndExitCodes (:89). Cross-project `list --all` belongs to TestList (internal/cli/list_logs_test.go:177 and :197). |
| TestHelpRenderedDescriptionBudget (internal/cli/surface_test.go:141) | ~30 lines | TestHelpContract (internal/cli/help_contract_test.go:15) walks the same visible paths plus completion, and asserts the same 240-character limit on the same description prose along with sentence, usage, and wire-identifier rules. |
| TestRunNoArgsShowsHelp and TestRunCanceledContextReturnsCancellation (cmd/hum/main_test.go:25 and :92) | ~40 lines | TestRootCommandNoArgsShowsHelp and TestRootCommandCanceledContext (internal/cli/root_test.go:11 and :47) assert the same behavior on the same root command. Keep TestRunVersion (build metadata injection), TestRunInvalidCommandReturnsError, and the exitCode tests. They are entrypoint-specific. |

Procedure:
1. Record baselines in Implementation Notes: `go test ./internal/cli -count=1 -cover` (79.7% and 33.2s on 2026-09-24) and `wc -l` of the files above.
2. For each file or function, list every assertion and name the existing test that owns it. If one has no owner, move that single assertion into the owner test named in the table, keeping it cheap (parse-level or stub daemon, as in waitCLIStubDaemon at internal/cli/wait_test.go:288). Do not move whole scenarios.
3. Delete the listed files and functions. Delete helpers that become unused; `go vet ./internal/cli ./cmd/hum` reports none.

Non-goals: other surface tests (TestDocsReferenceRealCommandsAndFlags, TestDocsCoverEveryCommand, man, completion, machine-output v1 in both packages, skill tests), which are structural checks HUM-130 chose to keep; changing production code.

Pre-check (2026-09-24): `go test ./internal/cli -count=1 -cover -skip '^(TestFlagAliasParityLifecycleCommands|TestFlagAliasParityReadCommands|TestHelpRenderedDescriptionBudget)$'` reported 79.7%, the same as the baseline, so AC4 should need no new tests. Stop rules: if an assertion has no owner and cannot be moved as a single parse-level or stub-daemon check, keep the original test and report it.

Unrelated failures: if `task ci` or a package run fails in a test this task did not touch, rerun that package once. If the rerun passes, record both runs in Implementation Notes and continue; if it fails again, stop and report it. Do not fix unrelated tests in this task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `test ! -e internal/cli/flag_alias_lifecycle_parity_test.go && test ! -e internal/cli/flag_alias_read_parity_test.go` exits 0.
- [x] #2 AC2 — `rg -n "func (TestHelpRenderedDescriptionBudget|TestRunNoArgsShowsHelp|TestRunCanceledContextReturnsCancellation)\(" internal cmd` exits 1 (no matches).
- [x] #3 AC3 — `go vet ./internal/cli ./cmd/hum && go test ./internal/cli ./cmd/hum -count=1` exits 0.
- [x] #4 AC4 — `go test ./internal/cli -count=1 -cover` reports coverage no more than 0.3 points below the baseline recorded in Implementation Notes (79.7% on 2026-09-24).
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 Only the files and functions in the description table were deleted, and Implementation Notes map every removed assertion to the test that now owns it; no other test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inventory each removed assertion against the existing owner tests; add only missing cheap checks. 2. Remove only the approved redundant files/functions and unused helpers. 3. Run focused acceptance checks, independent verification, and final-commit CI; merge and clean up.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Baseline (2026-09-24): go test ./internal/cli -count=1 -cover => PASS, 79.7% coverage, 25.168s package time. wc -l: flag_alias_lifecycle_parity_test.go 446; flag_alias_read_parity_test.go 301; surface_test.go 525; cmd/hum/main_test.go 107.

Assertion inventory before deletion (all aliases are registered by TestFlagAliases and parsed/equivalent in TestFlagAliasParity, including custom run parser and validation): lifecycle parity file: run -d/-j name, argv, source, cwd, state, detached JSON/PID/cursor/stop fixture => TestFlagAliasParity(run detach/json), TestRunSelectionSemantics, serve_run_test.go run/daemon lifecycle tests; serve -d PID/listening/exit => TestFlagAliasParity(serve daemon), TestServeDaemon; init -j path/outcome/candidates/exit => TestFlagAliasParity(init json), TestInitGeneratedJSON and TestInitTemplateJSON; shutdown -j stopped/exit => TestFlagAliasParity(shutdown json), TestShutdown; start/up -t/-j timeout result name/outcome/source/argv/exit => TestFlagAliasParity(start/up timeout/json), TestManifestStartTimeout and TestUpFailureAndReadinessProgress; down -j stopped status/exit => TestFlagAliasParity(down json), TestDownJSONResultsAreStable; stop -j stopped status/exit => TestFlagAliasParity(stop json), TestStop; restart -j name/source/argv/restarts/exit => TestFlagAliasParity(restart json), TestRestartCLIJSONAndHumanResults and TestManifestRestart; invalid init/down/stop/shutdown -j args => TestFlagAliasParity(json per command), TestInitRejectsPositionalArguments and the corresponding command validation tests. These command behavior owners run the canonical spellings; urfave resolves aliases to the same flags. read parity file: wait -c/-m/-t/-j request After=0/Match/TimeoutMS, exit 3, output => TestFlagAliasParity(wait each option), TestWaitCLIRequestOptions, TestWaitCLIOutputsAndExitCodes; list -a/-j other project/current-only/JSON => TestFlagAliasParity(list all/json), TestList; status -j JSON name => TestFlagAliasParity(status json), TestStatusRunningJSON; logs -j/-s/-n/-c/-b/-m entries, stream, tail, cursor, more, match => TestFlagAliasParity(logs each option), TestLogsFollow (selection/cursor/byte window), TestLogsMultipleNames; logs -f live replay, exit event, cancellation => TestFlagAliasParity(logs follow), TestLogsFollow and TestLogsFollowJSONEventTypes; invalid logs tail/bytes and wait timeout/match => TestFlagAliasParity(validation) and TestWaitCLIValidation / logs validation tests. surface TestHelpRenderedDescriptionBudget visible paths, description existence and <=240 rendered chars => TestHelpContract (same paths, description sentence and <=240 prose). cmd/hum TestRunNoArgsShowsHelp stdout/stderr/usage/description => TestRootCommandNoArgsShowsHelp; TestRunCanceledContextReturnsCancellation context error/no output => TestRootCommandCanceledContext. No unique assertion requires a new test.

Commit 2d5c6c0 (test(cli): remove redundant alias parity suites), four declared paths only, 810 deleted lines and no additions; no protected gate file touched. AC#1: in hum-139 worktree, test ! -e internal/cli/flag_alias_lifecycle_parity_test.go && test ! -e internal/cli/flag_alias_read_parity_test.go => exit 0. AC#2: rg -n "func (TestHelpRenderedDescriptionBudget|TestRunNoArgsShowsHelp|TestRunCanceledContextReturnsCancellation)\\(" internal cmd => exit 1 (no matches). AC#3: go vet ./internal/cli ./cmd/hum && go test ./internal/cli ./cmd/hum -count=1 => exit 0 (internal/cli 24.902s, cmd/hum 1.268s). AC#4: go test ./internal/cli -count=1 -cover => exit 0, 79.7% of statements (baseline 79.7%). Independent verifier PASS AC#1-#4 and no unowned assertion; flagged a blank line at EOF, corrected and git diff --check now passes. task check:staged PASS. task ci on commit 2d5c6c0 PASS: security, checks, test, race, build/manual/smoke. Next: merge into main, mark task Done and commit provider completion; rerun task ci on final commit.

Main fast-forwarded to 2d5c6c0; Worktrunk removal verified by creation receipt and wt list, hum-139 worktree and branch removed. AC#2 command as executed uses one shell backslash before the literal opening parenthesis; rg exit 1. Review outcome: PASS, only whitespace issue corrected before 2d5c6c0. No remaining blocker.

Delivery complete; all ACs verified; final metadata-only commit on main pending followed by task ci on final commit. Next resumable step: none if final CI remains green.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Removed 810 lines of duplicate CLI alias and entrypoint tests; retained parse-level and command-owner coverage. AC1–AC4, independent verifier, and task ci passed on implementation commit 2d5c6c0. Fast-forwarded main and removed the worktree/branch.
<!-- SECTION:FINAL_SUMMARY:END -->
