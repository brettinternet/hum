---
id: HUM-036
title: Serve MCP requests concurrently with cancellation
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - mcp
  - docs
milestone: m-4
dependencies: []
modified_files:
  - internal/mcp/server.go
  - internal/mcp/tools_test.go
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: enhancement
ordinal: 13700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum mcp` dispatches each JSON-RPC request in its own goroutine (responses are already serialized by writeMu), so `ping`, `logs`, `status`, and `list` are answered while a `wait` or `up` is in flight, and `notifications/cancelled` cancels the context of the matching in-flight request. docs/design.md states the concurrency and cancellation contract.

Why now: Server.Serve reads one line, handles it inline, then reads the next. A `wait` (30 s default) or a slow `up` stalls every other call including liveness pings, and cancellation notifications are silently dropped because they queue behind the in-flight call. An agent cannot read logs of process B while waiting on process A.

Scope: per-request goroutine, a bounded in-flight map keyed by request id for cancellation, notifications without an id stay no-ops, shutdown drains in-flight requests.

Non-goals: HTTP transport, sessions, streaming results, batching.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/mcp -run '^TestConcurrentRequests$' -count=1 -v` exits 0 and prints `--- PASS: TestConcurrentRequests`: a ping is answered within 1 s while a wait with a 5 s timeout is outstanding.
- [ ] #2 `go test ./internal/mcp -run '^TestCancelledNotificationCancelsWait$' -count=1 -v` exits 0 and prints `--- PASS: TestCancelledNotificationCancelsWait`.
- [ ] #3 `rg -n 'concurrent' docs/design.md` matches inside the MCP adapter section.
- [ ] #4 `task ci` exits 0.
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
