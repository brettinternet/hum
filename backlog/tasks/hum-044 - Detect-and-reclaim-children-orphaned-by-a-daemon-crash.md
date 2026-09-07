---
id: HUM-044
title: Detect and reclaim children orphaned by a daemon crash
status: Done
assignee:
  - '@brett'
created_date: '2026-09-06 16:15'
updated_date: '2026-09-07 00:06'
labels:
  - daemon
  - process
milestone: m-4
dependencies:
  - HUM-037
modified_files:
  - internal/daemon/runtime.go
  - internal/daemon/runtime_test.go
  - internal/daemon/server.go
  - internal/daemon/wire_protocol.go
  - internal/daemon/wire_protocol_test.go
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/process/process.go
  - internal/process/process_test.go
  - internal/process/identity_linux.go
  - internal/process/identity_darwin.go
  - internal/process/identity_linux_test.go
  - internal/process/identity_darwin_test.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/ergonomics_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/lifecycle_test.go
  - docs/design.md
priority: high
type: bug
ordinal: 21700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: after an ungraceful daemon death, the next daemon start deterministically reclaims every positively identified surviving process group with the normal TERM/grace/KILL sequence before accepting launch requests. It never adopts old children and never signals a process whose recorded identity no longer matches. list, status, and up expose what was reclaimed or left unresolved; up cannot start a duplicate while a matching orphan remains alive.

Scope: atomically maintain a mode-0600 runtime-state file containing format version, daemon identity, and for each live group its project root, name, leader PID, PGID, and OS process-start identity. Linux reads start identity from procfs; macOS reads it from the native process table in build-tagged implementations. After spawning a child, persist its complete identity before reporting launch success. If persistence fails, run TERM/grace/KILL and wait for terminal reconciliation; return the persistence error only after the group is gone. If cleanup cannot be confirmed, fail closed with an unresolved in-memory record that blocks a same project/name launch. Normal terminal removal updates state; graceful shutdown removes it.

On startup after the recorded daemon is confirmed dead, verify leader PID, PGID leadership, and start identity before signaling. Mismatch or unverifiable identity is never signaled and remains unresolved, blocking a same project/name launch. Successful reclamation removes the entry. State writes use same-directory temp, sync, close, rename, and cleanup. Corrupt state fails closed with an error naming the file and required operator action.

Developer experience: the daemon retains one startup-reconciliation summary for its lifetime. Human list/status/up write one concise warning to stderr per invocation. JSON list/status objects and MCP list/status/up object envelopes contain a top-level warnings array. CLI up NDJSON emits one typed warning event before per-process result records. Each warning contains project, name, outcome reclaimed or unresolved, and message. A clean start emits no warning.

Why now: an orphan can retain a port while Hum reports stopped and launches a duplicate, producing an unfindable address-in-use failure.

Non-goals: adoption, retained output/history, signaling identity mismatches, daemon-upgrade handoff with live children, Windows, or best-effort unsafe PGID cleanup.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/daemon -run '^TestRuntimeStateFile$' -count=1 -v` exits 0 and prints PASS for mode 0600, versioned content, atomic launch/exit updates, same-directory replacement, temp cleanup, actionable corrupt-state failure, and graceful-shutdown removal.
- [x] #2 `go test ./internal/daemon -run '^TestLaunchPersistenceFailureStopsChild$' -count=1 -v` exits 0 and prints PASS, proving launch success is not returned before durable identity state; persistence failure terminates and reaps the new group, while unconfirmed cleanup creates an unresolved blocker rather than an untracked launch.
- [x] #3 `go test ./internal/process -run '^TestProcessStartIdentity$' -count=1 -v` exits 0 and prints PASS on the host OS, and the repository cross-platform build task compiles the Linux and Darwin identity implementations.
- [x] #4 `go test ./internal/daemon -run '^TestStartupReclaimsRecordedGroups$' -count=1 -v` exits 0 and prints PASS for TERM success, grace-expired KILL, stale dead entries, and exact PID/PGID/start-identity verification before any signal.
- [x] #5 `go test ./internal/daemon -run '^TestStartupNeverSignalsReusedProcessIdentity$' -count=1 -v` exits 0 and prints PASS, proving recycled PID/PGID or unverifiable identity receives no signal, remains unresolved, and blocks a duplicate project/name launch.
- [x] #6 `go test ./internal/cli ./internal/mcp -run '^TestStartupReconciliationWarnings$' -count=1 -v` exits 0 and prints PASS for one human stderr warning, top-level JSON/MCP warnings arrays, one leading CLI up NDJSON warning event, daemon-lifetime visibility, and no warning after a clean start.
- [x] #7 `go test ./integration -run '^TestDaemonCrashReclaimsOrphans$' -count=1 -v` exits 0 and prints PASS: after daemon SIGKILL, the next up leaves exactly one child per declaration; no old child remains alive; and list never reports stopped while a matching orphan is alive.
- [x] #8 `task ci` exits 0.
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
1. Define and atomically maintain the versioned runtime-state identity records.
2. Reconcile only positively matched groups before serving; block duplicates for unresolved matches.
3. Propagate stable reconciliation warnings through protocol, CLI, MCP, integration tests, and docs.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Started implementation in isolated worktree; selected by task backlog:next.

Implementation commit e0abe14.
AC#1 PASS — go test ./internal/daemon -run ^TestRuntimeStateFile$ -count=1 -v.
AC#2 PASS — go test ./internal/daemon -run ^TestLaunchPersistenceFailureStopsChild$ -count=1 -v.
AC#3 PASS — go test ./internal/process -run ^TestProcessStartIdentity$ -count=1 -v; Linux amd64 and Darwin arm64 go test -c cross-compiles also passed.
AC#4 PASS — go test ./internal/daemon -run ^TestStartupReclaimsRecordedGroups$ -count=1 -v.
AC#5 PASS — go test ./internal/daemon -run ^TestStartupNeverSignalsReusedProcessIdentity$ -count=1 -v.
AC#6 PASS — go test ./internal/cli ./internal/mcp -run ^TestStartupReconciliationWarnings$ -count=1 -v; verifier also exercised human status error warnings and MCP error envelopes.
AC#7 PASS — go test ./integration -run ^TestDaemonCrashReclaimsOrphans$ -count=1 -v.
AC#8 PASS — task ci, including race tests and built-binary smoke coverage.
Independent verifier PASS for AC1-AC8 after its status-error warning finding was fixed and reverified. No tests were deleted, skipped, or weakened.
Modified-file deviation: internal/daemon/client.go captures daemon-lifetime warning metadata from wire responses; internal/cli/mcp.go exposes it through the MCP client adapter; internal/mcp/server.go adds warnings to MCP success and error envelopes. These glue changes are required to satisfy AC#6.

Merged to main as 07ccb40; post-merge task ci PASS.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented durable process-group identity state and safe daemon-crash reconciliation. Verified all focused AC commands, Linux/Darwin identity cross-compiles, full task ci, crash recovery integration, CLI/MCP warning propagation, and an independent verifier pass. Implementation commit: e0abe14.

Merged to main as 07ccb40 and re-ran task ci successfully.
<!-- SECTION:FINAL_SUMMARY:END -->
