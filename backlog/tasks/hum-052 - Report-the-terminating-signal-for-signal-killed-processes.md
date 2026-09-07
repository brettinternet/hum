---
id: HUM-052
title: Report the terminating signal for signal-killed processes
status: Done
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-07 09:40'
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
  - internal/process/process_test.go
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/daemon/server.go
  - internal/daemon/client.go
  - internal/daemon/daemon_test.go
  - internal/daemon/wire_protocol.go
  - internal/daemon/wire_protocol_test.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/list_logs_test.go
  - internal/cli/manifest_test.go
  - internal/cli/wait_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/lifecycle_test.go
  - docs/design.md
priority: low
type: enhancement
ordinal: 29700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: every signal-terminated process snapshot retains exit_status -1 and adds signal as {"name":"SIGTERM","number":15}. The signal appears consistently in list, status, up results, wait exit results, CLI JSON, protocol snapshots, and MCP structured content. Human output renders exit: signal SIGTERM (15); operator-stopped state from HUM-054 remains distinct from autonomous signal exit.

Scope: capture the terminating WaitStatus once in the process layer, carry canonical signal name/number through app and protocol types, and render the same shape at all CLI/MCP observation surfaces. Non-signal exits omit signal and keep their current numeric exit status.

Why now: exit_status -1 loses whether termination was TERM, KILL, SEGV, or another signal, preventing useful diagnosis and automation.

Non-goals: core-dump metadata, platform-independent signals beyond supported Unix targets, signal-based readiness, changing exit_status, or changing stopped/exited classification.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/process ./internal/app -run '^TestSignalExitReportsSignal$' -count=1 -v` exits 0 and prints PASS for TERM and KILL capture, canonical names/numbers, exit_status -1, and absent signal on numeric exits.
- [x] #2 `go test ./internal/protocol -run '^TestSignalExitRoundTrip$' -count=1 -v` exits 0 and prints PASS for optional signal objects in process, up, and wait snapshots without changing non-signal JSON.
- [x] #3 `go test ./internal/cli -run '^TestSignalExitRendering$' -count=1 -v` exits 0 and prints PASS across list, status, up, wait, human output, and CLI JSON, including stopped versus autonomous signal-exited records.
- [x] #4 `go test ./internal/mcp -run '^TestSignalExitSnapshots$' -count=1 -v` exits 0 and prints PASS across list, status, up, and wait structured content/text.
- [x] #5 `go test ./integration -run '^TestSignalExitObservation$' -count=1 -v` exits 0 and prints PASS, and `go test ./internal/cli -run '^TestSignalExitDocs$' -count=1` exits 0 with docs/design.md describing the schema and human rendering.
- [x] #6 `task ci` exits 0.
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
1. Capture and model canonical signal identity at process termination.
2. Propagate the optional object through every snapshot and result renderer.
3. Cover signal/non-signal parity across app, protocol, CLI, MCP, integration, and docs.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed for implementation in an isolated worktree.

AC#1 PASS — go test ./internal/process ./internal/app -run '^TestSignalExitReportsSignal$' -count=1 -v exited 0 and printed PASS for TERM/KILL, canonical names/numbers, exit_status -1, and numeric omission.
AC#2 PASS — go test ./internal/protocol -run '^TestSignalExitRoundTrip$' -count=1 -v exited 0 and printed PASS.
AC#3 PASS — go test ./internal/cli -run '^TestSignalExitRendering$' -count=1 -v exited 0 and printed PASS across list/status/up/wait/follow rendering and stopped distinction.
AC#4 PASS — go test ./internal/mcp -run '^TestSignalExitSnapshots$' -count=1 -v exited 0 and printed PASS across list/status/up/wait MCP content.
AC#5 PASS — go test ./integration -run '^TestSignalExitObservation$' -count=1 -v and go test ./internal/cli -run '^TestSignalExitDocs$' -count=1 both exited 0 and printed PASS.
AC#6 PASS — task ci exited 0 on final rebased commit 63bbcf9.
Independent verifier passed AC#1–AC#6 and confirmed no deleted, skipped, or weakened tests and no residual blockers. Scope expanded to internal/cli/manifest.go, internal/cli/mcp.go, internal/orchestrate/orchestrate.go, and internal/output/types.go because these are required propagation plumbing for up/MCP adapters and deterministic output exit events.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented canonical terminating-signal capture and propagation through process, app, protocol, daemon, CLI list/status/up/wait/follow output, MCP text/structured content, integration coverage, and design documentation. Non-signal exits omit signal and operator-stopped records remain distinct. Merged commit 63bbcf9 to main.
<!-- SECTION:FINAL_SUMMARY:END -->
