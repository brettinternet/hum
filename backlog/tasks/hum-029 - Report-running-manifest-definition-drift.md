---
id: HUM-029
title: Report running manifest definition drift
status: To Do
assignee: []
created_date: '2026-09-06 04:57'
updated_date: '2026-09-06 04:57'
labels:
  - cli
  - mcp
  - config
milestone: m-3
dependencies: []
modified_files:
  - internal/cli/manifest.go
  - internal/cli/manifest_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
  - internal/skill/SKILL.md
  - plugins/hum/skills/hum/SKILL.md
priority: high
type: bug
ordinal: 6700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: repeated `hum up` and MCP `up` distinguish a running process launched from an outdated manifest definition from one matching the current definition.

Scope: compare the effective argv, cwd, readiness configuration, TTY mode, and restart policy before returning `already_running`; return a stable non-success outcome with exact differing fields and `hum restart NAME` guidance. Keep CLI and MCP behavior aligned and correct the operator documentation.

Non-goals: do not automatically restart, stop, or mutate a running process; do not change explicit `restart` semantics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task test` exits 0, including coverage proving unchanged live definitions still return `already_running` in both CLI and MCP.
- [ ] #2 `task test` exits 0, including coverage proving each supported definition change is reported as drift rather than `already_running`, with stable machine-readable output and restart guidance.
- [ ] #3 `task test` exits 0, including dependency coverage proving readiness from an outdated incarnation cannot satisfy the current declaration.
- [ ] #4 `task check` exits 0 after CLI help and operator documentation state that only `restart` applies changes to a running process.
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
