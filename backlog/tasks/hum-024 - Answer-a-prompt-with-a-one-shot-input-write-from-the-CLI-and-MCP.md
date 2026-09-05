---
id: HUM-024
title: Answer a prompt with a one-shot input write from the CLI and MCP
status: To Do
assignee: []
created_date: '2026-09-05 14:24'
updated_date: '2026-09-05 15:21'
labels:
  - cli
  - daemon
  - protocol
  - docs
milestone: m-2
dependencies:
  - HUM-023
modified_files:
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
Outcome: `hum input NAME (--text TEXT | --base64 DATA)` and an MCP `input` tool perform one at-most-once, launch-cursor-scoped write to an already-running tty incarnation, release the exclusive HUM-022 lease, and return. A coding agent can therefore observe a clean prompt through bounded `logs` or `wait --match`, answer it, and confirm the result without holding an unbounded attached `run`. They never launch a process, wait for a launch, queue input, or retry a write.

Scope, CLI: `hum input <name> --text TEXT [--json]` and `hum input <name> --base64 DATA [--json]`; exactly one process name and exactly one payload flag are required, with extra positional arguments rejected. `--text` sends the argument's bytes exactly as received, with no implicit newline; callers append `\n` themselves, for example `--text $'y\n'`. `--base64` strictly accepts standard padded base64 without whitespace for bytes that are awkward on a command line, including Ctrl+C, Ctrl+D, and NUL. The decoded payload must be 1 to 32768 bytes; missing, empty, malformed, unpadded, and oversize payloads fail before daemon discovery or dialing. Human success output is `wrote N bytes to <name> at launch cursor C`; `--json` emits the existing stable JSON success/error conventions with `name`, decoded `bytes`, and `launch_cursor` on success. No short aliases are added.

Scope, target resolution and state: `input` is a bounded control command and never starts the daemon or a process. An unavailable daemon returns the existing unavailable-daemon error. An unknown or unresolved name returns existing `not_found` with `hum start` or `hum run` guidance. A resolved tty declaration with no runtime record returns client-facing `session_not_running` without opening an input attachment and points to `hum start NAME`; an existing non-tty declaration or record returns `input_not_tty` and points to `tty: true` or `--tty`. For a retained stopped tty record, the client attaches, observes the initial stopped state, releases, and returns `session_not_running`. Only an initial running tty state proceeds to a write.

Scope, delivery and ownership: the write targets exactly the launch cursor from the initial running state. Any occupied lease, whether held by attached `hum run` or another one-shot caller, fails immediately with existing `input_conflict`; callers may retry the whole command explicitly. If the incarnation exits or restarts after the state event, existing `input_closed` or `input_stale` is returned and no bytes are retried against a successor. After sending `input_write`, the client never resends it: a lost acknowledgement is an indeterminate result, preserving at-most-once client behavior. The transport releases or closes the lease on every success, validation-after-attach, stopped-state, stale/closed, cancellation, and transport-error path so a later owner can attach. The daemon does not append, echo, or retain submitted bytes; PTY line discipline or application echo remains ordinary child output.

Scope, MCP: add an `input` tool with required `project_root`, `name`, and exactly one of `text` or `base64`. Its JSON Schema uses `oneOf` so each payload branch excludes the other while the object remains closed to additional properties. MCP `text` writes the exact UTF-8 bytes represented by the JSON string; `base64` and payload bounds match the CLI. The result is `{"name","bytes","launch_cursor"}`, where `bytes` is the decoded/written byte count. Not-found, conflict, not-tty, not-running, too-large, closed, and stale failures remain typed tool errors. The adapter calls only the daemon client and exposes no resize, streaming, or attach tool.

Scope, protocol: reuse HUM-022's `input_attach`, `input_state`, `input_write`, `input_release`, and typed input errors. `session_not_running` is a CLI/MCP-facing error derived from absent/stopped state rather than a new daemon operation or wire response. Protocol shape and version remain unchanged.

