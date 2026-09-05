---
name: hum
description: Manage and inspect project development processes through hum MCP tools, with the hum CLI as a fallback when MCP is unavailable.
---

# hum process control

Use the bundled hum MCP tools when available. Pass the absolute current project root as `project_root`. Use the equivalent `hum` CLI commands only when MCP is unavailable.

## Start and inspect

- Try `up` first; it starts every resolved process and waits for readiness by default. The CLI equivalent is `hum up`.
- Use `start` for one resolved process. The CLI equivalent is `hum start <name>`.
- Use `list` to discover processes and inspect source and readiness.
- Read bounded output with `logs`. For CLI fallback, use `hum logs --tail 100 <name>` or continue from a cursor with `hum logs --after-cursor <cursor> --json <name>`.
- Use `wait` for a bounded later condition, including before another client starts the name.
- For intermediate work, use `stop`, run the work, then `start`; the durable session keeps terminal observers attached.
- After process-definition changes, use `restart`.
- Use `remove` only to discard the runtime session, retained output, and launch state; it never edits `hum.yaml`.
- Use `down` only when the developer asks you to stop everything in the project; a later `up` restarts only resolved definitions.

## Conservative discovery

An absent `hum.yaml` is normal when conservative discovery resolves exactly one candidate named `dev`. If discovery finds no candidate or is ambiguous, or the project needs multiple commands, a custom cwd, or readiness, ask the developer to run `hum init` and commit the resulting `hum.yaml`. Do not run `hum init` yourself.

## Command boundary

Never derive or run underlying development commands, including npm, bun, yarn, or pnpm-style commands. The MCP server intentionally has no arbitrary-command tool. Never use raw `hum run ... -- ...`; use resolved definitions and the lifecycle operations above.

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
