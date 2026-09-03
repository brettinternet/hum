---
id: HUM-005
title: Serve the private NDJSON daemon protocol
status: Done
assignee: []
created_date: '2026-09-02 17:06'
updated_date: '2026-09-03 05:54'
labels:
  - daemon
  - protocol
  - security
milestone: m-0
dependencies:
  - HUM-004
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
- [x] #1 `go test ./internal/protocol` exits 0 and covers NDJSON round trips, the version-carrying hello, streaming log events, readiness, signal/shutdown operations, start requests carrying cwd and environment with no response echoing it, malformed/oversized requests, typed errors, and stable field names.
- [x] #2 `go test ./internal/daemon -run TestPrivateSocket` exits 0 and proves 0700 runtime-directory and 0600 socket permissions for XDG and temporary fallbacks.
- [x] #3 `go test ./internal/daemon -run 'TestClientDisconnect|TestMultipleFollowers'` exits 0 and proves a disconnected attached client leaves the managed process running, followers are independent, and later clients can reconnect.
- [x] #4 `go test ./internal/daemon -run 'TestSocketOwnership|TestStaleRuntimeRecovery|TestConcurrentStartup'` exits 0 and proves one live daemon cannot be displaced, stale PID/socket artifacts recover safely, and concurrent startup attempts select exactly one daemon.
- [x] #5 `go test ./internal/daemon -run 'TestReadinessHandshake|TestShutdown'` exits 0 and proves readiness is reported only after the socket accepts requests, default shutdown refuses with named active processes, and forced shutdown waits for process-tree termination before removing runtime artifacts.
- [x] #6 `go test ./internal/daemon -run 'TestHelloVersion|TestDaemonSignal'` exits 0 and proves a mismatched hello is rejected with a typed error naming the daemon version, shutdown still succeeds against a mismatched idle daemon, and SIGTERM to the daemon stops every managed group gracefully before the socket is removed.
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
Implementation commit: a27ed6e feat: serve private daemon protocol

AC#1 — `go test ./internal/protocol` passed. Covers bounded NDJSON round trips, frozen/versioned hello and shutdown shapes, all request/response/event operations, stable fields, typed malformed/oversized errors, start cwd/environment, and response environment rejection.
AC#2 — `go test ./internal/daemon -run TestPrivateSocket` passed. Covers XDG and temporary fallback runtime paths with 0700 directory and 0600 socket/artifact permissions.
AC#3 — `go test ./internal/daemon -run 'TestClientDisconnect|TestMultipleFollowers'` passed. Covers connection-independent managed processes, independent/reconnecting followers, cancellation/close, and poisoned-transport invalidation.
AC#4 — `go test ./internal/daemon -run 'TestSocketOwnership|TestStaleRuntimeRecovery|TestConcurrentStartup'` passed. Covers live-owner protection, safe stale recovery, and one winner under concurrent startup.
AC#5 — `go test ./internal/daemon -run 'TestReadinessHandshake|TestShutdown'` passed. Covers accept-backed readiness, named active-process refusal, serialized start/shutdown admission, and forced process-tree termination before artifact cleanup.
AC#6 — `go test ./internal/daemon -run 'TestHelloVersion|TestDaemonSignal'` passed. Covers typed version mismatch with daemon version, newer-client shutdown of an idle older daemon, SIGTERM/SIGINT process-tree cleanup, and socket removal.

Final gate — `task ci` passed on commit a27ed6e (gofmt, go vet, staticcheck, and `go test ./...`). `task check:staged` passed formatting and secret checks before commit.
Independent verification — PASS for AC#1 through AC#6; final adversarial review found no remaining hello-state defects after all corrections.
Modified-file contract — implementation commit changes only `internal/protocol/` and `internal/daemon/`. No test was deleted, skipped, or weakened; no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented the private bounded NDJSON daemon protocol and Unix-socket runtime: typed request/response/event transport, strict version negotiation with frozen mismatch shutdown, private runtime ownership and recovery, independent clients/followers, lifecycle-safe process dispatch, readiness, signal forwarding, forced/default shutdown, rotating logs, and complete acceptance coverage. All exact acceptance commands, task ci, staged checks, and independent verification passed on commit a27ed6e.
<!-- SECTION:FINAL_SUMMARY:END -->
