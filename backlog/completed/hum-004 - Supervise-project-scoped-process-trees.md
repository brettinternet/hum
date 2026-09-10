---
id: HUM-004
title: Supervise project-scoped process trees
status: Done
assignee: []
created_date: '2026-09-02 17:05'
updated_date: '2026-09-03 03:20'
labels:
  - process
  - output
milestone: m-0
dependencies:
  - HUM-002
  - HUM-003
modified_files:
  - internal/app/
  - internal/process/
priority: high
type: feature
ordinal: 400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: application services start, inspect, signal, stop, and retire named processes while the daemon, not any client connection, owns their lifecycle; every child runs with the requesting client's cwd and environment, and completed records cannot retain output or secret-bearing environments without bound.

Scope: narrow `internal/app` services over `internal/process`; project-root discovery walking up to the nearest `.git` directory or `.git` file (linked worktree) with cwd fallback; names unique within a project root; safe-name validation; direct argv execution without shell reconstruction; the child inherits exactly the cwd and environment supplied by the client (never the daemon's) with stdin attached to /dev/null; separate stdout/stderr capture appended into the process's HUM-003 output sequence; a launch record with project root, PID, process-group ID, cwd, argv, environment, start time, launch cursor, state, exit status, and restart count, where the environment is retained only for relaunch and excluded from every read model; SIGINT forwarding to a process group; single-process stop and all-process shutdown using SIGTERM, a bounded grace period, then SIGKILL only when necessary; duplicate-running-name rejection that names the running PID; concurrency-safe state transitions. A client disconnect without an explicit signal does not terminate its managed process. Active records are never evicted. After each terminal transition, evict the oldest completed records above `HUM_COMPLETED_RECORDS` across all project roots, clear their stored environments, and release their output buffers.

Non-goals: sockets, protocol encoding, CLI rendering, restart (HUM-012), persistence across daemon restart, PTY or arbitrary stdin forwarding, Windows, retries, or plugins.

Modified-file contract: internal/app/, internal/process/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/process` exits 0 and proves exact argv execution, that the child observes the supplied cwd and environment rather than the test process's, /dev/null stdin, separate stream capture, exit status, process-group creation, and SIGINT group forwarding.
- [x] #2 `go test ./internal/app -run TestProjectScopedNames` exits 0 and proves nearest Git-root scoping for both `.git` directories and `.git` worktree files, cwd fallback, safe-name rejection, duplicate-running-name rejection naming the PID, and same-name isolation across project roots.
- [x] #3 `go test ./internal/app -run 'TestStopProcessTree|TestShutdownProcessTrees'` exits 0 and proves SIGTERM targets each full group, the grace period is bounded, SIGKILL is used only when necessary, and all processes reach terminal state before daemon shutdown.
- [x] #4 `go test ./internal/app -run 'TestReadModelsExcludeEnvironment|TestCompletedRecordRetention'` exits 0 and proves no read model exposes an environment, active records are never evicted, terminal records are evicted oldest-first at the configured global bound, and eviction clears the environment and releases buffered output.
- [x] #5 `go test -race ./internal/app ./internal/process` exits 0 during concurrent start, output, signal, disconnect, exit, retention eviction, list, stop, and shutdown transitions.
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
Implementation commit: 2315865 feat: supervise project process trees

AC#1 — `go test ./internal/process` passed. Covers direct argv, exact cwd/client environment, /dev/null stdin, stdout/stderr capture and partial flush, exit status, PGID lifecycle, SIGINT forwarding, and escaped-holder bounds.
AC#2 — `go test ./internal/app -run TestProjectScopedNames` passed. Covers nearest `.git` directory/file roots, lexical cwd fallback, safe names, duplicate PID errors, and cross-root isolation.
AC#3 — `go test ./internal/app -run 'TestStopProcessTree|TestShutdownProcessTrees'` passed. Covers full-group TERM, bounded grace, conditional KILL, ESRCH races, and all-terminal shutdown.
AC#4 — `go test ./internal/app -run 'TestReadModelsExcludeEnvironment|TestCompletedRecordRetention'` passed. Covers environment-free models, active retention, global terminal-time eviction, environment clearing, and output release.
AC#5 — `go test -race ./internal/app ./internal/process` passed. A repeated focused escaped-holder race run also passed: `go test -race ./internal/process -run TestWaitDoesNotFollowEscapedCaptureHolder -count=10`.

Final gate — `task ci` passed on commit 2315865 (gofmt, go vet, staticcheck, and `go test ./...`).
Independent verification — PASS for AC#1 through AC#5; adversarial app and process lifecycle reviews completed with all item-scoped findings corrected.
Modified-file contract — only `internal/app/` and `internal/process/` changed. No tests were deleted, skipped, or weakened; no protected gate files changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented concurrency-safe project-scoped process supervision with exact client launch context, bounded ordered output capture, process-group signaling and shutdown, environment-free read models, and global completed-record retention. All acceptance commands, race coverage, task ci, LSP diagnostics, and independent verification passed on commit 2315865.
<!-- SECTION:FINAL_SUMMARY:END -->
