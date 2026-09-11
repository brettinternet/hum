---
id: HUM-083
title: Return bounded context around matching log entries
status: Done
assignee: []
created_date: '2026-09-10 20:36'
updated_date: '2026-09-10 22:43'
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
  - internal/cli/flag_alias_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/logs_test.go
priority: medium
type: enhancement
ordinal: 57800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a bounded log read can return a symmetric number of retained entries before and after every regex match, so errors and stack traces are diagnosable without a second broad read. CLI and MCP expose the same option and preserve cursor, time, stream, tail, entry, and byte bounds.

Context: hum currently filters to matching entries only. That removes adjacent stack frames or causal lines and forces agents to issue wider follow-up reads. Context must be selected from one immutable retained snapshot so concurrent appends cannot change window membership during a request.

Selection contract: `context` is a non-negative entry count, defaults to 0, requires a non-empty `match`, and is valid only for bounded reads. First apply the exclusive after cursor, immutable since cutoff, and stream selector to determine eligible source entries. Evaluate the regex on those entries, expand each match by up to `context` eligible entries on each side, merge overlapping or adjacent windows, and keep source cursor order without duplicates. Context never crosses the after, since, stream, oldest-retained, or captured-latest boundaries. A zero context exactly preserves current match-only behavior.

Bounding contract: apply tail to the merged selected sequence, then whole-entry `max_entries` and `max_bytes` bounds. For forward reads, consume unselected source entries but set `next` immediately before the first selected match-or-context entry that could not be returned; returning with `next` as the next `after` must neither lose nor duplicate a selected entry. Preserve the current meaning of `more`, `truncated`, `oldest`, `latest`, and `evicted_through`, including empty/no-match and stale-cursor reads. Aggregate CLI reads apply the complete contract independently per process.

Surface contract: add `--context N` to CLI logs and `context` plus the currently missing `match` field to the MCP logs schema. Reject context without match and context with `--follow` before daemon contact; live followers do not buffer future after-context. Add `context` only to the bounded output wire request and bump the private protocol version. No MCP aggregate read is introduced.

Code map: output.ReadOptions is in internal/output/types.go; ring selection and bounds are in internal/output/ring.go; Store captures the immutable watermark and since cutoff in internal/output/store.go. protocol.OutputRequest and daemon conversion carry bounded read options. Single and aggregate CLI logs share flags in internal/cli/commands.go. MCP logs currently carries neither match nor context into its request.

Scope: implement the selection, bounding, cursor, protocol, CLI, aggregate CLI, MCP, help, and documentation contracts above. Preserve existing behavior when context is omitted or zero.

Non-goals: context on follow, MCP aggregate reads, multiline regex matching, time-based context counts, changing retained-log capacity, relevance ranking, or waiting for delayed after-context.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/output -run '^TestReadMatchContext$' -count=1 -v` exits 0 and prints PASS for before/after selection, clipping at after/since/stream/retention/snapshot boundaries, overlapping and adjacent window merging, cursor order without duplicates, multiple matches, zero context, and no-match results.
- [x] #2 `go test ./internal/output ./internal/protocol ./internal/daemon -run 'MatchContext' -count=1 -v` exits 0 and prints PASS for the bumped bounded-read wire contract, one immutable snapshot, tail then entry/byte bounds, stable `next`/`more`/`truncated`/`oldest`/`latest`/`evicted_through` metadata, stale cursors, and `next` stopping before the first unreturned selected entry.
- [x] #3 `go test ./internal/cli ./internal/mcp -run 'MatchContext' -count=1 -v` exits 0 and prints PASS for CLI `--context` on single and aggregate bounded reads with per-process bounds, MCP logs accepting `match` and `context` and matching CLI JSON for the same process, non-negative validation, zero-context compatibility, and context-without-match or context-with-follow rejection before daemon contact.
- [x] #4 `go test ./integration -run '^TestLogsMatchContext$' -count=1 -v` exits 0 and prints PASS for `hum logs api --match ERROR --context 2` returning only merged bounded context windows in cursor order, including composition with `--since`, `--stream`, and a continuation cursor.
- [x] #5 `task ci` exits 0 after CLI help, MCP tool descriptions, docs/design.md, and docs/coding-agents.md document match context ordering, immutable snapshot boundaries, cursor continuation, and the bounded-read-only restriction.
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
- [x] Implement immutable match-context selection and cursor/bounds semantics in output.
- [x] Carry context through protocol, daemon, CLI, aggregate CLI, and MCP with validation.
- [x] Add focused unit/integration coverage and update help/docs.
- [x] Run acceptance commands and task ci; obtain independent verifier review.
- [x] Commit in worktree, merge to main, update task evidence, and clean up.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation commit 65d6c3a merged to main.

AC#1 PASS — go test ./internal/output -run ^TestReadMatchContext$ -count=1 -v passed before/after selection, boundary clipping, merged windows, ordering, large/zero context, and no-match cases.

AC#2 PASS — go test ./internal/output ./internal/protocol ./internal/daemon -run MatchContext -count=1 -v passed protocol v18, immutable snapshot, metadata, stale cursor, bounds, and three-page continuation checks.

AC#3 PASS — go test ./internal/cli ./internal/mcp -run MatchContext -count=1 -v passed single/aggregate CLI, MCP schema/request parity, system-stream composition, zero-context compatibility, and pre-contact validation.

AC#4 PASS — go test ./integration -run ^TestLogsMatchContext$ -count=1 -v passed merged context windows with since, stream, and continuation cursor composition.

AC#5 PASS — independent verifier ran task ci; security, vet, staticcheck, full normal/race tests, build, and smoke passed. A prior local full run also passed; intermittent failures in unrelated tests were tracked by HUM-080 and passed focused reruns.

Review evidence — reviewer found trailing-context continuation and concurrent HUM-084 system-stream integration defects; both were fixed. Independent verifier then returned PASS for AC#1 through AC#5 and confirmed system/both compatibility.

Scope evidence — implementation used the declared files plus internal/cli/mcp.go for the required MCP help description and internal/protocol/codec_test.go plus internal/protocol/restart_policy_test.go for mandatory protocol-v18 invariant coverage. No test was deleted, skipped, or weakened; no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added bounded regex match context across output selection, protocol v18, daemon, single and aggregate CLI logs, MCP logs, help, and docs. Context windows use immutable eligible-entry snapshots, preserve stream/system selection and continuation cursors, and reject unsupported follow usage. All acceptance commands and independent verification passed; commit 65d6c3a merged to main.
<!-- SECTION:FINAL_SUMMARY:END -->
