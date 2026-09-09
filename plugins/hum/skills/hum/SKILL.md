---
name: hum
description: Manage and inspect project development processes through hum MCP tools, with the hum CLI as a fallback when MCP is unavailable.
---

# hum process control

Use the bundled hum MCP tools when available. Pass the absolute current project root as `project_root`. Use the equivalent `hum` CLI commands only when MCP is unavailable.

## Start and inspect

- Try `up` first; when `hum.yaml` declares `after`, it starts independent roots concurrently and launches each dependent only after every direct prerequisite is observed `ready`; each process timeout starts at its own launch or first running observation, with lexical final results. The bounded CLI equivalent is `hum up --detach`; interactive plain `hum up` keeps following aggregate output after startup.
- Use `start` for one explicitly named resolved process. It never pulls in `after` prerequisites. For a running or recovery-capable manifest record, changed argv, canonical cwd, readiness matcher, TTY, or normalized restart policy returns `definition_drift` with sorted `changed_fields` and `hum restart NAME` guidance; CLI `up` exits 1 for drift and drift cannot satisfy an `after` gate. The CLI equivalent is `hum start <name>`.
- `after` must be a unique same-manifest name list whose dependencies declare `ready`; unknown names, duplicates, self-reference, malformed values, cycles, and dependencies without readiness fail manifest validation. `up --no-wait` is rejected before daemon contact when any `after` is declared.
- Use `list` to discover processes and inspect source and readiness.
- Read bounded output with `logs`. For CLI fallback, use `hum logs --tail 100 <name>` or continue from a cursor with `hum logs --after-cursor <cursor> --json <name>`. Bound a recent time window with `since_ms`, or CLI `hum logs --since 5m <name>`.
- Use `signal` (CLI `hum signal NAME HUP`) to deliver one observational signal to a running process group; it never sets stop intent or cancels automatic relaunch, including for TERM and KILL.
- Use `wait` for a bounded later condition, including before another client starts the name.
- For intermediate work, use `stop`, run the work, then `start`; the durable session keeps terminal observers attached.
- After process-definition changes, use `restart`; only restart adopts changed definitions. `up` and `start` report active or recovery-capable definition drift instead of silently replacing them; CLI `up` exits 1 for drift, and only restart applies a changed definition.
- A manifest process may opt into `restart: on-failure`; the default is `never`. Unexpected exits retry after 1s, 2s, 4s, 8s, and 16s, at most five times. Inspect retained `logs` before editing a crashing process again.
- If an `after` prerequisite exits before readiness, `up` returns the failure and returns dependents as `skipped` with sorted direct `blocked_by` names rather than following its automatic successor. A skip may include read-only `existing_state` and process snapshot data, but it still means no launch occurred and still blocks dependents. Read the retained failure, then rerun `up` after the prerequisite is ready; skips keep aggregate exit precedence unchanged (1 request error, 3 early exit, 2 timeout, 0 success).
- Use `remove` only to discard the runtime session, retained output, and launch state; it never edits `hum.yaml`. CLI `hum remove --all` discards every runtime session only in the selected project or global scope, never other scopes or unlaunched manifest declarations.
- Use `down` only when the developer asks you to stop everything in the project; a later `up` restarts only resolved definitions.
- If `up` reports a manifest-sourced running, pending-recovery, or exhausted record as `removed_definition`, explicitly use `stop NAME` or `remove NAME` (or the CLI equivalents `hum stop NAME` and `hum remove NAME`); these lexical warnings do not change aggregate status and exclude ad-hoc/discovered sessions; removed records require an explicit stop or remove.
- For a bounded prompt response, observe with `logs` or `wait --match`, answer with `input` (or CLI fallback `hum input NAME --text VALUE`), then confirm with `wait --match`. Text sends exact bytes without a newline; use `hum input NAME --base64 PADDED_VALUE` for awkward bytes, and require strict padded base64 (standard alphabet) without whitespace. Payloads are 1-32768 bytes. Input targets only a running TTY, is at-most-once with no resend across a launch race, writes once at its launch cursor, fails immediately on ownership conflict, and never starts, waits, queues, retries, retains, or explicitly echoes input.

