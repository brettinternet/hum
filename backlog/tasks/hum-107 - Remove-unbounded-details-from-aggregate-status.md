---
id: HUM-107
title: Remove unbounded details from aggregate status
status: Done
assignee: []
created_date: '2026-09-12 02:29'
updated_date: '2026-09-12 03:42'
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
- [x] #1 mise exec go -- go test ./internal/cli -run "TestStatus.*Summary" -count=1 exits 0 and proves aggregate human status has exactly NAME, STATE, PID, READINESS, RESTART, and FOLLOWERS columns even for long exec-readiness definitions.
- [x] #2 mise exec go -- go test ./internal/cli -run "TestStatus.*(Human|JSON)" -count=1 exits 0 and proves `hum status NAME` retains readiness configuration and diagnostics while aggregate and named JSON retain all fields.
- [x] #3 task cli:test exits 0 with CLI, integration, and Herdr plugin tests passing.
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
1. Remove readiness configuration and diagnostic expansion from the aggregate human status renderer while preserving the fixed six summary columns. 2. Add renderer and command-level coverage for long exec readiness plus named human and aggregate/named JSON field retention. 3. Update operator documentation to direct detailed inspection to `hum status NAME`. 4. Run all acceptance commands and `task ci`, obtain independent verifier evidence, commit, fast-forward merge to main, finalize the task, and clean up the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1 evidence: `mise exec go -- go test ./internal/cli -run "TestStatus.*Summary" -count=1` exited 0; command and renderer tests prove aggregate human status has exactly NAME, STATE, PID, READINESS, RESTART, and FOLLOWERS while long exec readiness details remain absent. AC#2 evidence: `mise exec go -- go test ./internal/cli -run "TestStatus.*(Human|JSON)" -count=1` exited 0; named human status retains readiness configuration and diagnostics, and named plus aggregate JSON retain readiness metadata. AC#3 evidence: `task cli:test` exited 0 with all Go packages and 12 Herdr plugin tests passing. Delivery evidence: commit c1af98b (`fix(cli): bound aggregate status`) fast-forward merged to main after rebasing onto merged HUM-106 work. `task check:staged` passed before commit. `task ci` passed on the rebased final commit, including security, vet, staticcheck, full tests, race tests, build, manpage, and smoke checks. Independent verifier returned PASS for AC1, AC2, and AC3 with no defects. Diff is limited to five declared paths; no test was deleted, skipped, or weakened; no protected gate file was modified.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Removed readiness configuration and diagnostics from aggregate human status while preserving its six operational columns. Named human status and all JSON output remain detailed. Added focused regression coverage and operator documentation, merged c1af98b to main, and passed the full CI gate.
<!-- SECTION:FINAL_SUMMARY:END -->
