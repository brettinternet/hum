---
id: DRAFT-002
title: Answer a prompt with a one-shot input write from the CLI and MCP
status: Draft
assignee: []
created_date: '2026-09-05 14:24'
labels:
  - cli
  - daemon
  - protocol
  - docs
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum input <name> --text 'y\n'` (and an MCP `input` tool with the same semantics) writes bytes to a running tty session's terminal once and returns, so a coding agent that sees a prompt in bounded `logs` can answer it without holding an unbounded attached run.

Shape: acquire the HUM-022 input lease for the duration of the write and release it; fail with the existing typed conflict when an attached owner holds the lease; target the current launch cursor so a stale write never reaches a successor incarnation; bounded payload; text and base64 forms; no retention or echo of the input in logs. Non-tty sessions reject input with an actionable message pointing at `tty: true`/`--tty`.

Non-goals: PTY resize over MCP, streaming input, multiple owners, input over MCP for non-tty sessions.

Depends on HUM-022.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->
