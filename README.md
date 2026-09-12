# hum

[![CI](https://github.com/brettinternet/hum/actions/workflows/ci.yaml/badge.svg)](https://github.com/brettinternet/hum/actions/workflows/ci.yaml)

Keep processes running between commands—with bounded logs, readiness checks, dependencies, JSON/MCP output, and controlled TTY input. See the [changelog](CHANGELOG.md).

```text
hum.yaml ──> hum daemon ──> db ──> api ──> web
                  │
                  └── bounded logs <── CLI / coding agents
```

Run a process and follow its retained logs

[![Demo of hum supervising an ad-hoc clock process while another terminal follows its logs](docs/demo.gif)](docs/demo.tape)

Start and stop a dependency-ordered stack

[![Demo of hum starting a stack by readiness, recovering failed work, and stopping every process](docs/demo-up-down.gif)](docs/demo-up-down.tape)

Let a coding agent diagnose a failed process from its retained logs

[![Demo of Codex using hum MCP to read a failed process's logs and identify its missing environment variable](docs/demo-codex.gif)](docs/demo-codex.tape)

## Install

Install on macOS with [Homebrew](https://brew.sh/):

```sh
brew trust --formula brettinternet/tap/hum
brew install brettinternet/tap/hum
man hum
```

Homebrew installs the generated `hum(1)` manual. Release archives also include `hum.1` for other package integrations.

Or download and verify the latest macOS or Linux release directly:

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/brettinternet/hum/main/install.sh | sh
```

Set `HUM_VERSION=0.9.0` (with or without the leading `v`) to pin a release, or set
`HUM_INSTALL_DIR` to install somewhere other than `$HOME/.local/bin`.

Or install releases with [mise](https://mise.jdx.dev/):

```toml
[tools]
"github:brettinternet/hum" = "latest"
```

To build from a checkout, see [development setup](docs/development.md).

## Quickstart

Try a portable clock process in a directory:

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

`hum up` follows output. Press Ctrl+C to detach, then stop:

```sh
hum down
```

SchemaStore-aware editors load [`hum.schema.json`](hum.schema.json) for `hum.yaml` automatically.
`hum init` also adds an inline schema directive. Other editors can select the root schema manually.
The Go manifest parser remains authoritative.

## Start processes

Without `hum.yaml`, `hum up` finds conventional `dev` tasks in Mise, Task, Just, Make,
`package.json`, Deno, Composer, `bin/dev`, and Mix projects with a literal Phoenix dependency.

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

`hum up` starts in dependency order and follows output. Once startup completes, Ctrl+C detaches
and `hum down` stops; the daemon owns the processes, so closing the follower never kills them.
Ctrl+C during startup aborts instead and stops what that `hum up` launched.
Use `hum up --detach` to wait and return, or `hum up --full` for full readiness details.

For checks that do not emit a reliable startup message, use an executable probe. Exit status 0 marks the process ready. For example, check PostgreSQL inside Docker Compose:

```yaml
ready:
  exec: [docker, compose, exec, -T, db, pg_isready, -U, postgres]
  interval: 1s
  timeout: 30s
```

Or check an HTTP readiness endpoint:

```yaml
ready:
  exec: [curl, --fail, --silent, --show-error, "http://127.0.0.1:3000/readyz"]
  interval: 1s
  timeout: 30s
```

JSON and redirected output stay bounded:

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
hum up --full
hum status
hum status web
hum logs --follow
hum start web
hum stop web
hum down
```

`hum status` shows a compact project overview. `hum status NAME` adds readiness and diagnostics.

`hum start NAME` does not start dependencies. `ready.exec` runs exact argv without a shell, inherits
cwd/env, and retries every second by default. It gates startup, not liveness. `hum down` stops project
processes concurrently. See [design and command semantics](docs/design.md).

### Manifest environments

Load shared values from files, then override or remove them per process:

```yaml
version: 1
environment:
  files: [.env]
processes:
  api:
    argv: [bun, run, api]
    env:
      PORT: "3001"
      LEGACY_DATABASE_URL: null
```

```dotenv
# .env
DATABASE_URL=postgres://localhost/app
LEGACY_DATABASE_URL=postgres://localhost/old
export PORT = "3000" # overridden by api.env
LITERAL_DOLLAR='$NAME'
```

The result for `api` includes `PORT=3001` and no `LEGACY_DATABASE_URL`:

```text
caller environment → .env → processes.api.env
```

- `inherit: false` skips the caller environment.
- `env` accepts strings or `null`; quote numeric and boolean YAML values.
- Every file is required, relative to the selected manifest, and inside the project root.
- Files accept UTF-8 assignments, comments, `export`, whitespace, and whole quoted values.
- Hum does not discover files or expand `$NAME`, `${...}`, `$()`, or backticks. Single-quote literal forms or use an external loader.

No configuration preserves the caller environment exactly. `start`, `up`, declared `run`, and
`restart` load files before daemon contact; read-only commands do not. Processes and automatic
relaunches keep their launch snapshot. Use `hum restart NAME` to reload changes.

Limits: 16 files; 1 MiB and 4,096 assignments per file; 4 MiB per environment; 8 MiB per encoded
request. Environment metadata stays private, but child and probe output is unredacted. Do not print secrets.

### Operate from anywhere

Use `--project DIR` or `-C DIR` before or after the subcommand:

```sh
hum --project /path/to/checkout up
hum status -C ../checkout api
hum run preview --project /path/to/checkout -- bun run preview
```

Select an alternate manifest with `--file PATH` or `-F PATH`:

```sh
hum -F hum.dev.yaml up
hum restart -F hum.test.yaml api
```

- A relative selector starts from the invocation directory.
- Ad-hoc runs use the selected directory; manifest `cwd` stays project-relative.
- `--file` must name a regular file inside the project.
- Without `--file`, Hum uses `hum.yaml`, or conventional discovery when it is absent.
- All manifests share one project namespace. The same process name cannot run twice through separate files.
- Runtime-only commands use `--file` only to identify the project.
- `--file` is unavailable on `version`, `serve`, `init`, `shutdown`, `mcp`, and `skill`.

`hum init` creates only `hum.yaml`; Hum does not merge manifests or support overlays. `-d` means
`--detach` for `daemon`, `run`, and `up`.

## Sessions

Run a named process without a manifest:

```sh
hum run preview -- bun run preview
hum run preview --detach -- bun run preview
hum attach preview
hum logs preview --follow --tail 0
hum wait preview --match "ready"
hum stop preview       # preserve state
hum remove preview     # discard state
hum remove --all       # discard sessions in this scope
```

Put scope selectors before the child `--`. Foreground runs propagate exit status, stop on Ctrl+C
or SIGTERM, and detach on SIGHUP. Detached runs belong to the daemon; `hum attach NAME` and
`hum logs --follow` only observe them.

## Restart on failure

Manifest processes default to `never`. Enable bounded recovery:

```yaml
processes:
  api:
    argv: [bun, run, api]
    restart: on-failure
```

A non-zero exit retries after `1s`, `2s`, `4s`, `8s`, and `16s`, then stops. Manual controls win.
Automatic relaunches reuse the previous definition; `hum restart NAME` adopts manifest changes.
Status, JSON, and MCP show recovery state and counts.

For `stop_grace`:

- Omit it to inherit the daemon default.
- Use `0s` for an immediate kill.
- Restarts adopt changes; automatic relaunches and orphan reclaim keep the previous policy.
- Status, JSON, and MCP show the effective value and whether it was inherited.

## Aggregate logs

```sh
hum logs --follow                                  # declared processes
hum logs web worker --tail 50                     # selected processes
hum logs web --stream stdout --match Listening    # matching stdout
hum logs web --stream system                      # supervision events
```

Ad-hoc sessions are selected by name. The default stream, `both`, includes stdout, stderr, and
system events. `--after-cursor` pages from the oldest retained entry; otherwise logs start with the
newest default window. Ctrl+C closes only the follower.

Human output uses `[NAME]` prefixes; JSON uses named NDJSON events. Logs `next` is the consumed cursor.
A process `next_cursor` is the next cursor to assign.

## JSON and NDJSON

Every supported CLI `--json` result and NDJSON record includes `schema_version: 1`. See the
[version 1 CLI machine-output contract](docs/cli-json-v1.md) for covered commands, required and
optional fields, framing, ordering, exit-code interaction, Compatibility rules, and the boundary
from Hum's private daemon protocol. Attached `hum run` remains raw child output and does not use the CLI JSON contract.

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

Detect Hum's CLI machine-output contract before relying on JSON field semantics:

```sh
hum version --json
# {"schema_version":1,"version":"<version>","build_time":"<time>"}
```

This feature-detection call does not resolve a project or contact the daemon.

### Herdr plugin

With `hum` and Python 3.10+ on Herdr's `PATH`, install the process picker:

```sh
herdr plugin install brettinternet/hum/plugins/herdr --yes
```

The picker uses the public CLI contract to open followed logs or interactive attachments for the
selected workspace. See the [Herdr plugin guide](plugins/herdr/README.md) for actions and ownership.

### Claude Code plugin

With `hum` on `PATH`, install the Claude Code plugin:

```sh
claude plugin marketplace add brettinternet/hum
claude plugin install hum@hum
```

### Codex plugin

Install the Codex plugin from a checkout with `hum` on `PATH`:

```sh
codex plugin marketplace add .
codex plugin add hum@hum
```

The plugins bundle the hum skill and MCP registration. If plugin installation is
unavailable, register `hum mcp` manually as described in the
[coding-agent setup](docs/coding-agents.md).

`hum mcp` exposes project processes, bounded output, and one-shot TTY input.

```json
// .mcp.json
{
  "mcpServers": {
    "hum": {
      "command": "hum",
      "args": ["mcp"]
    }
  }
}
```

Separate worktrees run independently:

```sh
cd .worktrees/agent-a
hum up --detach

cd .worktrees/agent-b
hum up --detach

hum list --all
hum --project .worktrees/agent-a down
```

See [coding-agent setup](docs/coding-agents.md) for Claude Code, Cursor, MCP, and the shell-only skill.

## TTY input

Enable TTY support per process:

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

Each TTY has one input owner. Input is sent once, never queued or echoed. `logs --follow` receives output only. `--json` and MCP expose the same operation result.

## Project scopes

```sh
hum status                            # nearest Git root
hum -C ../other-worktree status       # another project, even if removed
hum list                              # compact name, state, and PID
hum list --full                       # all human-readable process details
hum list --all                        # every scope; combine with --full if needed
hum -g run proxy -- caddy run         # machine-wide ad-hoc session
hum signal proxy HUP --global         # global selector after positionals
```

Project roots are canonical: symlink aliases share a scope, while separate worktrees do not. Use
`--project PATH` or `-C PATH`
to select another project. Use `--global` or `-g` only for machine-wide ad-hoc sessions, before
`run`'s child `--`.

`--global` conflicts with `--project` and `list --all`; `init` and `up` reject it. JSON reports
`scope` as `project` or `global`; global records omit `project_root`.
