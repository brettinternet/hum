---
id: HUM-064
title: Enforce each MCP tool input contract before side effects
status: Done
assignee: []
created_date: '2026-09-10 01:49'
updated_date: '2026-09-10 07:41'
labels: []
dependencies: []
modified_files:
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - internal/mcp/server_test.go
  - internal/cli/commands.go
  - internal/cli/surface_test.go
  - docs/coding-agents.md
priority: high
type: bug
ordinal: 40700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Runtime MCP validation exactly matches each advertised closed input schema before project resolution or daemon contact. Fields that belong to another tool are rejected, unsupported scope combinations are stable `invalid_request` errors, and aggregate tools cannot silently ignore a selector. Evidence: every request decodes into `commonInput`; `down` accepts schema-invalid `name` and still stops all processes, `up` accepts and ignores `name`, global `up` succeeds as an empty no-op, and global `list` plus `all: true` reaches a daemon wire error. Scope: use narrow per-tool field validation or request types, make schemas contain only applicable properties, reject global `up` and global/all list locally, and correct MCP help text. Non-goals: do not change valid tool results, daemon protocol semantics, or add new MCP tools.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `mise exec go -- go test -race ./internal/mcp && mise exec go -- go test ./internal/cli` exits 0.
- [x] #2 `mise exec go -- go test ./internal/mcp -run TestToolInputSchemaRuntimeConformance -count=1` exits 0 after invoking every tool with each known-but-inapplicable field and proving `invalid_request` occurs before resolver or daemon calls.
- [x] #3 `mise exec go -- go test ./internal/mcp -run TestRejectsUnsupportedAggregateInputs -count=1` exits 0 after checking named `up`, named `down`, global `up`, and global `list` with `all: true`, with no daemon contact or lifecycle mutation.
- [x] #4 `mise exec go -- go test ./internal/mcp ./internal/cli -run "Test.*MCP.*(Help|ScopeSchema)" -count=1` exits 0 and proves schema/help agree that project scope requires `project_root` and global scope forbids it.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1: `mise exec go -- go test -race ./internal/mcp && mise exec go -- go test ./internal/cli` passed.
AC#2: `mise exec go -- go test ./internal/mcp -run TestToolInputSchemaRuntimeConformance -count=1` passed; every tool rejected each known inapplicable field before resolver/client access.
AC#3: `mise exec go -- go test ./internal/mcp -run TestRejectsUnsupportedAggregateInputs -count=1` passed; named up/down, global up, and global list all were rejected without resolver, daemon, or lifecycle calls.
AC#4: `mise exec go -- go test ./internal/mcp ./internal/cli -run "Test.*MCP.*(Help|ScopeSchema)" -count=1` passed; project/global root, project-only up, and list-all scope contracts agree.
Review: adversarial review found schema-value preflight gaps for explicit empty scope and ambiguous remove; fixed with property type/constraint, required-field, scope, oneOf-equivalent, and aggregate validation before resolution. No remaining item-scoped findings.
Modified-file contract: implementation changed only declared paths; the Backlog.md task file changed only for required workflow state/evidence.

Independent verifier: PASS for AC#1-AC#4 on the final staged implementation; it reran all four exact commands, extra uncached MCP race/full tests, and task ci, and found no validation regressions.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: confirmed. `decodeInput` (internal/mcp/tools.go:415) decodes every tool into the shared `commonInput` union with DisallowUnknownFields, so only fields outside the union are rejected; per-tool rejection exists only for text/base64 (input) and signal. Dispatch (:690-760) passes `name` through for `up` and ignores it for `down`. `s.resolve` (:489) maps an empty project_root to a global Resolution with no definitions, so global `up` is an empty no-op and global `list` with all forwards to the daemon.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented MCP tool-specific closed-schema preflight validation before project resolution and daemon access, rejected unsupported aggregate scope/selector combinations, aligned schemas/help/docs, and added conformance and no-side-effect regression coverage. Final `task ci` passed.
<!-- SECTION:FINAL_SUMMARY:END -->
