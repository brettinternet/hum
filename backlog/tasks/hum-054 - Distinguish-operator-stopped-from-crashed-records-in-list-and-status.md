---
id: HUM-054
title: Distinguish operator-stopped from crashed records in list and status
status: To Do
assignee: []
created_date: '2026-09-06 17:07'
updated_date: '2026-09-06 17:21'
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
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/daemon/wire_protocol.go
  - internal/daemon/wire_protocol_test.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/list_logs_test.go
  - internal/cli/ergonomics_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/down_test.go
  - docs/design.md
priority: medium
type: enhancement
ordinal: 31700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a retained record whose last incarnation was terminated by `stop` or project-wide `down` reports `state: stopped`; a process that terminates without an operator stop reports `state: exited` with its exit details, including successful exit code 0 and signal termination. Human list/status, CLI JSON, protocol snapshots, and MCP snapshots expose the distinction consistently so intentional stops are not presented like autonomous exits.

Scope: record operator-stop intent at the supervisor transition that owns termination, carry the resulting state through protocol and clients, and render stopped versus exited distinctly in list and status. Restart still returns the replacement running incarnation after a successful restart. Remove still deletes the record. Daemon shutdown leaves no queryable daemon record. A future signal command remains an autonomous signal exit unless that task explicitly adopts operator-stop semantics after observed termination.

Why now (2026-09-06 audit): completed records are visible in list, but a process intentionally stopped with `hum stop` or `hum down` and one that exits on its own both currently show `exited`. The supervisor already tracks operator-owned termination for relaunch policy, so the same transition can classify the retained snapshot without inference in clients.

Non-goals: retaining tombstones after remove or shutdown, exposing the stopped intermediate incarnation from a successful restart, calling clean autonomous exits crashes, persisting history, changing control exit codes, or changing relaunch policy.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/app -run '^TestProcessTerminalState$' -count=1 -v` exits 0 and prints PASS for stop producing a retained stopped record; autonomous code 0, nonzero, and signal termination producing exited records; and unchanged relaunch decisions.
- [ ] #2 `go test ./internal/cli -run '^TestListStatusTerminalStates$' -count=1 -v` exits 0 and prints PASS, proving human and JSON list/status distinguish stopped from exited, retain autonomous exit details, include stopped manifest records, and never label an autonomous code-0 completion as stopped.
- [ ] #3 `go test ./internal/mcp -run '^TestTerminalStateSnapshots$' -count=1 -v` and `go test ./internal/protocol -run '^TestTerminalStateWireRoundTrip$' -count=1 -v` both exit 0 and print PASS with the same stopped/exited state and exit-detail contract.
- [ ] #4 `go test ./integration -run '^TestDownWorkflow$' -count=1 -v` exits 0 and prints PASS, proving down leaves retained stopped records; focused app tests also prove successful restart returns running and remove makes the record not found rather than exposing an obsolete stopped snapshot.
- [ ] #5 `task ci` exits 0.
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
1. Model stopped versus autonomous exited at the supervisor termination seam and preserve exit details only where semantically valid.
2. Carry the state through protocol, CLI, and MCP snapshots; render stopped and exited distinctly.
3. Cover stop/down, clean/nonzero/signal exits, restart replacement, remove absence, and cross-client parity.
<!-- SECTION:PLAN:END -->
