---
id: HUM-046
title: Wait for readiness in restart with a timeout
status: Done
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-07 03:42'
labels:
  - cli
  - mcp
milestone: m-4
dependencies:
  - HUM-035
modified_files:
  - internal/cli/commands.go
  - internal/cli/restart_test.go
  - internal/cli/flag_alias_test.go
  - internal/cli/help_contract_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - docs/design.md
priority: medium
type: enhancement
ordinal: 23700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum restart NAME...` waits for each replacement incarnation to become ready, or running_unverified when no matcher exists. --timeout is a positive per-name duration measured from that name launch; --no-wait returns after spawn. Readiness failures are reported per name and remaining names continue, while request/validation errors stop subsequent restarts. Aggregate exit precedence is 1 request/error, then 3 exited_before_ready, then 2 timed_out, then 0 success. MCP restart exposes the same single-name result fields.

Scope: reuse the shared HUM-035 readiness/classification path after each successful replacement. CLI results contain name, outcome, readiness, pid, launch_cursor, and message when applicable. MCP structured content and text carry the equivalent fields. Help and docs state timing, continuation, and exit behavior.

Why now: restart is the drift-recovery path but currently returns while readiness is still starting, so users and agents cannot know whether the adopted definition actually recovered.

Non-goals: changing definition adoption, adding after dependency ordering, changing stop mechanics, or adding whole-invocation timeout.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/cli -run '^TestRestartWaitsForReadiness$' -count=1 -v` exits 0 and prints PASS for ready, running_unverified, exited_before_ready, timed_out, and --no-wait with timeout measured independently from each launch.
- [x] #2 `go test ./internal/cli -run '^TestRestartMixedOutcomes$' -count=1 -v` exits 0 and prints PASS, proving readiness failures do not skip later names, request errors do, results preserve input order, and exit precedence is 1 > 3 > 2 > 0.
- [x] #3 `go test ./internal/mcp -run '^TestRestartWaitsForReadiness$' -count=1 -v` exits 0 and prints PASS, proving name, outcome, readiness, pid, launch_cursor, and optional message match the CLI semantics.
- [x] #4 `go test ./internal/cli -run '^TestRestartReadinessDocs$' -count=1 -v` exits 0 and prints PASS for --timeout/--no-wait help plus docs/design.md timing, continuation, and exit behavior.
- [x] #5 `task ci` exits 0.
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
1. Reuse shared readiness observation after restart and expose exact result fields.
2. Add per-name timeout/no-wait behavior plus aggregate continuation and exit precedence.
3. Cover mixed outcomes, MCP parity, help/docs, and final gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed for implementation in task/hum-046 worktree.

AC#1 PASS — go test ./internal/cli -run '^TestRestartWaitsForReadiness$' -count=1 -v exited 0; final verifier also passed it 20/20, including retained empty matchers.
AC#2 PASS — go test ./internal/cli -run '^TestRestartMixedOutcomes$' -count=1 -v exited 0.
AC#3 PASS — go test ./internal/mcp -run '^TestRestartWaitsForReadiness$' -count=1 -v exited 0, including text/structured parity and explicit timeout_ms=0 rejection.
AC#4 PASS — go test ./internal/cli -run '^TestRestartReadinessDocs$' -count=1 -v exited 0.
AC#5 PASS — task ci exited 0 on final commit e50b1fa after rebasing onto main.
Independent verifier: PASS for AC#1-#5 and DoD #3-#6. Adversarial review findings for retained readiness matchers and empty regex matchers were fixed and reverified.
Modified-file contract: exact; only the seven declared paths changed. No tests were deleted, skipped, or weakened; no protected gate files changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented per-name restart readiness waiting for CLI and MCP, including --timeout/--no-wait, ordered continuation, aggregate exit precedence, structured result parity, retained readiness matchers, help/docs, and focused coverage. Merged commit: e50b1fa.
<!-- SECTION:FINAL_SUMMARY:END -->
