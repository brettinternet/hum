---
id: DEVPROC-009
title: Add cursor-based wait command
status: To Do
assignee: []
created_date: '2026-09-02 17:07'
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
Outcome: after the first vertical slice is green, clients can wait efficiently for existing or new matching output, process exit, or a deadline without receiving unsolicited logs.

Scope: `devproc wait <name> --after-cursor N [--match REGEX] [--timeout DURATION] [--json]`; check retained output before blocking; subscribe internally to append/exit transitions without polling races; return a new cursor and distinguish matched, exited, and timed_out outcomes in human and stable JSON output; release waiters on cancellation/disconnection; validate cursors, regexes, durations, names, and server bounds.

Non-goals: streaming output, follow mode, shell retries, proactive injection, persistence, or MCP.

Modified-file contract: internal/output/, internal/app/, internal/protocol/, internal/cli/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/output ./internal/app -run Wait` exits 0 and proves buffered-first matching, no missed append race, exit wakeup, timeout, cancellation, and monotonic returned cursors.
- [ ] #2 `go test ./internal/protocol ./internal/cli -run Wait` exits 0 and covers required after-cursor, optional regex/timeout, stable matched/exited/timed_out JSON, human output, and clear validation errors.
- [ ] #3 The built-binary integration scenario proves one wait returns immediately for buffered matching output, one blocks until new matching output, one returns exited, and one returns timed_out with a new cursor.
- [ ] #4 `go test -race ./internal/output ./internal/app ./internal/daemon` exits 0 with concurrent append, exit, timeout, disconnect, and multiple waiters.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
- [ ] #7 Committed on main with the task ID in the commit subject and a Task: trailer
<!-- DOD:END -->
