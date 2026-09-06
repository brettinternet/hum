# hum

[![CI](https://github.com/brettinternet/hum/actions/workflows/ci.yaml/badge.svg)](https://github.com/brettinternet/hum/actions/workflows/ci.yaml)

`hum` is a local process supervisor for humans and coding agents. It keeps
project processes alive between commands and gives every client the same
bounded logs and lifecycle controls.

```sh
# shell A
hum run clock -- ./clock.sh

# shell B
hum logs clock --follow

# ^ these can also be run out of order
```

[![Demo of hum supervising a process, retaining its logs, and stopping it](docs/demo.gif)](docs/demo.tape)

With no configuration, `hum up` finds one conventional `dev` task in Mise,
Task, Just, Make, `package.json`, Deno, Composer, `bin/dev`, or Phoenix.

For projects with multiple processes, commit a `hum.yaml`. Declare readiness before using a process as an `after` dependency; `hum up` starts independent roots concurrently and then gates dependents:

```yaml
version: 1
processes:
  db:
    argv: [docker, compose, up, db]
    ready:
      match: "ready"
  api:
    argv: [bun, run, api]
    after: [db]
    ready:
      match: "Listening"
  web:
    argv: [bun, run, dev]
    after: [api]
    ready:
      match: "Local:"
```

Existing Task or Just definitions can remain the source of truth; `hum.yaml`
can forward to them while adding supervision-specific readiness:

```yaml
version: 1
processes:
  web:
    argv: [task, "dev:web"]
    ready:
      match: "Listening on"
  worker:
    argv: [just, dev-worker]
```

```sh
hum up                         # ordered readiness gates; output is lexical
hum status web
hum logs web worker --tail 50
hum stop web
# run migrations, installs, or other intermediate work
hum start web                  # starts only the explicitly named process
hum down                       # stops project processes concurrently
```

Run a one-off named process without a manifest:

```sh
hum run preview -- bun run preview
```

Process names are durable supervision sessions. Attached `run` and `logs --follow`
stay open across stops and launches until Ctrl+C; they may attach before the first
launch. `wait` is the bounded alternative for automation. `stop` preserves the
session and retained launch state, while `remove` stops and discards runtime state
without editing `hum.yaml`. `hum status <name>` and `hum list --all` report the
number of live attached `run` and `logs --follow` clients as `followers`; this is
a read-only observation and never warns, prompts, or blocks `remove`. Records
without a live daemon session report zero. Unobserved completed sessions remain
bounded by eviction. `down` stops all project processes; a later `up` restarts resolved
definitions, not retained ad hoc sessions. Daemon loss ends followers nonzero
with a diagnostic; followers do not reconnect.

### Crash relaunch

A manifest process may opt into the bounded policy `restart: on-failure` (the
only other value is `never`, which is the default). Unknown values and non-string
values are rejected with the manifest file and process context. Discovered
processes and ad-hoc sessions always use `never`; `hum init` leaves an inert,
commented `restart: on-failure` example in generated templates.

For an opted-in process, a non-zero exit or signal not owned by an explicit
`stop`, `down`, `restart`, `remove`, or shutdown (including their TERM/KILL
control) schedules at most five
automatic relaunches after 1s, 2s, 4s, 8s, and 16s. A spawn failure consumes
that attempt and is retained as a bounded system entry. An automatic
incarnation that survives 30 seconds resets the counter; exit zero and
operator controls also reset it. Backoff is generation-guarded, so a stop or
manual start/restart wins cleanly and no stale timer can launch a child.
Automatic relaunches reuse the last effective argv, cwd, environment, readiness,
and TTY rather than rereading the manifest; use an explicit start/restart to
adopt edits. Readiness timeout never triggers a relaunch.

Status, list, CLI JSON, and MCP snapshots expose `restart`, `relaunches`, and
`next_launch_at` while backoff is pending. Retained logs include each failed
incarnation and the `relaunching`, spawn-failure, and final `gave up` boundaries;
followers remain attached through backoff and exhaustion. Before editing again,
agents should read the failing incarnation's retained output with `hum logs` (or
MCP `logs`) so the crash is diagnosed rather than hidden by recovery.

### Ordered startup

A process may declare `after: [db, queue]`. Every dependency must be a unique
name in the same manifest, must not be the owner, and must declare `ready`;
unknown names, duplicates, cycles, non-lists, and non-string elements are
manifest errors with indexed `process "name".after[index]` context. Readiness is
the gate, and each process's timeout starts when that process launches (or is
first observed already running). Independent roots overlap.

`hum up` emits stable lexical results after all definitions settle. A failed or
unready prerequisite produces `outcome: skipped` with every direct unsatisfied
`blocked_by` name sorted; skips do not add an exit code, so aggregate precedence
remains request error 1, exited before ready 3, timed out 2, and success 0. Before
finalizing a skip, `up` observes any retained record without changing it: JSON
reports `existing_state: running|exited` with its snapshot, while human output
says `existing process running`, `existing process exited`, or `not launched`.
A skipped node still blocks dependents even when its retained record is running.
`up --no-wait` is rejected before daemon contact when any `after` is declared.
`start NAME...` remains explicit-only and never pulls in prerequisites; `down`
remains concurrent. If an `on-failure` prerequisite is recovering, that
invocation records its early exit and skips dependents rather than following the
successor; rerun `hum up` after the prerequisite is ready.

