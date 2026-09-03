---
id: HUM-008
title: Add process status command
status: Done
assignee: []
created_date: '2026-09-02 17:07'
updated_date: '2026-09-03 12:55'
labels:
  - cli
  - process
  - protocol
milestone: m-0
dependencies:
  - HUM-007
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
Outcome: after the lifecycle slice is green, clients can inspect one project-scoped process without fetching logs or mutating daemon state, and agents can anchor later `wait` or `logs` calls on the returned cursor.

Scope: `hum status <name> [--json]`; application and protocol query; human output; stable JSON containing name, project root, PID, process-group ID, cwd, argv array, start time, state, exit status when available, restart count, and `next_cursor` (the sequence position after the newest retained entry); the recorded environment is never included; clear not-found and invalid-name errors. Status never auto-starts the daemon; if unavailable it fails with `Nothing is running. Start a process with hum run <name> -- <command>.` HUM-014 later adds the `readiness` field and the resolved-name variant of that message.

Non-goals: wait semantics, log content, polling, persistence, metrics, daemon auto-start, readiness, or MCP.

Modified-file contract: internal/app/, internal/protocol/, internal/cli/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/app ./internal/protocol ./internal/cli -run Status` exits 0 and covers running, exited, signaled, invalid-name, missing-process, and unavailable-daemon states.
- [x] #2 Against the integration fixture, `hum status api --json` decodes with exact argv, PID/PGID, cwd, RFC3339 start time, running state, nullable exit status, restart count, and `next_cursor`, contains no environment field, and changes neither process nor cursor state.
- [x] #3 After fixture exit, human and JSON status report the terminal state and exit status; unknown names fail with a typed error, while an unavailable daemon fails without starting one and prints `Nothing is running. Start a process with hum run <name> -- <command>.`
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
Claimed for implementation in an isolated worktree on 2026-09-03.

AC#1 evidence: `mise exec go -- go test ./internal/app ./internal/protocol ./internal/cli -run Status -count=1` exited 0; focused Status tests cover running, exited, signaled, invalid-name, missing-process, and unavailable-daemon states. Additional daemon transport coverage passed with `mise exec go -- go test ./internal/daemon -run Status -count=1`.
AC#2 evidence: `TestStatusRunningJSON`, exercised by the focused command, drove the real daemon fixture and decoded the exact argv, PID/PGID, cwd, RFC3339 start time, running state, null exit status, restart count, and one-past `next_cursor`; it asserted the exact 11-field JSON object, no environment key, repeated identical output, and unchanged process/cursor snapshots.
AC#3 evidence: `TestStatusExitedAndSignaled`, `TestStatusErrors`, `TestStatusUnavailableDoesNotStartDaemon`, and `TestStatusVersionMismatchClosesConnectionWithoutReplacement`, exercised by the focused command, passed human/JSON terminal status, typed invalid/missing errors, exact unavailable text, no runtime artifacts, and upgrade mismatch connection closure/no replacement.
Gate evidence: `task ci` exited 0 on implementation commit 2d08664; gofmt verification, go vet, staticcheck, and `go test ./...` all passed.
Independent verifier: PASS for AC#1, AC#2, AC#3 and overall. Adversarial reviews found and verified fixes for mismatch-client connection closure, legacy response-shape preservation, Get cursor presence enforcement, regression-test cleanup, and protocol upgrade negotiation; final versioning review PASS.
Modified-file deviation: `internal/output/` adds the minimal lock-protected one-past cursor accessor required to report `next_cursor` without reading/copying logs. `internal/daemon/` adds bounded Get-only cursor transport/presence enforcement and preserves start/list/stop shapes. These are necessary protocol plumbing; no unrelated behavior changed. `internal/cli/list_logs_test.go` now derives the daemon protocol version from `protocol.Version` after the required v1-to-v2 schema bump. No test was deleted, skipped, or weakened, and no protected gate file changed.
Implementation commit: 2d08664.

Merge commit on main: 76887f5.
Merged-main evidence: `task ci` exited 0 on 76887f5; gofmt verification, go vet, staticcheck, integration tests, and every Go package passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added read-only `hum status <name> [--json]` with exact human/JSON process metadata, one-past output cursor, terminal exit reporting, typed errors, and no daemon autostart or environment disclosure. Added cursor metadata plumbing and presence enforcement, preserved legacy start/list/stop response shapes, and bumped the protocol version for safe old-daemon negotiation. Focused acceptance, full tests, task ci on implementation and merged main, independent verification, and final adversarial review passed. Implementation 2d08664; merged as 76887f5.
<!-- SECTION:FINAL_SUMMARY:END -->
