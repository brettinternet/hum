---
id: HUM-007
title: Prove daemon lifecycle and observation end to end
status: To Do
assignee: []
created_date: '2026-09-02 17:06'
updated_date: '2026-09-03 01:32'
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
- [ ] #1 `go test ./integration -run 'TestForegroundServe|TestDetachedServe|TestAutomaticStartup|TestVersionMismatch' -count=1` exits 0 and proves foreground operation, detached readiness/PID/socket reporting and terminal isolation, idempotency, concurrent run startup, stale artifact recovery, idle-daemon replacement on version mismatch, and clear startup failures.
- [ ] #2 `go test ./integration -run 'TestAttachedRun|TestDetachedRun|TestReconnect' -count=1` exits 0 and proves client cwd and environment inheritance, live stdout/stderr, managed exit codes, Ctrl+C process-group forwarding, SIGTERM detaching the client without stopping the process, detached name/PID output, and attached-client loss without managed-process termination.
- [ ] #3 `go test ./integration -run 'TestLogFollowers|TestNDJSONFollow' -count=1` exits 0 and proves initial bounded filters, cursor delivery, multiple followers, eviction reporting, cancellation without process termination, and operation after the original run client disconnects.
- [ ] #4 `go test ./integration -run 'TestStopTree|TestShutdown' -count=1` exits 0 and proves stop removes child/grandchild trees, default shutdown refuses and lists `<project root>: <name>` entries, and `--stop-processes` uses SIGTERM/grace/SIGKILL before daemon exit.
- [ ] #5 `test "$(grep -c "runs-on: macos-latest" .github/workflows/ci.yaml)" -ge 1 && test "$(grep -c "runs-on: ubuntu-latest" .github/workflows/ci.yaml)" -ge 1 && test "$(grep -c "task ci" .github/workflows/ci.yaml)" -ge 2` exits 0 and proves the committed GitHub Actions workflow defines macOS and Linux jobs that each invoke `task ci`; successful remote job results are owner-confirmed after task completion and are not an acceptance or completion gate.
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

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-03 01:32
---
Owner decision: implementing and locally inspecting the macOS/Linux GitHub Actions workflow completes AC#5. Agents must not request push or Actions permissions, wait for remote CI, or require the remote jobs to pass. The owner will confirm remote results and create a follow-up issue for failures.
---
<!-- COMMENTS:END -->
