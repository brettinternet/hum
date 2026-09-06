---
id: HUM-053
title: Explain a wait timeout on a name that was never launched
status: To Do
assignee: []
created_date: '2026-09-06 16:59'
updated_date: '2026-09-06 17:41'
labels:
  - cli
  - mcp
  - daemon
  - protocol
milestone: m-4
dependencies: []
modified_files:
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/daemon/server.go
  - internal/daemon/server_test.go
  - internal/daemon/client.go
  - internal/daemon/daemon_test.go
  - internal/daemon/wire_protocol.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/mcp.go
  - internal/cli/mcp_test.go
  - internal/cli/wait_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - docs/design.md
priority: low
type: enhancement
ordinal: 30700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: when CLI or MCP wait times out, the result includes process_observed. It is false only when no runtime record for NAME existed at any point during that wait; human output then adds: no process named "NAME" was observed during the wait; check the name or start it first. It is true when a record existed initially or appeared during the wait, including a record later stopped or removed. Exit code remains 2.

Scope: track observation inside the single daemon wait request and return the boolean on both true and false timeout paths. Add the field to CLI JSON and MCP structured content; successful match/exit results may omit it because the process was necessarily observed. Waiting for undeclared names remains supported.

Why now: a typo and a legitimately slow process currently produce the same silent timeout, often after the 30-second default. The daemon has the authoritative lifecycle view and can explain the distinction without an extra client round trip.

Non-goals: preflight rejection, extra get requests, changing timeouts/exit codes, renaming wait outcome, or requiring a manifest declaration.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/app ./internal/daemon -run '^TestWaitProcessObserved$' -count=1 -v` exits 0 and prints PASS for no record, an initial record, a record launched during the wait, and a record observed then removed before timeout.
- [ ] #2 `go test ./internal/protocol -run '^TestWaitProcessObservedRoundTrip$' -count=1 -v` exits 0 and prints PASS for explicit true and false timeout values and unchanged successful result encoding.
- [ ] #3 `go test ./internal/cli -run '^TestWaitTimeoutExplainsNeverObserved$' -count=1 -v` exits 0 and prints PASS for exact actionable human text, JSON process_observed false/true, empty stderr in JSON mode, and unchanged exit code 2.
- [ ] #4 `go test ./internal/mcp -run '^TestWaitTimeoutExplainsNeverObserved$' -count=1 -v` exits 0 and prints PASS for the same boolean definition and guidance in structured content/text.
- [ ] #5 `go test ./internal/cli -run '^TestWaitObservedDocs$' -count=1 -v` exits 0 and prints PASS for docs/design.md and CLI help naming process_observed and the no-extra-round-trip behavior.
- [ ] #6 `task ci` exits 0.
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
1. Track whether a matching runtime record exists at any point in daemon wait.
2. Propagate process_observed through protocol, CLI, and MCP timeout results.
3. Cover never-observed, initially observed, later observed/removed, guidance, docs, and final gates.
<!-- SECTION:PLAN:END -->
