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

MCP requests with IDs run concurrently up to 64 in flight.

- A 65th request is rejected with `-32001` without starting, and a duplicate in-flight ID is
  rejected with `-32600`; notifications and responses consume no slots.
- `notifications/cancelled` cancels exactly its matching request and returns `-32800`, while an
  unknown cancellation ID does nothing.
- Responses remain serialized through the Serve-owned closeable response transport.
- On stdin EOF or parent cancellation, request contexts are cancelled, handlers are given at
  most one second to finish, the response transport is closed to unblock writes, and the writer
  is joined before `hum mcp` returns within two seconds; no handler or writer goroutines are
  left behind.
- A request abandoned that way returns the tool error code `cancelled` rather than `internal`.

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

Tool calls accept `scope`: omit it for project scope and provide the absolute `project_root`; use `scope: "global"` without `project_root` for retained global sessions. `up` supports project scope only. Use `list` with `all: true` only from project scope; it includes global records.

Each tool rejects fields outside its advertised closed input schema before project resolution or daemon contact. Aggregate `up` and `down` do not accept `name`.

- Prefer `up` over sequencing `start` calls when `hum.yaml` declares `after`: independent roots
  launch concurrently, each dependency waits for readiness, and each process timeout starts at
  its launch or first running observation; final results are lexical.
- `up` reports `skipped` with sorted direct `blocked_by` names when a prerequisite fails.
- A skip may include read-only `existing_state` and process snapshot data; it still means no
  launch occurred and still blocks dependents.
- A changed running or recovery-capable manifest record returns `definition_drift` with sorted
  `changed_fields` and `hum restart NAME` guidance; CLI `up` exits 1 for drift and it never
  satisfies an `after` dependency.
- The readiness matcher and normalized restart policy are comparison boundaries; environment and
  readiness timeout are not compared.
- A removed manifest-sourced running or recovery-capable record returns `removed_definition`
  with `hum stop NAME` or `hum remove NAME` guidance; it is lexical and does not change
  aggregate status, and ad-hoc/discovered records are excluded; removed records require an
  explicit stop or remove.
- CLI `up --no-wait` and MCP `no_wait: true` are rejected before daemon contact for such a
  manifest.
- `start` remains explicitly named and never pulls in prerequisites.
- If an `on-failure` prerequisite is recovering, rerun `up` after it is ready rather than
  expecting the same invocation to follow its successor.
- The server exposes `start`, `up`, `down`, `list`, `status`, `logs`, `wait`, `input`,
  `restart`, `stop`, `remove`, and `signal`.
- Its bounded `input` tool accepts exact non-empty text or strict padded base64 without
  whitespace for an already-running TTY incarnation; text sends exact bytes without a newline
  and returns its launch cursor; it never starts, waits, queues, retries, retains, or echoes
  input and an ownership conflict fails immediately; payloads are bounded at 1-32768 bytes.
- The one-shot operation is at-most-once and does not resend across a launch race; hum neither
  retains nor explicitly echoes submitted bytes.
- It has no arbitrary-command `run` or unbounded follow tool; agents use bounded `wait` and
  `logs`.
- Bounded `logs` reads the newest default window; `tail`, `after`, and `since_ms` (CLI `--since
  DURATION`) narrow it to an exact tail, a cursor continuation, or a recent time window.
- Set `stream: "system"` (CLI `--stream system`) for hum-generated supervision entries without
  child noise. Omitted or explicit `both` includes stdout, stderr, and system.
- For restart-with-work, use `stop`, run the intermediate command, then `start`: the durable
  session preserves terminal followers.
- `remove` is different from `stop`: it discards retained runtime state and output but never
  edits `hum.yaml`.
- The CLI form is `hum signal NAME SIGNAL [--json]`; scope selectors may appear before or after
  those positional arguments. The observational `signal` tool delivers
  one supported named or positive decimal signal to a running process group and returns
  `{"name":"NAME","signal":{"name":"SIGHUP","number":1},"status":"sent"}` with its canonical
  SIG-prefixed name and number; it never sets stop intent or cancels automatic relaunch,
  including for TERM and KILL.
- Invalid, missing, and stopped targets return `invalid_signal`, `not_found`, and `not_running`.
- `down` preserves sessions; a later `up` starts resolved definitions only, leaving ad hoc
  sessions stopped.

