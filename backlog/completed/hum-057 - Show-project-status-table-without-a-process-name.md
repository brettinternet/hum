---
id: HUM-057
title: Show project status table without a process name
status: Done
assignee: []
created_date: '2026-09-08 21:43'
updated_date: '2026-09-08 21:56'
labels: []
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/status_test.go
  - internal/cli/render_test.go
  - internal/cli/surface_test.go
  - README.md
  - docs/design.md
priority: medium
type: enhancement
ordinal: 34700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: hum status without a name shows a compact operational table for every process resolved in the current project, including unlaunched hum.yaml declarations, while hum status NAME retains its detailed output.

Scope: reuse the current-project process resolution and manifest merge used by hum list; render fixed NAME, STATE, PID, READINESS, RESTART, and FOLLOWERS columns; return the existing list collection shape in JSON mode; update CLI help and operator documentation.

Non-goals: changing named status fields, MCP status semantics, cross-project list --all, process lifecycle behavior, or daemon protocol.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 mise exec go -- go test ./internal/cli -run TestStatus -count=1 exits 0 and proves no-name human and JSON status summarize current-project processes while named status remains detailed.
- [x] #2 mise exec go -- go test ./internal/cli -run TestStatusSummaryJSON -count=1 exits 0 and proves no-name JSON uses the existing processes collection shape including manifest declarations.
- [x] #3 task ci exits 0 before release.
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
1. Share current-project list resolution between list and aggregate status.
2. Add the compact status table and preserve named detail output.
3. Add focused tests and documentation, then run staged and CI gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Modified-file contract expanded to internal/cli/surface_test.go because the existing help contract intentionally asserted the replaced named-only status documentation.

AC#1 PASS — mise exec go -- go test ./internal/cli -run TestStatus -count=1 exited 0; no-name human and JSON summaries, manifest declarations, and named detailed status passed.
AC#2 PASS — mise exec go -- go test ./internal/cli -run TestStatusSummaryJSON -count=1 exited 0; aggregate JSON decoded through the existing listJSON processes collection and included unlaunched declarations.
AC#3 PASS — task ci exited 0; vet, staticcheck, all tests, race tests, build, and smoke test passed.

Independent verifier PASS — reran all three acceptance commands, checked the staged diff and file contract, found no list regression or weakened tests. Residual explicit --project coverage is mitigated by sharing the existing list resolution helper.

DoD evidence — task ci passed on the final content; git diff --cached --check passed; all implementation paths match the modified-file contract, with the provider-owned task file as expected; no test was deleted, skipped, or weakened; no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added aggregate hum status: without a process name it now renders a compact current-project table with manifest declarations, while named status retains full details. JSON reuses the existing processes collection shape and list/status share project resolution. Updated help and docs; focused tests, full CI, staged checks, and independent verification passed.
<!-- SECTION:FINAL_SUMMARY:END -->
