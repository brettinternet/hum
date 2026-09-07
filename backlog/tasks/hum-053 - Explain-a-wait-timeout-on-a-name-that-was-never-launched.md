---
id: HUM-053
title: Explain a wait timeout on a name that was never launched
status: Done
assignee:
  - '@brett'
created_date: '2026-09-06 16:59'
updated_date: '2026-09-07 09:08'
labels:
  - cli
  - mcp
  - daemon
  - protocol
milestone: m-4
dependencies: []
modified_files:
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/daemon/server.go
  - internal/daemon/server_test.go
  - internal/daemon/client.go
  - internal/daemon/daemon_test.go
  - internal/daemon/wire_protocol.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/mcp.go
  - internal/cli/mcp_test.go
  - internal/cli/wait_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - docs/design.md
priority: low
type: enhancement
ordinal: 30700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: when CLI or MCP wait times out, the result includes process_observed. It is false only when no runtime record for NAME existed at any point during that wait; human output then adds: no process named "NAME" was observed during the wait; check the name or start it first. It is true when a record existed initially or appeared during the wait, including a record later stopped or removed. Exit code remains 2.

Scope: track observation inside the single daemon wait request and return the boolean on both true and false timeout paths. Add the field to CLI JSON and MCP structured content; successful match/exit results may omit it because the process was necessarily observed. Waiting for undeclared names remains supported.

Why now: a typo and a legitimately slow process currently produce the same silent timeout, often after the 30-second default. The daemon has the authoritative lifecycle view and can explain the distinction without an extra client round trip.

Non-goals: preflight rejection, extra get requests, changing timeouts/exit codes, renaming wait outcome, or requiring a manifest declaration.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/app ./internal/daemon -run '^TestWaitProcessObserved$' -count=1 -v` exits 0 and prints PASS for no record, an initial record, a record launched during the wait, and a record observed then removed before timeout.
- [x] #2 `go test ./internal/protocol -run '^TestWaitProcessObservedRoundTrip$' -count=1 -v` exits 0 and prints PASS for explicit true and false timeout values and unchanged successful result encoding.
- [x] #3 `go test ./internal/cli -run '^TestWaitTimeoutExplainsNeverObserved$' -count=1 -v` exits 0 and prints PASS for exact actionable human text, JSON process_observed false/true, empty stderr in JSON mode, and unchanged exit code 2.
- [x] #4 `go test ./internal/mcp -run '^TestWaitTimeoutExplainsNeverObserved$' -count=1 -v` exits 0 and prints PASS for the same boolean definition and guidance in structured content/text.
- [x] #5 `go test ./internal/cli -run '^TestWaitObservedDocs$' -count=1 -v` exits 0 and prints PASS for docs/design.md and CLI help naming process_observed and the no-extra-round-trip behavior.
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
1. Track a keyed monotonic runtime-observation generation across record replacement and use it within one daemon wait request.
2. Propagate explicit timeout process_observed values through daemon wire protocol, protocol v13, CLI JSON/human rendering, and MCP structured content/text.
3. Cover never-observed, initially observed, launched during wait, observed/removed, replacement-record churn, compatibility, guidance, and docs.
4. Run focused acceptance commands, independent verification, and task ci before and after merge.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed by @brett for implementation in worktree hum-053-wait-observed.

Implementation commit: 84d1c36; merged to main as b2350a7.
Review: adversarial review found and implementation fixed protocol-version compatibility and record-replacement observation gaps.
AC#1: `go test ./internal/app ./internal/daemon -run '^TestWaitProcessObserved$' -count=1 -v` passed, including no record, initial record, launch during wait, removal, and replacement-record churn.
AC#2: `go test ./internal/protocol -run '^TestWaitProcessObservedRoundTrip$' -count=1 -v` passed for explicit true/false timeout values and unchanged match/exit encoding.
AC#3: `go test ./internal/cli -run '^TestWaitTimeoutExplainsNeverObserved$' -count=1 -v` passed for actionable human text, JSON booleans, empty JSON stderr, and exit code 2.
AC#4: `go test ./internal/mcp -run '^TestWaitTimeoutExplainsNeverObserved$' -count=1 -v` passed for structured content and guidance text.
AC#5: `go test ./internal/cli -run '^TestWaitObservedDocs$' -count=1 -v` passed for design docs and CLI/MCP help.
AC#6: `task ci` passed on 84d1c36 and again on merged main b2350a7.
Independent verifier: PASS for AC#1-AC#6; protocol v13 rejects a v12 daemon before wait rendering.
Modified-file deviation: internal/protocol/restart_policy_test.go updates the existing protocol-version invariant from 12 to required version 13; the verifier confirmed this is strictly required. No tests were deleted, skipped, or weakened, and no protected gate files changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented daemon-authoritative wait observation across runtime record replacement, explicit protocol v13 timeout encoding, CLI/MCP guidance, and documentation. Focused AC tests, independent verification, and task ci passed before and after merge. Delivered in 84d1c36 and merged as b2350a7.
<!-- SECTION:FINAL_SUMMARY:END -->
