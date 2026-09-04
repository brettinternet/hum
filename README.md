# hum

`hum` is a local process supervisor for humans and coding agents. It keeps
project processes alive between commands and gives every client the same
bounded logs and lifecycle controls.

```sh
hum up
hum logs dev --follow
hum down
```

![Demo of hum supervising a process, retaining its logs, and stopping it](docs/demo.gif)

[Demo source](docs/demo.tape)

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
