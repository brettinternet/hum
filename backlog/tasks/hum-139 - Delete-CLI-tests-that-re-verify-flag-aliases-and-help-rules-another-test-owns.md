---
id: HUM-139
title: Delete CLI tests that re-verify flag aliases and help rules another test owns
status: To Do
assignee: []
created_date: '2026-09-24 22:51'
updated_date: '2026-09-24 22:52'
labels:
  - cli
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
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `test ! -e internal/cli/flag_alias_lifecycle_parity_test.go && test ! -e internal/cli/flag_alias_read_parity_test.go` exits 0.
- [ ] #2 AC2 — `rg -n "func (TestHelpRenderedDescriptionBudget|TestRunNoArgsShowsHelp|TestRunCanceledContextReturnsCancellation)\(" internal cmd` exits 1 (no matches).
- [ ] #3 AC3 — `go vet ./internal/cli ./cmd/hum && go test ./internal/cli ./cmd/hum -count=1` exits 0.
- [ ] #4 AC4 — `go test ./internal/cli -count=1 -cover` reports coverage no more than 0.3 points below the baseline recorded in Implementation Notes (79.7% on 2026-09-24).
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 Only the files and functions in the description table were deleted, and Implementation Notes map every removed assertion to the test that now owns it; no other test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->
