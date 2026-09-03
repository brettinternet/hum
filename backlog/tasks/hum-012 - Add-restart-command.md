---
id: HUM-012
title: Add restart command
status: Done
assignee:
  - '@brett'
created_date: '2026-09-02 20:13'
updated_date: '2026-09-03 16:35'
labels:
  - cli
  - process
  - output
  - protocol
milestone: m-0
dependencies:
  - HUM-007
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
Outcome: after the lifecycle slice is green, a human or agent can restart managed processes by name without knowing their commands, and followers and cursors remain valid across the restart.

Scope: `hum restart <name>... [--json]`; an application service that runs the stop sequence (SIGTERM, bounded grace, SIGKILL if needed), then relaunches the recorded argv, cwd, and environment under the same name while the name stays reserved; new PID and start time and an incremented restart count; each incarnation records its launch cursor so the HUM-009 `wait` default `--after-cursor` moves to the new incarnation and pre-restart lines can never satisfy a later wait; the output sequence continues without cursor reset and records a system entry marking the restart; existing `logs --follow` clients observe the marker and the new output; a process that already exited can be restarted; several names produce one stable result per name; human output and stable JSON with name, pid, restarts, and the new launch cursor; clear not-found and invalid-name errors. Restart never auto-starts the daemon; if unavailable it fails with `Nothing is running. Start a process with hum run <name> -- <command>.`

Non-goals: manifest-driven relaunch (next milestone), changing argv/cwd/env, automatic restart on crash, backoff policies, `down`, or MCP.

Modified-file contract: internal/output/, internal/app/, internal/protocol/, internal/cli/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/app ./internal/protocol ./internal/cli -run Restart` exits 0 and covers running and exited processes, name reservation during restart, identical argv/cwd/env relaunch, incremented restart count and recorded launch cursor, several names with one result per name, invalid-name, missing-process, and unavailable-daemon states.
- [x] #2 `go test ./internal/output -run TestSystemEntry` exits 0 and proves a restart marker is appended as a system entry with a monotonic cursor visible to bounded reads and followers.
- [x] #3 Against the integration fixture, `hum restart api --json` returns a new PID with restarts incremented and the new launch cursor, a follower started before the restart prints the marker followed by the first line of the new process, `hum logs api --after-cursor <pre-restart cursor>` returns output spanning both incarnations, and `hum wait api --match <line only the first incarnation printed> --timeout 1s` after the restart times out with code 2 instead of matching the old line.
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
1. Add output system-entry coverage and implement app.Supervisor.Restart(ctx, cwd, name) as a same-record, same-store stop/relaunch that reserves the name, preserves argv/cwd/env, appends a restart marker, increments restart count, records the new launch cursor, and makes default waits ignore prior incarnations.
2. Add the restart protocol request/response and daemon transport/dispatch, including follower continuity across the intermediate exit; this necessarily touches internal/daemon/ and will be justified in Implementation Notes because no restart request can reach the supervisor otherwise.
3. Add `hum restart <name>... [--json]` with stable per-name human/JSON results and existing invalid-name, not-found, and daemon-unavailable conventions.
4. Add focused app/output/protocol/CLI tests plus an integration restart scenario covering a pre-existing follower, output spanning incarnations, and a post-restart wait that cannot match old output.
5. Run the acceptance commands, task ci, and an independent verifier; record evidence, commit, merge to main, finalize the backlog item, and remove the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1 evidence: `go test ./internal/app ./internal/protocol ./internal/cli -run Restart` exited 0; app, protocol, and CLI restart tests passed.
AC#2 evidence: `go test ./internal/output -run TestSystemEntry` exited 0; the system marker read/follower test passed.
AC#3 evidence: `go test ./integration -run Restart -count=1` exited 0; the real CLI/daemon fixture proved new PID/count/cursor, pre-existing follower continuity, spanning logs, and old-output wait timeout code 2.
DoD evidence: `task ci` exited 0 on the final branch commit c89ce2c and again on merged main; `task check:staged` exited 0 before both branch commits.
Modified-file deviation: `internal/daemon/` is required to transport and dispatch the new protocol operation and keep followers connected across the intermediate exit. `integration/restart_test.go` is required by AC#3's integration-fixture contract. No gate file or test was weakened or removed.
Independent verifier evidence: CorrectedRestartVerifier returned PASS for AC#1, AC#2, and AC#3. Adversarial review identified follower, wait-boundary, shutdown-admission, failed-restart, and retention races; fixes added explicit same-store launch boundaries, terminal-exit republishing, restart-aware eviction, Start failure re-eviction, shutdown admission locking, and deterministic regression tests. CorrectedRestartReviewer returned PASS after all findings were fixed.
Delivery: implementation commits 92cd609 and c89ce2c merged to main.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented `hum restart <name>... [--json]` end to end with preserved launch specs, same-store monotonic output, restart markers, follower continuity, incarnation-scoped waits, stable results, protocol v4, and concurrency-safe lifecycle admission/retention. AC1-AC3 and `task ci` pass on merged main; independent verifier and final adversarial review returned PASS.
<!-- SECTION:FINAL_SUMMARY:END -->
