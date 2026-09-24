# hum

[![CI](https://github.com/brettinternet/hum/actions/workflows/ci.yaml/badge.svg)](https://github.com/brettinternet/hum/actions/workflows/ci.yaml)

Hum runs your dev processes in the background. You and your coding agent can start, check, and stop them from any terminal.

[![Demo of hum supervising a clock process while another terminal follows its logs](docs/demo.gif)](docs/demo.tape)

```text
you (CLI) ───┐                    ┌─▶ db
             ├──▶ hum daemon ─────┼─▶ api   starts after db is ready
agent (MCP) ─┘    keeps the logs  └─▶ web   starts after api is ready
```

Processes keep running when you close the terminal that started them.

<table>
<tr>
<td width="50%"><a href="docs/demo-up-down.tape"><img src="docs/demo-up-down.gif" alt="Demo of hum starting a stack in order, recovering failed work, and stopping every process"></a><br>Start and stop a stack in order</td>
<td width="50%"><a href="docs/demo-codex.tape"><img src="docs/demo-codex.gif" alt="Demo of Codex using hum MCP to read a failed process's logs and find its missing environment variable"></a><br>An agent finds why a process failed</td>
</tr>
</table>

## Install

```sh
brew trust --formula brettinternet/tap/hum
brew install brettinternet/tap/hum
```

| Other ways | Command |
| --- | --- |
| Script (macOS, Linux) | `curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/brettinternet/hum/main/install.sh \| sh` |
| [mise](https://mise.jdx.dev/) | `mise use -g github:brettinternet/hum` |
| From source | See [development setup](docs/development.md) |

The script verifies checksums and installs to `~/.local/bin`. Set `HUM_VERSION=0.9.0` to pin a release or `HUM_INSTALL_DIR` to change the location. Homebrew and release archives include the `hum(1)` man page.

<details>
<summary>Windows (amd64)</summary>

Download the release zip, check its SHA-256, and unpack it:

```powershell
$version = '1.2.3' # replace with the release version
$asset = "hum-$version-windows-x64.zip"
$base = "https://github.com/brettinternet/hum/releases/download/v$version"
Invoke-WebRequest "$base/$asset" -OutFile $asset
Invoke-WebRequest "$base/checksums.txt" -OutFile checksums.txt
$line = Get-Content checksums.txt | Where-Object { $_ -match "^([0-9a-fA-F]{64})  \./$([regex]::Escape($asset))$" }
if (@($line).Count -ne 1) { throw 'hum checksum entry missing or duplicated' }
if ((Get-FileHash $asset -Algorithm SHA256).Hash -ine ($line -split ' ')[0]) { throw 'hum checksum mismatch' }
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\hum'
Expand-Archive $asset -DestinationPath $installDir -Force
$env:Path += ";$installDir" # add this directory to your user PATH for future shells
& (Join-Path $installDir 'hum.exe') --version
```

Everything works except `hum signal`. TTY processes use ConPTY: in `hum attach`, Ctrl+] detaches, Ctrl+C goes to the process, and Ctrl+D and Ctrl+Z are passed to the process as ordinary keys (many Windows programs treat Ctrl+Z as end of input). See [Windows behavior](docs/design.md#platforms).

</details>

## Quickstart

Save this as `hum.yaml` in a Git repository:

```yaml
version: 1
processes:
  db:
    argv: [sh, -c, "sleep 1; echo ready; exec sleep 3600"]
    ready: {match: ready}
  api:
    argv: [sh, -c, "echo listening; while :; do date; sleep 2; done"]
    after: [db]
    ready: {match: listening}
```

Start it, check it, read its logs, and stop it:

```console
$ hum up --detach
hum up: db: started; waiting for readiness
hum up: db: ready
hum up: api: started; waiting for readiness
hum up: api: ready
NAME  RESULT   STATE    PID
api   started  running  28813
db    started  running  27456

$ hum status
NAME  STATE    PID    READINESS  RESTART  FOLLOWERS
api   running  28813  ready      never    0
db    running  27456  ready      never    0

$ hum logs api --tail 2
Thu Sep 24 15:48:03 MDT 2026
Thu Sep 24 15:48:05 MDT 2026
next cursor: 7

$ hum down
api stopped
db stopped
```

Without `--detach`, `hum up` follows the logs. Ctrl+C stops following; the processes keep running.

`hum init` writes a starter `hum.yaml`. Editors with SchemaStore check it against [`hum.schema.json`](hum.schema.json). See [`hum.example.yaml`](hum.example.yaml) for every option.

## Everyday commands

| To | Run |
| --- | --- |
| Start everything and follow logs | `hum up` |
| Start in the background | `hum up --detach` |
| Start `api` and what it needs | `hum up api` |
| See what's running | `hum status` |
| See one process in detail | `hum status api` |
| See every project | `hum list --all` |
| Read recent logs | `hum logs api --tail 50` |
| Read only errors | `hum logs api --stream stderr` |
| Follow all logs | `hum logs --follow` |
| Wait for a log line | `hum wait api --match ready --timeout 30s` |
| Restart and reload config | `hum restart api` |
| Stop one process | `hum stop api` |
| Stop everything | `hum down` |
| See starts, exits, and failures | `hum events --failed` |
| Check setup without starting anything | `hum doctor` |

Add `--json` for machine-readable output ([format](docs/cli-json-v1.md)). `hum start api` starts only `api`; `hum up api` also starts everything listed in its `after`. Full command reference: [design](docs/design.md) or `man hum`.

## Configure processes

### Wait until ready

A process listed in `after` waits until this check passes:

| Ready when | Config |
| --- | --- |
| a log line matches | `ready: {match: "Listening"}` |
| a command exits 0 | `ready: {exec: [pg_isready, -h, localhost]}` |
| an HTTP GET returns 2xx | `ready: {http: "http://127.0.0.1:3000/readyz"}` |
| a TCP port accepts | `ready: {tcp: "127.0.0.1:5432"}` |
| the process itself exits 0 | `ready: {exit: 0}` |

`exec`, `http`, and `tcp` checks retry every `interval` (1s); every check gives up after `timeout` (30s). They run only at startup; Hum does not monitor health. HTTP and TCP targets must use an IP address or `localhost`.

Use `exit: 0` for setup steps such as migrations:

```yaml
processes:
  migrate:
    argv: [bun, run, migrate]
    ready: {exit: 0}
  api:
    argv: [bun, run, api]
    after: [migrate]
```

If `migrate` fails, `api` does not start.

### Restart on failure

```yaml
processes:
  api:
    argv: [bun, run, api]
    restart: on-failure # retry after 1s, 2s, 4s, 8s, 16s, then give up
    stop_grace: 5s      # wait between SIGTERM and SIGKILL (default 10s; 0s kills at once)
```

`hum stop` always wins over automatic restarts.

### Environment

```yaml
version: 1
environment:
  files: [.env]
processes:
  api:
    argv: [bun, run, api]
    env:
      PORT: "3001"
      LEGACY_DATABASE_URL: null # remove it
```

```text
your shell env  →  .env  →  processes.api.env     (later wins)
```

Set `environment.inherit: false` to skip your shell env. Hum never expands `$VAR`, `$(...)`, or backticks. Changes apply on `hum restart`. Limits and file syntax: [design](docs/design.md#environment).

## Run without a manifest

```sh
hum run preview -- bun run preview            # run in the foreground
hum run preview --detach -- bun run preview   # run in the background
hum attach preview                            # watch it
hum logs preview --follow
hum stop preview                              # stop, keep logs
hum remove preview                            # stop and forget
```

## Projects and worktrees

Each Git root is its own project, so every worktree gets its own processes:

```sh
hum -C .worktrees/agent-a up --detach
hum -C .worktrees/agent-b up --detach
hum list --all
hum -C .worktrees/agent-a down
```

Hum does not assign ports. To run two worktrees at once, give each a different `PORT` in a Git-ignored `.env.local` listed under `environment.files`.

| Pick a project or manifest | Example |
| --- | --- |
| Another project | `hum -C ../other status` |
| Another manifest | `hum -F hum.test.yaml up` |
| Machine-wide, no project | `hum -g run proxy -- caddy run` |

Hum uses `--file` if given, else `.hum.yaml`, else `hum.yaml`. It never merges them. Put a personal `.hum.yaml` in `.gitignore` to use Hum without changing shared config.

## Interactive processes

```yaml
processes:
  console:
    argv: [./console]
    tty: true
```

```sh
hum attach console                   # type into it; Ctrl+] detaches
hum input console --text 'yes'       # answer a prompt without attaching
```

Only one client can type at a time. Input is sent once and never queued.

## Coding agents

Install a plugin, or point any MCP client at `hum mcp`:

```sh
claude plugin marketplace add brettinternet/hum && claude plugin install hum@hum   # Claude Code
codex plugin marketplace add . && codex plugin add hum@hum                         # Codex, from a checkout
herdr plugin install brettinternet/hum/plugins/herdr --yes                         # Herdr process picker
```

```json
{ "mcpServers": { "hum": { "command": "hum", "args": ["mcp"] } } }
```

Scripts can check the JSON format version first:

```console
$ hum version --json
{"schema_version":1,"version":"<version>","build_time":"<time>"}
```

See [coding-agent setup](docs/coding-agents.md) and the [Herdr plugin](plugins/herdr/README.md).

## Shell completion

```sh
source <(hum completion bash)                              # bash
source <(hum completion zsh)                               # zsh
hum completion fish > ~/.config/fish/completions/hum.fish  # fish
```

## Compared to other tools

| | Hum | [pitchfork](https://pitchfork.jdx.dev/) | [Overmind](https://github.com/DarthSim/overmind) | [Hivemind](https://github.com/DarthSim/hivemind) | [mprocs](https://github.com/pvolok/dekit/blob/master/README-mprocs.md) |
| --- | --- | --- | --- | --- | --- |
| Config | YAML, exact argv | TOML, shell | Procfile | Procfile | YAML or Procfile |
| Ready checks | match, exec, HTTP, TCP, exit | match, command, HTTP, TCP, delay | – | – | – |
| Log history | bounded output + event history | SQLite with search | tmux | – | TUI, files |
| Resume reads from a cursor | yes: `--after-cursor` returns `next` | – | – | – | – |
| Type into a process | attach, one-shot input | – | tmux | stdin | TUI |
| MCP tools | 13 | 5 | – | – | – |
| UI | none (Herdr panes) | TUI, web | tmux | terminal | TUI |

`–` means not documented.

Pick pitchfork for ports, a reverse proxy, boot start, cron, file-watch restarts, lifecycle hooks, or a UI. Pick Hum for exact argv, per-worktree isolation with no setup, and a small, stable API for agents: cursor-paged `logs` and `events`, `wait`, `input`, `signal`, and versioned JSON. An agent can read a page, act, then continue from the last cursor without missing or repeating a line. They can share a repo: keep Hum config in a Git-ignored `.hum.yaml`.

## Non-goals

Hum does not provide a UI, port allocation, a reverse proxy, scheduling, boot start, file-watch restarts, health monitoring, resource limits, log queries, or shell templating. See [decision-001](backlog/decisions/decision-001%20-%20Hum-stays-a-process-API-no-port-allocation-proxying-or-shell-level-conveniences.md).
