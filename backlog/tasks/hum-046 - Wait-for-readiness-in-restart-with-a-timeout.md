---
id: HUM-046
title: Wait for readiness in restart with a timeout
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - cli
  - mcp
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/restart_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - docs/design.md
priority: medium
type: enhancement
ordinal: 23700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum restart NAME...` waits for each restarted process to reach `ready` (or `running_unverified` when no matcher is declared) with `--timeout` and `--no-wait` behaving exactly like `start`, and reports `ready`, `exited_before_ready`, or `timed_out` with the same exit-code precedence. MCP `restart` gains the same fields.

Why now: `restart` is the only command that adopts a changed definition (drift recovery), yet it returns `readiness=starting` immediately, so the drift-recovery path can never confirm that the new definition came up.

Non-goals: changing what restart adopts, changing `after` ordering for restart.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestRestartWaitsForReadiness$' -count=1 -v` exits 0 and prints PASS for ready, exited-before-ready (exit 3), timed-out (exit 2), and --no-wait.
- [ ] #2 `go test ./internal/mcp -run '^TestRestartWaitsForReadiness$' -count=1 -v` exits 0 and prints PASS.
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
