---
id: HUM-064
title: Enforce each MCP tool input contract before side effects
status: To Do
assignee: []
created_date: '2026-09-10 01:49'
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
- [ ] #1 `mise exec go -- go test -race ./internal/mcp ./internal/cli` exits 0.
- [ ] #2 A table-driven schema/runtime conformance test invokes every tool with each known-but-inapplicable field and exits 0 after proving `invalid_request` is returned before resolver or daemon calls.
- [ ] #3 Focused MCP tests for named `up`, named `down`, global `up`, and global `list` with `all: true` exit 0 and prove no daemon contact or lifecycle mutation occurs.
- [ ] #4 A focused help/schema snapshot command exits 0 and shows project scope requires `project_root`, global scope forbids it, and the `hum mcp` description states the same rule.
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
