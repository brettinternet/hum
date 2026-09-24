---
id: HUM-129
title: >-
  Make MCP output schemas accept the http and tcp readiness snapshots hum
  returns
status: Done
assignee: []
created_date: '2026-09-23 21:53'
updated_date: '2026-09-24 06:50'
labels:
  - mcp
  - contract
  - reviewed
milestone: m-1
dependencies: []
modified_files:
  - internal/mcp/tools.go
  - internal/mcp/*_test.go
priority: high
type: bug
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: MCP clients that validate `structuredContent` against a tool's `outputSchema` accept every result hum returns. Today they reject `start`, `up`, `list`, and `status` results for any process that uses `ready.http` or `ready.tcp`. The MCP TypeScript SDK client performs this check and throws "Structured content does not match the tool's output schema" (packages/client/src/client/client.ts in modelcontextprotocol/typescript-sdk), so SDK-based agents cannot read the state of those projects through hum.

Evidence (built binary, 2026-09-23): with `ready: {tcp: "127.0.0.1:18766"}` running, MCP `list` returned `"readiness":{"method":"tcp","target":"127.0.0.1:18766","interval":1000000000,"state":"ready",...}`. The `list` outputSchema's readiness object has `method` enum `["match","exec"]`, no `target` property, and `additionalProperties: false`.

Cause: internal/mcp/tools.go:310-319 builds the nested `readiness` object schema with a stale method enum and no `target`. The process schema (:332-350) embeds it, and the `start`, `up`, `list`, and `status` output schemas embed the process schema (tool list at :484-496). protocol.Readiness (internal/protocol/protocol.go:1117-1127) emits method, target, argv, interval, state, cursor, time, match, and diagnostic. The flat `readiness_method` field of the restart result (tools.go:379) already lists all four methods, which shows that the two definitions drifted. No test compares emitted structuredContent with the outputSchema: TestToolOutputSchemasAreObjects (internal/mcp/server_test.go:885) and the tool-name checks (internal/mcp/tools_test.go:631-636) only check shape and names.

Scope:
- Fix the readiness schema: `method` enum match, exec, http, tcp, plus a `target` string property. Define the method list once and use it at both :311 and :379 so they cannot drift again.
- Add TestOutputSchemasAcceptStructuredContent in internal/mcp. It uses a small test-only checker for the JSON Schema subset hum's output schemas use: type, properties, additionalProperties false, required, enum, const, items, oneOf, and minimum/maximum. It runs real tool calls through newTestServer and fakeClient (internal/mcp/tools_test.go:374 and :196) and validates the structuredContent of `start`, `up`, `list`, `status`, and `restart` for one process of each readiness method (match, exec, http, tcp) and one without readiness. It then validates every other tool's structuredContent from at least one successful call. Fix every other output-schema mismatch the checker finds, and list each fix in Implementation Notes.
- The checker lives in a `_test.go` file. Add no dependency.

Non-goals: input-schema changes; tool description text (HUM-126); changes to the emitted structuredContent itself.

Notes: objectSchema (tools.go:270) injects a `scope` property into every object schema, including nested ones. Leave it unless the checker shows it is wrong. HUM-124 adds an `exit` readiness method; after this task, the conformance test forces that method into the shared list.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `go test ./internal/mcp -run "^TestOutputSchemasAcceptStructuredContent$" -count=1 -v` exits 0; it validates start, up, list, status, and restart structuredContent for match, exec, http, tcp, and no-readiness processes plus one successful call of every other tool, and Implementation Notes record that it fails when the readiness enum fix is reverted.
- [x] #2 AC2 — `go test ./internal/mcp ./integration -run "MCP|Tool|Schema" -count=1` exits 0.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation (1656b2d, merged to main): Shared readinessMethods now includes match, exec, http, tcp; nested readiness permits target. Other output-schema mismatches found by the checker: restart readiness_match was missing; logs.entries and events.events can be null for empty slices, so both schemas permit array or null. The existing terminal-control schema assertion now checks the exact array/null contract rather than only array; no test deleted, skipped, or weakened. The checker rejects unknown schema types (regression test added).
AC#1: go test ./internal/mcp -run "^TestOutputSchemasAcceptStructuredContent$" -count=1 -v — PASS; independent verifier PASS. Temporarily reverting the nested readiness enum to match/exec made start/up/list/status fail for http and tcp ("not in enum [match exec]"); restored the fix and reran successfully.
AC#2: go test ./internal/mcp ./integration -run "MCP|Tool|Schema" -count=1 — PASS; independent verifier PASS.
Delivery: task check:staged PASS; task ci PASS on 1656b2d (security, check, test, race, smoke). Initial task ci exposed the outdated logs schema expectation and a transient CLI burst timeout; after updating the contract assertion, task ci passed on the amended final commit. Independent verifier review found and confirmed correction of the unknown-type checker gap; final review outcome PASS. Diff limited to internal/mcp/tools.go and internal/mcp/*_test.go; no protected gate files changed. No remaining blocker; next step: select next dependency-ready item.

Review (029ea9a): process argv schema now accepts null for sessions created before their first launch; nested tool error schema allows wire error details (down per-process failures). Both covered in TestOutputSchemasAcceptStructuredContent. No follow-up.
<!-- SECTION:NOTES:END -->
