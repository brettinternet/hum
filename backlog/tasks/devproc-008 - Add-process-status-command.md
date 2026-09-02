---
id: DEVPROC-008
title: Add process status command
status: To Do
assignee: []
created_date: '2026-09-02 17:07'
updated_date: '2026-09-02 20:05'
labels:
  - cli
  - process
  - protocol
milestone: m-0
dependencies:
  - DEVPROC-007
modified_files:
  - internal/app/
  - internal/protocol/
  - internal/cli/
priority: high
type: feature
ordinal: 800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: after the lifecycle slice is green, clients can inspect one project-scoped process without fetching logs or mutating daemon state.

Scope: `devproc status <name> [--json]`; application and protocol query; human output; stable JSON containing name, project root, PID, process-group ID, cwd, argv array, start time, state, and exit status when available; clear not-found and invalid-name errors. Status never auto-starts the daemon; if unavailable it returns `Start it with devproc serve --daemon.`

Non-goals: wait semantics, log content, polling, persistence, metrics, daemon auto-start, or MCP.

Modified-file contract: internal/app/, internal/protocol/, internal/cli/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/app ./internal/protocol ./internal/cli -run Status` exits 0 and covers running, exited, signaled, invalid-name, missing-process, and unavailable-daemon states.
- [ ] #2 Against the integration fixture, `devproc status api --json` decodes with exact argv, PID/PGID, cwd, RFC3339 start time, running state, and nullable exit status without changing process or cursor state.
- [ ] #3 After fixture exit, human and JSON status report the terminal state and exit status; unknown names fail with a typed error, while an unavailable daemon fails without starting one and includes `Start it with devproc serve --daemon.`
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
