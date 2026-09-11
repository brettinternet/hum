---
id: HUM-037
title: 'Return object structuredContent for the up, down, and list MCP tools'
status: Done
assignee:
  - '@brett'
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 22:09'
labels:
  - mcp
milestone: m-4
dependencies: []
modified_files:
  - internal/mcp/tools.go
  - internal/mcp/server.go
  - internal/mcp/tools_test.go
  - internal/mcp/server_test.go
  - integration/mcp_test.go
  - docs/coding-agents.md
priority: medium
type: bug
ordinal: 14700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: up, down, and list declare object outputSchema values and return object structuredContent: {"results":[...]} for up/down and {"processes":[...]} for list. Their existing text content remains byte-for-byte unchanged. Every other MCP tool keeps its current schema, structured payload, and text.

Scope: change only the three incompatible tool schemas/result envelopes, add schema/result/text parity tests across every negotiated MCP protocol version, and retain existing integration coverage.

Why now: the advertised MCP revisions require outputSchema.type and structuredContent to be objects. Strict clients reject the current array-shaped results.

Non-goals: CLI changes, new fields, text rewrites, or changes to the other tools.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/mcp -run '^TestToolOutputSchemasAreObjects$' -count=1 -v` exits 0 and prints PASS, proving every outputSchema has type object and every tools/call structuredContent decodes as an object for all supported protocol versions.
- [x] #2 `go test ./internal/mcp -run '^TestCollectionToolTextUnchanged$' -count=1 -v` exits 0 and prints PASS, proving up, down, and list text content is byte-for-byte unchanged and the other tools retain their schema and result payloads.
- [x] #3 `go test ./integration -run '^TestMCP' -count=1` exits 0 against the built binary.
- [x] #4 `task ci` exits 0.
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
1. Wrap the three array payloads in stable object envelopes and align their schemas.
2. Assert all tool schemas/results are objects while pinning existing text content.
3. Run negotiated-version integration coverage and the final gate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented object structuredContent envelopes for up/down (`results`) and list (`processes`) while preserving legacy text content. Updated schemas and integration helpers. Implementation commit: 6d2d81c; merged to main by chore merge commit. Reviewer: PASS, no findings. AC#1: `go test ./internal/mcp -run '^TestToolOutputSchemasAreObjects$' -count=1 -v` PASS (independent verifier). AC#2: `go test ./internal/mcp -run '^TestCollectionToolTextUnchanged$' -count=1 -v` PASS (independent verifier). AC#3: `go test ./integration -run '^TestMCP' -count=1` PASS (independent verifier). AC#4: `task ci` PASS on commit 6d2d81c (independent verifier). Scope: only five declared paths changed; no tests deleted, skipped, or weakened; no protected gate files changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Returned object MCP structuredContent for up, down, and list without changing their text output. Exact AC tests, integration MCP coverage, and task ci passed independently at 6d2d81c.
<!-- SECTION:FINAL_SUMMARY:END -->
