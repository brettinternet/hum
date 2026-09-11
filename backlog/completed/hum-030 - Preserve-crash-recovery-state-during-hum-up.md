---
id: HUM-030
title: Preserve crash recovery state during up reconciliation
status: Done
assignee: []
created_date: '2026-09-06 04:57'
updated_date: '2026-09-06 13:15'
labels:
  - cli
  - mcp
  - reliability
milestone: m-3
dependencies: []
modified_files:
  - internal/cli/manifest.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/manifest_test.go
  - internal/cli/restart_policy_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - internal/mcp/restart_policy_test.go
  - integration/relaunch_test.go
  - README.md
  - docs/design.md
priority: high
type: bug
ordinal: 5700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum up` and MCP `up` report an existing declaration’s automatic recovery without resetting its backoff or five-attempt budget.

Why now: broad reconciliation currently calls start for an exited record. The supervisor correctly treats start as an explicit override, so an otherwise harmless repeated `up` can cancel a pending timer or revive an exhausted crash loop.

Scope: when a resolved name has exited with `next_launch_at`, `up` returns `recovery_pending`; when its `restart: on-failure` budget is exhausted, `up` returns `recovery_exhausted`. Both outcomes preserve and expose runtime state, normalized restart policy, relaunch count, and `next_launch_at`, issue no start request, and make CLI `up` exit 3 because the declaration is not running. Repeated `up` does not move the deadline, consume an attempt, create an incarnation, or wait for an automatic successor. CLI `start NAME`, MCP `start`, and CLI/MCP `restart` remain explicit overrides that cancel pending recovery and launch immediately.

Docs: CLI help, README.md, and docs/design.md document the two outcomes, exit behavior, bounded observation, and targeted override.

Non-goals: changing retry delays, the stability window, the five-attempt limit, supervisor timer behavior, stopped records without automatic recovery, or targeted lifecycle semantics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `go test ./internal/cli -run "^TestUpPreservesCrashRecovery$" -count=1 -v` exits 0 and prints `--- PASS: TestUpPreservesCrashRecovery`. It proves pending and exhausted CLI `up` results use the exact outcomes and fields, exit 3, and send no start request.
- [x] #2 AC2 — `go test ./internal/mcp -run "^TestUpPreservesCrashRecovery$" -count=1 -v` exits 0 and prints `--- PASS: TestUpPreservesCrashRecovery`. It proves the equivalent MCP contract and that neither recovery state sends a start request.
- [x] #3 AC3 — `go test ./integration -run "^TestUpPreservesPendingRecovery$" -count=1 -v` exits 0 and prints `--- PASS: TestUpPreservesPendingRecovery`. Against the built binary and real daemon, repeated CLI and MCP `up` calls preserve one pending deadline and incarnation, while targeted start and restart still launch immediately.
- [x] #4 AC4 — `go test ./internal/cli -run "^TestUpRecoveryDocs$" -count=1 -v` exits 0 and prints `--- PASS: TestUpRecoveryDocs`. It proves CLI help, README.md, and docs/design.md state the recovery outcomes, exit behavior, no-successor wait, and targeted override.
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
- [x] T1 — Classify pending and exhausted recovery before the up path can issue a start request.
- [x] T2 — Render equivalent CLI and MCP outcomes while preserving targeted start and restart overrides.
- [x] T3 — Add real-daemon regression coverage and document the reconciliation contract.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation started in isolated worktree; selected by task backlog:next.

Implementation: 8fde34f (merged to main via 7949ca3).
AC#1 PASS — go test ./internal/cli -run "^TestUpPreservesCrashRecovery$" -count=1 -v; exits 0 and prints PASS.
AC#2 PASS — go test ./internal/mcp -run "^TestUpPreservesCrashRecovery$" -count=1 -v; exits 0 and prints PASS.
AC#3 PASS — go test ./integration -run "^TestUpPreservesPendingRecovery$" -count=1 -v; exits 0 and prints PASS.
AC#4 PASS — go test ./internal/cli -run "^TestUpRecoveryDocs$" -count=1 -v; exits 0 and prints PASS.
Verification: independent verifier passed AC1–AC4 and changed-file/no-test-weakening checks. Reviewer found one stale design-doc claim; corrected it and extended TestUpRecoveryDocs.
Gate: task ci passed on final merge commit 7949ca3. Changed files are all within the declared contract; no tests were deleted, skipped, or weakened; no protected gate files changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Preserved pending and exhausted automatic recovery during CLI/MCP up reconciliation, exposed normalized recovery metadata and exit behavior, retained targeted overrides, added unit/integration/doc coverage, and merged the verified implementation to main.
<!-- SECTION:FINAL_SUMMARY:END -->
