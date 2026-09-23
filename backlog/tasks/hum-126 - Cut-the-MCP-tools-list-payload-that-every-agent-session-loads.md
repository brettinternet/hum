---
id: HUM-126
title: Cut the MCP tools/list payload that every agent session loads
status: To Do
assignee: []
created_date: '2026-09-23 21:22'
updated_date: '2026-09-23 21:54'
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
ordinal: 9000
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
- [ ] #1 AC1 — `go test ./internal/mcp -run "^TestToolsListBudget$" -count=1 -v` exits 0; it asserts `result.tools` is at most 40,000 bytes, at most 17,000 bytes with every `outputSchema` removed, every tool description is at most 300 characters, every input-field description is at most 100 characters, and no output schema contains a `description` key.
- [ ] #2 AC2 — `go test ./internal/mcp ./integration -run "MCP|Tool|Schema|Input" -count=1` exits 0 with no removed or skipped input-validation cases, proving schema strictness is unchanged.
- [ ] #3 AC3 — Running `hum mcp` from a fresh build with initialize, notifications/initialized, and tools/list on stdin returns 13 tools whose serialized `result.tools` is at most 40,000 bytes (check with `jq -c .result.tools | wc -c`).
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
