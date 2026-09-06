---
id: HUM-041
title: Add a --since window to logs
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 17:42'
labels:
  - cli
  - mcp
  - daemon
milestone: m-4
dependencies:
  - HUM-045
modified_files:
  - internal/output/types.go
  - internal/output/ring.go
  - internal/output/ring_test.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/daemon/server.go
  - internal/daemon/server_test.go
  - internal/daemon/client.go
  - internal/daemon/daemon_test.go
  - internal/daemon/wire_protocol.go
  - internal/cli/commands.go
  - internal/cli/list_logs_test.go
  - internal/cli/flag_alias_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - docs/design.md
priority: low
type: enhancement
ordinal: 18700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum logs NAME --since 5m` and MCP logs with `since_ms` return entries at or after one request cutoff computed as request time minus the positive duration. Since composes deterministically with cursor, stream, match, tail, entry, and byte selection while preserving cursor and terminal-stripping contracts.

Scope: capture one cutoff per CLI/MCP request. Apply the existing after-cursor boundary first, then since, stream, and match predicates, then tail selection, then entry/byte bounds; emitted entries remain chronological. In follow mode, since filters the initial retained replay and all later entries naturally pass. Both surfaces reject zero, negative, malformed, or overflowing durations before daemon startup/contact.

Why now: trailing time windows match how operators and agents investigate recent failures; requiring counts or prior cursors adds needless friction.

Non-goals: absolute timestamps, until, changing stored timestamps, changing follow delivery after subscription, or changing cursor inclusivity.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/output -run '^TestReadSince$' -count=1 -v` exits 0 and prints PASS for cutoff inclusivity, after-cursor then since/stream/match filtering, tail then entry/byte bounds using HUM-045 newest-entry clipping, chronological output, and unchanged next/truncation cursor semantics.
- [ ] #2 `go test ./internal/cli -run '^TestLogsSince$' -count=1 -v` exits 0 and prints PASS for single and aggregate logs, initial follow replay, --since documented as long-only, and malformed, zero, negative, and overflowing durations rejected before daemon startup.
- [ ] #3 `go test ./internal/mcp -run '^TestLogsSince$' -count=1 -v` exits 0 and prints PASS for positive since_ms, the same boundary/composition semantics as CLI, and invalid values rejected without daemon contact.
- [ ] #4 `go test ./internal/protocol ./internal/daemon -run 'Since' -count=1 -v` exits 0 and prints PASS for protocol, wire, client, and server propagation, and `go test ./internal/cli -run '^TestLogsSinceDocs$' -count=1` exits 0 with docs/design.md examples and exact ordering.
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
1. Add a cutoff to output read options and preserve the explicit filter/tail/bounds order.
2. Thread validated positive duration values through protocol, daemon, CLI, and MCP.
3. Cover boundary inclusivity, combinations, follow replay, validation, and docs.
<!-- SECTION:PLAN:END -->
