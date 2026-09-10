---
id: HUM-007
title: Prove daemon lifecycle and observation end to end
status: Done
assignee:
  - '@agent'
created_date: '2026-09-02 17:06'
updated_date: '2026-09-03 11:22'
labels:
  - integration
  - tooling
  - security
milestone: m-0
dependencies:
  - HUM-006
  - HUM-011
modified_files:
  - integration/
  - internal/testutil/
  - Taskfile.dist.yaml
  - .github/workflows/ci.yaml
priority: high
type: task
ordinal: 700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the lifecycle/run/log/stop/shutdown slice, including detached daemon startup, is exercised through the built binary on macOS and Linux before status, wait, restart, or skill work may begin.

Scope: deterministic fixture processes (including one that prints its cwd and selected environment variables and one that spawns a grandchild); foreground and detached daemon harnesses isolated through `HUM_RUNTIME_DIR`; readiness/PID/socket reporting; detached standard-stream/session isolation; bounded daemon log; idempotent and concurrent auto-start; stale PID/socket recovery; version-mismatch replacement and refusal; exact-argv attached and detached launch with client cwd and environment inheritance; live separate stdout/stderr; attached exit status, SIGINT forwarding, and detach-on-SIGTERM; client disconnect/reconnect; multiple bounded followers and cancellation; NDJSON follow events; incremental cursor continuation, truncation, regex, and stream filtering; observed exit status; stop; shutdown refusal and graceful all-process tree termination.

Non-goals: implementing status, wait, restart, or the skill, benchmarks, persistence, PTY or arbitrary stdin, Windows, OS-service installation, or MCP.

Modified-file contract: integration/, internal/testutil/, Taskfile.dist.yaml, .github/workflows/ci.yaml.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./integration -run 'TestForegroundServe|TestDetachedServe|TestAutomaticStartup|TestVersionMismatch' -count=1` exits 0 and proves foreground operation, detached readiness/PID/socket reporting and terminal isolation, idempotency, concurrent run startup, stale artifact recovery, idle-daemon replacement on version mismatch, and clear startup failures.
- [x] #2 `go test ./integration -run 'TestAttachedRun|TestDetachedRun|TestReconnect' -count=1` exits 0 and proves client cwd and environment inheritance, live stdout/stderr, managed exit codes, Ctrl+C process-group forwarding, SIGTERM detaching the client without stopping the process, detached name/PID output, and attached-client loss without managed-process termination.
- [x] #3 `go test ./integration -run 'TestLogFollowers|TestNDJSONFollow' -count=1` exits 0 and proves initial bounded filters, cursor delivery, multiple followers, eviction reporting, cancellation without process termination, and operation after the original run client disconnects.
- [x] #4 `go test ./integration -run 'TestStopTree|TestShutdown' -count=1` exits 0 and proves stop removes child/grandchild trees, default shutdown refuses and lists `<project root>: <name>` entries, and `--stop-processes` uses SIGTERM/grace/SIGKILL before daemon exit.
- [x] #5 `test "$(grep -c "runs-on: macos-latest" .github/workflows/ci.yaml)" -ge 1 && test "$(grep -c "runs-on: ubuntu-latest" .github/workflows/ci.yaml)" -ge 1 && test "$(grep -c "task ci" .github/workflows/ci.yaml)" -ge 2` exits 0 and proves the committed GitHub Actions workflow defines macOS and Linux jobs that each invoke `task ci`; successful remote job results are owner-confirmed after task completion and are not an acceptance or completion gate.
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
1. Add a reusable built-binary harness and deterministic standalone fixtures under internal/testutil/ and integration/.
2. Add exact top-level integration tests for daemon lifecycle/version handling, attached and detached run/reconnect, log followers/NDJSON, and process-tree stop/shutdown.
3. Update GitHub Actions so separate macOS and Linux jobs each run task ci; keep Taskfile changes minimal and only if integration coverage needs explicit wiring.
4. Run all five acceptance commands, task ci, independent verification, and adversarial review; fix every item-scoped defect.
5. Record evidence, commit the branch, merge to main, rerun task ci on main, and remove the worktree and branch.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed for implementation in an isolated worktree on 2026-09-03.

AC#1 evidence: `mise exec go -- go test ./integration -run 'TestForegroundServe|TestDetachedServe|TestAutomaticStartup|TestVersionMismatch' -count=1` exited 0 (`ok hum/integration 14.672s`).
AC#2 evidence: `mise exec go -- go test ./integration -run 'TestAttachedRun|TestDetachedRun|TestReconnect' -count=1` exited 0 (`ok hum/integration 6.596s`).
AC#3 evidence: `mise exec go -- go test ./integration -run 'TestLogFollowers|TestNDJSONFollow' -count=1` exited 0 (`ok hum/integration 2.247s`).
AC#4 evidence: `mise exec go -- go test ./integration -run 'TestStopTree|TestShutdown' -count=1` exited 0 (`ok hum/integration 5.555s`).
AC#5 evidence: the exact committed-workflow grep command exited 0; `.github/workflows/ci.yaml` defines explicit `ubuntu-latest` and `macos-latest` jobs, each invoking literal `task ci`.
Gate evidence: `task ci` exited 0; integration passed in 30.150s and all project packages passed.
Independent verifier: PASS for AC#1 through AC#5 and overall. Adversarial final diff review: PASS.
Modified-file deviation: `internal/daemon/server.go` is intentionally changed. The new built-binary TestShutdown exposed a deterministic shutdown acknowledgement race: forced teardown completed but Server.Serve returned before the request goroutine encoded success, so the CLI exited 1 with EOF. A shutdown-response-only barrier now preserves the protocol acknowledgement before process exit. No test was deleted, skipped, or weakened. The task carries the tooling label, authorizing the workflow gate change.

Implementation commit: b33e78e. Merge commit on main: 8937f92.
Final-commit evidence: `task ci` exited 0 on b33e78e.
Merged-main evidence: `task ci` exited 0 on 8937f92; integration passed in 28.435s and every project package passed.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-03 01:32
---
Owner decision: implementing and locally inspecting the macOS/Linux GitHub Actions workflow completes AC#5. Agents must not request push or Actions permissions, wait for remote CI, or require the remote jobs to pass. The owner will confirm remote results and create a follow-up issue for failures.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added built-binary macOS/Linux integration coverage for daemon lifecycle, automatic startup, run/reconnect, logs/followers, process-tree stop, and shutdown; added deterministic fixtures and dual-platform CI. The tests exposed and fixed a shutdown acknowledgement race. All five acceptance commands, final-commit and merged-main task ci, independent verification, and adversarial review passed. Implementation b33e78e; merged as 8937f92.
<!-- SECTION:FINAL_SUMMARY:END -->
