---
id: DEVPROC-009
title: Add cursor-based wait command
status: To Do
assignee: []
created_date: '2026-09-02 17:07'
updated_date: '2026-09-02 20:13'
labels:
  - cli
  - output
  - protocol
milestone: m-0
dependencies:
  - DEVPROC-007
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

Scope: `devproc wait <name> [--after-cursor N] [--match REGEX] [--timeout DURATION] [--json]`; `--after-cursor` defaults to 0 so retained output is searched first; subscribe internally to append/exit transitions without polling races; return a new cursor and distinguish matched, exited, and timed_out outcomes in human and stable JSON output; exit codes 0 when the awaited condition happened (a match when `--match` is given, otherwise exit), 2 on timeout, 3 when the process exited before matching, and 1 for errors; release waiters on cancellation/disconnection; validate cursors, regexes, durations, names, and server bounds. Wait never auto-starts the daemon; if unavailable it returns `Start it with devproc serve --daemon.`

Non-goals: streaming output (`logs --follow` owns that behavior), shell retries, persistence, daemon auto-start, or MCP.

Modified-file contract: internal/output/, internal/app/, internal/protocol/, internal/cli/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/output ./internal/app -run Wait` exits 0 and proves buffered-first matching from cursor 0 by default, no missed append race, exit wakeup, timeout, cancellation, and monotonic returned cursors.
- [ ] #2 `go test ./internal/protocol ./internal/cli -run Wait` exits 0 and covers optional after-cursor, optional regex/timeout, stable matched/exited/timed_out JSON, human output, exit codes 0/2/3/1, validation errors, and unavailable-daemon guidance without auto-start.
- [ ] #3 The built-binary integration scenario proves one wait returns 0 immediately for buffered matching output, one blocks until new matching output then returns 0, one returns exited with code 3 when matching, one without `--match` returns 0 on exit, one returns timed_out with code 2 and a new cursor, and an unavailable daemon returns `Start it with devproc serve --daemon.` without creating runtime state.
- [ ] #4 `go test -race ./internal/output ./internal/app ./internal/daemon` exits 0 with concurrent append, exit, timeout, disconnect, log followers, and multiple waiters.
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
