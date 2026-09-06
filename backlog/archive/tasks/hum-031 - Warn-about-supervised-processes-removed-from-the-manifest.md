---
id: HUM-031
title: Warn about supervised processes removed from the manifest
status: To Do
assignee: []
created_date: '2026-09-06 04:57'
updated_date: '2026-09-06 05:07'
labels:
  - cli
  - mcp
  - config
milestone: m-3
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/manifest.go
  - internal/cli/render.go
  - internal/cli/manifest_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - README.md
  - docs/design.md
priority: medium
type: enhancement
ordinal: 8700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum up` makes obsolete manifest-sourced sessions visible after a declaration is removed or a branch switch changes the manifest.

Scope: report running or recovery-capable sessions that belong to the project and were manifest-sourced but are absent from the current resolved declarations, with `hum stop` or `hum remove` guidance. Provide stable CLI JSON and MCP representation.

Non-goals: do not automatically stop, remove, or disable relaunch for obsolete sessions; do not report ad-hoc sessions as removed declarations.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task test` exits 0, including coverage proving CLI and MCP `up` report a running removed manifest session with actionable stop/remove guidance.
- [ ] #2 `task test` exits 0, including coverage proving a removed manifest session pending automatic relaunch is also reported.
- [ ] #3 `task test` exits 0, including coverage proving declared sessions and ad-hoc sessions are not falsely reported as removed declarations.
- [ ] #4 `task check` exits 0 after human and machine-readable output contracts are documented.
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

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Superseded by HUM-029, which now covers changed and removed manifest declarations under one stable up-reconciliation contract.
<!-- SECTION:FINAL_SUMMARY:END -->
