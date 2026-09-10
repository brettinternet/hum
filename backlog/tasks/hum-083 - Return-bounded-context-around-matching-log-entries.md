---
id: HUM-083
title: Return bounded context around matching log entries
status: To Do
assignee: []
created_date: '2026-09-10 20:36'
updated_date: '2026-09-10 20:38'
labels:
  - output
  - protocol
  - cli
  - integration
  - docs
dependencies: []
references:
  - 'https://github.com/nc9/taskmux'
modified_files:
  - docs/design.md
  - docs/coding-agents.md
  - internal/output/types.go
  - internal/output/ring.go
  - internal/output/ring_test.go
  - internal/output/store.go
  - internal/output/store_test.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/daemon/wire_protocol.go
  - internal/daemon/wire_protocol_test.go
  - internal/cli/commands.go
  - internal/cli/list_logs_test.go
  - internal/cli/help_contract_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/logs_test.go
priority: medium
type: enhancement
ordinal: 57800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: bounded log reads can include a requested number of retained entries before and after each regex match, making errors and stack traces diagnosable without a second broad log read. CLI and MCP expose the same option and preserve hum cursor, filtering, and byte-bound guarantees.

Context: hum currently filters to matching entries only. That often removes the stack frames or causal lines adjacent to an error and forces agents to issue wider follow-up reads. Context windows should be selected from the same immutable retained snapshot, merged when they overlap, and returned in source cursor order.

Scope: add one symmetric context-entry count to bounded single-process and aggregate reads; define its position in the existing filter order; deduplicate overlapping windows; preserve consumed-cursor, truncation, eviction, entry-limit, and byte-limit semantics; expose it through CLI, JSON, MCP, help, and docs.

Non-goals: multiline regex matching, time-based context, changing retained-log capacity, relevance ranking, or buffering delayed after-context for live followers. Reject context with follow rather than introducing unbounded follower state.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 go test ./internal/output -run "^TestReadMatchContext$" -count=1 -v exits 0 and prints PASS for before/after selection, boundary clipping, overlapping-window deduplication, source order, multiple matches, and no-match results.
- [ ] #2 go test ./internal/output ./internal/daemon -run "MatchContext" -count=1 -v exits 0 and prints PASS, proving context remains subject to entry and byte limits and reports stable next, more, truncated, oldest, latest, and evicted-through metadata.
- [ ] #3 go test ./internal/cli ./internal/mcp -run "MatchContext" -count=1 -v exits 0 and prints PASS for CLI/MCP parity, aggregate per-process bounds, non-negative validation, context-without-match rejection, and context-with-follow rejection.
- [ ] #4 go test ./integration -run "^TestLogsMatchContext$" -count=1 -v exits 0 and prints PASS for hum logs api --match ERROR --context 2 returning only the merged bounded context windows.
- [ ] #5 task ci exits 0 after CLI help, MCP descriptions, docs/design.md, and docs/coding-agents.md document context filtering and its bounded-read-only restriction.
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
