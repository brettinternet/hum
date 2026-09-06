---
id: HUM-052
title: Report the terminating signal for signal-killed processes
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 17:41'
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
- [ ] #1 `go test ./internal/process ./internal/app -run '^TestSignalExitReportsSignal$' -count=1 -v` exits 0 and prints PASS for TERM and KILL capture, canonical names/numbers, exit_status -1, and absent signal on numeric exits.
- [ ] #2 `go test ./internal/protocol -run '^TestSignalExitRoundTrip$' -count=1 -v` exits 0 and prints PASS for optional signal objects in process, up, and wait snapshots without changing non-signal JSON.
- [ ] #3 `go test ./internal/cli -run '^TestSignalExitRendering$' -count=1 -v` exits 0 and prints PASS across list, status, up, wait, human output, and CLI JSON, including stopped versus autonomous signal-exited records.
- [ ] #4 `go test ./internal/mcp -run '^TestSignalExitSnapshots$' -count=1 -v` exits 0 and prints PASS across list, status, up, and wait structured content/text.
- [ ] #5 `go test ./integration -run '^TestSignalExitObservation$' -count=1 -v` exits 0 and prints PASS, and `go test ./internal/cli -run '^TestSignalExitDocs$' -count=1` exits 0 with docs/design.md describing the schema and human rendering.
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
1. Capture and model canonical signal identity at process termination.
2. Propagate the optional object through every snapshot and result renderer.
3. Cover signal/non-signal parity across app, protocol, CLI, MCP, integration, and docs.
<!-- SECTION:PLAN:END -->
