---
id: HUM-045
title: Default bounded logs to the newest window and unify cursor naming
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 17:33'
labels:
  - cli
  - mcp
  - docs
milestone: m-4
dependencies: []
modified_files:
  - internal/output/ring.go
  - internal/output/ring_test.go
  - internal/cli/commands.go
  - internal/cli/list_logs_test.go
  - internal/cli/render.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - internal/protocol/protocol.go
  - docs/design.md
  - README.md
priority: medium
type: enhancement
ordinal: 22700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: bounded logs with no selector return the newest window on both CLI and MCP, equivalent to tail with the configured default entry limit. Explicit after-cursor/after_cursor keeps forward paging from the oldest eligible retained entry. Tail reads clipped by entry or byte bounds keep the newest matching entries and emit them chronologically. Existing cursor field names remain unchanged and are documented side by side.

Scope: change ring tail clipping direction, make no-selector CLI and MCP requests select the default tail, and document logs next as the last consumed source cursor versus process next_cursor as the next cursor to be assigned. Explicit cursor, follow, stream, match, and limit semantics remain otherwise unchanged.

Why now: default logs currently show the oldest retained startup output and can hide the newest crash. Even explicit tail can keep the oldest portion of its window when byte-clipped, which defeats the diagnostic intent.

Non-goals: renaming MCP fields, changing follow delivery, changing explicit forward paging, or adding new flags.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/output -run '^TestTailKeepsNewestBoundedWindow$' -count=1 -v` exits 0 and prints PASS for entry- and byte-clipped tails, filtered tails, chronological output, More/Truncated metadata, and newest matching entries retained.
- [ ] #2 `go test ./internal/cli -run '^TestLogsDefaultNewestWindow$' -count=1 -v` exits 0 and prints PASS, proving single and aggregate no-selector logs end at the newest retained entry while explicit --after-cursor still pages forward from the oldest eligible entry.
- [ ] #3 `go test ./internal/mcp -run '^TestLogsDefaultNewestWindow$' -count=1 -v` exits 0 and prints PASS with the same newest default and unchanged explicit after_cursor behavior.
- [ ] #4 `go test ./internal/cli -run '^TestCursorDocs$' -count=1 -v` exits 0 and prints PASS, proving docs/design.md explains logs next and process next_cursor side by side without renaming either MCP field; README guidance uses the newest default.
- [ ] #5 `task ci` exits 0.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Make tail selection and clipping retain the newest eligible entries while preserving chronological emission and cursor metadata.
2. Apply the newest default consistently at CLI and MCP request construction.
3. Cover explicit paging, filters, truncation, cursor documentation, and final gates.
<!-- SECTION:PLAN:END -->
