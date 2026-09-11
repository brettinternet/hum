---
id: HUM-061
title: Bound the control-interrupt latch to the interrupt it belongs to
status: Done
assignee: []
created_date: '2026-09-09 21:09'
updated_date: '2026-09-09 21:25'
labels:
  - cli
  - daemon
milestone: m-4
dependencies: []
priority: medium
type: bug
ordinal: 37700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A forwarded control interrupt sets record.controlIntent (internal/app/app.go, SignalControlScoped), which is cleared only by that incarnation's exit or an explicit Start. When the child ignores SIGINT and the operator then detaches with SIGHUP, the latch survives: a later autonomous non-zero exit is treated as operator-intended and the declared `restart: on-failure` policy is silently skipped, with nothing in status explaining why.

Reproduced: a declared on-failure process with `trap '' INT` gives relaunches=0 and next_launch_at=nil after Ctrl+C then SIGHUP, while the same process without the Ctrl+C schedules a successor.

Not fixed during the HUM-058/059/060 review because the obvious narrowing (honour the latch only for a signal-terminated exit) contradicts TestControlSignalSuppressesOnFailureRestart: a child that traps SIGINT and exits 17 must still suppress relaunch. Choosing between bounding the latch in time (clear it if the child outlives the stop grace) and clearing it when the attached client detaches is a product decision.

Non-goals: changing observational `hum signal`, `Supervisor.Signal`, or MCP `signal` semantics; changing stop/down operatorStop handling.

Modified-file contract: internal/app/app.go, internal/app/app_test.go, and integration/run_reconnect_test.go if end-to-end coverage is added.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 go test ./internal/app ./internal/daemon -run ControlSignal -count=1 -v exits 0 and prints PASS, proving a trapped SIGINT exit still suppresses relaunch while a later autonomous failure after a non-fatal interrupt still schedules a successor.
- [x] #2 task ci exits 0.
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
AC#1 — `go test ./internal/app ./internal/daemon -run ControlSignal -count=1 -v` exited 0 and printed PASS; the immediate trapped-SIGINT exit suppressed relaunch and the post-grace autonomous failure scheduled and launched a successor.
AC#2 — `task ci` exited 0, including vet, staticcheck, all tests, race tests, build, and smoke coverage.
Independent verifier — PASS for AC#1 and AC#2; it also repeated the focused tests 20 times and under `-race`.
Scope — implementation touches only `internal/app/app.go` and `internal/app/app_test.go`; this task file changed only as the authoritative provider record required by the workflow. No tests were deleted, skipped, or weakened, and no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Bound forwarded control intent to the configured stop-grace window. Exits caused promptly by the control signal still suppress on-failure relaunch, while children that survive the grace resume autonomous restart-policy behavior. Added deterministic coverage for the surviving-child failure and successor launch.
<!-- SECTION:FINAL_SUMMARY:END -->
