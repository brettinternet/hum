---
id: HUM-048
title: Add a signal command and MCP tool
status: Done
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-07 07:17'
labels:
  - cli
  - mcp
  - daemon
milestone: m-4
dependencies: []
modified_files:
  - internal/signals/signals.go
  - internal/signals/signals_test.go
  - internal/cli/commands.go
  - internal/cli/signal_test.go
  - internal/cli/help_contract_test.go
  - internal/cli/mcp.go
  - internal/cli/mcp_test.go
  - internal/cli/flag_alias_test.go
  - internal/cli/json_errors_test.go
  - internal/cli/surface_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/protocol/restart_policy_test.go
  - internal/daemon/server.go
  - internal/daemon/client.go
  - internal/daemon/wire_protocol.go
  - internal/daemon/daemon_test.go
  - internal/app/app.go
  - internal/app/app_test.go
  - integration/mcp_test.go
  - docs/design.md
  - docs/coding-agents.md
priority: low
type: feature
ordinal: 25700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum signal NAME SIGNAL` and MCP signal deliver one observational signal to the process group of a running record. SIGNAL accepts case-insensitive names with optional SIG prefix and positive decimal forms of signals in the supported named table for the current OS. CLI and MCP return a stable sent result containing the target name and canonical signal name/number.

Scope: move signal parsing into a small shared signals package used by CLI, MCP, and daemon. Support the named Unix signals already accepted by the daemon plus USR1 and USR2 where available; decimal input is accepted only when it maps to that table. Add a distinct supervisor signal path that never sets stop control intent or cancels pending relaunch policy, including for TERM and KILL; only stop/down/restart keep control semantics. Deliver to the process group. Reject empty, zero, negative, malformed, unknown-name, unsupported-number, missing-record, and non-running requests before signaling, with typed invalid_signal, not_found, and not_running errors. Human success is one concise line; CLI JSON and MCP structured content use {"name":...,"signal":{"name":"SIGHUP","number":1},"status":"sent"}.

Why now: common reload/debug workflows require signals, but users must currently bypass Hum and rediscover process groups manually.

Non-goals: unnamed real-time signals, readiness signaling, arbitrary PIDs, multiple signals per invocation, Windows, or treating signal delivery as operator stop intent.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/signals -run '^TestParse$' -count=1 -v` exits 0 and prints PASS for case-insensitive names, optional SIG prefixes, named-table decimal equivalents, canonical SIG-prefixed results, and rejection of unnamed/unsupported numeric signals.
- [x] #2 `go test ./internal/app -run '^TestObservationalSignalPreservesLifecyclePolicy$' -count=1 -v` exits 0 and prints PASS, proving TERM and KILL through signal do not set stop intent or cancel configured relaunch, while stop/down/restart retain control behavior.
- [x] #3 `go test ./internal/cli -run '^TestSignalCommand$' -count=1 -v` exits 0 and prints PASS for HUP/SIGHUP aliases, USR1/USR2 where supported, numeric delivery to leader and child, exact human/JSON success, and no signal for invalid input.
- [x] #4 `go test ./internal/cli -run '^TestSignalCommandErrors$' -count=1 -v` exits 0 and prints PASS for empty, zero, negative, malformed, unknown-name, unsupported-number, missing-record, and stopped-record inputs with stable typed codes and actionable messages.
- [x] #5 `go test ./internal/mcp -run '^TestSignalTool$' -count=1 -v` exits 0 and prints PASS for the same grammar, process-group delivery, result object, and invalid_signal/not_found/not_running codes.
- [x] #6 `go test ./internal/daemon -run '^TestSignalCanonicalRoundTrip$' -count=1 -v` exits 0 and prints PASS, proving daemon dispatch and response preserve canonical name/number; `go test ./internal/cli ./internal/mcp -run '^TestSignalDocs$' -count=1 -v` exits 0 for CLI help, MCP schema descriptions, docs/design.md, and docs/coding-agents.md examples.
- [x] #7 `task ci` exits 0.
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
1. Add shared signal parsing and stable result/error types over the existing group operation.
2. Expose CLI and MCP surfaces with consistent validation and output.
3. Test named/numeric group delivery, errors, help/docs, and final gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed for implementation in an isolated worktree.

Scope deviation justified: directly impacted CLI and integration surface-contract tests, the production MCP adapter, and the protocol-version assertion were updated so the new command/tool is exercised through real wiring and protocol v12 is enforced.
AC#1: go test ./internal/signals -run '^TestParse$' -count=1 -v — PASS.
AC#2: go test ./internal/app -run '^TestObservationalSignalPreservesLifecyclePolicy$' -count=1 -v — PASS.
AC#3: go test ./internal/cli -run '^TestSignalCommand$' -count=1 -v — PASS; independent manual verification delivered HUP, USR1, and USR2 to leader and child.
AC#4: go test ./internal/cli -run '^TestSignalCommandErrors$' -count=1 -v — PASS.
AC#5: go test ./internal/mcp -run '^TestSignalTool$' -count=1 -v — PASS; independent production bin/hum mcp verification returned canonical SIGHUP and delivered to leader and child.
AC#6: go test ./internal/daemon -run '^TestSignalCanonicalRoundTrip$' -count=1 -v and go test ./internal/cli ./internal/mcp -run '^TestSignalDocs$' -count=1 -v — PASS; supplementary TestToolSchemas and real tools/list verification confirmed the signal schema.
AC#7: task ci — PASS on final commit 96f9b99.
Independent verifier: PASS for AC#1 through AC#7; no tests deleted, skipped, or weakened; no protected gate files changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented shared canonical signal parsing, observational process-group delivery, CLI and MCP signal surfaces, stable typed errors/results, protocol v12, documentation, and coverage.
Implementation commits: 85f3dde, 96f9b99.
Merged to main in 008f114.
Verification: every AC command and task ci passed; independent verifier passed AC#1–AC#7, including production MCP delivery.
<!-- SECTION:FINAL_SUMMARY:END -->
