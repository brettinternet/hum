---
id: HUM-069
title: Make the complete test suite race-clean
status: Done
assignee: []
created_date: '2026-09-10 01:51'
updated_date: '2026-09-10 08:46'
labels:
  - tooling
dependencies: []
modified_files:
  - internal/cli/tty.go
  - internal/cli/tty_test.go
  - internal/cli/serve_run_test.go
  - cmd/hum/integration_test.go
  - internal/testutil/harness.go
  - Taskfile.dist.yaml
  - .github/workflows/ci.yaml
priority: high
type: bug
ordinal: 45700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `go test -race ./...` passes locally and CI runs that complete race surface. Evidence: the full race run detects unsynchronized `exec.Cmd` reads in `cmd/hum/integration_test.go` and `internal/cli/serve_run_test.go`, plus a production race between `ttyInput.start` reading `os.File.Fd` and the forwarding goroutine closing that file. The current `task race` excludes CLI, integration, MCP, protocol, and command packages, so CI misses these failures. Scope: fix the TTY ownership race, make process-test harnesses synchronize completion state, and expand the race task to all packages with any necessary deterministic test timeouts. Non-goals: do not skip race-sensitive tests, serialize the entire suite to hide races, or weaken lifecycle assertions.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `mise exec go -- go test -race ./... -count=1` exits 0 with no `WARNING: DATA RACE` output.
- [x] #2 `task race` exits 0 and its logged Go package pattern is `./...`, covering command, CLI, MCP, protocol, and integration packages.
- [x] #3 `task ci` exits 0 after running the expanded race target.
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
AC#1 PASS — `mise exec go -- go test -race ./... -count=1` exited 0 with no `WARNING: DATA RACE` output.
AC#2 PASS — `task race` exited 0 and logged `mise exec go -- go test -race ./...`, covering every package.
AC#3 PASS — `task ci` exited 0 after checks, all tests, the expanded race target, build, and smoke.
Independent verifier — PASS for AC#1, AC#2, and AC#3; confirmed no tests were deleted, skipped, or weakened and the tooling label authorizes the Taskfile gate change.
Modified-file deviation — `internal/cli/list_logs_test.go` now waits for the second bounded follow event before cancellation because the expanded race run exposed its premature single-event cancellation; the lifecycle assertion remains unchanged. The authoritative task file changed to record claim, evidence, completion, and release required by repository workflow.
Review — traced cached TTY descriptor ownership, every affected asynchronous command waiter, interrupt lifecycle timing under the race runtime, bounded follow cancellation, and the CI race invocation; no item-scoped defects remain.
Delivery — included in the final commit containing this completion record.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: reproduced with `mise exec go -- go test -race ./... -count=1` (exit 1, ~80s for internal/cli). Four DATA RACE reports: three test-side (cmd/hum/integration_test.go:308,400,558-559; internal/cli/serve_run_test.go:1092,1131,1965,1999-2000, all unsynchronized exec.Cmd reads) and one production race between `ttyInput.start` reading stdin.Fd (internal/cli/tty.go:108) and the forwarding goroutine closing it (tty.go:228). Failing tests: TestBuiltBinaryIntegration, TestAttachedRunInterruptLifecycle, TestTTYCLI. Labelled tooling because Taskfile.dist.yaml and .github/workflows/ci.yaml are gate files.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Cache TTY descriptor state before concurrent shutdown, synchronize child-process completion in integration harnesses, make race-sensitive lifecycle tests deterministic, and run the race detector across every package.
<!-- SECTION:FINAL_SUMMARY:END -->
