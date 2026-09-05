---
id: HUM-022
title: Forward standard input to an attached supervised process
status: To Do
assignee: []
created_date: '2026-09-05 13:55'
updated_date: '2026-09-05 14:07'
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
Outcome: attached `hum run --stdin <name>` opts one supervised process session into byte-for-byte standard-input forwarding over an OS pipe while preserving hum's detached daemon ownership, separate stdout/stderr capture, and existing output following.

What this does and does not support: this is pipe-based stdin, not a terminal. It serves piped producers (`cat data | hum run --stdin importer -- ./import`), tools that read stdin plainly (shell `read`, Python `input()`/`click.confirm`, y/n prompts that do not check for a terminal), and non-TTY REPLs. The child sees `isatty(0) == false` and has no controlling terminal (the daemon runs under `setsid`; children only get their own process group), so tools that gate prompts on a TTY behave exactly as they do under any pipe: npx package confirmation, Prisma `migrate dev`, inquirer/survey/dialoguer-style prompts, and anything that opens `/dev/tty` will refuse to prompt, choose non-interactive defaults, or fail. Those need the PTY mode tracked in DRAFT-001, which reuses this task's lease, protocol, and cleanup rules. Docs must state this boundary plainly and name the workaround for now: the tool's own non-interactive flag (`npx --yes`, `--force`, `CI=1`, and similar).

Scope, CLI: add a long-only `--stdin` flag to attached `hum run`; reject `--stdin` with `--detach`. `hum run --stdin name -- command...` and `hum run --stdin name` for a resolved or retained session acquire the session's exclusive input lease before launch/attach, then copy local stdin in bounded chunks while the existing follower renders stdout/stderr. Local stdin may be a terminal, a pipe, or already at EOF (`< /dev/null`); all are copied the same way. Plain `hum run`, `start`, `up`, `restart`, and MCP behavior remain unchanged unless an input lease is currently attached. `logs --follow` stays output-only.

Scope, launch and lifecycle: a child launched while its session has the input lease receives the read end of an OS pipe as stdin; all other children continue receiving `/dev/null` and immediate EOF. The daemon owns the write end. An existing running incarnation started with `/dev/null` cannot be upgraded; `run --stdin` fails with an actionable stop-and-rerun message. If the input-owning attached run survives stop/restart, it does not read local stdin while the session is stopped; the next launch receives a fresh pipe and forwarding resumes. Every write names the launch cursor it targets so a delayed write can never enter a successor incarnation.

Scope, EOF and disconnects: an ordinary local stdin EOF (Ctrl+D at the start of a line on a terminal, end of a piped producer, or stdin already at EOF) sends exactly one explicit EOF to the current child by closing that incarnation's daemon-owned write end; output following remains attached according to existing durable-run semantics. Abrupt transport loss, Ctrl+C, or cancellation releases the lease without sending EOF or stopping/signalling the child; observer disconnects never mutate managed process lifecycle. Once explicit EOF is sent, the same incarnation rejects later writes. A later incarnation gets a new open pipe while the surviving lease remains attached.

Scope, ownership and flow control: exactly one input owner is allowed per named session, including pre-launch and stopped sessions; competing `run --stdin` attempts fail immediately and name the existing lease conflict without disturbing either client. Output followers remain unlimited. Input is never stored in the output ring, retained, logged, rendered, or replayed. Writes are synchronous bounded chunks with no daemon-side queue: the client reads the next local chunk only after the daemon acknowledges the previous one, and the daemon writes each chunk straight to the child pipe, so the kernel pipe buffer is the only buffering. A child that does not read stalls only its input owner's connection, exactly as a shell pipeline would; unrelated clients and lifecycle requests stay responsive. A stalled write is unblocked with a typed closed/stale-incarnation error by process exit, restart, remove, client cancellation, and daemon shutdown. Oversize chunks are rejected with a typed error. Removal and daemon shutdown close the lease; process exit closes only the incarnation input path.

Scope, protocol: extend the private versioned daemon protocol and typed app/process boundaries with explicit input attach/release, per-launch readiness (the owner learns each launch cursor and incarnation exit so it can pause local reads while stopped and target writes at the current incarnation), bounded byte writes, and EOF. JSON may represent `[]byte` as base64, but arbitrary bytes must round-trip exactly, including NUL and invalid UTF-8. Keep the existing separate output-follow connection; use a dedicated input connection and do not put raw bytes directly onto the NDJSON socket framing. Input transport cleanup must be race-free against exit, restart, remove, client cancellation, and daemon shutdown.

