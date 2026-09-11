---
id: HUM-084
title: Allow logs to select daemon system entries
status: Done
assignee: []
created_date: '2026-09-10 20:36'
updated_date: '2026-09-10 21:55'
labels:
  - output
  - protocol
  - cli
  - integration
  - docs
dependencies: []
modified_files:
  - README.md
  - docs/design.md
  - docs/coding-agents.md
  - internal/cli/commands.go
  - internal/cli/list_logs_test.go
  - internal/cli/help_contract_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - internal/daemon/wire_protocol_test.go
  - integration/logs_test.go
priority: medium
type: enhancement
ordinal: 58800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: humans and coding agents can select only hum-generated supervision entries with `--stream system` or MCP `stream: "system"`, yielding a lifecycle-oriented view without child stdout or stderr noise.

Context: hum already records launch, restart, recovery, and other daemon boundaries in the existing system stream and includes them in ordinary combined reads. The daemon protocol already supports selecting that stream, but CLI validation and help reject it and the MCP logs tool exposes no stream selector.

Selection contract: CLI logs accepts `stdout`, `stderr`, `system`, or `both` for single-process, aggregate, bounded, and follow forms because they share the same existing selector. MCP logs accepts the same enum for its bounded single-process read. Omission continues to mean `both`; `both` continues to include stdout, stderr, and system. Invalid values fail before daemon contact. Cursor, since, tail, match, entry, byte, truncation, and aggregate per-process behavior are unchanged.

Code map: output.System and output.SystemMask already exist; protocol.StreamSystem and daemon `streamMask` already map `system` to that mask, so no daemon production change or protocol-version bump is expected. CLI logs has two validation sites and one shared flag description in internal/cli/commands.go. MCP commonInput and the logs tool schema/request in internal/mcp/tools.go currently omit stream. Existing renderers already preserve the entry stream name.

Scope: expose the existing selector through CLI and MCP; cover bounded, aggregate, follow, JSON, schema, help, and documentation behavior. Keep current defaults and raw system-entry rendering.

Non-goals: a separate event store or events command, changing lifecycle emission, typed lifecycle metadata, arbitrary stream combinations, MCP aggregate or follow tools, flag-value shell completion, changing session ownership, or changing the daemon's lenient internal fallback for unknown stream values.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/daemon -run 'SystemStream' -count=1 -v` exits 0 and prints PASS, proving the existing daemon selector returns only retained system entries while stdout, stderr, both, cursors, since, match, and bounds remain unchanged.
- [x] #2 `go test ./internal/cli -run 'SystemStream' -count=1 -v` exits 0 and prints PASS for single and aggregate bounded reads, single and aggregate follow requests, JSON rendering with `stream: "system"`, help listing all four values, omitted/both compatibility, and invalid-stream rejection before daemon contact.
- [x] #3 `go test ./internal/mcp -run 'SystemStream' -count=1 -v` exits 0 and prints PASS, proving the logs schema advertises the four-value stream enum, omission sends `both`, `system` returns only system entries with unchanged bounded metadata, and an invalid value is rejected before daemon contact.
- [x] #4 `go test ./integration -run '^TestLogsSystemStream$' -count=1 -v` exits 0 and prints PASS, proving `--stream system` returns retained launch/restart boundaries and excludes child stdout/stderr for bounded and follow reads while omitted and explicit `--stream both` remain equivalent.
- [x] #5 `task ci` exits 0 after README.md, docs/design.md, docs/coding-agents.md, CLI help, and MCP tool descriptions document system as the supervision-only stream and state that both still includes all three concrete streams.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented in 3faf4be (merged fast-forward to main).

AC#1: go test ./internal/daemon -run SystemStream -count=1 -v passed.

AC#2: go test ./internal/cli -run SystemStream -count=1 -v passed.

AC#3: go test ./internal/mcp -run SystemStream -count=1 -v passed.

AC#4: go test ./integration -run ^TestLogsSystemStream$ -count=1 -v passed.

AC#5: task ci passed on final commit 3faf4be.

Independent verifier: PASS for AC#1-AC#5; confirmed exactly the 10 declared paths changed, tests were additive, and no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added CLI and MCP system-stream selection with validation, schema/help/docs coverage, and daemon, CLI, MCP, and integration tests. Merged commit 3faf4be to main.
<!-- SECTION:FINAL_SUMMARY:END -->
