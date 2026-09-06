---
id: HUM-037
title: 'Return object structuredContent for the up, down, and list MCP tools'
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - mcp
milestone: m-4
dependencies: []
modified_files:
  - internal/mcp/tools.go
  - internal/mcp/server.go
  - internal/mcp/tools_test.go
  - integration/mcp_test.go
  - docs/coding-agents.md
priority: medium
type: bug
ordinal: 14700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the `up`, `down`, and `list` tools declare `outputSchema` with `type: object` and return `structuredContent` as an object, for example `{"results": [...]}` for up and down and `{"processes": [...]}` for list. Text content is unchanged.

Why now: the server advertises MCP protocol version 2025-06-18 (and since commit a0bb06e negotiates 2024-11-05 and 2025-03-26); those schema revisions fix `outputSchema.type` to `object` and type `structuredContent` as an object. Array outputs only became legal in a later revision. Strict clients and SDKs validating tool definitions or results reject these three tools today.

Non-goals: changing CLI output, adding fields, changing the other eight tools.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/mcp -run '^TestToolOutputSchemasAreObjects$' -count=1 -v` exits 0 and prints `--- PASS: TestToolOutputSchemasAreObjects`: every tool outputSchema has type object and every tools/call structuredContent decodes as a JSON object.
- [ ] #2 `go test ./integration -run '^TestMCP' -count=1` exits 0 against the built binary.
- [ ] #3 `task ci` exits 0.
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
