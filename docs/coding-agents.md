# Coding-agent setup

## Install the Codex plugin

The plugin bundles the hum workflow skill and MCP registration. Install `hum`
on `PATH`, then from a hum repository checkout run:

```sh
codex plugin marketplace add .
codex plugin add hum@hum
```

Start a new Codex session after installation. The plugin runs `hum mcp`, so the
executable must remain available on `PATH` in Codex's environment.

Use the manual MCP registration below for other coding agents or when plugin
installation is unavailable.

`hum mcp` is an MCP server over stdio. Register it once by pointing the client
directly at the `hum` executable. Use an absolute path; do not wrap the command
in `sh -c` or include a project path in the registration.

## Register the MCP server manually

Claude Code:

```sh
claude mcp add --transport stdio hum -- /absolute/path/to/hum mcp
```

Codex CLI:

```sh
codex mcp add hum -- /absolute/path/to/hum mcp
```

Cursor and other clients that accept an `mcpServers` configuration:

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

Every tool call requires `project_root`, set to the project's absolute path.
Prefer `up` over sequencing `start` calls when `hum.yaml` declares `after`:
independent roots launch concurrently, each dependency waits for readiness, and
each process timeout starts at its launch or first running observation; final
results are lexical. `up` reports `skipped` with sorted direct
`blocked_by` names when a prerequisite fails. A skip may include read-only
`existing_state` and process snapshot data; it still means no launch occurred
and still blocks dependents. CLI `up --no-wait` and MCP
`no_wait: true` are rejected before daemon contact for such a manifest. `start` remains explicitly named and never
pulls in prerequisites. If an `on-failure` prerequisite is recovering, rerun
`up` after it is ready rather than expecting the same invocation to follow its
successor.
The server exposes `start`, `up`, `down`, `list`, `status`, `logs`, `wait`,
`input`, `restart`, `stop`, and `remove`. Its bounded `input` tool accepts
exact non-empty text or strict padded base64 without whitespace for an
already-running TTY incarnation; text sends exact bytes without a newline and returns its launch cursor;
it never starts, waits, queues, retries, retains, or echoes input and an
ownership conflict fails immediately; payloads are bounded at 1-32768 bytes.
The one-shot operation is at-most-once and does not resend across a launch race;
hum neither retains nor explicitly echoes submitted bytes. It has no
arbitrary-command `run` or unbounded follow tool; agents use bounded
`wait` and `logs`. For restart-with-work, use
`stop`, run the intermediate command, then `start`: the durable session preserves
terminal followers. `remove` is different from `stop`: it discards retained
runtime state and output but never edits `hum.yaml`. `down` preserves sessions;
a later `up` starts resolved definitions only, leaving ad hoc sessions stopped.

See [the MCP design](design.md#mcp-stdio-adapter) for detailed behavior and
failure semantics.

## Deterministic environments

MCP does not run an interactive shell or activate Mise, nvm, direnv, or similar
hooks. If a process needs environment activation, commit a wrapper and use its
exact argv in `hum.yaml`:

```yaml
processes:
  web:
    argv: [./tools/run-with-project-env, bun, run, dev]
```

## Crash relaunch policy

A manifest process can opt in with `restart: on-failure`; `never` is the
strict default and the only other accepted value. A manifest `after` list must
contain unique same-manifest names that declare `ready`; malformed values,
unknown names, self-reference, duplicates, and cycles are rejected. Discovery and ad-hoc sessions
always use `never`, and invalid or non-string values fail manifest validation.
The policy retries non-zero or signal exits after 1s, 2s, 4s, 8s, and 16s, at
most five times. Spawn failures consume an attempt. An automatic child alive for
30 seconds resets the counter; stop, down, restart, remove, shutdown, and a
manual start cancel pending work. Automatic launches retain their last argv,
cwd, environment, readiness, and TTY, so explicitly restart after changing a
definition. Read `restart`, `relaunches`, and `next_launch_at` in status/list
snapshots. Followers stay attached and bounded logs retain failure and relaunch
boundaries. Always read the failing incarnation's retained output with `logs`
before editing again.

## Shell-only fallback

When MCP is unavailable, `hum skill` prints the embedded Agent Skills file for
installation in an agent's normal skill directory. MCP remains the preferred
integration.

### Bounded output

Bounded child-output reads and matches use byte-wise terminal-control-stripped
text, applied independently per entry to each stdout/stderr stream. This covers
bounded `logs` (including JSON and tail), MCP `logs`, `logs --match`,
`wait --match`, and readiness matches. System entries remain raw; stored bytes remain
raw; cursors and `MaxBytes`/entry-limit accounting also use raw stored lengths.
Patterns containing raw ESC bytes no longer match stripped child text; a `^`
anchor now matches colourised output whose raw first byte is ESC. There is no
`--raw` flag or other raw opt-out. A control-only bounded child entry
is retained with empty text. `logs --follow --match` selects using stripped child
text but emits selected raw entries, and follow plus attached `run` rendering
stay raw. The strip does not collapse carriage-return redraw frames or emulate a
terminal; a sequence split
across entries can leave its tail visible.

### Interactive sessions

Leave `tty` off unless a tool genuinely requires a controlling terminal; prefer
its non-interactive mode (`npx --yes`, `CI=1`, or `--force`). A manifest can opt
in with `tty: true`, or an operator can use `hum run NAME --tty -- COMMAND`. Only
one attached run forwards input; competing runs and `logs --follow` are
output-only. The owner uses raw mode and alone forwards SIGWINCH resizes;
Ctrl-] detaches input, raw mode is restored after panic, terminal echo is
controlled by the child, and Ctrl-C is forwarded only in TTY mode (normal
non-TTY runs still use Ctrl-C to detach
observation); Ctrl-D and Ctrl-Z are forwarded in TTY mode. MCP reports `tty` and
provides the same bounded `input` tool for exact prompt responses. Stop/restart
preserves the lease across successors,
remove and shutdown close it, and `shutdown --stop-processes` is required when
active work must be stopped. For a one-shot prompt response, use MCP `input` or
`hum input NAME --text VALUE`; text does not add a newline, and `--base64`
requires strict padded base64 without whitespace. The bounded loop is observe with
`wait --match` or `logs`, answer with `input`, then confirm with `wait --match`.
