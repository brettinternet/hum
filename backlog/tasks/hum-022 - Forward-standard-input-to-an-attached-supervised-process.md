---
id: HUM-022
title: Forward standard input to an attached supervised process
status: To Do
assignee: []
created_date: '2026-09-05 13:55'
labels:
  - cli
  - daemon
  - protocol
  - process
  - docs
dependencies:
  - HUM-020
modified_files:
  - internal/process/
  - internal/app/
  - internal/protocol/
  - internal/daemon/
  - internal/cli/
  - integration/
  - cmd/hum/integration_test.go
  - README.md
  - docs/design.md
  - go.mod
  - go.sum
priority: medium
type: feature
ordinal: 3300
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: attached `hum run --stdin <name>` can opt one supervised process session into byte-for-byte standard-input forwarding while preserving hum's detached daemon ownership and existing output following. This is pipe-based stdin only: it supports piped producers and non-TTY REPLs, not terminal emulation.

Scope, CLI: add a long-only `--stdin` flag to attached `hum run`; reject `--stdin` with `--detach` and JSON does not change the byte stream. `hum run --stdin name -- command...` and `hum run --stdin name` for a resolved or retained session acquire the session's exclusive input lease before launch/attach, then copy local stdin in bounded chunks while the existing follower renders stdout/stderr. Plain `hum run`, `start`, `up`, restart, and MCP behavior remain unchanged unless an input lease is currently attached. `logs --follow` stays output-only.

Scope, launch and lifecycle: a child launched while its session has the input lease receives the read end of an OS pipe as stdin; all other children continue receiving `/dev/null` and immediate EOF. The daemon owns the write end. An existing running incarnation started with `/dev/null` cannot be upgraded; `run --stdin` fails with an actionable stop-and-rerun message. If the input-owning attached run survives stop/restart, it does not read or buffer local stdin while stopped; the next launch receives a fresh pipe and forwarding resumes. Every write identifies the launch cursor so delayed writes can never enter a successor incarnation. Queued bytes are bounded and discarded at incarnation exit.

Scope, EOF and disconnects: an ordinary local stdin EOF sends one explicit EOF to the current child by closing that incarnation's daemon-owned pipe, but output following remains attached according to existing durable-run semantics. Abrupt transport loss, Ctrl+C, or cancellation releases the lease without sending EOF or stopping/signalling the child; this preserves the rule that observer disconnects do not mutate managed process lifecycle. Once explicit EOF is sent, the same incarnation rejects later writes. A later incarnation gets a new open pipe while the surviving lease remains attached.

Scope, ownership and backpressure: exactly one input owner is allowed per named session, including pre-launch and stopped sessions; competing `run --stdin` attempts fail immediately and name the existing lease conflict without disturbing either client. Output followers remain unlimited. Input is never stored in the output ring, retained, logged, rendered, or replayed. Use a small bounded per-incarnation queue and one ordered writer so a child that does not read cannot grow daemon memory or block unrelated daemon requests; saturation returns a typed/actionable backpressure error. Removal and daemon shutdown close the lease; process exit closes only the incarnation input path.

Scope, protocol: extend the private versioned daemon protocol and typed app/process boundaries with explicit input attachment/ownership, launch readiness, bounded byte writes, and EOF. JSON may represent `[]byte` as base64, but arbitrary bytes must round-trip exactly, including NUL and invalid UTF-8. Keep the existing separate output-follow connection; do not put raw bytes directly onto the NDJSON socket framing. Input transport cleanup must be race-free against exit, restart, remove, client cancellation, and daemon shutdown.

Signal behavior: do not put the caller terminal into raw mode. On a normal terminal, canonical line editing remains local and Ctrl+C continues to interrupt/detach the hum client rather than becoming byte 0x03 for the child. Existing explicit signal commands remain the way to signal a managed process.

Non-goals: PTYs, controlling terminals, raw terminal mode, terminal resizing/SIGWINCH, TUI/full-screen application support, forwarding stdout differently, merging stdout/stderr, multiple writers, stdin over MCP, manifest stdin configuration, automatic stdin enablement for `start` or `up`, input retention/replay, reconnecting a lost input owner, Windows support, or changing ordinary `/dev/null` stdin behavior.

Modified-file contract: internal/process/, internal/app/, internal/protocol/, internal/daemon/, internal/cli/, integration/, cmd/hum/integration_test.go, README.md, docs/design.md, go.mod, go.sum. `go.mod` and `go.sum` may change only if a focused Unix terminal/pipe dependency is genuinely required; PTY dependencies are out of scope.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/process -run 'Test.*Stdin' -count=1` exits 0 and proves ordinary launches still observe immediate stdin EOF, opted-in launches receive exact ordered bytes including NUL and invalid UTF-8, explicit EOF is observed exactly once, writes after EOF fail, blocked readers cannot cause unbounded buffering, and process exit closes/cancels the input writer without leaking goroutines or descriptors.
- [ ] #2 AC2 — `go test ./internal/app -run 'Test.*(Stdin|Input)' -count=1` exits 0 and proves one exclusive input lease may attach before launch or to an input-enabled running incarnation, a second owner is rejected without disruption, an existing `/dev/null` incarnation cannot be upgraded, writes are launch-cursor scoped and ordered, stale/queued input never crosses an incarnation boundary, stopped sessions do not consume or buffer input, explicit EOF closes only current child stdin, disconnect releases ownership without EOF or process signalling, and remove/shutdown close the lease.
- [ ] #3 AC3 — `go test ./internal/protocol ./internal/daemon -run 'Test.*(Stdin|Input)' -count=1` exits 0 and proves the bumped private protocol carries exclusive attach/release, launch readiness, bounded arbitrary-byte writes, typed backpressure/stale-incarnation errors, and explicit EOF over NDJSON-safe payloads; cleanup is race-free across cancellation, exit, restart, remove, and shutdown; and unrelated lifecycle requests remain responsive when a child does not read stdin.
- [ ] #4 AC4 — `go test ./internal/cli -run 'Test.*Run.*Stdin' -count=1` exits 0 and proves `run --stdin` parses both resolved/retained and ad hoc forms, rejects `--detach`, forwards piped bytes, keeps the existing output follower attached after local EOF and across process incarnations, pauses local reads while stopped, resumes with a fresh pipe after launch, reports lease conflicts and non-upgradable running processes actionably, preserves Ctrl+C detach semantics, and introduces no stdin option on logs, start, up, manifests, or MCP.
- [ ] #5 AC5 — `go test ./integration -run 'TestStdinForwarding' -count=1` exits 0 and proves with the built binary that piped binary input reaches the child exactly, EOF is delivered, a disconnected input client neither stops the child nor sends EOF, two clients cannot write concurrently, ordinary processes still receive `/dev/null`, stale bytes do not reach a restarted child, and daemon control remains responsive under child-input backpressure.
- [ ] #6 AC6 — `go test ./internal/cli -run 'Test.*(Help|Surface).*Stdin' -count=1` exits 0 and README.md plus docs/design.md document `hum run --stdin`, exclusive ownership, EOF/disconnect/restart behavior, non-retention of input, unchanged Ctrl+C semantics, default `/dev/null`, and the explicit limitation that pipe forwarding is not a PTY and does not support TUIs.
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
