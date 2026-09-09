# hum

[![CI](https://github.com/brettinternet/hum/actions/workflows/ci.yaml/badge.svg)](https://github.com/brettinternet/hum/actions/workflows/ci.yaml)

Keep local project processes running between commands. `hum` gives humans and coding agents bounded logs, readiness checks, dependencies, JSON/MCP output, and controlled TTY input.

```text
hum.yaml ──> hum daemon ──> db ──> api ──> web
                  │
                  └── bounded logs <── CLI / coding agents
```

[![Demo of hum supervising a process, retaining its logs, and stopping it](docs/demo.gif)](docs/demo.tape)

## Install

Install the latest release with [mise](https://mise.jdx.dev/):

```toml
[tools]
"github:brettinternet/hum" = "latest"
```

To build from a checkout, see [development setup and checks](docs/development.md).

## Quickstart

Try a portable clock process in a fresh directory:

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
```

`hum up` follows output. After a clock line, press Ctrl+C to detach, then stop:

```sh
hum down
```

## Start processes

Without configuration, `hum up` finds a conventional `dev` task in Mise, Task, Just, Make, `package.json`, Deno, Composer, `bin/dev`, or Phoenix.

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

`hum up` starts processes, gates dependents on `ready`, and follows prefixed terminal output. Ctrl+C detaches; `hum down` stops. `hum up --detach` waits and returns. JSON and redirected output stay bounded:

```yaml
processes:
  web:
    argv: [task, "dev:web"]
    ready:
      match: "Listening on"
```

```sh
hum up
hum up --detach
hum status
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

Project selection follows these rules:

- A relative selector starts from the invocation directory.
- The nearest Git root becomes the project root. Without Git, the selected directory is the root.
- Ad-hoc `run` commands use the selected directory as their cwd.
- Manifest `cwd` values stay relative to the project root.
- `init` writes at the project root. `list --all` merges that project's declarations.
- Guidance uses an absolute, shell-safe selector when paths contain spaces.

`--project` does not apply to `serve`, `shutdown`, `mcp`, or `skill`. `-d` means `serve --daemon`, `run --detach`, or `up --detach`.

## Sessions

Run a named process without a manifest:

```sh
hum run preview -- bun run preview
hum logs preview --follow
hum wait preview --match "ready"
hum stop preview
hum remove preview
```

Named sessions are durable. `hum status` summarizes the project; add NAME for details. `hum attach NAME` joins one; `--tail 0` skips replay. TTY attach owns input and resize; attachments follow output. `stop` preserves state; `remove` discards it.

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

Manual controls win. Relaunches reuse the last process definition. Use `hum restart NAME` to adopt changes. Status, JSON, and MCP expose recovery state and relaunch counts. Diagnose failures with `hum logs`.

## Aggregate logs

```sh
hum logs --follow
hum logs web worker --tail 50
hum logs web --stream stdout --match Listening
```

Log selection is predictable:

| Input | Result |
| --- | --- |
| No names | Declared processes in lexical order; ad-hoc sessions are excluded |
| No `--after-cursor` | newest default window |
| `--after-cursor` without `--tail` | Page forward from the oldest retained entry |
| Ctrl+C while following | Close followers; do not stop processes |

Human output is prefixed with `[NAME]`; JSON output uses named NDJSON events. Logs `next` is the consumed cursor; process `next_cursor` is the next cursor to assign.

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

A TTY has one input owner and sends exact text or strict padded base64 once; it never queues or echoes input. `logs --follow` receives output only. Use `--json` for the operation result; MCP provides the same operation.

## Project scopes

Scope is automatic from the invocation directory: canonical Git/worktree roots make symlink aliases share records while separate worktrees stay separate; child cwd remains lexical. Use `hum --project PATH` or `-C PATH`, including for removed known worktrees; `hum list --all` discovers scopes. JSON includes `scope` and `project_root`.
