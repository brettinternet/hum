---
id: HUM-106
title: Keep hum up summaries within terminal width
status: Done
assignee: []
created_date: '2026-09-12 02:29'
updated_date: '2026-09-12 03:42'
labels: []
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/render_test.go
  - internal/cli/manifest_test.go
  - internal/cli/surface_test.go
  - internal/cli/flag_alias_test.go
  - README.md
  - docs/design.md
priority: medium
type: enhancement
ordinal: 78800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Human `hum up` summaries remain scannable at ordinary terminal widths even when readiness uses long matchers, exec argv, intervals, or diagnostics.

Scope: keep the default final summary to fixed NAME, RESULT, STATE, and PID columns; preserve actionable failure, definition-drift, and readiness diagnostics outside the fixed-width success table; provide an explicit `--full` human-output mode for the complete readiness configuration; keep startup progress and `--json`/MCP output unchanged.

Non-goals: changing readiness behavior, daemon or MCP contracts, attached child output, process ordering, or exit codes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 mise exec go -- go test ./internal/cli -run "Test.*Up.*(Summary|Output|Help)" -count=1 exits 0 and proves default successful `hum up` rows contain only NAME, RESULT, STATE, and PID while `--full` retains complete readiness details.
- [x] #2 mise exec go -- go test ./internal/cli -run "Test.*Up.*(Failure|Drift|Readiness)" -count=1 exits 0 and proves compact mode retains actionable failure, definition-drift, timeout, and readiness diagnostics without widening successful rows.
- [x] #3 task cli:test exits 0 with CLI, integration, and Herdr plugin tests passing and existing JSON output unchanged.
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
1. Add an explicit hum up --full human-output flag while leaving JSON, MCP, progress, ordering, and exit behavior unchanged. 2. Make the default ordered hum up final table fixed to NAME, RESULT, STATE, and PID; retain the existing complete readiness column only in full mode. 3. Add focused render, command/help, failure/drift/readiness, alias-surface, and documentation coverage. 4. Run the acceptance commands and task ci, obtain independent verifier evidence, then commit, merge to main, finalize the task, and clean up the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation commit 3d65abb; merged to main as 8bf9201. AC#1: mise exec go -- go test ./internal/cli -run "Test.*Up.*(Summary|Output|Help)" -count=1 passed (independent verifier: ok hum/internal/cli 1.173s). AC#2: mise exec go -- go test ./internal/cli -run "Test.*Up.*(Failure|Drift|Readiness)" -count=1 passed (independent verifier: ok hum/internal/cli 4.395s). AC#3: task cli:test passed with all Go packages and 12 Herdr plugin tests passing. DoD: task ci passed on final main merge commit 8bf9201, including security, vet, staticcheck, tests, race, installer, build, manpage, and smoke gates. Independent verifier rerun returned PASS for every AC after empty-matcher and readiness-diagnostic findings were fixed. Diff is limited to all eight declared paths; no test was deleted, skipped, or weakened, and no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Kept default hum up summaries to NAME, RESULT, STATE, and PID; added --full for complete readiness configuration and preserved bounded actionable readiness diagnostics outside the table. Verified all acceptance commands, independent verifier PASS, and task ci on merge commit 8bf9201.
<!-- SECTION:FINAL_SUMMARY:END -->
