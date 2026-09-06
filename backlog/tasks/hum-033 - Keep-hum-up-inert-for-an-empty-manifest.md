---
id: HUM-033
title: Keep hum up inert for an empty manifest
status: To Do
assignee: []
created_date: '2026-09-06 04:57'
labels:
  - cli
  - daemon
milestone: m-3
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/manifest_test.go
  - internal/cli/render.go
  - README.md
  - docs/design.md
priority: low
type: bug
ordinal: 10700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: running CLI `hum up` with zero resolved process definitions performs no daemon lifecycle work, matching MCP behavior.

Scope: return before creating or connecting to a daemon when resolution succeeds with an empty declaration set; provide a clear human result and preserve stable empty machine output.

Non-goals: do not change daemon startup for any non-empty manifest or conventional discovery result.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task test` exits 0, including coverage proving CLI `hum up` with `processes: {}` does not start a daemon and returns the documented human result.
- [ ] #2 `task test` exits 0, including coverage proving JSON output for an empty manifest remains valid and contains no process results.
- [ ] #3 `task test` exits 0, including coverage proving a non-empty manifest still starts the daemon and launches its definitions.
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