### Aggregate logs

After `hum up`, use `hum logs --follow` to watch every current declaration in one
terminal. `hum logs [NAME...]` accepts names in selection order. Omitting names
resolves the current project's declarations once in lexical order, excludes ad-hoc
sessions, and keeps that membership fixed for the command. Duplicate names are rejected, and
`--after-cursor` remains a single-explicit-name option. Aggregate bounded reads apply
`--stream`, `--match`, `--tail`, and each byte or entry limit independently to every
name; output follows selection order. Human aggregate entries are written atomically
with a `[NAME]` prefix, while JSON uses the existing named NDJSON event objects.
Aggregate follow opens one follower per selected session, serializes writes, keeps
session errors named and isolated, and closes every follower on daemon loss or
output failure. Ctrl+C closes those followers without signaling any managed process.
A single explicit name retains the existing human and JSON output unchanged.

## Install

Install the latest macOS or Linux release with [mise](https://mise.jdx.dev/):

```toml
[tools]
"github:brettinternet/hum" = "latest"
```

## Build

Install mise, then:

```sh
mise install
task init
task cli:build
./bin/hum --help
```

## Coding agents

Codex users can install the bundled hum skill and MCP registration from a
repository checkout after placing `hum` on `PATH`:

```sh
codex plugin marketplace add .
codex plugin add hum@hum
```

`hum mcp` exposes the same project processes, bounded output, and one-shot TTY
input over MCP for manual registration with other coding agents.

See [coding-agent setup](docs/coding-agents.md) for Claude Code, Cursor, the MCP
tool surface, and the shell-only skill fallback.

## Documentation

- [Design and command semantics](docs/design.md)
- [Development setup and checks](docs/development.md)
- [Coding-agent setup](docs/coding-agents.md)

### Interactive TTY sessions

TTY support is opt-in. Add `tty: true` to a process in `hum.yaml`, or use
`hum run NAME --tty -- COMMAND...` (and optionally `--detach`) for an ad-hoc
session. A TTY session has one attached input owner; other `hum run` clients
follow output only, while `logs --follow` is always output-only. Attached TTY
runs preserve terminal control sequences and retain the merged child stream as
`stdout`; `stderr` has no child entries. Terminal echo is produced by the
child, so password secrecy depends on the child disabling echo.

The attached terminal is put in raw mode and restored on detach, transport
loss, signals, panics, and errors. The owner alone forwards SIGWINCH resizes. Press
Ctrl-] to detach only input; Ctrl-C, Ctrl-D, and Ctrl-Z are forwarded to the
child in TTY mode. Non-TTY runs keep the
existing Ctrl-C observer-detach behavior. A TTY lease survives ordinary stop
and restart, targets each launch cursor, discards input while stopped, and is
closed by remove or daemon shutdown. Bare `hum shutdown` still refuses while
work is active; use `hum shutdown --stop-processes` to apply the normal grace
sequence.

For bounded request/response input, observe with `hum logs` or `hum wait --match`,
answer with `hum input NAME --text 'value'` (or MCP `input`), then confirm with
`hum wait --match`. Use `hum input NAME --base64 PADDED_VALUE` for exact binary
bytes. Text sends exact bytes without a newline (it never adds one); base64 must
be strict padded base64 (standard alphabet) without whitespace and decode to at most 32 KiB.
`input` only targets an already-running TTY incarnation, writes once at its
initial launch cursor (at-most-once, with no resend across a launch race), fails
immediately on an ownership conflict when another client owns input, and never
starts, waits, queues, retries, retains, or explicitly echoes the payload.
Success prints the acknowledged byte count and launch cursor; `--json` emits
`name`, `bytes`, and `launch_cursor`. MCP exposes the same bounded operation as
its `input` tool.

Bounded child-output reads (`hum logs`, including JSON and tail, and MCP `logs`)
and child-output matches (`logs --match`, `wait --match`, and readiness matches)
use terminal-control-stripped text. The strip is byte-wise and per entry:
stdout/stderr control sequences are removed, while system entries remain raw.
Patterns containing raw ESC bytes no longer match stripped child text; a `^`
anchor now matches colourised output whose raw first byte is ESC. Stored bytes,
cursors, and limit accounting remain raw, so a control-only bounded
child entry is retained with empty text and raw byte limits still apply.
`logs --follow --match` selects child entries using stripped text but emits the selected
raw entry; follow and attached `run` rendering remain raw. There is no `--raw`
flag or other raw opt-out. Stripping does not emulate a terminal or collapse
redraws: a sequence split across entries can leave its tail visible, and
carriage-return redraw frames stay
separate. Keep TTY off when a tool's non-interactive mode (`--yes`, `CI=1`, or
`--force`) is sufficient.
