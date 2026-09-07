---
id: HUM-055
title: Attach to a running session without starting it
status: Done
assignee: []
created_date: '2026-09-06 19:09'
updated_date: '2026-09-07 06:08'
labels:
  - cli
  - tty
  - integration
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/serve_run_test.go
  - internal/cli/run_args_test.go
  - internal/cli/surface_test.go
  - internal/cli/flag_alias_test.go
  - README.md
  - docs/design.md
priority: medium
type: enhancement
ordinal: 32700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a human-facing `hum attach NAME` command joins an already-running supervision session without starting, restarting, or otherwise changing process lifecycle. It provides the explicit connect semantic needed by shells and terminal UIs such as Herdr while `hum run` retains its existing start-or-attach behavior.

Scope: reuse the existing attached-session stream and TTY input lease. A running TTY target receives raw input and resize forwarding under the existing single-owner rules; a running non-TTY target follows output without gaining input. Add `--tail N` to bound retained replay before live output, with `--tail 0` suppressing replay. Missing and stopped targets return an actionable error and remain unchanged. Keep this human-facing: MCP continues to use bounded logs, wait, and one-shot input.

Why now: `hum run NAME` can launch a stopped declaration using the caller environment, so an external UI cannot safely label it Attach. Overmind demonstrates that explicit, lifecycle-independent attachment is a useful operator primitive.

Non-goals: launching or restarting; command argv; one-shot command execution or exit-code propagation; a Herdr dependency or pane management; tmux integration; a new MCP tool; changes to retained-output storage.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/cli -run '^TestAttachRunningSession$' -count=1 -v` exits 0 and prints PASS, proving attach joins a running TTY session, forwards raw input and resize under the existing exclusive lease, detaches without stopping the child, and gives a second attachment output without input ownership.
- [x] #2 `go test ./internal/cli -run '^TestAttachNeverStartsSession$' -count=1 -v` exits 0 and prints PASS, proving missing and stopped names return actionable errors without launching, restarting, or changing the retained record.
- [x] #3 `go test ./internal/cli -run '^TestAttachTail$' -count=1 -v` exits 0 and prints PASS for exact final-N retained replay in source order, `--tail 0` suppressing replay while preserving live output, non-negative validation before daemon contact, and no retention mutation.
- [x] #4 `go test ./internal/cli -run '^TestAttachSurface$' -count=1 -v` exits 0 and prints PASS for command help and documentation that distinguish read-only log following, non-starting attach, and start-or-attach run, including copy-pasteable examples for an external terminal UI.
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
- [x] Inspect existing run attachment, stream, TTY lease, and CLI argument patterns.
- [x] Add `hum attach NAME [--tail N]` without lifecycle mutation, reusing stream and TTY ownership behavior.
- [x] Add focused integration, argument, and surface tests plus README/design documentation.
- [x] Run each acceptance command and task ci; obtain independent review and verifier evidence.
- [x] Commit in the worktree, merge to main, record final backlog evidence, and clean up.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Scope contract updated: internal/cli/flag_alias_test.go must list the new root attach command and its tail alias; the existing exact command/flag surface gate otherwise rejects the required command.

AC#1 PASS — `go test ./internal/cli -run "^TestAttachRunningSession$" -count=1 -v` passed on the final implementation.
AC#2 PASS — `go test ./internal/cli -run "^TestAttachNeverStartsSession$" -count=1 -v` passed; after integration with terminal-state changes, the stopped-state assertion was aligned in commit fa14b25.
AC#3 PASS — `go test ./internal/cli -run "^TestAttachTail$" -count=1 -v` passed; the test covers multi-page >64 KiB replay, replay-before-live ordering, tail zero, pre-contact validation, and unchanged retention.
AC#4 PASS — `go test ./internal/cli -run "^TestAttachSurface$" -count=1 -v` passed.
AC#5 PASS — `task ci` passed on final main commit fa14b25, including vet, staticcheck, all tests, race tests, build, and smoke.
Independent verifier PASS — no validated findings after iterative concurrency review; AC1-AC5 and DoD scope/test-integrity/protected-file checks passed.
Delivery — implementation commit f29fdf2, merge commit 3835033, integration-alignment commit fa14b25. `task check:staged` passed before commits. No tests were deleted, skipped, or weakened; no protected gate files changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added `hum attach NAME [--tail N]` as a lifecycle-independent running-session connection with exclusive TTY input/resize ownership, exact paged retained replay, buffered live handoff, actionable missing/stopped errors, dedicated help, tests, and docs. All exact acceptance tests and final `task ci` passed; independent verification returned PASS. Merged to main in 3835033 with final integration adjustment fa14b25.
<!-- SECTION:FINAL_SUMMARY:END -->
