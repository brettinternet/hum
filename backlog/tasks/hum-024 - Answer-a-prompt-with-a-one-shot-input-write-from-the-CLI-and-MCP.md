---
id: HUM-024
title: Answer a prompt with a one-shot input write from the CLI and MCP
status: To Do
assignee: []
created_date: '2026-09-05 14:24'
updated_date: '2026-09-05 15:12'
labels:
  - cli
  - daemon
  - protocol
  - docs
milestone: m-2
dependencies:
  - HUM-022
modified_files:
  - internal/app/
  - internal/protocol/
  - internal/daemon/
  - internal/cli/
  - internal/mcp/
  - internal/skill/
  - internal/testutil/
  - integration/
  - plugins/hum/skills/hum/SKILL.md
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: feature
ordinal: 3500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum input <name> --text TEXT` (or `--base64 DATA`) and an MCP `input` tool write bytes to a running tty session's terminal once and return, so a coding agent that sees a prompt in bounded `logs` can answer it without holding an unbounded attached `run`. The write is a short-lived HUM-022 input lease: acquire, write, release.

Scope, CLI: `hum input <name> --text TEXT [--json]` and `hum input <name> --base64 DATA [--json]`; exactly one of the two is required. `--text` sends the string bytes exactly as given, with no implicit newline; callers append `\n` themselves (`--text $'y\n'`). `--base64` accepts standard padded base64 for bytes that are awkward on a command line (Ctrl+C, Ctrl+D, NUL). The decoded payload must be 1 to 32768 bytes; empty and oversize payloads fail validation before any daemon call. Human output prints `wrote N bytes to <name> at launch cursor C`; `--json` prints `{"name","bytes","launch_cursor"}`. `--text` has short alias `-x` (no other alias; `-t` stays reserved for `--timeout`). `input` is a control command: it never starts the daemon or a process, and an unavailable daemon is the existing unavailable-daemon error.

Scope, semantics: the CLI opens the HUM-022 dedicated input connection, attaches with `tty: true` and no staging argv, receives the initial state event, and proceeds only when that state is running; a stopped or never-launched session fails with typed `session_not_running` and a message naming `hum start`/`hum run`. The write targets the launch cursor from that state event so a stale write never reaches a successor incarnation. On acknowledgement the client releases the lease and exits 0. An attached `hum run` owner holding the lease yields the existing typed conflict, exit 1, with a message saying the attached run owns input. A non-tty session yields typed `input_not_tty`, exit 1, with a message pointing at `tty: true` or `--tty`. The daemon does not append, echo, or retain the submitted bytes; PTY line-discipline or application echo remains ordinary child output. Two concurrent one-shot writes serialize through the lease: the second waits at most 5 seconds for the lease and then fails with the conflict error.

Scope, MCP: add an `input` tool with required `project_root`, `name`, and exactly one of `text` or `base64`, mirroring the CLI semantics and payload bound; the result is `{"name","bytes","launch_cursor"}`. Conflict, not-tty, not-running, and too-large failures return the typed error codes above as tool errors. The adapter continues to construct no app services in-process and calls the daemon client only; it exposes no resize, streaming, or attach.

Scope, protocol: no new operations. The one-shot path is a client-side use of `input_attach`, `input_state`, `input_write`, and `input_release`. Add only the `session_not_running` and `input_not_tty` typed error codes if HUM-022 does not already define equivalents; otherwise reuse them. The protocol version bumps only if a code is added.

Agents and docs: README.md, docs/design.md (CLI list, command semantics, MCP tool count and list), docs/coding-agents.md, CLI help, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` document the prompt-answering loop: `logs` or `wait --match` to see the prompt, `input` to answer, `wait --match` to confirm. Agent guidance still prefers a tool's non-interactive mode and leaves tty off unless a tool requires it.

Non-goals: input to non-tty sessions; resize over CLI or MCP; streaming or multi-message input; multiple simultaneous owners; retaining or replaying input; input from `logs --follow`; waiting for a stopped session to start; an interactive REPL mode; Windows.

Modified-file contract: internal/app/, internal/protocol/, internal/daemon/, internal/cli/, internal/mcp/, internal/skill/, internal/testutil/, integration/, plugins/hum/skills/hum/SKILL.md, README.md, docs/design.md, docs/coding-agents.md. No go.mod or go.sum change.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/cli -run "^TestInputCommand$" -count=1 -v` exits 0 and prints `--- PASS: TestInputCommand`. It proves `hum input` requires exactly one of `--text`/`--base64`, rejects empty and >32768-byte payloads before any daemon call, sends `--text` bytes exactly with no implicit newline, decodes `--base64` including NUL and control bytes, prints the human and `--json` success forms with the launch cursor, maps conflict, `input_not_tty`, `session_not_running`, and unavailable-daemon failures to exit 1 with the documented messages, never starts a daemon or process, and supports `-x` for `--text` only.
- [ ] #2 AC2 — `go test ./internal/daemon -run "^TestOneShotInputWrite$" -count=1 -v` exits 0 and prints `--- PASS: TestOneShotInputWrite`. It proves a one-shot client attaches, receives a running state, writes at that launch cursor, is acknowledged, and releases so a later owner can attach; a stopped session receives `session_not_running` without a write; a non-tty session receives `input_not_tty`; a write issued while an attached owner holds the lease receives the typed conflict; two one-shot writers serialize with the second failing after the 5 second bound if the first never releases; the daemon retains no submitted bytes in the ring; and the protocol version changes only if a new error code was added.
- [ ] #3 AC3 — `go test ./internal/mcp -run "^TestInputTool$" -count=1 -v` exits 0 and prints `--- PASS: TestInputTool`. It proves the tool list contains eleven tools including `input`, its schema requires `project_root` and `name` and exactly one of `text`/`base64`, payload validation matches the CLI, the result carries `name`, `bytes`, and `launch_cursor`, conflict/not-tty/not-running/too-large map to typed tool errors, and the adapter calls only the daemon client with no in-process app services.
- [ ] #4 AC4 — `go test ./integration -run "^TestOneShotInputAnswersPrompt$" -count=1 -v` exits 0 and prints `--- PASS: TestOneShotInputAnswersPrompt`. With the built binary and a TTY-gated fixture launched detached with `--tty`, it proves `hum wait --match` sees the prompt, `hum input NAME --text $'yes\n'` returns 0 with the launch cursor, the fixture receives exactly `yes\n` and prints its confirmation, a second `hum input` after the fixture exits fails with `session_not_running`, `hum input` against a non-tty session fails with `input_not_tty`, and `hum input` while an attached `hum run` owns the lease fails with the conflict message while the attached run keeps working.
- [ ] #5 AC5 — `go test ./internal/cli ./internal/skill -run "^TestInputDocs$" -count=1 -v` exits 0 and prints both named PASS lines. README.md, docs/design.md, docs/coding-agents.md, CLI help, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` document the `input` command and MCP tool, the exact-bytes/no-implicit-newline rule, the 32 KiB bound, the running-only and tty-only constraints, the attached-owner conflict, the see-answer-confirm loop, and that hum neither retains nor echoes input.
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
