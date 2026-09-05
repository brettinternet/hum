---
id: HUM-022
title: Opt a supervised session into a pseudo-terminal for interactive devtools
status: To Do
assignee: []
created_date: '2026-09-05 13:55'
updated_date: '2026-09-05 14:24'
labels:
  - cli
  - daemon
  - process
  - output
  - protocol
  - config
  - docs
dependencies:
  - HUM-020
modified_files:
  - internal/process/
  - internal/output/
  - internal/app/
  - internal/protocol/
  - internal/daemon/
  - internal/cli/
  - internal/project/
  - internal/mcp/
  - internal/skill/
  - integration/
  - cmd/hum/integration_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
  - go.mod
  - go.sum
priority: medium
type: feature
ordinal: 3300
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a supervised session can opt into a pseudo-terminal so devtools that gate prompts on a TTY (npx package confirmation, Prisma `migrate dev`, create-* scaffolders, inquirer/survey/dialoguer-style prompts, tools that open `/dev/tty`, and REPLs) prompt normally and accept answers typed into an attached `hum run`. The default stays `/dev/null` stdin with separate stdout/stderr pipes so retained logs remain clean for agents. Pipe-based stdin forwarding is deliberately not built: the child would still see `isatty(0) == false` and the tools above would refuse to prompt or pick non-interactive defaults.

Scope, opt-in: `tty: true` (boolean, default false) per process in `hum.yaml`, and a long-only `--tty` flag on ad hoc `hum run <name> -- command...` (attached or `--detach`). `--tty` on a declared name is rejected with the existing "declared in hum.yaml; use hum start" rule. `start`, `up`, `restart`, and MCP `start`/`up` honor the manifest value; retained ad hoc launches remember `tty` and `start`/`restart` reuse it. A running non-tty incarnation cannot be upgraded: `run --tty` on it fails with an actionable stop-and-rerun message, and a manifest `tty` edit takes effect at the next launch like any other definition edit. `status`, `list`, and their MCP equivalents report `tty`.

Scope, launch: a tty launch allocates a PTY pair; the child receives the slave as stdin, stdout, and stderr, becomes a session leader (`Setsid`) with the slave as its controlling terminal (`Setctty`), and keeps its own process group so `stop`, `down`, `restart`, `remove`, and the grace/SIGKILL path are unchanged. The daemon owns the master, reads output from it, and applies window size to it. Initial size comes from the attached input owner when one is present at launch, otherwise 80x24. EIO/EOF on the master after child exit ends output capture without leaking descriptors or goroutines. Because the terminal cannot outlive the daemon, daemon shutdown stops tty sessions with the ordinary SIGTERM/grace/SIGKILL sequence instead of letting the closing master deliver SIGHUP; docs state that tty sessions do not survive daemon shutdown while non-tty sessions keep today's behavior. Non-tty launches are byte-for-byte unchanged.

Scope, output: a tty session produces one merged stream recorded as `stdout`; `stderr` never appears for it. The ring stores the raw bytes exactly once under existing bounds and eviction. Attached `run` writes raw bytes to the local terminal so colours and redraws render. Every bounded reader (`logs`, `wait`, MCP `logs`, and readiness `match`) sees text from one shared sanitiser: ANSI CSI/OSC/SS3 escape sequences removed, `\r\n` normalised to `\n`, and carriage-return redraws collapsed to the final rendering of each line; NUL and invalid UTF-8 outside escape sequences are preserved. Plain text passes through the sanitiser unchanged, so non-tty sessions are unaffected and no `--raw` flag exists.

Scope, input ownership: exactly one input owner per named session, including pre-launch and stopped sessions. Attached `hum run <name>` on a tty session acquires the input lease automatically; if another client already owns it, the attach succeeds as output-only and prints one notice naming the conflict. `logs --follow` stays output-only. Input is never stored in the output ring, retained, logged, rendered, or replayed. Writes are synchronous bounded chunks over a dedicated input connection with no daemon-side queue; the owner reads the next local chunk only after the daemon acknowledges the previous one, and every write and resize names the launch cursor it targets so nothing enters a successor incarnation. A stalled write is unblocked with a typed closed/stale-incarnation error by exit, restart, remove, client cancellation, and daemon shutdown, and it never blocks other connections. Removal and daemon shutdown close the lease; process exit closes only the incarnation's terminal.

Scope, client terminal: when the input owner's local stdin is a terminal, attached `run` switches it to raw mode for the life of the attachment and restores it on every exit path, including detach, transport loss, SIGTERM/SIGHUP, and panics. All bytes are forwarded, so Ctrl+C, Ctrl+D, and Ctrl+Z reach the child's line discipline and act on the child. Ctrl+] (0x1d) is the detach chord: it never reaches the child, detaches only this observer, and is announced in the attach boundary line. SIGWINCH and the initial size are forwarded as resizes by the input owner only. When local stdin is not a terminal (`echo y | hum run name`), bytes are forwarded without raw mode and local EOF releases the input lease while the client remains an output-only follower. Bytes typed while the session is stopped are discarded; forwarding resumes at the next launch on the fresh terminal. Non-tty sessions keep today's behavior exactly: Ctrl+C detaches the observer and nothing is forwarded.

Scope, protocol: bump the private protocol version. Launch specs and process snapshots carry `tty`; add input attach/release, bounded byte writes, and resize; JSON may carry `[]byte` as base64 but arbitrary bytes must round-trip exactly. Keep the existing separate output-follow connection; do not put raw bytes directly onto the NDJSON socket framing. Input transport cleanup must be race-free against exit, restart, remove, client cancellation, and daemon shutdown.

