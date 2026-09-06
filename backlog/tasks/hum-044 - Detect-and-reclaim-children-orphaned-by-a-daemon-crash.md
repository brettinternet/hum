---
id: HUM-044
title: Detect and reclaim children orphaned by a daemon crash
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - daemon
  - process
milestone: m-4
dependencies: []
modified_files:
  - internal/daemon/runtime.go
  - internal/daemon/server.go
  - internal/app/app.go
  - internal/process/process.go
  - internal/daemon/daemon_test.go
  - integration/lifecycle_test.go
  - docs/design.md
priority: high
type: bug
ordinal: 21700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: after the daemon dies without shutting down (kill -9, OOM, panic), the next daemon start finds the process groups the dead daemon was supervising, terminates them with the normal TERM/grace/KILL sequence (or adopts them, if adoption proves feasible), and `hum list`/`hum up` report what happened. `hum list` never reports `stopped` for a child that is demonstrably still running under init.

Why now (reproduced 2026-09-06): `hum up`, `kill -9 <daemon>`, then `hum list` says `svc stopped`, `hum down` says nothing is running, and `hum up` starts a duplicate while the orphan keeps its port, producing an unfindable EADDRINUSE. The runtime dir already holds hum.pid/hum.ready for the dead daemon, so a crash is detectable.

Scope: record supervised pgids in a small runtime-dir state file (runtime state, not persistent history) and reconcile it on startup; surface a one-line warning through `list`, `status`, and `up` when reclamation happened.

Non-goals: persisting output or launch history, surviving daemon upgrades with live children, Windows.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./integration -run '^TestDaemonCrashReclaimsOrphans$' -count=1 -v` exits 0 and prints PASS: after SIGKILL of the daemon, the next `hum up` leaves exactly one child per declaration and no orphan from the previous daemon is alive.
- [ ] #2 `go test ./internal/daemon -run '^TestStartupReclaimsRecordedGroups$' -count=1 -v` exits 0 and prints PASS.
- [ ] #3 `task ci` exits 0.
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