## Crash relaunch policy

Only explicit manifest definitions may use `restart: on-failure`; discovered and
ad-hoc sessions always use `never`, and manifest values are strict. Spawn
failures consume attempts, a child that survives 30 seconds resets the counter,
and explicit lifecycle controls cancel pending work. Automatic attempts retain
the last effective argv, cwd, environment, readiness, and TTY. Snapshots expose
`restart`, `relaunches`, and `next_launch_at`; followers stay attached through
backoff and exhaustion, while bounded logs retain failures and system boundaries.
Read the failing incarnation's output before changing the definition.

## Conservative discovery

An absent `hum.yaml` is normal when conservative discovery resolves exactly one candidate named `dev`. If discovery finds no candidate or is ambiguous, or the project needs multiple commands, a custom cwd, or readiness, ask the developer to run `hum init` and commit the resulting `hum.yaml`. Do not run `hum init` yourself.

## Foreground run ownership

`hum run NAME -- COMMAND` owns exactly one incarnation: it streams raw child output, propagates the child exit status, stops on Ctrl+C or SIGTERM, and detaches on SIGHUP. Use `hum run NAME --detach -- COMMAND` for daemon ownership, and `hum attach NAME` or `hum logs NAME --follow` for durable observation.

## Command boundary

Never derive or run underlying development commands, including npm, bun, yarn, or pnpm-style commands. The MCP server intentionally has no arbitrary-command tool. Never use raw `hum run ... -- ...`; use resolved definitions and the lifecycle operations above.

## Bounded output

Bounded child-output reads and matches use byte-wise terminal-control-stripped
text, independently per entry for each stdout/stderr stream. This covers
`hum logs` (including JSON and tail), `hum logs --match`, `hum wait --match`,
readiness matches, and MCP `logs`. System entries remain raw; stored bytes remain raw;
cursors and byte-limit accounting use raw stored lengths. Patterns containing
raw ESC bytes no longer match stripped child text; a `^` anchor now matches
colourised output whose raw first byte is ESC. A control-only bounded
child entry remains present with empty text. `hum logs --follow --match` selects
stripped child text but emits selected raw entries, while follow and attached
`run` rendering remain raw. There is no `--raw` flag or other raw opt-out.
Stripping is not terminal emulation or redraw collapsing: split sequences can
leave a tail visible and carriage-return redraw
frames remain separate.

## Optional TTY sessions

Leave `tty` off unless a tool requires a controlling terminal; prefer a
non-interactive mode such as `npx --yes`, `CI=1`, or `--force`. A manifest process
can set `tty: true`; an ad-hoc command can use the CLI TTY option with a command separator.
Only one attached run owns input; `hum logs --follow` is output-only. The owner uses raw
mode and alone forwards SIGWINCH resizes. Ctrl-] detaches input, raw mode is restored after
panic, terminal echo is child output, and Ctrl-C is forwarded only while TTY input is owned;
Ctrl-D and Ctrl-Z are forwarded too. After Ctrl-], foreground Ctrl+C uses the control-signal
rules. SIGTERM stops the foreground incarnation and SIGHUP detaches. TTY output is merged as
stdout and may contain ANSI controls. Stop/restart preserves the lease across launch cursors;
remove and shutdown close it. MCP reports `tty` and provides bounded `input` for exact prompt responses.

## Project and global scopes

hum selects project scope automatically from the invocation directory; symlink aliases share a canonical scope while separate worktrees remain distinct. Use `hum --project /path/to/main` (or `-C`) for explicit cross-worktree access, including a removed worktree. Use `hum --global` (`-g`) only for machine-wide ad-hoc retained sessions; it works around lifecycle commands and `run` NAME, conflicts with `--project` and `list --all`, and `init`/`up` reject it. Global `start`/`restart` reuse retained launch specifications without reading a manifest, and child cwd remains the lexical run directory. Project not-found guidance can produce `hum --global logs proxy`; `hum list --all` discovers every scope. MCP tool calls accept `scope`: omit it for project scope with an absolute `project_root`, or pass `scope: "global"` without `project_root`. JSON `scope` is `project` or `global`; global records omit `project_root`.