See [the MCP design](design.md#mcp-adapter) for detailed behavior and
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

A manifest process can opt in with `restart: on-failure`; `never` is the strict default and the
only other accepted value.

- A manifest `after` list must contain unique same-manifest names that declare `ready`;
  malformed values, unknown names, self-reference, duplicates, and cycles are rejected.
- Discovery and ad-hoc sessions always use `never`, and invalid or non-string values fail
  manifest validation.
- The policy retries non-zero or signal exits after 1s, 2s, 4s, 8s, and 16s, at most five times.
- Spawn failures consume an attempt.
- An automatic child alive for 30 seconds resets the counter; stop, down, restart, remove,
  shutdown, and a manual start cancel pending work.
- Automatic launches retain their last argv, cwd, environment, readiness, and TTY, so explicitly
  restart after changing a definition.
- `start` and `up` report active/recovery-capable definition drift instead of silently adopting
  those edits; CLI `up` exits 1 and only `restart` adopts them.
- Only restart applies a changed definition.
- The readiness matcher and normalized restart policy are compared; environment and readiness
  timeout are not.
- Read `restart`, `relaunches`, and `next_launch_at` in status/list snapshots.
- Followers stay attached and bounded logs retain failure and relaunch boundaries.
- Always read the failing incarnation's retained output with `logs` before editing again.

## Shell-only fallback

When MCP is unavailable, `hum skill` prints the embedded Agent Skills file for
installation in an agent's normal skill directory. MCP remains the preferred
integration.

### Bounded output

Bounded child-output reads and matches use byte-wise terminal-control-stripped text, applied
independently per entry to each stdout/stderr stream.

- This covers bounded `logs` (including JSON and tail), MCP `logs`, `logs --match`, `wait
  --match`, and readiness matches.
- System entries remain raw; stored bytes remain raw; cursors and `MaxBytes`/entry-limit
  accounting also use raw stored lengths.
- Patterns containing raw ESC bytes no longer match stripped child text; a `^` anchor now
  matches colourised output whose raw first byte is ESC.
- There is no `--raw` flag or other raw opt-out.
- A control-only bounded child entry is retained with empty text.
- `logs --follow --match` selects using stripped child text but emits selected raw entries, and
  follow plus attached `run` rendering stay raw.
- The strip does not collapse carriage-return redraw frames or emulate a terminal; a sequence
  split across entries can leave its tail visible.

### Interactive sessions

Ad-hoc commands require the explicit boundary in `hum run NAME [options] -- COMMAND`; scope selectors
must appear before it, and all later tokens are child argv. Foreground `hum run NAME -- COMMAND` owns
exactly one incarnation, streams raw output, propagates its exit status, stops on Ctrl+C or SIGTERM,
and detaches on SIGHUP. Use `hum run NAME --detach -- COMMAND`
for daemon ownership, and `hum attach NAME` or `hum logs NAME --follow` for durable observation.

Leave `tty` off unless a tool genuinely requires a controlling terminal; prefer its
non-interactive mode (`npx --yes`, `CI=1`, or `--force`).

- A manifest can opt in with `tty: true`, or an operator can use `hum run NAME --tty --
  COMMAND`.
- Only one attached run forwards input; competing runs and `logs --follow` are output-only.
- The owner uses raw mode and alone forwards SIGWINCH resizes; Ctrl-] detaches input, raw mode
  is restored after panic, terminal echo is controlled by the child, and Ctrl-C is forwarded
  only while TTY input is owned; after Ctrl-] releases input, foreground Ctrl+C uses the control
  signal rules. SIGTERM stops the foreground incarnation and SIGHUP detaches without stopping it.
- MCP reports `tty` and provides the same bounded `input` tool for exact prompt responses.
- Stop/restart preserves the lease across successors, remove and shutdown close it, and
  `shutdown --stop-processes` is required when active work must be stopped.
- For a one-shot prompt response, use MCP `input` or `hum input NAME --text VALUE`; text does
  not add a newline, and `--base64` requires strict padded base64 without whitespace.
Use a bounded observe, answer, confirm loop:

```text
wait --match "prompt" ──> input ──> wait --match "complete"
```

The same loop works with bounded `logs` instead of the first `wait`.

## Canonical project scopes

hum selects project scope automatically from the invocation directory. symlink aliases share a canonical scope while separate worktrees do not. Use `hum --project /path/to/main` for explicit cross-worktree access. Use `hum --global` (`-g`) only for machine-wide ad-hoc retained sessions; it works before or after ordinary positional arguments and before `run`'s required `--` child boundary, conflicts with `--project` and `list --all`, and `init`/`up` reject it. Global `start`/`restart` reuse retained launch specifications and never read the caller's manifest; child cwd remains the lexical run directory. `hum remove --all` removes every runtime session only in the selected project or global scope; it never spans scopes or targets unlaunched declarations. Use `hum --global logs proxy` for a global match reported by project not-found guidance, or `hum list --all` to discover all scopes. JSON `scope` is `project` or `global`; global records omit `project_root`.
