---
id: HUM-022
title: Opt a supervised session into a pseudo-terminal for interactive devtools
status: To Do
assignee: []
created_date: '2026-09-05 13:55'
updated_date: '2026-09-05 14:46'
labels:
  - cli
  - daemon
  - process
  - output
  - protocol
  - config
  - docs
milestone: m-2
dependencies:
  - HUM-020
modified_files:
  - internal/process/
  - internal/app/
  - internal/protocol/
  - internal/daemon/
  - internal/cli/
  - internal/project/
  - internal/mcp/
  - internal/skill/
  - internal/testutil/
  - integration/
  - plugins/hum/skills/hum/SKILL.md
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
Outcome: a supervised session can opt into a pseudo-terminal so devtools that require a TTY before prompting (npx package confirmation, Prisma `migrate dev`, create-* scaffolders, inquirer/survey/dialoguer-style prompts, `/dev/tty` readers, and REPLs) prompt normally and accept terminal input from an attached `hum run`. The default remains `/dev/null` stdin with separate stdout/stderr pipes. Pipe-only stdin forwarding is deliberately not built because it would leave `isatty(0) == false` and no controlling terminal.

Scope, opt-in and retention: `tty: true` is a strict boolean process field in `hum.yaml`, default false. The canonical ad hoc forms are `hum run NAME --tty -- COMMAND...` and `hum run NAME --tty --detach -- COMMAND...`; `--tty` requires an ad hoc command and has no short alias. Supplying a command for a declared name remains rejected with the existing "declared in hum.yaml; use hum start" error. CLI and MCP `start`, `up`, and `restart` use the current manifest `tty` value whenever they launch a new incarnation; an already-running incarnation is not changed. Retained ad hoc definitions remember `tty`, and later `start` or `restart` reuses it. Replacing a stopped ad hoc definition may change `tty`; a running non-tty incarnation cannot be upgraded and reports an actionable stop-and-rerun error.

Scope, launch and shutdown: a tty launch allocates a PTY pair. The child receives the slave as stdin, stdout, and stderr, and `Setsid` plus `Setctty` makes it the session leader with the controlling terminal; `setsid` also establishes PGID=PID, so the tty path does not combine `Setsid` with `Setpgid`. Existing negative-PGID stop, down, restart, remove, SIGTERM/grace/SIGKILL, and descendant cleanup semantics remain intact. Initial size comes from the attached owner when present at launch, otherwise 80x24; an owner attaching later applies its current size immediately. The daemon owns the master and keeps it open until the process group and output capture finish. EIO/EOF after child exit ends capture without descriptor or goroutine leaks. Bare `hum shutdown` continues to refuse while any process is active; `hum shutdown --stop-processes` stops tty and non-tty sessions through the existing grace sequence. Every successful daemon shutdown closes each PTY master only after its process group exits, and no tty session survives daemon shutdown. Non-tty launch and shutdown behavior is unchanged.

Scope, output: a tty incarnation has one merged child stream recorded as `stdout`; `stderr` has no child entries, so `logs --stream stderr` returns none. The existing ring retains the merged bytes once under its current entry, cursor, eviction, and byte-bound rules. Attached run and follow paths do not strip ANSI or interpret screen state, so ordinary UTF-8 terminal colours and redraw controls render when replayed; bounded `logs`, `wait --match`, readiness `match`, and MCP logs see the same retained bytes, including terminal control sequences. The existing JSON string output representation is unchanged and makes no new byte-fidelity guarantee for invalid UTF-8. Sanitized logs, terminal emulation, and alternate raw/base64 output representations are separate work.

Scope, input ownership: exactly one attached `hum run` owns input for a named tty session. It requests the lease before launching a known `tty: true` manifest definition or retained tty ad hoc definition, and when attaching to an already-running tty incarnation. An unresolved never-launched name and a known non-tty session do not reserve a lease. A competing attach remains an output follower, prints one conflict notice, and never disrupts the owner. `logs --follow` stays output-only. Hum never explicitly appends or replays submitted input, but normal PTY line-discipline or application echo is child output and may therefore be rendered and retained; password secrecy relies on the child disabling terminal echo.

