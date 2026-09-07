---
id: HUM-041
title: Add a --since window to logs
status: Done
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-07 06:55'
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
- [x] #1 `go test ./internal/output -run '^TestReadSince$' -count=1 -v` exits 0 and prints PASS for cutoff inclusivity, after-cursor then since/stream/match filtering, tail then entry/byte bounds using HUM-045 newest-entry clipping, chronological output, and unchanged next/truncation cursor semantics.
- [x] #2 `go test ./internal/cli -run '^TestLogsSince$' -count=1 -v` exits 0 and prints PASS for single and aggregate logs, initial follow replay, --since documented as long-only, and malformed, zero, negative, and overflowing durations rejected before daemon startup.
- [x] #3 `go test ./internal/mcp -run '^TestLogsSince$' -count=1 -v` exits 0 and prints PASS for positive since_ms, the same boundary/composition semantics as CLI, and invalid values rejected without daemon contact.
- [x] #4 `go test ./internal/protocol ./internal/daemon -run 'Since' -count=1 -v` exits 0 and prints PASS for protocol, wire, client, and server propagation, and `go test ./internal/cli -run '^TestLogsSinceDocs$' -count=1` exits 0 with docs/design.md examples and exact ordering.
- [x] #5 `task ci` exits 0.
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
1. Add a cutoff to output read options and preserve the explicit filter/tail/bounds order.
2. Thread validated positive duration values through protocol, daemon, CLI, and MCP.
3. Cover boundary inclusivity, combinations, follow replay, validation, and docs.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented immutable request-time cutoffs for CLI and MCP logs, including aggregate and initial follow replay semantics. Bumped the private protocol from v10 to v11 so older daemons cannot silently ignore the cutoff.

AC#1 evidence: `go test ./internal/output -run '^TestReadSince$' -count=1 -v` exited 0; TestReadSince PASS, including inclusive boundary, filter/tail/bounds order, chronological entries, eviction/truncation, cursors, and follow replay/live behavior.
AC#2 evidence: `go test ./internal/cli -run '^TestLogsSince$' -count=1 -v` exited 0; TestLogsSince and duration/aggregate subtests PASS, including single, aggregate, follow replay, long-only flag, and pre-contact validation.
AC#3 evidence: `go test ./internal/mcp -run '^TestLogsSince$' -count=1 -v` exited 0; TestLogsSince and slow-resolution/live-daemon subtests PASS, including cutoff capture before resolution and invalid values without daemon contact.
AC#4 evidence: `go test ./internal/protocol ./internal/daemon -run 'Since' -count=1 -v` and `go test ./internal/cli -run '^TestLogsSinceDocs$' -count=1` exited 0; protocol round-trip, v11 negotiation, wire/client/server propagation, and docs ordering PASS.
AC#5 evidence: `task ci` exited 0 on final task branch commit 067dce0 after syncing current main; gofmt, vet, staticcheck, full tests, race tests, build, and smoke PASS. Independent verifier reran every exact AC command on 067dce0 and returned PASS for AC#1-#5.

Modified-file deviations: `internal/output/store.go` is necessary to limit since filtering to the initial retained follow replay while allowing all post-subscription entries; `internal/protocol/restart_policy_test.go` updates the repository-wide protocol-version assertion from v10 to v11. No tests were deleted, skipped, or weakened; no protected gate file changed. Implementation commit e5f67a1; merged to main as bb2a3dd.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added `hum logs --since` and MCP `since_ms` with one inclusive request-time cutoff, deterministic filter/bounds composition, initial follow replay filtering, validation before daemon contact, protocol v11 propagation, docs, and regression coverage. All focused acceptance commands and `task ci` passed independently; merged to main as bb2a3dd.
<!-- SECTION:FINAL_SUMMARY:END -->
