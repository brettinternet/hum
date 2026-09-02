---
id: DEVPROC-012
title: Add restart command
status: To Do
assignee: []
created_date: '2026-09-02 20:13'
updated_date: '2026-09-02 20:27'
labels:
  - cli
  - process
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
ordinal: 950
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: after the lifecycle slice is green, a human or agent can restart one managed process by name without knowing its command, and followers and cursors remain valid across the restart.

Scope: `hum restart <name> [--json]`; an application service that runs the stop sequence (SIGTERM, bounded grace, SIGKILL if needed), then relaunches the recorded argv, cwd, and environment under the same name while the name stays reserved; new PID and start time and an incremented restart count; the output sequence continues without cursor reset and records a system entry marking the restart; existing `logs --follow` clients observe the marker and the new output; a process that already exited can be restarted; human output and stable JSON with name, pid, restarts, and cursor; clear not-found and invalid-name errors. Restart never auto-starts the daemon; if unavailable it returns `Start it with hum serve --daemon.`

Non-goals: manifest-driven relaunch (next milestone), changing argv/cwd/env, automatic restart on crash, backoff policies, or MCP.

Modified-file contract: internal/output/, internal/app/, internal/protocol/, internal/cli/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/app ./internal/protocol ./internal/cli -run Restart` exits 0 and covers running and exited processes, name reservation during restart, identical argv/cwd/env relaunch, incremented restart count, invalid-name, missing-process, and unavailable-daemon states.
- [ ] #2 `go test ./internal/output -run TestSystemEntry` exits 0 and proves a restart marker is appended as a system entry with a monotonic cursor visible to bounded reads and followers.
- [ ] #3 Against the integration fixture, `hum restart api --json` returns a new PID with restarts incremented, a follower started before the restart prints the marker followed by the first line of the new process, and `hum logs api --after-cursor <pre-restart cursor>` returns output spanning both incarnations.
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
