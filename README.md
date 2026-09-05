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

For projects with multiple processes, commit a `hum.yaml`:

```yaml
version: 1
processes:
  web:
    argv: [bun, run, dev]
    ready:
      match: "Local:"
  worker:
    argv: [bun, run, worker]
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
hum up
hum status web
hum logs worker --tail 50
hum stop web
# run migrations, installs, or other intermediate work
hum start web
hum down
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

`hum mcp` exposes the same project processes and bounded output over MCP for
manual registration with other coding agents.

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
sequence. MCP reports `tty` in process snapshots but deliberately has no input
tool.

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
