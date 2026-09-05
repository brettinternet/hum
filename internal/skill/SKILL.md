---
name: hum
description: Use this skill when a shell-only agent needs to manage or inspect project processes through hum as a fallback when MCP is unavailable.
---

# hum process control

Use MCP as the primary integration. Use this skill only for shell-only fallback workflows.

## Start and inspect

- Try `hum up` first; it starts every resolved process and waits for readiness by default.
- Use `hum start <name>` for one resolved process; it waits for readiness unless you opt out.
- Use `hum list` for discovery, and to inspect each process's source and readiness.
- Read bounded output with `hum logs --tail 100 <name>` or `hum logs --after-cursor <cursor> --json <name>`.
- Use `hum wait <name>` for a bounded later condition, including before another client starts the name.
- Never use unbounded `hum logs <name> --follow`; it is for interactive terminals.
- For intermediate work, use `hum stop <name>`, run the work, then `hum start <name>`; the durable session keeps observers attached.
- After process-definition changes, use `hum restart <name>`.
- Use `hum remove <name>` only to discard the runtime session, retained output, and launch state; it never edits `hum.yaml`.
- Use `hum down` to stop everything in the current project; a later `hum up` restarts only resolved definitions.

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
shutdown close it. MCP reports `tty` but has no input tool.
