---
id: HUM-107
title: Remove unbounded details from aggregate status
status: To Do
assignee: []
created_date: '2026-09-12 02:29'
labels: []
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/render.go
  - internal/cli/render_test.go
  - internal/cli/status_test.go
  - internal/cli/surface_test.go
  - README.md
  - docs/design.md
priority: medium
type: enhancement
ordinal: 79800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum status` without a process name remains a compact operational overview and cannot become terminal-wide because one process has a long readiness command, matcher, or diagnostic.

Scope: keep the fixed NAME, STATE, PID, READINESS, RESTART, and FOLLOWERS columns; omit readiness configuration and diagnostics from the aggregate table; direct operators to `hum status NAME` for the existing detailed process view; keep named status and all JSON output unchanged.

Non-goals: adding another status output mode, changing the six summary columns, changing named status fields, truncating process names, or changing daemon/MCP behavior.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 mise exec go -- go test ./internal/cli -run "TestStatus.*Summary" -count=1 exits 0 and proves aggregate human status has exactly NAME, STATE, PID, READINESS, RESTART, and FOLLOWERS columns even for long exec-readiness definitions.
- [ ] #2 mise exec go -- go test ./internal/cli -run "TestStatus.*(Human|JSON)" -count=1 exits 0 and proves `hum status NAME` retains readiness configuration and diagnostics while aggregate and named JSON retain all fields.
- [ ] #3 task cli:test exits 0 with CLI, integration, and Herdr plugin tests passing.
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
