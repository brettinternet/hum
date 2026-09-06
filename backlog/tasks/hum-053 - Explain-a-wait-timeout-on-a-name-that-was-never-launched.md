---
id: HUM-053
title: Explain a wait timeout on a name that was never launched
status: To Do
assignee: []
created_date: '2026-09-06 16:59'
labels:
  - cli
  - mcp
  - daemon
  - protocol
milestone: m-4
dependencies: []
modified_files:
  - internal/protocol/protocol.go
  - internal/daemon/server.go
  - internal/app/app.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/mcp/tools.go
  - docs/design.md
priority: low
type: enhancement
ordinal: 30700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: when `hum wait NAME` (or MCP `wait`) times out and NAME had no runtime record for the whole wait, the result says so: human output `outcome: timed_out (no process named "NAME" was launched during the wait; check the name or start it first)`, and the JSON/MCP result carries `launched: false` (name to be settled) so agents can distinguish a typo from a slow process. Waiting for a never-launched name stays supported, because ad-hoc sessions may legitimately be started by another client during the wait.

Why now (observed 2026-09-06): `hum wait totally-unknown --timeout 3s` silently burns the full timeout and exits 2, indistinguishable from a genuinely slow process; the default is 30 s. A pre-wait `get` from the CLI was tried and rejected because it adds a round trip and breaks the single-request wait stub used by tests, so the daemon should report the fact in the wait response instead.

Non-goals: rejecting waits on undeclared names, changing wait exit codes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestWaitTimeoutExplainsNeverLaunched$' -count=1 -v` exits 0 and prints PASS for the human message and the JSON field.
- [ ] #2 `go test ./internal/mcp -run '^TestWaitTimeoutExplainsNeverLaunched$' -count=1 -v` exits 0 and prints PASS.
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