Agents and docs: README.md, docs/design.md (CLI list, command semantics, MCP tool count/list), docs/coding-agents.md, CLI help, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` document the bounded prompt loop: `logs` or `wait --match` to observe, `input` to answer, then `wait --match` to confirm. Guidance still prefers a tool's non-interactive mode and leaves tty off unless required.

Non-goals: input to non-tty sessions; resize over CLI or MCP; streaming or multi-message input; multiple simultaneous owners; automatic waiting, queuing, retry, resend, or replay; retaining input; input from `logs --follow`; an interactive REPL mode; short flag aliases; Windows.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/cli -run "^TestInputCommand$" -count=1 -v` exits 0 and prints `--- PASS: TestInputCommand`. It proves exactly one NAME and exactly one of `--text`/`--base64` are required; extra names, empty text, malformed or unpadded base64, base64 whitespace, and decoded payloads over 32768 bytes fail before daemon discovery/dial; text bytes have no implicit newline; base64 carries NUL and control bytes; success human/JSON output includes the decoded byte count and launch cursor; JSON errors follow the existing convention; unavailable, not-found, non-tty, not-running, conflict, closed, and stale cases exit 1 with actionable messages; and the command never starts a daemon or process and has no short payload alias.
- [ ] #2 AC2 — `go test ./internal/daemon -run "^TestOneShotInputWrite$" -count=1 -v` exits 0 and prints `--- PASS: TestOneShotInputWrite`. Against the daemon client/transport it proves a running tty attach receives an initial cursor, sends exactly one write at that cursor, receives acknowledgement, releases, and returns the decoded byte count; an occupied lease fails immediately; a stopped initial state releases without writing and maps to `session_not_running`; non-tty maps to `input_not_tty`; exit/restart races map to `input_closed`/`input_stale` without retrying a successor; cancellation and lost acknowledgement cause no resend; every terminal path closes or releases the lease so a later owner can attach; submitted bytes are not appended to the ring; and protocol shape/version are unchanged.
- [ ] #3 AC3 — `go test ./internal/mcp -run "^TestInputTool$" -count=1 -v` exits 0 and prints `--- PASS: TestInputTool`. It proves the tool list contains eleven tools including `input`; the closed input schema requires `project_root` and `name` and uses `oneOf` to accept exactly one non-empty `text` or `base64`; missing/both payloads, malformed or unpadded base64, whitespace, and decoded oversize are rejected before a fake client invocation; text is converted to its exact JSON-string UTF-8 bytes; success contains `name`, decoded `bytes`, and `launch_cursor`; and not-found/conflict/not-tty/not-running/too-large/closed/stale map to typed tool errors while the fake records only the expected daemon-client calls.
- [ ] #4 AC4 — `go test ./integration -run "^TestOneShotInputAnswersPrompt$" -count=1 -v` exits 0 and prints `--- PASS: TestOneShotInputAnswersPrompt`. With the built binary and a tty-gated fixture launched detached with `--tty`, it proves `hum wait --match` sees a stripped prompt, `hum input NAME --text $'yes\n'` returns 0 with the launch cursor, the fixture receives exactly `yes\n` and prints confirmation, input is absent from retained output except for child-controlled terminal echo, a second input after exit returns `session_not_running`, a non-tty target returns `input_not_tty`, and an occupied attached-run lease returns immediate `input_conflict` without disrupting the owner.
- [ ] #5 AC5 — `go test ./internal/cli ./internal/skill -run "^TestInputDocs$" -count=1 -v` exits 0 and prints both named PASS lines. README.md, docs/design.md, docs/coding-agents.md, CLI help, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` document the CLI/MCP input surface, exact-bytes/no-newline and strict-base64 rules, 32 KiB bound, running/tty constraints, immediate ownership conflict, launch-race and at-most-once behavior, the observe-answer-confirm loop, and that hum neither retains nor explicitly echoes input.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
- [ ] T1 — Add one shared daemon-client one-shot helper with exact target-state, cursor, at-most-once, error-mapping, and cleanup semantics.
- [ ] T2 — Add the strict CLI and MCP surfaces and focused fake-client tests without changing the private protocol.
- [ ] T3 — Update operator/agent documentation and prove the built-binary prompt-answering loop.
<!-- SECTION:PLAN:END -->
