---
id: HUM-052
title: Report the terminating signal for signal-killed processes
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 17:21'
labels:
  - process
  - protocol
  - cli
  - mcp
milestone: m-4
dependencies:
  - HUM-054
modified_files:
  - internal/process/process.go
  - internal/app/app.go
  - internal/protocol/protocol.go
  - internal/daemon/wire_protocol.go
  - internal/cli/render.go
  - internal/mcp/tools.go
  - docs/design.md
priority: low
type: enhancement
ordinal: 29700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a process terminated by a signal reports `exit_status: -1` plus a new `signal` field (name such as `SIGTERM` and its number) in status, list, up results, CLI JSON, and MCP snapshots; human status prints `exit: signal SIGTERM (15)`.

Why now: today `hum down` (SIGTERM), a SIGSEGV, and an OOM SIGKILL are indistinguishable; -1 is an in-band sentinel and the signal is retained nowhere, so agents cannot tell an operator stop from a crash.

Non-goals: core dumps, signal-based readiness.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/app -run '^TestSignalExitReportsSignal$' -count=1 -v` exits 0 and prints PASS for TERM and KILL.
- [ ] #2 `go test ./internal/cli -run '^TestStatusShowsSignal$' -count=1 -v` and `go test ./internal/mcp -run '^TestStatusShowsSignal$' -count=1 -v` exit 0 and print PASS.
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
