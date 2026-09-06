---
id: HUM-036
title: Serve MCP requests concurrently with cancellation
status: Done
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 21:40'
labels:
  - mcp
  - docs
milestone: m-4
dependencies: []
modified_files:
  - internal/mcp/server.go
  - internal/mcp/server_test.go
  - internal/cli/mcp.go
  - internal/cli/mcp_test.go
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: enhancement
ordinal: 13700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: hum mcp serves up to 64 JSON-RPC requests concurrently while serializing responses. A request with an in-flight ID is cancellable through notifications/cancelled; cancellation returns JSON-RPC error code -32800. A 65th request receives server-busy code -32001 without starting work. Reuse of an in-flight request ID receives invalid-request code -32600. Ping, logs, status, and list remain responsive while wait or up is running.

Scope: add per-request contexts, a mutex-protected 64-entry in-flight registry keyed by request ID, deterministic overload and duplicate-ID responses, cancellation, and bounded shutdown. Notifications and responses do not consume request slots. Serve owns a closeable response transport. On stdin EOF or parent cancellation it cancels all requests, waits at most two seconds for handlers, then closes the response transport to unblock any write and waits for the writer goroutine before returning.

Why now: one slow request currently blocks every subsequent request, including cancellation and liveness checks, making the MCP adapter frustrating and unsafe for concurrent agents.

Non-goals: HTTP transport, sessions, streaming results, request batching, configurable limits, or leaving handler/writer goroutines alive after Serve returns.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/mcp -run '^TestConcurrentRequests$' -count=1 -v` exits 0 and prints PASS, proving ping, logs, status, and list respond within one second while a five-second wait and a blocked up are in flight.
- [x] #2 `go test ./internal/mcp -run '^TestRequestCancellation$' -count=1 -v` exits 0 and prints PASS, proving notifications/cancelled targets exactly one request ID, returns code -32800 for that request, leaves unrelated work running, and treats unknown cancellation IDs as no-ops.
- [x] #3 `go test ./internal/mcp -run '^TestRequestCapacityAndIDs$' -count=1 -v` exits 0 and prints PASS for a 64-request bound, immediate -32001 overload, -32600 duplicate in-flight IDs, slot release after every terminal path, and notifications not consuming slots.
- [x] #4 `go test ./internal/mcp -run '^TestConcurrentServerShutdown$' -count=1 -v` exits 0 and prints PASS, proving EOF and parent cancellation cancel stuck handlers, close/unblock a stuck response writer, join all handler/writer goroutines, and return within two seconds without partial response frames.
- [x] #5 `go test ./internal/mcp -race -run '^(TestConcurrentRequests|TestRequestCancellation|TestRequestCapacityAndIDs|TestConcurrentServerShutdown)$' -count=1` exits 0, and `go test ./internal/mcp -run '^TestMCPConcurrencyDocs$' -count=1` exits 0 with docs/design.md and docs/coding-agents.md stating the limit, codes, cancellation, transport ownership, and shutdown contract.
- [x] #6 `task ci` exits 0.
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
1. Introduce the bounded request registry and serialized response lifecycle.
2. Dispatch requests concurrently with duplicate, overload, cancellation, and bounded-shutdown handling.
3. Add race-safe lifecycle tests and document exact codes and limits.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed for implementation in an isolated worktree.

Implementation complete in worktree hum-036-mcp-concurrency. Added internal/cli/commands.go beyond the original modified-file list because newCLICommands overwrites mcpCLICommand.Description; updating that caller is required for the shipped hum mcp --help text, verified by TestMCPHelp.
AC#1 — go test ./internal/mcp -run '^TestConcurrentRequests$' -count=1 -v: PASS.
AC#2 — go test ./internal/mcp -run '^TestRequestCancellation$' -count=1 -v: PASS, including cancellation of a response queued behind a blocked writer.
AC#3 — go test ./internal/mcp -run '^TestRequestCapacityAndIDs$' -count=1 -v: PASS, including response-backpressure ID and slot retention.
AC#4 — go test ./internal/mcp -run '^TestConcurrentServerShutdown$' -count=1 -v: PASS, including EOF/cancellation shutdown, blocked writer closure, and complete-frame validation.
AC#5 — go test ./internal/mcp -race -run '^(TestConcurrentRequests|TestRequestCancellation|TestRequestCapacityAndIDs|TestConcurrentServerShutdown)$' -count=1: PASS; go test ./internal/mcp -run '^TestMCPConcurrencyDocs$' -count=1: PASS.
AC#6 — task ci: PASS.
Independent verifier: PASS for AC#1 through AC#6 after cancellation/backpressure and complete-frame review fixes. Reviewer findings were resolved and focused race tests remained green.

Final implementation commit 50b8ea2; task ci passed after rebasing onto current main and on the final commit.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented bounded concurrent MCP request serving with targeted cancellation, deterministic duplicate/overload errors, serialized closeable responses, and bounded shutdown. Added race-safe lifecycle, backpressure, frame-integrity, CLI help, and documentation coverage. Verified all focused tests, the focused race suite, independent verifier review, task check:staged, and task ci on commit 50b8ea2.
<!-- SECTION:FINAL_SUMMARY:END -->
