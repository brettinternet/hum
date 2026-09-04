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
- Use `hum wait <name>` for a later condition, such as matching output or process exit.
- After process-definition changes, use `hum restart <name>`.
- Use `hum stop <name>` only when the developer asks you to stop that process.
- Use `hum down` to stop everything in the current project.

## Conservative discovery

An absent `hum.yaml` is normal when conservative discovery resolves exactly one candidate named `dev`.
If discovery finds no candidate or is ambiguous, or if you need multiple commands, a custom cwd, or readiness, ask the developer to run `hum init` and commit the resulting `hum.yaml`. Do not run `hum init` yourself.

## Command boundary

Never derive or run underlying development commands, including npm, bun, yarn, or pnpm-style commands. Never use raw `hum run ... -- ...`; use resolved definitions and the commands above instead.
