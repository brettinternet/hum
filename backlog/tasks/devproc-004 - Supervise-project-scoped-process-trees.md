---
id: DEVPROC-004
title: Supervise project-scoped process trees
status: To Do
assignee: []
created_date: '2026-09-02 17:05'
updated_date: '2026-09-02 17:34'
labels:
  - process
  - output
milestone: m-0
dependencies:
  - DEVPROC-002
  - DEVPROC-003
modified_files:
  - internal/app/
  - internal/process/
priority: high
type: feature
ordinal: 400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: application services start, inspect, signal, and stop named processes while the daemon—not any client connection—owns their lifecycle.

Scope: narrow `internal/app` services over `internal/process`; nearest Git-root discovery with cwd fallback; names unique within a project root; safe-name validation; direct argv execution without shell reconstruction; separate stdout/stderr capture; recorded PID, process-group ID, cwd, argv, start time, state, and exit status; SIGINT forwarding to an attached process group; single-process stop and all-process shutdown using SIGTERM, a bounded grace period, then SIGKILL only when necessary; concurrency-safe state transitions. A client disconnect without an explicit signal does not terminate its managed process.

Non-goals: sockets, protocol encoding, CLI rendering, persistence across daemon restart, PTY or arbitrary stdin forwarding, Windows, retries, or plugins.

Modified-file contract: internal/app/, internal/process/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/process` exits 0 and proves exact argv/cwd execution, separate stream capture, exit status, process-group creation, and SIGINT group forwarding.
- [ ] #2 `go test ./internal/app -run TestProjectScopedNames` exits 0 and proves nearest Git-root scoping, cwd fallback, safe-name rejection, duplicate-running-name rejection, and same-name isolation across project roots.
- [ ] #3 `go test ./internal/app -run 'TestStopProcessTree|TestShutdownProcessTrees'` exits 0 and proves SIGTERM targets each full group, the grace period is bounded, SIGKILL is used only when necessary, and all processes reach terminal state before daemon shutdown.
- [ ] #4 `go test -race ./internal/app ./internal/process` exits 0 during concurrent start, output, signal, disconnect, exit, list, stop, and shutdown transitions.
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
