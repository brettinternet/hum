---
id: HUM-032
title: Report existing process state when dependency launch is skipped
status: To Do
assignee: []
created_date: '2026-09-06 04:57'
updated_date: '2026-09-06 05:07'
labels:
  - cli
  - mcp
  - dependencies
milestone: m-3
dependencies:
  - HUM-026
modified_files:
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/manifest_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - docs/design.md
priority: medium
type: bug
ordinal: 9700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: when an `after` dependency blocks a launch, `up` does not misleadingly imply that an existing dependent process is stopped or unchecked.

Scope: observe and include the dependent session state when dependency resolution skips launching it; distinguish “launch skipped, existing process still running” from a skipped absent/stopped session in human, JSON, and MCP output.

Non-goals: do not stop an existing dependent, reinterpret `after` as a runtime health dependency, or change dependency ordering.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task test` exits 0, including coverage proving a blocked dependent with an existing running incarnation is reported as running with only its new launch skipped.
- [ ] #2 `task test` exits 0, including coverage proving a blocked dependent without a running incarnation remains clearly reported as not launched.
- [ ] #3 `task test` exits 0, including coverage proving CLI human/JSON and MCP output carry equivalent state.
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
Absorbed into HUM-026 before dependency-ordered up ships; blocked launch results and retained runtime state are one delivery contract.
<!-- SECTION:FINAL_SUMMARY:END -->
