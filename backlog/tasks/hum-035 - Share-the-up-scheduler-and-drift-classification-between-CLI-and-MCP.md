---
id: HUM-035
title: Share the up scheduler and drift classification between CLI and MCP
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - cli
  - mcp
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/manifest.go
  - internal/cli/commands.go
  - internal/mcp/tools.go
  - internal/orchestrate/orchestrate.go
  - internal/orchestrate/orchestrate_test.go
  - docs/design.md
priority: medium
type: task
ordinal: 12700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: one implementation of the `after` DAG scheduler, per-node readiness wait, and the definition_drift, removed_definition, recovery_pending, recovery_exhausted, and skipped classification, consumed by both internal/cli and internal/mcp through a small process-snapshot interface (or by converting protocol.Process to app.Process at the MCP edge). CLI and MCP become renderers of one result type.

Why now: roughly 400-500 lines are maintained twice: canonicalManifestCwd, isManifestSource, definitionChangedFields/manifestChangedFields, ensureDefinition/ensureManifestStart, waitForReadiness/manifestReadinessResult, recoveryOutcome, removedDefinitionResults, and the scheduler in internal/cli/commands.go versus internal/mcp/tools.go. They already diverge (the CLI records process.Readiness.Match unconditionally, MCP only when non-empty), and every fix (HUM-029, HUM-030, HUM-032, the 2026-09-06 stale-snapshot fix) had to be mirrored by hand.

Scope: introduce the shared package (final name chosen during implementation), move the duplicated logic and its unit tests there, delete both copies. Behavior, output, and exit codes are unchanged.

Non-goals: new features, output changes, daemon protocol changes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `rg -n 'func waitForReadiness|func definitionChangedFields|func canonicalManifestCwd|func recoveryOutcome' internal/mcp/tools.go` prints nothing and `rg -n 'func manifestReadinessResult|func manifestChangedFields|func manifestRecoveryOutcome' internal/cli/manifest.go` prints nothing.
- [ ] #2 `go test ./internal/cli ./internal/mcp ./integration -count=1` exits 0 with no test deleted, skipped, or weakened.
- [ ] #3 `task ci` exits 0.
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
