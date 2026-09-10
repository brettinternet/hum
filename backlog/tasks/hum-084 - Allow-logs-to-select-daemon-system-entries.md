---
id: HUM-084
title: Allow logs to select daemon system entries
status: To Do
assignee: []
created_date: '2026-09-10 20:36'
updated_date: '2026-09-10 20:45'
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
Outcome: humans and coding agents can request only hum-generated supervision entries with --stream system or the equivalent MCP field, yielding a bounded lifecycle-oriented view without application stdout or stderr noise.

Context: hum already stores launch, restart, recovery, and other daemon-generated boundaries in the existing system stream and includes them in ordinary combined reads, but the documented CLI and MCP surfaces only advertise stdout, stderr, and both. Exposing the existing selector provides most of an event-history workflow without another database or command family.

Code map: output.System and output.SystemMask exist, protocol.StreamSystem is the string system, and streamMask in internal/daemon/wire_protocol.go already maps system to SystemMask, so no daemon code change is expected. The CLI logs --stream flag is shared by single and aggregate reads in internal/cli/commands.go; both validation sites accept only stdout, stderr, and both, and the flag usage text omits system. The MCP logs tool input schema has no stream field at all. Shell completion (internal/cli/completion.go) completes process names only, so there is no flag-value completion to update.

Scope: accept system at both CLI validation sites and in the flag usage text; add an optional stream field (enum stdout, stderr, system, both; default both) to the MCP logs tool; preserve current default and both behavior; support single and aggregate bounded reads; update the help contract, MCP schema, rendering contracts, and human and agent documentation.

Non-goals: a separate event store or events command, changing lifecycle emission, typed lifecycle metadata, selecting multiple arbitrary stream combinations, changing follow/session ownership, flag-value shell completion, or changing the daemon's lenient mapping of unknown stream names to all streams.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 go test ./internal/daemon -run "SystemStream" -count=1 -v exits 0 and prints PASS, proving system selection returns only retained daemon entries while stdout, stderr, both, cursors, and bounds remain unchanged.
- [ ] #2 go test ./internal/cli -run "SystemStream" -count=1 -v exits 0 and prints PASS for hum logs NAME --stream system, aggregate hum logs --stream system, JSON rendering, help text listing system, and invalid-stream rejection.
- [ ] #3 go test ./internal/mcp -run "SystemStream" -count=1 -v exits 0 and prints PASS, proving the logs tool schema advertises stream with system, an omitted stream keeps both behavior, and stream system returns only system entries with unchanged bounded metadata.
- [ ] #4 go test ./integration -run "^TestLogsSystemStream$" -count=1 -v exits 0 and prints PASS, proving --stream system returns the retained launch or restart boundary and excludes child stdout and stderr while --stream both is unchanged.
- [ ] #5 task ci exits 0 after README.md, docs/design.md, and docs/coding-agents.md document system as the supervision-only stream and state that both keeps its existing behavior.
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
