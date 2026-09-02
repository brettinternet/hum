---
id: DEVPROC-005
title: Serve the private NDJSON daemon protocol
status: To Do
assignee: []
created_date: '2026-09-02 17:06'
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
Outcome: local clients can invoke application services over a user-private Unix socket without coupling the wire protocol to CLI rendering.

Scope: internal newline-delimited JSON request/response types; bounded request decoding; stable typed errors; one long-running daemon owning application state; client calls independent of connection lifetime; socket under the configured runtime directory; directory mode 0700 and socket mode 0600; stale-socket handling that never displaces a live daemon; graceful serve shutdown. Enforce server-side maximum output/request limits and reject malformed messages, invalid cursors, and unsupported operations.

Non-goals: public protocol compatibility, TCP, authentication, TLS, auto-starting the daemon, MCP, or product CLI commands.

Modified-file contract: internal/protocol/, internal/daemon/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/protocol` exits 0 and covers NDJSON round trips, malformed/oversized requests, typed errors, and stable field names.
- [ ] #2 `go test ./internal/daemon -run TestPrivateSocket` exits 0 and proves 0700 runtime-directory and 0600 socket permissions for XDG and temporary fallbacks.
- [ ] #3 `go test ./internal/daemon -run TestClientDisconnect` exits 0 and proves disconnecting a start request leaves the managed process running and later clients can reconnect.
- [ ] #4 `go test ./internal/daemon -run TestSocketOwnership` exits 0 and proves a second server cannot replace a live socket while a verified stale socket is recovered safely.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
- [ ] #7 Committed on main with the task ID in the commit subject and a Task: trailer
<!-- DOD:END -->
