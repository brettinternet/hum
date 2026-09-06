# hum

[![CI](https://github.com/brettinternet/hum/actions/workflows/ci.yaml/badge.svg)](https://github.com/brettinternet/hum/actions/workflows/ci.yaml)

Agent-oriented process supervisor with retained bounded logs, independent followers, readiness/dependencies, structured JSON/MCP, and controlled TTY input.

`hum` keeps local project processes running between commands, with bounded logs and lifecycle controls. Let your agents see your stdout.

```sh
hum run clock -- ./clock.sh
hum logs clock --follow
```

[![Demo of hum supervising a process, retaining its logs, and stopping it](docs/demo.gif)](docs/demo.tape)

## Start processes

With no configuration, `hum up` finds a conventional `dev` task in Mise, Task, Just, Make, `package.json`, Deno, Composer, `bin/dev`, or Phoenix.

For multiple processes, add `hum.yaml`:

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

`hum up` starts independent processes concurrently and waits for each `ready` match before starting dependents. Existing Task and Just commands can remain the source of truth:

```yaml
processes:
  web:
    argv: [task, "dev:web"]
    ready:
      match: "Listening on"
```

```sh
hum up
hum status web
hum logs web --tail 50
hum start web
hum stop web
hum down
```

`start` is explicit and does not start dependencies. `down` stops project processes concurrently. See [design and command semantics](docs/design.md) for validation and exit details.

## Sessions

Run a named process without a manifest:

```sh
hum run preview -- bun run preview
hum logs preview --follow
hum wait preview --match "ready"
hum stop preview
hum remove preview
```

Names identify durable sessions. Attached `run` and `logs --follow` clients stay attached across stops and launches until Ctrl+C. `wait` is bounded for automation. `stop` preserves session state; `remove` stops and discards it. `hum status NAME` and `hum list --all` show live followers. Followers do not reconnect after daemon loss.

## Restart on failure

Manifest processes default to `never`. Enable bounded recovery with `on-failure`:

```yaml
processes:
  api:
    argv: [bun, run, api]
    restart: on-failure
```

A non-zero exit schedules at most five relaunches after:

```text
1s, 2s, 4s, 8s, 16s
```

Manual controls win. Relaunches reuse the last effective process definition and do not reread the manifest. Use `hum restart NAME` to adopt definition changes. Status, JSON, and MCP snapshots expose recovery state and relaunch counts. Read retained logs with `hum logs` to diagnose failures.

## Aggregate logs

```sh
hum logs --follow
hum logs web worker --tail 50
hum logs web --stream stdout --match Listening
```

Without names, logs selects the current project declarations once, in lexical order. Ad-hoc sessions are excluded. Named output is prefixed with `[NAME]`; JSON uses named NDJSON events. Limits and filters apply independently to each process. Ctrl+C closes log followers without stopping processes.

## Install and build

Install the latest release with [mise](https://mise.jdx.dev/):

```toml
[tools]
"github:brettinternet/hum" = "latest"
```

Build from a checkout:

```sh
mise install
task init
task cli:build
./bin/hum --help
```

## Coding agents

With `hum` on `PATH`:

```sh
codex plugin marketplace add .
codex plugin add hum@hum
```

`hum mcp` exposes project processes, bounded output, and one-shot TTY input over MCP.

See [coding-agent setup](docs/coding-agents.md) for Claude Code, Cursor, MCP, and the shell-only skill.

## TTY input

TTY support is opt-in:

```yaml
processes:
  console:
    argv: [./console]
    tty: true
```

```sh
hum run console --tty -- ./console
hum logs console
hum input console --text 'value'
hum input console --base64 PADDED_VALUE
```

A TTY has one input owner. Other `run` clients and `logs --follow` receive output only. Ctrl-] detaches input. Ctrl-C, Ctrl-D, and Ctrl-Z go to the child.

`--text` sends exact bytes without a newline. `--base64` accepts strict padded base64 up to 32 KiB. `input` targets only a running TTY, sends once, and never queues, retries, or echoes the payload. Use `--json` for `name`, `bytes`, and `launch_cursor`. MCP provides the same `input` operation.

TTY log matches strip terminal control sequences from child text; emitted follow output remains raw. Keep TTY off when non-interactive modes such as `--yes`, `CI=1`, or `--force` are sufficient.
