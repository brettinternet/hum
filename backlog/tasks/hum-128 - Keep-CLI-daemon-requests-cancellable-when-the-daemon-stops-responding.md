---
id: HUM-128
title: Keep CLI daemon requests cancellable when the daemon stops responding
status: Done
assignee: []
created_date: '2026-09-23 21:29'
updated_date: '2026-09-24 06:50'
labels:
  - cli
  - daemon
  - security
  - reviewed
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/*_test.go
  - docs/design.md
priority: medium
type: bug
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a CLI command waiting on a daemon response that never arrives exits promptly on SIGTERM or SIGHUP, and requests deliberately sent after cancellation are bounded, so a wedged same-user daemon cannot leave hum processes that only SIGKILL ends.

Evidence (security audit, 2026-09-23): cmd/hum/main.go turns SIGTERM and SIGHUP into command-context cancellation, which replaces their default terminate action, but internal/cli/commands.go sends 14 unary daemon requests with context.Background(): Start (742-747), Follow setup (789, 1254, 1476), InputAttach (809), ControlSignal (898), Stop (904-906, 2278), Get (934), Remove (2361), and Shutdown (2890). Against a same-user socket that answers hello and get but never answers start, `hum run demo --detach -- /usr/bin/true` was still blocked 3 s after SIGTERM on macOS and needed SIGKILL; on Linux under `timeout 10` it was still blocked after 5 minutes.

Scope:
- Requests made while the command runs observe the command context, so SIGTERM or SIGHUP cancels them with an error naming the request and a non-zero exit.
- Requests deliberately sent after cancellation (the attached-run SIGTERM stop bridge at commands.go:898-906 and cleanup Stop, Remove, and Shutdown) still go out after cancellation, but under a deadline of the effective stop grace plus fixed slack instead of an unbounded context.
- Established follow and attach streams keep their current behavior.

Non-goals: daemon-side changes; MCP, whose requests are already cancellable per call; new flags or configuration; SIGINT handling.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `go test ./internal/cli -run "^TestUnresponsiveDaemonHonorsTermination$" -count=1 -v` exits 0; against a same-user listener that completes hello and get but never answers start, `hum run NAME --detach -- true` exits non-zero within 2 s of SIGTERM, and separately within 2 s of SIGHUP, with stderr naming the unanswered daemon request.
- [x] #2 AC2 — `go test ./internal/cli -run "^TestTerminationStopRequestIsBounded$" -count=1 -v` exits 0; when an attached `hum run` receives SIGTERM and the daemon never answers the resulting stop request, the command exits non-zero no later than the effective stop grace plus the fixed slack.
- [x] #3 AC3 — `go test ./integration -run "^TestAttachedRunForegroundLifecycle$" -count=1 -v` exits 0, including its existing "SIGTERM stops the incarnation" and "SIGHUP detaches and retained logs remain readable" subtests.
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
1. Route live CLI unary requests through command context; bound attached launch handoff and post-cancel stop/remove/shutdown by grace plus slack.
2. Add stalled-daemon SIGTERM/SIGHUP tests for detached start and attached stop, preserving follow semantics.
3. Run focused and CI checks, independent verification, commit, merge to main, finalize task and remove owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation commits 76f71e7 and 3f8ca46 fast-forwarded to main. Attached-run startup retains signal handoff under a bounded independent context; all other in-flight unary calls observe cancellation. Stop, remove, shutdown, up cleanup and down workers use deadline-bound calls.
AC#1: go test ./internal/cli -run "^TestUnresponsiveDaemonHonorsTermination$" -count=1 -v — PASS for SIGTERM and SIGHUP (independent verifier).
AC#2: go test ./internal/cli -run "^TestTerminationStopRequestIsBounded$" -count=1 -v — PASS (independent verifier); tightened timing margin to 400ms and reran focused tests twice, PASS.
AC#3: go test ./integration -run "^TestAttachedRunForegroundLifecycle$" -count=1 -v — PASS including SIGTERM and SIGHUP (independent verifier and post-fix local run).
Review: one independent verifier pass returned PASS on AC1–AC3 but flagged unbounded down worker Stop. Corrected in 76f71e7 before final gate; added TestDownStalledStopHonorsTermination in 3f8ca46; focused test passed twice. No second general review. Initial task ci had unrelated timing failure in TestAttachStreamsBurstWithoutAborting; focused rerun passed. task ci passed on final commit 3f8ca46, including race, vet, staticcheck, smoke. Diff limited to declared paths; no tests removed or protected gate files changed. Next step: mark acceptance/DoD, finalize and release claim, remove owned worktree.

Review (e838d39): live CLI requests are no longer time-bounded by CLI grace (remove and shutdown --stop-processes failed when admitted stop_grace exceeded it); only post-cancellation cleanup is bounded. down shares one cleanup deadline (longest active grace) across remaining waves (TestDownTerminationSharesOneCleanupDeadline). Follow, input attach, and signal setup errors now name the request. No follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Bound CLI daemon requests on termination, preserved attached-run signal handoff and added stalled-daemon regression coverage. AC1–AC3 and task ci passed on commits 76f71e7/3f8ca46, fast-forwarded to main.
<!-- SECTION:FINAL_SUMMARY:END -->
