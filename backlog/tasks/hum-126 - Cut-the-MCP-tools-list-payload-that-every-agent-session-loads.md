---
id: HUM-126
title: Cut the MCP tools/list payload that every agent session loads
status: Done
assignee: []
created_date: '2026-09-23 21:22'
updated_date: '2026-09-24 19:07'
labels:
  - mcp
  - docs
  - contract
milestone: m-4
dependencies:
  - HUM-129
  - HUM-130
modified_files:
  - internal/mcp/tools.go
  - internal/mcp/server.go
  - internal/mcp/*_test.go
  - docs/coding-agents.md
  - internal/skill/SKILL.md
  - plugins/hum/skills/hum/SKILL.md
  - integration/mcp_test.go
priority: medium
type: enhancement
ordinal: 15000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: registering `hum mcp` costs agents far less context while keeping every closed-schema guarantee. Measured against a built binary (initialize, then tools/list), the 13-tool `result.tools` array is 53,715 bytes: tool descriptions about 9.3 KB (up alone 1,849 chars, start 1,276, restart 1,280), input-field descriptions about 6.7 KB, output-schema descriptions about 5.2 KB, and schema structure about 29 KB. Without output schemas it is 26,226 bytes. Descriptions restate docs/design.md semantics (precedence rules, recovery states, probe scheduling) that an agent does not need to choose and call a tool; those details belong in docs/coding-agents.md and the skill, which agents read on demand.

Scope:
- Each tool description states what the tool does and when to use it in at most 300 characters.
- Each input-field description is at most 100 characters; output schemas carry no `description` keys.
- Schema strictness is unchanged: types, `required`, enums, `const`, `additionalProperties: false`, and allOf/not constraints stay as they are, and every existing input-validation test keeps passing.
- Semantics removed from descriptions move to docs/coding-agents.md when not already documented there. HUM-130 (a dependency) removes the tests that pin description wording. If a test still pins wording you change, stop and record it in Implementation Notes instead of deleting it.
- Add a budget test that builds the tools/list result in-process and fails when `result.tools` exceeds 40,000 bytes or exceeds 17,000 bytes with every `outputSchema` removed, and when any description breaks the length rules above.

Non-goals: removing tools or output schemas; changing tool names, fields, or behavior; changing structuredContent; CLI help text (HUM-127).

Implementation context (commit 465b774):
- Server.toolDefinitions (internal/mcp/tools.go:295-505) builds every schema, and the tool list is at :484-496. tools/list returns s.toolDefinitions() (internal/mcp/server.go:696), so the budget test can json.Marshal(NewServer(Options{}).toolDefinitions()) with no daemon.
- Shared input descriptions are repeated in almost every tool, so shortening each once shrinks all of them: root (:296), nameResolved and nameExisting (:297-298), manifest (:299, about 330 characters), waitProps (:300-305), and the `scope` description that objectSchema injects (:272).
- Output-schema descriptions live in readiness (:311-318), process (:342-345), restart (:355-359), and other result schemas under :310-470. Remove `description` keys only from output schemas. Input schemas keep their descriptions: assertSchemaPropertiesDescribed (internal/mcp/tools_test.go:711) requires a non-empty description on every input property, including oneOf branches.
- HUM-129 (a dependency) rewrites the readiness output schema and adds an output-schema conformance test; build on it rather than editing the same lines in parallel.
- For AC3, write the three JSON-RPC lines to `hum mcp` stdin, as the stdio check at the end of TestToolSchemas does (internal/mcp/tools_test.go:694-703).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `go test ./internal/mcp -run "^TestToolsListBudget$" -count=1 -v` exits 0; it asserts `result.tools` is at most 40,000 bytes, at most 17,000 bytes with every `outputSchema` removed, every tool description is at most 300 characters, every input-field description is at most 100 characters, and no output schema contains a `description` key.
- [x] #2 AC2 — `go test ./internal/mcp ./integration -run "MCP|Tool|Schema|Input" -count=1` exits 0 with no removed or skipped input-validation cases, proving schema strictness is unchanged.
- [x] #3 AC3 — Running `hum mcp` from a fresh build with initialize, notifications/initialized, and tools/list on stdin returns 13 tools whose serialized `result.tools` is at most 40,000 bytes (check with `jq -c .result.tools | wc -c`).
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
1. Inventory schema descriptions and existing on-demand documentation; shorten tool/input copy and remove output descriptions without altering schema constraints.
2. Add an in-process tools/list budget and recursive description contract test; update on-demand docs for any displaced semantics.
3. Run focused MCP/integration and stdio checks, independent verifier, task ci on final commit; merge into main and finish provider evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented shorter tool/input descriptions, stripped only output descriptions, and added in-process budget contract. Focused AC1 and AC2 tests pass; fresh stdio build returned 13 tools and 37659 bytes including jq newline. Independent verification and final task ci pending.

Commit 28b00e6 fast-forwarded to main. AC#1: go test ./internal/mcp -run "^TestToolsListBudget$" -count=1 -v passed; in-process budget and description contract passed. AC#2: go test ./internal/mcp ./integration -run "MCP|Tool|Schema|Input" -count=1 passed both packages; input-validation tests retained. AC#3: go build -o /tmp/hum-126-mcp-budget-cli ./cmd/hum; initialize, notifications/initialized, tools/list on stdin returned 13 tools; jq -c .result.tools | wc -c measured 37659 bytes including newline. Independent verifier returned PASS for AC1, AC2, AC3 and found no item-scoped defects. task ci passed on 28b00e6 (second invocation; first encountered known HUM-134 attach burst parallel-load flake). Only declared implementation paths changed; no tests deleted/skipped/weakened and no protected gate files modified. Next: check AC/DoD, commit task record, remove owned worktree.

All AC and DoD checkboxes verified; task marked Done (releasing claim). Remaining delivery: commit this provider record on main, rerun task ci on final commit, then remove owned worktree.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Reduced MCP tools/list to 37659 bytes for 13 tools while preserving closed schemas; AC1/AC2, independent verification, and task ci passed on commit 28b00e6, fast-forwarded to main.
<!-- SECTION:FINAL_SUMMARY:END -->
