---
id: HUM-041
title: Add a --since window to logs
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - cli
  - mcp
  - daemon
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/output/ring.go
  - internal/output/types.go
  - internal/protocol/protocol.go
  - internal/daemon/server.go
  - internal/mcp/tools.go
  - docs/design.md
priority: low
type: enhancement
ordinal: 18700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum logs NAME --since 5m` (and MCP `logs` with `since_ms`) returns only entries whose timestamp falls within the trailing window, composable with --match, --tail, --stream, and the byte and entry limits; bounded semantics, cursors, and stripping are unchanged.

Why now: operators and agents ask "what happened in the last two minutes" far more often than "the last N entries"; today the only selectors are --tail and --after-cursor, which require knowing counts or cursors.

Non-goals: absolute timestamps, --until, follow-mode filtering.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/output -run '^TestReadSince' -count=1 -v` exits 0 and prints PASS for window selection interacting with tail, match, and limits.
- [ ] #2 `go test ./internal/cli -run '^TestLogsSince' -count=1 -v` and `go test ./internal/mcp -run '^TestLogsSince' -count=1 -v` exit 0 and print PASS.
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
