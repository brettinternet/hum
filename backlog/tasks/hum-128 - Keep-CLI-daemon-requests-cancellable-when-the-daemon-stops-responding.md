---
id: HUM-128
title: Keep CLI daemon requests cancellable when the daemon stops responding
status: To Do
assignee: []
created_date: '2026-09-23 21:29'
labels:
  - cli
  - daemon
  - security
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/*_test.go
  - docs/design.md
priority: medium
type: bug
ordinal: 11000
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
- [ ] #1 AC1 — `go test ./internal/cli -run "^TestUnresponsiveDaemonHonorsTermination$" -count=1 -v` exits 0; against a same-user listener that completes hello and get but never answers start, `hum run NAME --detach -- true` exits non-zero within 2 s of SIGTERM, and separately within 2 s of SIGHUP, with stderr naming the unanswered daemon request.
- [ ] #2 AC2 — `go test ./internal/cli -run "^TestTerminationStopRequestIsBounded$" -count=1 -v` exits 0; when an attached `hum run` receives SIGTERM and the daemon never answers the resulting stop request, the command exits non-zero no later than the effective stop grace plus the fixed slack.
- [ ] #3 AC3 — `go test ./integration -run "^TestAttachedRunForegroundLifecycle$" -count=1 -v` exits 0, including its existing "SIGTERM stops the incarnation" and "SIGHUP detaches and retained logs remain readable" subtests.
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
