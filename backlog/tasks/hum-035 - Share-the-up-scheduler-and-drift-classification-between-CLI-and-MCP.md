---
id: HUM-035
title: Share the up scheduler and drift classification between CLI and MCP
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 17:31'
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
- [ ] #1 `go test ./internal/orchestrate -run '^TestOrchestrateUp$' -count=1 -v` exits 0 and prints PASS for DAG ordering, concurrent roots, readiness success/timeout/early exit, definition drift, removed definitions, recovery pending/exhausted, direct blocked-by names, and deterministic result ordering.
- [ ] #2 `go test ./internal/cli ./internal/mcp -run '^TestUpAdapterParity$' -count=1 -v` exits 0 and prints PASS, proving identical definitions and process snapshots yield identical outcome, readiness, changed_fields, blocked_by, guidance, and ordering fields at both adapters.
- [ ] #3 `test -z "$(rg -n 'func (waitForReadiness|definitionChangedFields|canonicalManifestCwd|recoveryOutcome|manifestReadinessResult|manifestChangedFields|manifestRecoveryOutcome)' internal/cli internal/mcp)"` exits 0, proving the duplicate implementations are gone.
- [ ] #4 `go test ./internal/cli ./internal/mcp ./internal/orchestrate ./integration -count=1` exits 0 with every pre-refactor behavioral case retained in one of those suites.
- [ ] #5 `task ci` exits 0.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Define the smallest shared snapshot, operations, and result types needed by both adapters.
2. Move scheduling, readiness, drift, recovery, removal, and skipped classification with all existing cases.
3. Add adapter parity tests, delete only the superseded implementations, and run focused and final gates.
<!-- SECTION:PLAN:END -->
