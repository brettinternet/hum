---
id: HUM-009
title: Add cursor-based wait command
status: Done
assignee: []
created_date: '2026-09-02 17:07'
updated_date: '2026-09-03 14:36'
labels:
  - cli
  - output
  - protocol
milestone: m-0
dependencies:
  - HUM-007
modified_files:
  - internal/output/
  - internal/app/
  - internal/protocol/
  - internal/cli/
priority: high
type: feature
ordinal: 900
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: after the lifecycle slice is green, clients can wait efficiently for existing or new matching output, process exit, or a deadline without receiving unsolicited logs, and scripts can branch on the exit code.

Scope: `hum wait <name> [--after-cursor N] [--match REGEX] [--timeout DURATION] [--json]`; `--after-cursor` defaults to the process launch cursor (the cursor recorded at its most recent launch, which is 0 until HUM-012 introduces restart) so retained output of the current incarnation is searched first and an earlier incarnation can never satisfy a later wait; `--timeout` defaults to 30s so a caller never blocks indefinitely; command help states that without `--match` wait returns on exit and that waiting for declared readiness is `hum start <name>` (HUM-014); subscribe internally to append/exit transitions without polling races; return a new cursor and distinguish matched, exited, and timed_out outcomes in human and stable JSON output; exit codes 0 when the awaited condition happened (a match when `--match` is given, otherwise exit), 2 on timeout, 3 when the process exited before matching, and 1 for errors; release waiters on cancellation/disconnection; validate cursors, regexes, durations, names, and server bounds. Wait never auto-starts the daemon; if unavailable it fails with `Nothing is running. Start a process with hum run <name> -- <command>.`

Non-goals: streaming output (`logs --follow` owns that behavior), shell retries, persistence, daemon auto-start, readiness expressions, or MCP.

Modified-file contract: internal/output/, internal/app/, internal/protocol/, internal/cli/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/output ./internal/app -run Wait` exits 0 and proves buffered-first matching from the launch cursor by default, no missed append race, exit wakeup, timeout, cancellation, and monotonic returned cursors.
- [x] #2 `go test ./internal/protocol ./internal/cli -run Wait` exits 0 and covers optional after-cursor, optional regex, the 30s default timeout and explicit `--timeout`, help text naming exit-wait and `hum start`, stable matched/exited/timed_out JSON, human output, exit codes 0/2/3/1, validation errors, and unavailable-daemon guidance without auto-start.
- [x] #3 The built-binary integration scenario proves one wait returns 0 immediately for buffered matching output, one blocks until new matching output then returns 0, one returns exited with code 3 when matching, one without `--match` returns 0 on exit, one returns timed_out with code 2 and a new cursor, and an unavailable daemon prints `Nothing is running. Start a process with hum run <name> -- <command>.` without creating runtime state.
- [x] #4 `go test -race ./internal/output ./internal/app ./internal/daemon` exits 0 with concurrent append, exit, timeout, disconnect, log followers, and multiple waiters.
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
AC#1 `go test ./internal/output ./internal/app -run Wait` exited 0; buffered/default and explicit cursor behavior, append wakeup, exit, timeout, cancellation, large lines, concurrent waiters, and monotonic cursors passed.
AC#2 `go test ./internal/protocol ./internal/cli -run Wait` exited 0; request options, 30s/default and explicit timeouts, help, stable JSON/human outcomes, exit codes, validation, and unavailable-daemon behavior passed.
AC#3 `go test ./integration -run Wait -count=1 -timeout 120s` exited 0; built binaries proved pre-buffered and new matches, exited code 3, no-match exit success, timeout code 2/new cursor, and unavailable daemon without runtime state.
AC#4 `go test -race ./internal/output ./internal/app ./internal/daemon` exited 0; append/exit/timeout/disconnect/follower/multiple-waiter paths passed under the race detector.
Final gate: `task ci` exited 0 on final implementation commit c8abe11.
Independent verification: PASS for AC#1-AC#4. Adversarial review findings (protocol version negotiation and large retained lines) were fixed and affected gates reran green.
Scope deviation: internal/daemon/ is required for wait wire dispatch, timeout validation, disconnect cancellation, and client connection ownership; integration/wait_test.go is required for AC#3 built-binary/runtime-state proof. No protected gate file changed. No test was deleted, skipped, or weakened.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented `hum wait` with cursor-based buffered matching, append/exit subscriptions, bounded timeout and cancellation, stable human/JSON outcomes, script exit codes, daemon transport, validation, and built-binary/race coverage. Commit c8abe11; final `task ci` passed.
<!-- SECTION:FINAL_SUMMARY:END -->
