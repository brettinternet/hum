---
id: HUM-035
title: Share the up scheduler and drift classification between CLI and MCP
status: Done
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 20:31'
labels:
  - cli
  - mcp
milestone: m-4
dependencies: []
modified_files:
  - internal/orchestrate/orchestrate.go
  - internal/orchestrate/orchestrate_test.go
  - internal/cli/manifest.go
  - internal/cli/commands.go
  - internal/cli/manifest_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/manifest_test.go
  - docs/design.md
priority: medium
type: task
ordinal: 12700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: one shared orchestration package owns the after DAG scheduler, per-node readiness wait, and definition_drift, removed_definition, recovery_pending, recovery_exhausted, and skipped classifications. CLI and MCP adapt their process snapshots into that package and render the same result model; behavior, fields, ordering, and exit codes remain unchanged.

Scope: move the duplicated scheduling and classification logic plus its behavioral unit cases into internal/orchestrate; retain thin CLI and MCP adapters and explicit parity tests. Every existing behavioral case must move or remain covered even if its original test function is removed.

Why now: roughly 400–500 lines are duplicated and have already diverged. Each correctness fix currently requires two implementations and two opportunities for semantic drift.

Non-goals: new lifecycle features, output changes, daemon protocol changes, or a generic orchestration framework beyond current CLI/MCP behavior.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/orchestrate -run '^TestOrchestrateUp$' -count=1 -v` exits 0 and prints PASS for DAG ordering, concurrent roots, readiness success/timeout/early exit, definition drift, removed definitions, recovery pending/exhausted, direct blocked-by names, and deterministic result ordering.
- [x] #2 `go test ./internal/cli ./internal/mcp -run '^TestUpAdapterParity$' -count=1 -v` exits 0 and prints PASS, proving identical definitions and process snapshots yield identical outcome, readiness, changed_fields, blocked_by, guidance, and ordering fields at both adapters.
- [x] #3 `test -z "$(rg -n 'func (waitForReadiness|definitionChangedFields|canonicalManifestCwd|recoveryOutcome|manifestReadinessResult|manifestChangedFields|manifestRecoveryOutcome)' internal/cli internal/mcp)"` exits 0, proving the duplicate implementations are gone.
- [x] #4 `go test ./internal/cli ./internal/mcp ./internal/orchestrate ./integration -count=1` exits 0 with every pre-refactor behavioral case retained in one of those suites.
- [x] #5 `task ci` exits 0.
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
1. Define the smallest shared snapshot, operations, and result types needed by both adapters.
2. Move scheduling, readiness, drift, recovery, removal, and skipped classification with all existing cases.
3. Add adapter parity tests, delete only the superseded implementations, and run focused and final gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed for implementation in an isolated worktree.

Implementation commit 806c49f, merged to main as 0b758d5.
AC#1 PASS — `go test ./internal/orchestrate -run '^TestOrchestrateUp$' -count=1 -v` passed all required scheduler/classification subtests.
AC#2 PASS — `go test ./internal/cli ./internal/mcp -run '^TestUpAdapterParity$' -count=1 -v` passed in both adapters.
AC#3 PASS — duplicate-function `rg` assertion exited 0 with no matches.
AC#4 PASS — `go test ./internal/cli ./internal/mcp ./internal/orchestrate ./integration -count=1` passed.
AC#5 PASS — `task ci` passed on final implementation commit 806c49f.
Independent verifier reproduced PASS for AC#1–AC#5 and confirmed the modified-file contract, no weakened/deleted/skipped tests, and no protected gate changes. Reviewer findings fixed: CLI now preserves fresh readiness and retained skipped snapshots; MCP rounds positive sub-millisecond remaining timeouts up; the new orchestrator test is race-free (`go test -race ./internal/orchestrate -count=1` PASS).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Moved up scheduling, readiness, drift, removal, recovery, and skip classification into internal/orchestrate; CLI and MCP now adapt to the shared model with parity coverage. Commit 806c49f was merged to main as 0b758d5. All five acceptance commands, task ci, and the independent verifier passed.
<!-- SECTION:FINAL_SUMMARY:END -->
