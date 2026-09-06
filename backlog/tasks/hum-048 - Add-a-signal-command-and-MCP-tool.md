---
id: HUM-048
title: Add a signal command and MCP tool
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
  - internal/cli/signal_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - docs/design.md
  - docs/coding-agents.md
priority: low
type: feature
ordinal: 25700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum signal NAME SIGNAL` (names like HUP, USR1, or numbers) delivers a signal to the process group of a running record, and MCP exposes the same as a `signal` tool; unknown signals and stopped records return typed errors.

Why now: the daemon protocol already implements Signal (internal/daemon/client.go, internal/app/app.go) but no CLI or MCP surface reaches it, so reload-on-SIGHUP workflows are impossible.

Non-goals: signal-based readiness, signalling ad-hoc children outside the group.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestSignalCommand$' -count=1 -v` exits 0 and prints PASS for HUP delivery, an unknown signal error, and a stopped record error.
- [ ] #2 `go test ./internal/mcp -run '^TestSignalTool$' -count=1 -v` exits 0 and prints PASS.
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
