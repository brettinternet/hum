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
The server exposes `start`, `up`, `down`, `list`, `status`, `logs`, `wait`,
`restart`, `stop`, and `remove`. It has no arbitrary-command `run` or unbounded
follow tool; agents use bounded `wait` and `logs`. For restart-with-work, use
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
observation); Ctrl-D and Ctrl-Z are forwarded in TTY mode. MCP reports `tty` but
has no input tool. Stop/restart preserves the lease across successors,
remove and shutdown close it, and `shutdown --stop-processes` is required when
active work must be stopped.
