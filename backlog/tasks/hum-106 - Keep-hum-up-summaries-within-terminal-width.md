---
id: HUM-106
title: Keep hum up summaries within terminal width
status: To Do
assignee: []
created_date: '2026-09-12 02:29'
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
- [ ] #1 mise exec go -- go test ./internal/cli -run "Test.*Up.*(Summary|Output|Help)" -count=1 exits 0 and proves default successful `hum up` rows contain only NAME, RESULT, STATE, and PID while `--full` retains complete readiness details.
- [ ] #2 mise exec go -- go test ./internal/cli -run "Test.*Up.*(Failure|Drift|Readiness)" -count=1 exits 0 and proves compact mode retains actionable failure, definition-drift, timeout, and readiness diagnostics without widening successful rows.
- [ ] #3 task cli:test exits 0 with CLI, integration, and Herdr plugin tests passing and existing JSON output unchanged.
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
