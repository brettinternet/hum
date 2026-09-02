---
id: DEVPROC-005
title: Serve the private NDJSON daemon protocol
status: To Do
assignee: []
created_date: '2026-09-02 17:06'
updated_date: '2026-09-02 20:13'
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
Outcome: local clients can invoke and observe application services over a user-private Unix socket, coordinate one daemon instance, detect a daemon built from an older binary, and shut it down without coupling the daemon core to CLI rendering.

Scope: internal NDJSON request/response and streaming event types; a per-connection hello carrying the protocol version, with the hello and shutdown message shapes frozen so a newer client can always retire an idle older daemon; bounded request decoding; stable typed errors; readiness handshake; one long-running daemon owning application state; start requests that carry the client's argv, cwd, and environment while no response ever echoes the environment; client calls independent of connection lifetime; multiple bounded log-follow streams with cursor/eviction events; signal-forward and shutdown operations; daemon SIGTERM/SIGINT handling that runs the stop-all sequence before exit; socket, PID, startup-lock, and bounded/rotating `daemon.log` paths in the configured runtime directory; directory mode 0700 and socket mode 0600; stale PID/socket recovery that never displaces a live daemon; locking that serializes concurrent startup attempts; graceful server shutdown. Enforce server-side request/output limits and reject malformed messages, invalid cursors, and unsupported operations.

Non-goals: public protocol compatibility beyond the frozen hello and shutdown shapes, TCP, authentication, TLS, CLI process detachment, MCP, product CLI rendering, operating-system service installation, launchd, systemd, or login startup.

Modified-file contract: internal/protocol/, internal/daemon/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/protocol` exits 0 and covers NDJSON round trips, the version-carrying hello, streaming log events, readiness, signal/shutdown operations, start requests carrying cwd and environment with no response echoing it, malformed/oversized requests, typed errors, and stable field names.
- [ ] #2 `go test ./internal/daemon -run TestPrivateSocket` exits 0 and proves 0700 runtime-directory and 0600 socket permissions for XDG and temporary fallbacks.
- [ ] #3 `go test ./internal/daemon -run 'TestClientDisconnect|TestMultipleFollowers'` exits 0 and proves a disconnected attached client leaves the managed process running, followers are independent, and later clients can reconnect.
- [ ] #4 `go test ./internal/daemon -run 'TestSocketOwnership|TestStaleRuntimeRecovery|TestConcurrentStartup'` exits 0 and proves one live daemon cannot be displaced, stale PID/socket artifacts recover safely, and concurrent startup attempts select exactly one daemon.
- [ ] #5 `go test ./internal/daemon -run 'TestReadinessHandshake|TestShutdown'` exits 0 and proves readiness is reported only after the socket accepts requests, default shutdown refuses with named active processes, and forced shutdown waits for process-tree termination before removing runtime artifacts.
- [ ] #6 `go test ./internal/daemon -run 'TestHelloVersion|TestDaemonSignal'` exits 0 and proves a mismatched hello is rejected with a typed error naming the daemon version, shutdown still succeeds against a mismatched idle daemon, and SIGTERM to the daemon stops every managed group gracefully before the socket is removed.
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
