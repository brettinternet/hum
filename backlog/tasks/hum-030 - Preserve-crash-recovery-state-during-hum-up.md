---
id: HUM-030
title: Preserve crash recovery state during hum up
status: To Do
assignee: []
created_date: '2026-09-06 04:57'
labels:
  - cli
  - mcp
  - reliability
milestone: m-3
dependencies: []
modified_files:
  - internal/cli/manifest.go
  - internal/cli/manifest_test.go
  - internal/cli/commands.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - internal/cli/render.go
  - README.md
  - docs/design.md
priority: high
type: bug
ordinal: 7700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: broad reconciliation through `hum up` or MCP `up` observes pending or exhausted on-failure recovery without resetting its backoff or retry budget.

Scope: expose recovery state, relaunch count, and `next_launch_at` consistently; a pending retry remains scheduled, and an exhausted session remains exhausted. Targeted `start NAME` and `restart NAME` remain explicit operator overrides.

Non-goals: do not alter the fixed retry delays, stability window, retry count, or targeted lifecycle commands.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task test` exits 0, including coverage proving repeated CLI and MCP `up` calls during pending recovery preserve the scheduled retry and do not launch another child.
- [ ] #2 `task test` exits 0, including coverage proving repeated `up` cannot bypass the five-attempt limit or revive an exhausted crash loop.
- [ ] #3 `task test` exits 0, including coverage proving CLI and MCP results expose current recovery state, relaunch count, and `next_launch_at` after an exit.
- [ ] #4 `task test` exits 0, including coverage proving targeted `start NAME` and `restart NAME` still cancel pending backoff and launch immediately.
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
