---
name: hum
description: Use this skill when a shell-only agent needs to manage or inspect project processes through hum as a fallback when MCP is unavailable.
---

# hum process control

Use MCP as the primary integration. Use this skill only for shell-only fallback workflows.

## Start and inspect

- Try bounded `hum up --detach` first; interactive plain `hum up` keeps following aggregate output after startup. When `hum.yaml` declares `after`, up starts independent roots concurrently and launches each dependent only after every direct prerequisite is observed `ready`. It waits for readiness by default; each process's readiness timeout starts at its own launch or first running observation; results settle in lexical order.
- Use `hum start <name>` for one resolved process; it waits for readiness unless you opt out. `start NAME...` is explicit-only and never pulls in `after` prerequisites. For a running or recovery-capable manifest record, changed argv, canonical cwd, readiness matcher, TTY, or normalized restart policy returns `definition_drift` with sorted `changed_fields` and `hum restart NAME` guidance; CLI `up` exits 1 for drift and the drift cannot satisfy an `after` gate.
- `after` lists must name unique same-manifest processes that declare `ready`; cycles, unknown names, duplicates, self-reference, malformed lists, and dependencies without readiness are manifest errors. `up --no-wait` is rejected before daemon contact when any `after` is declared.
- Use `hum list` for discovery, and to inspect each process's source and readiness.
- Read bounded output with `hum logs --tail 100 <name>` or `hum logs --after-cursor <cursor> --json <name>`; use `hum logs --since 5m <name>` for a recent time window.
- Use `hum signal <name> HUP` to deliver one observational signal to a running process group; it never sets stop intent or cancels automatic relaunch, including for TERM and KILL.
- Use `hum wait <name>` for a bounded later condition, including before another client starts the name.
- Never use unbounded `hum logs <name> --follow`; it is for interactive terminals.
- For intermediate work, use `hum stop <name>`, run the work, then `hum start <name>`; the durable session keeps observers attached.
- After process-definition changes, use `hum restart <name>`. `hum up` and `hum start <name>` report active or recovery-capable definition drift instead of silently adopting edits; CLI `up` exits 1 and only restart applies a changed definition.
- A manifest process may opt into `restart: on-failure`; the default is `never`. It retries unexpected non-zero or signal exits after 1s, 2s, 4s, 8s, and 16s, for at most five automatic attempts. Read `hum status NAME` and the retained `hum logs NAME` output before editing a failing/crashing process again; recovery does not replace diagnosis.
- If an `after` prerequisite exits before readiness, `up` reports the prerequisite failure and returns dependents as `skipped` with sorted direct `blocked_by` names; it does not follow an automatic successor. A skip may include read-only `existing_state` and process snapshot data, but it still means no launch occurred and still blocks dependents. Read the failure, then rerun `hum up` after the prerequisite is ready. Skips do not change aggregate exit precedence (1 request error, 3 early exit, 2 timeout, 0 success).
- Use `hum remove <name>` only to discard the runtime session, retained output, and launch state; it never edits `hum.yaml`.
- Use `hum down` to stop everything in the current project; a later `hum up` restarts only resolved definitions.
- If `hum up` reports a manifest-sourced running, pending-recovery, or exhausted record as `removed_definition`, explicitly use `hum stop <name>` or `hum remove <name>`. Removed warnings are lexical, do not change aggregate status, and exclude ad-hoc and discovered sessions; removed records require an explicit stop or remove.
- To answer a bounded TTY prompt, observe with `hum logs` or `hum wait --match`, answer with `hum input <name> --text <value>`, then confirm with `hum wait --match`. Text is sent as exact bytes without a newline. Use `--base64 <value>` for exact binary bytes; it requires strict padded base64 (standard alphabet) without whitespace. Payloads are 1-32768 bytes. Input requires a running TTY, is at-most-once with no resend across a launch race, writes once at its initial launch cursor, fails immediately on ownership conflict, and never starts, waits, queues, retries, retains, or explicitly echoes bytes.

## Crash relaunch policy

Only explicit manifest definitions may set `restart: on-failure`; discovered and
ad-hoc sessions remain `never`. Manifest values are strict. Spawn failures
consume attempts, an automatic launch that survives 30 seconds resets the
counter, and explicit start/up/restart/stop/down/remove or shutdown cancels
pending work. Automatic attempts reuse the last effective launch specification.
Snapshots expose `restart`, `relaunches`, and `next_launch_at`; followers remain
attached through backoff and exhaustion, and retained bounded logs include the
failed incarnation and system boundaries. Inspect those logs before editing
again. The failing incarnation's retained output is the diagnostic source.

## Conservative discovery

An absent `hum.yaml` is normal when conservative discovery resolves exactly one candidate named `dev`.
If discovery finds no candidate or is ambiguous, or if you need multiple commands, a custom cwd, or readiness, ask the developer to run `hum init` and commit the resulting `hum.yaml`. Do not run `hum init` yourself.

## Command boundary

Never derive or run underlying development commands, including npm, bun, yarn, or pnpm-style commands. Never use raw `hum run ... -- ...`; use resolved definitions and the commands above instead.

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
Only one attached run owns input. A competing run and `hum logs --follow` are
output-only. The owner uses raw mode and alone forwards SIGWINCH resizes;
Ctrl-] detaches input, raw mode is restored after panic, terminal echo is
child output, and Ctrl-C is forwarded only for TTY runs; Ctrl-D and Ctrl-Z are forwarded too; ordinary runs keep
Ctrl-C observer detach. TTY output is merged as stdout and may contain ANSI
controls. Stop/restart preserves the lease across launch cursors; remove and
shutdown close it. MCP reports `tty` and provides the same bounded `input` tool for exact prompt responses.

hum selects scope automatically from the invocation directory. Git roots and linked worktrees are canonicalized physically, so symlink aliases share records while separate worktrees do not. Child cwd remains lexical. Use `hum --project /path/to/main` (or `-C` as shorthand) for explicit cross-worktree access; observation can address a removed known worktree, but launch requires an existing directory. `hum list --all` discovers every project scope. A local not-found result never falls through silently: use the copyable `--project` command or `hum list --all`. JSON process records contain `scope` (`project`) and canonical `project_root`.