Scope, input transport and incarnation state: the lease uses a dedicated duplex connection, separate from output follow. After attach, the daemon sends an initial state event and one event on every launch and exit; each event identifies running/stopped state and the current launch cursor so a durable owner can target a successor without polling. Writes are synchronous and carry at most 32 KiB after base64 decoding; the client reads the next local chunk only after acknowledgement, and an oversized request fails atomically with typed `input_too_large`. Every write and resize names its launch cursor. Closed and stale-incarnation failures are typed, write no prefix to a successor, and unblock pending operations on exit, restart, remove, client cancellation, and daemon shutdown without blocking other connections. Resize requires non-zero uint16 columns and rows and fails validation before any ioctl. Process exit closes the incarnation PTY but preserves the lease; removal, detach, transport loss, and daemon shutdown close the lease.

Scope, client terminal: when the owner's stdin is a terminal, attached run applies the owner's current window size on acquisition and at launch, switches local stdin to raw mode for the attachment, forwards every terminal byte except Ctrl+] (0x1d), and restores the terminal after detach, transport loss, SIGTERM/SIGHUP, and panic. Ctrl+] is consumed locally, detaches only this observer, and is announced in the attach boundary. Ctrl+C, Ctrl+D, and Ctrl+Z are forwarded as bytes; the child terminal mode determines their effect. The owner alone forwards SIGWINCH resizes. While the session is stopped the client reads and discards local terminal bytes, then resumes forwarding to the next launch cursor. Non-terminal stdin is forwarded without raw mode; EOF releases the input lease while output following continues. Non-tty attached runs preserve Ctrl+C detach and never forward input.

Scope, protocol and presentation: bump the private protocol version. Launch specifications, retained definitions, and process snapshots carry `tty`. Add exclusive input attach/release, input state events, bounded base64 byte writes, resize, acknowledgements, and typed conflict, too-large, closed, and stale-incarnation errors on the dedicated input connection; do not place unencoded arbitrary bytes into NDJSON. CLI `status --json`/`list --json` and MCP status/list include required boolean `tty`. Human status prints `tty: true|false`; human list adds `tty=true` only for tty records so ordinary non-tty list output remains unchanged.

Agents and docs: README.md, docs/design.md, docs/coding-agents.md, CLI help, the embedded skill, and the plugin skill document `tty: true`, ad hoc `--tty`, merged/control-bearing retained output, exclusive input ownership, terminal echo, raw mode, Ctrl+] detach, tty versus non-tty Ctrl+C behavior, restart/stop/shutdown behavior, and the absence of MCP input. Agent guidance leaves tty off unless a tool requires it and prefers the tool's non-interactive mode (`npx --yes`, `CI=1`, `--force`) because bounded logs are not terminal-sanitized. A one-shot CLI/MCP input operation remains DRAFT-002.

Non-goals: tty by default; pipe-mode stdin forwarding; one-shot `hum input` or MCP input; input from `logs --follow`; a `--raw` or sanitized logs mode; byte-exact invalid-UTF-8 output over JSON; terminal emulation, screen-state tracking, scrollback, or multiplexing; multiple input owners; reconnecting a lost owner; input retention or replay; suppressing terminal/application echo; changing non-tty launch, signal, or shutdown semantics; or Windows.

