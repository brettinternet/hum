# hum

[![CI](https://github.com/brettinternet/hum/actions/workflows/ci.yaml/badge.svg)](https://github.com/brettinternet/hum/actions/workflows/ci.yaml)

Agent-oriented process supervisor with retained bounded logs, independent followers, readiness/dependencies, structured JSON/MCP, and controlled TTY input.

`hum` keeps local project processes running between commands, with bounded logs and lifecycle controls. Let your agents see your stdout.

[![Demo of hum supervising a process, retaining its logs, and stopping it](docs/demo.gif)](docs/demo.tape)

## Install

Install the latest release with [mise](https://mise.jdx.dev/):

```toml
[tools]
"github:brettinternet/hum" = "latest"
```

To build from a checkout, see [development setup and checks](docs/development.md).

## Quickstart

In a fresh directory, create a portable clock process and try the full lifecycle:

```sh
mkdir hum-quickstart && cd hum-quickstart
git init -q
cat > hum.yaml <<'YAML'
version: 1
processes:
  clock:
    argv: [sh, -c, "while :; do date; sleep 1; done"]
YAML
hum run hello --detach -- sh -c 'printf "hello from hum\\n"'
hum up
hum logs --follow
```

After the first clock line, press Ctrl+C to stop following logs, then stop the project process:

```sh
hum down
```

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
hum logs --follow
hum start web
hum stop web
hum down
```

`start` is explicit and does not start dependencies. `down` stops project processes concurrently. See [design and command semantics](docs/design.md) for validation and exit details.

### Operate from anywhere

Project-scoped commands accept a persistent `--project DIR` selector (or `-C DIR`) before or after the subcommand:

```sh
hum --project /path/to/checkout up
hum status -C ../checkout api
hum run preview --project /path/to/checkout -- bun run preview
```

A relative selector is resolved from the invocation directory, cleaned to an existing directory, and then resolved to the nearest Git root (or that directory when no Git marker exists). The selected directory is the cwd for ad-hoc `run`; manifest process `cwd` values remain relative to the resolved project root. `init` writes at that root, and `list --all` still merges declarations from the selected project. Follow-up guidance uses an absolute, shell-safe `--project` selector when the path needs spaces. The selector is not applicable to `serve`, `shutdown`, `mcp`, or `skill`. Existing `-d` aliases remain `serve --daemon` and `run --detach`.

## Sessions

Run a named process without a manifest:

```sh
hum run preview -- bun run preview
hum logs preview --follow
hum wait preview --match "ready"
hum stop preview
hum remove preview
```

Names identify durable sessions. `hum attach NAME` joins only a running session; `--tail 0` skips replay. TTY input and resize use the exclusive lease; non-TTY attach follows output. `hum logs --follow` is read-only; `hum run NAME` starts or attaches. `stop` preserves state; `remove` discards it. Status shows followers.

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

Without names, logs selects lexical declarations once; ad-hoc sessions are excluded. Bounded reads without `--after-cursor` show the newest default window; explicit cursors without `--tail` page forward from the oldest retained entry. Output is `[NAME]`-prefixed or named NDJSON; Ctrl+C only closes followers. Logs `next` is the consumed cursor; process `next_cursor` is the next assigned cursor.

## Shell completion

Completion is opt-in and does not start a daemon:

```sh
# bash
source <(hum completion bash)

# zsh
source <(hum completion zsh)

# fish
hum completion fish > ~/.config/fish/completions/hum.fish
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
