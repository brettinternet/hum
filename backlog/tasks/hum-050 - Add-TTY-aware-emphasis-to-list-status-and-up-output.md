---
id: HUM-050
title: 'Add TTY-aware emphasis to list, status, and up output'
status: Done
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-07 06:31'
labels:
  - cli
milestone: m-4
dependencies:
  - HUM-054
modified_files:
  - internal/cli/render.go
  - internal/cli/render_test.go
  - internal/cli/commands.go
  - internal/cli/ergonomics_test.go
  - docs/design.md
priority: low
type: enhancement
ordinal: 27700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: when stdout is a terminal, TERM is not dumb, and NO_COLOR is absent, list, status, and up add minimal ANSI emphasis. Running/ready is green; starting is yellow; operator-stopped is cyan; autonomous successful exit is dim; failed exit, error, timed_out, definition_drift, recovery_exhausted, and dependency-skipped outcomes are red; headers are bold. Piped output and JSON remain byte-for-byte unchanged.

Scope: centralize TTY/color detection and style only renderer-owned labels, never names, paths, messages, or child output. Any presence of NO_COLOR, including an empty value, disables styling. TERM=dumb disables styling. Color choice is fixed and has no configuration beyond those conventions.

Why now: dense lifecycle output makes failures and intentional stops hard to scan, but color must never contaminate scripts, snapshots, JSON, or logs.

Non-goals: themes, terminal capability probing beyond TTY/TERM/NO_COLOR, colorizing child output, or styling arbitrary message text.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/cli -run '^TestColorPolicy$' -count=1 -v` exits 0 and prints PASS for TTY enablement, pipe disablement, any-present NO_COLOR, TERM=dumb, and JSON never emitting ANSI.
- [x] #2 `go test ./internal/cli -run '^TestLifecycleColorMapping$' -count=1 -v` exits 0 and prints PASS for the exact running/ready, starting, stopped, successful-exit, failed/error/timeout/drift/recovery/skipped, and header styles while names, paths, messages, and child text remain unstyled.
- [x] #3 `go test ./internal/cli -run '^TestUncoloredOutputUnchanged$' -count=1 -v` exits 0 and prints PASS against pre-change pipe and JSON goldens for list, status, and up.
- [x] #4 `go test ./internal/cli -run '^TestColorDocs$' -count=1 -v` exits 0 and prints PASS for docs/design.md stating the palette and disable rules.
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
1. Add one renderer color policy with exact state/outcome mappings.
2. Apply it only to list, status, and up labels while preserving non-color bytes.
3. Cover TTY/environment matrices, all mapped states, docs, and final gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed by @brett for implementation in worktree task/hum-050.

Implemented centralized TTY/TERM/NO_COLOR color policy and renderer-owned lifecycle/header styling in list, status, and up. Reviewer found start inherited up styling and up progress used stdout policy; fixed both so start stays plain and redirected progress remains unstyled. Implementation commit d596057; merged to main as fd7fd41.

AC#1 evidence: go test ./internal/cli -run ^TestColorPolicy$ -count=1 -v — PASS on merged main, including start/plain and redirected-progress coverage.
AC#2 evidence: go test ./internal/cli -run ^TestLifecycleColorMapping$ -count=1 -v — PASS on merged main.
AC#3 evidence: go test ./internal/cli -run ^TestUncoloredOutputUnchanged$ -count=1 -v — PASS on merged main.
AC#4 evidence: go test ./internal/cli -run ^TestColorDocs$ -count=1 -v — PASS on merged main.
AC#5 evidence: task ci — PASS on merged main fd7fd41. An earlier pre-commit run exposed a flaky existing TestUpProgressBlockedExistingState failure; its focused rerun passed and subsequent full gates passed.

Independent verifier: PASS for AC#1–AC#5 and DoD scope/test/protected-file checks on d596057. Final diff is limited to docs/design.md, internal/cli/commands.go, internal/cli/render.go, and internal/cli/render_test.go; no tests were deleted, skipped, or weakened and no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added fixed TTY-aware lifecycle emphasis to list, status, and up while preserving piped, JSON, start, redirected progress, names, paths, messages, and child output. Merged as fd7fd41; all focused tests, independent verification, and task ci passed.
<!-- SECTION:FINAL_SUMMARY:END -->
