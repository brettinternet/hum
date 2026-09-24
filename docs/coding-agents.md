# Coding agents

Agents use Hum through MCP (`hum mcp`) or the CLI. Both talk to the same daemon, so you and your
agent see the same processes and logs.

```text
agent ──MCP──▶ hum mcp ──┐
                         ├──▶ hum daemon ──▶ your processes
you ───CLI──▶ hum ───────┘
```

## Install a plugin

Each plugin bundles the Hum skill and MCP registration. `hum` must be on `PATH` in the agent's
environment. Start a new agent session after installing.

Claude Code:

```sh
claude plugin marketplace add brettinternet/hum
claude plugin install hum@hum
```

Codex, from a Hum checkout:

```sh
codex plugin marketplace add .
codex plugin add hum@hum
```

Herdr process picker (needs Python 3.10+; see the [plugin guide](../plugins/herdr/README.md)):

```sh
herdr plugin install brettinternet/hum/plugins/herdr --yes
```

## Register the MCP server manually

Point the client straight at the `hum` executable with an absolute path. Do not wrap it in `sh -c`
or add a project path; each tool call names its project.

```sh
claude mcp add --transport stdio hum -- /absolute/path/to/hum mcp   # Claude Code
codex mcp add hum -- /absolute/path/to/hum mcp                      # Codex CLI
```

Cursor and other `mcpServers` clients:

```json
{
  "mcpServers": {
    "hum": {
      "command": "/absolute/path/to/hum",
      "args": ["mcp"]
    }
  }
}
```

### Over SSH

SSH can carry Hum's CLI or MCP stream; Hum itself has no remote transport.

```sh
ssh devbox hum --project /srv/app logs web --stream stderr --tail 100
```

```json
{
  "mcpServers": {
    "hum-remote": {
      "command": "ssh",
      "args": ["-T", "-o", "BatchMode=yes", "devbox", "/usr/local/bin/hum", "mcp"]
    }
  }
}
```

SSH login and host-key checks must work without prompts. Tool calls use remote paths, so pass a
remote absolute `project_root` such as `/srv/app`.

## Tools

| Tool | CLI | Use it to |
| --- | --- | --- |
| `up` | `hum up` | start declarations in `after` order and wait until ready |
| `start` | `hum start` | start named processes without their prerequisites |
| `status` | `hum status` | check the project or one process |
| `list` | `hum list` | list processes; `all: true` covers every scope |
| `logs` | `hum logs` | read bounded output |
| `wait` | `hum wait` | wait for a log match or exit |
| `events` | `hum events` | read recent starts, exits, and failures |
| `input` | `hum input` | answer a TTY prompt once |
| `restart` | `hum restart` | restart and apply a changed definition |
| `stop` | `hum stop` | stop a process and keep its logs |
| `remove` | `hum remove` | stop a process and discard its state |
| `down` | `hum down` | stop the whole project |
| `signal` | `hum signal` | send a signal without changing restart policy |

Every call takes an absolute `project_root` (resolved to its Git root), or `scope: "global"` for
machine-wide ad-hoc sessions. `start`, `up`, `restart`, and `list` also accept `manifest` to pick
an alternate file. Nothing follows forever: `logs`, `wait`, and `events` are bounded, and there is
no arbitrary-command tool.

Example call:

```json
{"name": "logs", "arguments": {"project_root": "/home/me/app", "name": "api", "stream": "stderr", "tail": 50}}
```

Full argument, result, and error semantics: [MCP adapter](design.md#mcp-adapter).

## Workflows

Start the stack and confirm it is up:

```text
up ──▶ status
```

If `up` reports `exited_before_ready`, `timed_out`, or `skipped` (with `blocked_by`), read the
failing process's logs before changing anything:

```text
logs {name, stream: "stderr", tail: 50} ──▶ fix ──▶ restart {name}
```

Answer an interactive prompt:

```text
wait {match: "prompt"} ──▶ input {text: "yes\n"} ──▶ wait {match: "complete"}
```

`input` sends exact bytes with no added newline, at most once. Bounded `logs` can replace the first
`wait`.

Find a match with surrounding lines:

```json
{"name": "logs", "arguments": {"project_root": "/home/me/app", "name": "api", "match": "panic", "context": 5}}
```

To page forward, pass the returned `next` back as `after`.

To jump from a failed exit to its logs, read `events` for the process and find its `launch`
`log_cursor`; pass that cursor to `logs` as `after` to read its incarnation. The `exit`
`log_cursor` marks its last output entry (use it to read later output). If the first launch has
no cursor, omit `after`. For example:

```text
events {names: ["api"], failed: true} ──▶ events {names: ["api"]} (find preceding launch)
  ──▶ logs {name: "api", after: <launch log_cursor>}
```

Log cursors only apply to the same retained session: `hum remove` and daemon replacement discard
output but not event history. Compare log entry `time` to event `time` if a cursor may be stale.

## Tips

- Prefer `up` over a series of `start` calls. It handles `after` ordering, readiness, and setup
  steps (`ready: {exit: 0}`); `start` never pulls in prerequisites.
- After editing `hum.yaml`, use `restart`. `start` and `up` report `definition_drift` instead of
  adopting changes, and automatic restarts keep the old definition.
- `restart: on-failure` retries after 1s, 2s, 4s, 8s, and 16s. While it is retrying, `up` reports
  `recovery_pending`; rerun `up` once the process is ready. Check `restart`, `relaunches`, and
  `next_launch_at` in `status`.
- `remove` discards runtime state and output; it never edits `hum.yaml`. Use `stop` to keep logs.
- `signal` never stops automatic restarts, even for TERM or KILL. Use `stop`.
- To restart with work in between, `stop`, do the work, then `start`; followers stay attached.
- Leave `tty` off unless a tool truly needs a terminal. Prefer a non-interactive mode such as
  `npx --yes`, `CI=1`, or `--force`.
- Separate worktrees are separate projects. Pass each worktree's own `project_root`.

### Deterministic environments

MCP runs no interactive shell and does not activate mise, nvm, direnv, or similar hooks. Put what a
process needs in the manifest:

```yaml
environment:
  inherit: true
  files: [.env]
processes:
  web:
    argv: [bun, run, dev]
    env:
      PORT: "3000"
      OLD_URL: null
```

Values are literal; Hum never expands `$NAME`. Environment values never appear in tool results, but
child output is not redacted, so commands should not print secrets. Rules and limits:
[Environment](design.md#environment).

## Shell-only fallback

Without MCP, `hum skill` prints the embedded Agent Skills file for the agent's skill directory.
Check the JSON contract first, then use `--json` on CLI commands:

```console
$ hum version --json
{"schema_version":1,"version":"<version>","build_time":"<time>"}
$ hum logs api --stream stderr --tail 50 --json
$ hum wait api --match ready --timeout 30s --json
```

See [CLI JSON v1](cli-json-v1.md) for the format and [Canonical project scopes](design.md#canonical-project-scopes)
for `--project` and `--global`.