Agents: docs/coding-agents.md and the bundled skill tell agents to leave `tty` off for processes they start unless the tool requires a terminal, to prefer the tool's non-interactive flag (`npx --yes`, `CI=1`, `--force`), and that bounded `logs`/`wait` return sanitised text. Answering a prompt from an agent is the separate one-shot input task (DRAFT-002); MCP gains no input tool here.

Non-goals: pipe-mode stdin forwarding, tty by default, one-shot `hum input` or MCP input (DRAFT-002), a `--raw` logs flag, terminal emulation or screen-state tracking in the daemon, scrollback or multiplexing, multiple input owners, reconnecting a lost owner, input retention/replay, PTY for non-tty sessions, changing non-tty stop/shutdown semantics, or Windows.

Modified-file contract: internal/process/, internal/output/, internal/app/, internal/protocol/, internal/daemon/, internal/cli/, internal/project/, internal/mcp/, internal/skill/, integration/, cmd/hum/integration_test.go, README.md, docs/design.md, docs/coding-agents.md, go.mod, go.sum. `go.mod`/`go.sum` may add only `github.com/creack/pty`, `golang.org/x/term`, and `golang.org/x/sys`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/process -run 'Test.*Tty' -count=1` exits 0 and proves a tty launch gives the child a controlling terminal (`test -t 0` and opening `/dev/tty` succeed inside the child), merges stdout and stderr into one stream, applies the initial and later window sizes, delivers written bytes exactly including NUL and invalid UTF-8, ends capture on child exit without leaking descriptors or goroutines, still terminates the whole process group on SIGTERM/SIGKILL, and leaves non-tty launches byte-for-byte unchanged (`/dev/null` stdin, separate streams).
- [ ] #2 AC2 — `go test ./internal/output -run 'Test.*Sanitize' -count=1` exits 0 and proves the shared sanitiser removes CSI/OSC/SS3 escape sequences, normalises `\r\n`, collapses carriage-return redraws to the final rendering of each line, preserves NUL and invalid UTF-8 outside escape sequences, passes plain text through unchanged, and that the ring stores raw bytes once under existing bounds.
- [ ] #3 AC3 — `go test ./internal/app -run 'Test.*(Tty|Input)' -count=1` exits 0 and proves one exclusive input lease may attach before launch or to a tty incarnation, a second owner is refused without disruption, a running non-tty incarnation cannot be upgraded, retained ad hoc launches remember `tty`, writes and resizes are launch-cursor scoped and a stale one fails with a typed error and never reaches a successor, readiness `match` evaluates sanitised text, snapshots report `tty`, remove and daemon shutdown close the lease, daemon shutdown stops tty sessions with the ordinary grace sequence while leaving non-tty sessions alone, and bounded reads return sanitised text while the follower path returns raw bytes.
- [ ] #4 AC4 — `go test ./internal/protocol ./internal/daemon -run 'Test.*(Tty|Input)' -count=1` exits 0 and proves the bumped protocol carries `tty` on launch specs and snapshots, exclusive attach/release, bounded arbitrary-byte writes, resize, and typed oversize/closed/stale-incarnation errors over NDJSON-safe payloads on a dedicated input connection; a write stalled on a non-reading child is unblocked by cancellation, exit, restart, remove, and shutdown without racing cleanup; and unrelated requests on other connections stay responsive while it is stalled.
- [ ] #5 AC5 — `go test ./internal/cli ./internal/project ./internal/mcp -run 'Test.*(Tty|Input)' -count=1` exits 0 and proves `tty` parses as a boolean and rejects other values with file and entry context, `--tty` is accepted only on ad hoc run and rejected for declared names, start/up/restart and MCP start/up honor the manifest value, attached run on a tty session enters raw mode and restores the terminal on detach, transport loss, and signal, Ctrl+] detaches without reaching the child, Ctrl+C is forwarded for tty sessions and still detaches for non-tty sessions, SIGWINCH forwards a resize, a second attach is output-only with one notice, piped local stdin forwards without raw mode and releases the lease at EOF, typing while stopped is discarded and resumes after the next launch, bounded logs return sanitised text, status/list and MCP status/list expose `tty`, and MCP exposes no input tool.
- [ ] #6 AC6 — `go test ./integration -run 'TestTty' -count=1` exits 0 and proves with the built binary that a fixture which refuses to prompt unless `test -t 0` succeeds prompts under `hum run --tty` and receives the typed answer, `hum logs` shows the sanitised prompt and answer output without escape sequences, `hum status` reports tty, detaching neither stops the child nor sends EOF, a second client attaches output-only, stop then start gives the still-attached owner a fresh terminal, `hum shutdown` stops the tty session, and non-tty processes still receive `/dev/null` with separate streams.
- [ ] #7 AC7 — `go test ./internal/cli ./internal/skill -run 'Test.*(Help|Surface|Instructions).*Tty' -count=1` exits 0 and README.md, docs/design.md, docs/coding-agents.md, CLI help, and the bundled skill document `tty: true` and `--tty`, the clean-log default, merged output and sanitised bounded reads, exclusive input ownership, raw mode with Ctrl+] detach and forwarded Ctrl+C for tty sessions versus Ctrl+C detach for non-tty sessions, non-upgradable running incarnations, tty sessions not surviving daemon shutdown, and guidance for agents to prefer non-interactive flags and leave `tty` off unless required.
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