Modified-file contract: internal/process/, internal/app/, internal/protocol/, internal/daemon/, internal/cli/, internal/project/, internal/mcp/, internal/skill/, internal/testutil/, integration/, plugins/hum/skills/hum/SKILL.md, README.md, docs/design.md, docs/coding-agents.md, go.mod, go.sum. `go.mod`/`go.sum` may add only `github.com/creack/pty`, `golang.org/x/term`, and their required `golang.org/x/sys` dependency.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/process -run '^TestTTYProcess$' -count=1 -v` exits 0, prints `--- PASS: TestTTYProcess`, and proves a tty launch gives the child a controlling terminal (`test -t 0` and opening `/dev/tty` succeed), uses `Setsid`/`Setctty` with PGID=PID, merges stdout/stderr once as stdout, applies initial/late-owner/SIGWINCH sizes, delivers written bytes exactly including NUL and invalid UTF-8, treats post-exit EIO/EOF as normal capture completion without descriptor or goroutine leaks, terminates the entire group through SIGTERM/SIGKILL, keeps the PTY master open through group cleanup, and leaves non-tty `/dev/null` stdin and separate streams unchanged.
- [ ] #2 AC2 — `go test ./internal/app -run '^TestTTYSessionInput$' -count=1 -v` exits 0, prints `--- PASS: TestTTYSessionInput`, and proves a known tty definition can acquire one lease before launch or while running; unresolved/non-tty sessions do not reserve one; a second owner is refused without disruption; retained ad hoc launches remember tty; running non-tty incarnations cannot be upgraded; state events identify stopped/running successors and launch cursors; writes/resizes are cursor-scoped and stale operations never reach a successor; exit/restart/remove/cancellation/shutdown unblock pending operations with typed errors; removal/shutdown close the lease while ordinary exit preserves it; snapshots report tty; and successful shutdown uses the existing grace sequence for tty and non-tty groups without closing a PTY master early.
- [ ] #3 AC3 — `go test ./internal/protocol -run '^TestTTYProtocol$' -count=1 -v` and `go test ./internal/daemon -run '^TestTTYInputTransport$' -count=1 -v` both exit 0 and print the corresponding named PASS line. They prove the bumped protocol carries required boolean tty fields; a dedicated duplex input connection implements exclusive attach/release, launch/exit state events, acknowledgements, at-most-32-KiB base64 byte writes, and validated uint16 resize; arbitrary input bytes round-trip exactly; oversize requests fail atomically; conflict/too-large/closed/stale errors remain typed; stalled writes are unblocked by cancellation, exit, restart, remove, and shutdown without cleanup races; and unrelated connections remain responsive.
- [ ] #4 AC4 — `go test ./internal/project -run '^TestTTYManifest$' -count=1 -v`, `go test ./internal/cli -run '^TestTTYCLI$' -count=1 -v`, and `go test ./internal/mcp -run '^TestTTYMCP$' -count=1 -v` all exit 0 and print the corresponding named PASS line. They prove tty accepts only YAML booleans with file/entry context on errors; canonical ad hoc `--tty` forms and declared-name rejection; CLI/MCP start, up, and restart propagation; retained definitions; required JSON/MCP snapshot booleans and human rendering; merged stdout with no tty stderr entries; raw-mode restoration after detach, transport loss, signals, and panic; Ctrl+] consumption; tty Ctrl+C forwarding versus non-tty detach; owner-only resize; output-only conflict attachment with one notice; piped EOF lease release; stopped-session discard and successor resume; unchanged control-bearing bounded output/readiness semantics; and no MCP input tool.
- [ ] #5 AC5 — `go test ./integration -run '^TestTTYInteractiveSession$' -count=1 -v`, `go test ./internal/cli -run '^TestTTYHelpAndDocs$' -count=1 -v`, and `go test ./internal/skill -run '^TestTTYInstructions$' -count=1 -v` all exit 0 and print the corresponding named PASS line. The built-binary test proves a TTY-gated fixture prompts under ad hoc and manifest tty modes, receives typed input, reports tty, exposes merged control-bearing logs and possible terminal echo, keeps the process alive after Ctrl+] detach, attaches a second client output-only, preserves one owner across stop/start with a fresh terminal, makes bare shutdown refuse active work, makes `shutdown --stop-processes` stop the tty group, and leaves non-tty behavior unchanged. The contract tests prove README.md, docs/design.md, docs/coding-agents.md, CLI help, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` document the complete operator and agent contract.
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
- [ ] T1 — Add strict manifest/ad hoc `tty` propagation, retained-definition state, protocol snapshots, and CLI/MCP presentation without changing non-tty behavior.
- [ ] T2 — Add the PTY launch path, controlling-terminal/process-group setup, merged capture, resize, descriptor cleanup, and shutdown ordering.
- [ ] T3 — Add the app-level exclusive lease and launch-cursor-scoped write/resize lifecycle, including successor state events and race-free cancellation.
- [ ] T4 — Add the dedicated daemon/client input connection and versioned protocol operations with bounded base64 payloads and typed errors.
- [ ] T5 — Add attached-run raw-mode, signal, detach-chord, piped-input, stopped-session discard, conflict-notice, and terminal-restoration behavior.
- [ ] T6 — Update operator/agent documentation and prove the complete workflow with focused package suites and a built-binary integration test.
<!-- SECTION:PLAN:END -->