Signal behavior: do not put the caller terminal into raw mode. On a normal terminal, canonical line editing and echo remain local, Ctrl+D is local EOF, and Ctrl+C continues to interrupt/detach the hum client rather than becoming byte 0x03 for the child. Existing explicit signal commands remain the way to signal a managed process.

Non-goals: PTYs, controlling terminals, raw terminal mode, terminal resizing/SIGWINCH, TUI/full-screen application support, satisfying TTY-gated prompts (DRAFT-001), one-shot `hum input`/MCP input writes (a later task; the lease model must simply not preclude an acquire-write-release owner), forwarding stdout differently, merging stdout/stderr, multiple writers, manifest stdin configuration, automatic stdin enablement for `start` or `up`, input retention/replay, reconnecting a lost input owner, daemon-side input queues, Windows support, or changing ordinary `/dev/null` stdin behavior.

Modified-file contract: internal/process/, internal/app/, internal/protocol/, internal/daemon/, internal/cli/, integration/, cmd/hum/integration_test.go, README.md, docs/design.md, go.mod, go.sum. `go.mod` and `go.sum` may change only if a focused Unix pipe/terminal-detection dependency is genuinely required; PTY dependencies are out of scope.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/process -run 'Test.*Stdin' -count=1` exits 0 and proves ordinary launches still observe immediate stdin EOF, opted-in launches receive exact ordered bytes including NUL and invalid UTF-8, explicit EOF is observed exactly once, writes after EOF fail, a child that does not read blocks only the pending writer and never buffers beyond the kernel pipe, closing the incarnation input path unblocks a pending write with a typed error, and process exit closes the input path without leaking goroutines or descriptors.
- [ ] #2 AC2 — `go test ./internal/app -run 'Test.*(Stdin|Input)' -count=1` exits 0 and proves one exclusive input lease may attach before launch or to an input-enabled running incarnation, a second owner is rejected without disruption, an existing `/dev/null` incarnation cannot be upgraded, writes are launch-cursor scoped and ordered, a write targeting an exited or superseded incarnation fails with a typed stale-incarnation error and never reaches a successor, stopped sessions do not consume input, explicit EOF closes only current child stdin, disconnect releases ownership without EOF or process signalling, and remove/shutdown close the lease.
- [ ] #3 AC3 — `go test ./internal/protocol ./internal/daemon -run 'Test.*(Stdin|Input)' -count=1` exits 0 and proves the bumped private protocol carries exclusive attach/release, per-launch readiness, bounded arbitrary-byte writes, typed oversize-chunk and closed/stale-incarnation errors, and explicit EOF over NDJSON-safe payloads on a dedicated input connection; a write stalled on a non-reading child is unblocked by cancellation, exit, restart, remove, and shutdown without racing cleanup; and unrelated lifecycle requests on other connections remain responsive while that write is stalled.
- [ ] #4 AC4 — `go test ./internal/cli -run 'Test.*Run.*Stdin' -count=1` exits 0 and proves `run --stdin` parses both resolved/retained and ad hoc forms, rejects `--detach`, forwards piped bytes including a producer larger than the pipe buffer completely and in order under flow control, delivers local EOF (including stdin already at EOF) exactly once, keeps the existing output follower attached after local EOF and across process incarnations, pauses local reads while stopped, resumes with a fresh pipe after launch, reports lease conflicts and non-upgradable running processes actionably, preserves Ctrl+C detach semantics, and introduces no stdin option on logs, start, up, manifests, or MCP.
- [ ] #5 AC5 — `go test ./integration -run 'TestStdinForwarding' -count=1` exits 0 and proves with the built binary that piped binary input reaches the child exactly, EOF is delivered, a disconnected input client neither stops the child nor sends EOF, two clients cannot write concurrently, ordinary processes still receive `/dev/null`, stale bytes do not reach a restarted child, the child observes a non-terminal stdin (`test -t 0` fails), and `hum status`/`hum stop` remain responsive while a producer is stalled on a child that does not read stdin.
- [ ] #6 AC6 — `go test ./internal/cli -run 'Test.*(Help|Surface).*Stdin' -count=1` exits 0 and README.md plus docs/design.md document `hum run --stdin`, exclusive ownership, Ctrl+D/piped EOF, disconnect and restart behavior, non-retention of input, unchanged Ctrl+C semantics, default `/dev/null`, which tools this serves (plain stdin readers, piped producers, non-TTY REPLs), and the explicit boundary that pipe forwarding is not a PTY: TTY-gated prompts (npx confirmation, Prisma `migrate dev`, inquirer-style prompts, `/dev/tty` readers) and TUIs are unsupported, the workaround is the tool's non-interactive flag, and PTY mode is a separate task.
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
