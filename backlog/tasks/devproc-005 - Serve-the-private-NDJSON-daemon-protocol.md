---
id: DEVPROC-005
title: Serve the private NDJSON daemon protocol
status: To Do
assignee: []
created_date: '2026-09-02 17:06'
updated_date: '2026-09-02 20:05'
labels:
  - daemon
  - protocol
  - security
milestone: m-0
dependencies:
  - DEVPROC-004
modified_files:
  - internal/protocol/
  - internal/daemon/
priority: high
type: feature
ordinal: 500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: local clients can invoke and observe application services over a user-private Unix socket, coordinate one daemon instance, and shut it down without coupling the daemon core to CLI rendering.

Scope: internal NDJSON request/response and streaming event types; bounded request decoding; stable typed errors; readiness handshake; one long-running daemon owning application state; client calls independent of connection lifetime; multiple bounded log-follow streams with cursor/eviction events; signal-forward and shutdown operations; socket, PID, startup-lock, and bounded/rotating `daemon.log` paths in the configured runtime directory; directory mode 0700 and socket mode 0600; stale PID/socket recovery that never displaces a live daemon; locking that serializes concurrent startup attempts; graceful server shutdown. Enforce server-side request/output limits and reject malformed messages, invalid cursors, and unsupported operations.

Non-goals: public protocol compatibility, TCP, authentication, TLS, CLI process detachment, MCP, product CLI rendering, operating-system service installation, launchd, systemd, or login startup.

Modified-file contract: internal/protocol/, internal/daemon/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/protocol` exits 0 and covers NDJSON round trips, streaming log events, readiness, signal/shutdown operations, malformed/oversized requests, typed errors, and stable field names.
- [ ] #2 `go test ./internal/daemon -run TestPrivateSocket` exits 0 and proves 0700 runtime-directory and 0600 socket permissions for XDG and temporary fallbacks.
- [ ] #3 `go test ./internal/daemon -run 'TestClientDisconnect|TestMultipleFollowers'` exits 0 and proves a disconnected attached client leaves the managed process running, followers are independent, and later clients can reconnect.
- [ ] #4 `go test ./internal/daemon -run 'TestSocketOwnership|TestStaleRuntimeRecovery|TestConcurrentStartup'` exits 0 and proves one live daemon cannot be displaced, stale PID/socket artifacts recover safely, and concurrent startup attempts select exactly one daemon.
- [ ] #5 `go test ./internal/daemon -run 'TestReadinessHandshake|TestShutdown'` exits 0 and proves readiness is reported only after the socket accepts requests, default shutdown refuses with named active processes, and forced shutdown waits for process-tree termination before removing runtime artifacts.
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
