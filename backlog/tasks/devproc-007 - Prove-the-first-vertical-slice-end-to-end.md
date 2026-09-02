---
id: DEVPROC-007
title: Prove daemon lifecycle and observation end to end
status: To Do
assignee: []
created_date: '2026-09-02 17:06'
updated_date: '2026-09-02 20:13'
labels:
  - integration
  - tooling
  - security
milestone: m-0
dependencies:
  - DEVPROC-006
  - DEVPROC-011
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

Scope: deterministic fixture processes (including one that prints its cwd and selected environment variables and one that spawns a grandchild); foreground and detached daemon harnesses isolated through `DEVPROC_RUNTIME_DIR`; readiness/PID/socket reporting; detached standard-stream/session isolation; bounded daemon log; idempotent and concurrent auto-start; stale PID/socket recovery; version-mismatch replacement and refusal; exact-argv attached and detached launch with client cwd and environment inheritance; live separate stdout/stderr; attached exit status, SIGINT forwarding, and detach-on-SIGTERM; client disconnect/reconnect; multiple bounded followers and cancellation; NDJSON follow events; incremental cursor continuation, truncation, regex, and stream filtering; observed exit status; stop; shutdown refusal and graceful all-process tree termination.

Non-goals: implementing status, wait, restart, or the skill, benchmarks, persistence, PTY or arbitrary stdin, Windows, OS-service installation, or MCP.

Modified-file contract: integration/, internal/testutil/, Taskfile.dist.yaml, .github/workflows/ci.yaml.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./integration -run 'TestForegroundServe|TestDetachedServe|TestAutomaticStartup|TestVersionMismatch' -count=1` exits 0 and proves foreground operation, detached readiness/PID/socket reporting and terminal isolation, idempotency, concurrent run startup, stale artifact recovery, idle-daemon replacement on version mismatch, and clear startup failures.
- [ ] #2 `go test ./integration -run 'TestAttachedRun|TestDetachedRun|TestReconnect' -count=1` exits 0 and proves client cwd and environment inheritance, live stdout/stderr, managed exit codes, Ctrl+C process-group forwarding, SIGTERM detaching the client without stopping the process, detached name/PID output, and attached-client loss without managed-process termination.
- [ ] #3 `go test ./integration -run 'TestLogFollowers|TestNDJSONFollow' -count=1` exits 0 and proves initial bounded filters, cursor delivery, multiple followers, eviction reporting, cancellation without process termination, and operation after the original run client disconnects.
- [ ] #4 `go test ./integration -run 'TestStopTree|TestShutdown' -count=1` exits 0 and proves stop removes child/grandchild trees, default shutdown refuses and lists `<project root>: <name>` entries, and `--stop-processes` uses SIGTERM/grace/SIGKILL before daemon exit.
- [ ] #5 The macOS and Linux CI jobs each run `task ci` and the built-binary integration suite successfully.
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
