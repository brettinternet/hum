---
name: hum
description: Manage and inspect project processes with the hum CLI when hum MCP tools are unavailable.
---

# hum

hum runs a project's dev processes under a shared daemon. Use the MCP tools (`hum mcp`) when you have them. Use this CLI guide only when you don't.

## Rules

- Start processes through hum. Never run dev commands such as npm, bun, yarn, or pnpm directly, and never wrap them in `hum run`.
- Never run `hum logs --follow` or plain `hum up`. They stream until interrupted. Use the bounded commands below.
- Read logs before changing a failing process.
- Run `hum down` only when the developer asks to stop everything.

## Start

- `hum up --detach` starts every declared process in `after` order and returns once all are ready. `hum up api --detach` starts `api` and its prerequisites.
- `hum start api` starts one process without its prerequisites.
- Both wait for readiness. Add `--no-wait` to return right after launch, or `--timeout 60s` to change the limit.
- hum reads `.hum.yaml`, then `hum.yaml`, or the file given with `--file`. It never merges them. With no manifest, commands return `manifest_missing`; run `hum init` to draft `hum.yaml` from project files.

`up` exits 0 when all are ready, 1 on a request error or `definition_drift`, 2 on timeout, and 3 when a process exits before ready. Dependents of a failed prerequisite are `skipped` and list it in `blocked_by`. Read the failure, fix it, then rerun `hum up`.

## Inspect

- `hum list` shows processes. `hum list --full` adds source and readiness.
- `hum status api` shows state, restart counters, and TCP listeners.
- `hum list --port 3000` finds Hum processes on a port across every worktree. Check any incomplete entries before treating a miss as clear.
- `hum events` shows recent starts, exits, and failures. Add `--failed`, `--since 10m`, or names to narrow it.

## Logs

- `hum logs api --tail 100` reads recent output.
- `hum logs api --stream stderr`, or `--stream system` for supervision events.
- `hum logs api --since 5m` reads a time window.
- `hum logs api --match panic --context 5` shows matches with surrounding lines.
- `hum logs api --json --after-cursor N --session-id ID` continues from a cursor. Always pair a cursor with its `session_id`. A stale ID returns `cursor_stale`.

Matching ignores ANSI color codes. Output stays raw.

## Wait and input

- `hum wait api --match "listening" --timeout 30s` waits for new output. Without `--match`, it waits for exit. It works before the process starts.
- To answer a TTY prompt: `hum wait api --match "Continue?"`, then `hum input api --text $'y\n'`, then `hum wait` again to confirm. No newline is added for you. Use `--base64` for raw bytes. Input is sent once and is never retried.

## Change

- After editing a definition, run `hum restart api`. `up` and `start` report `definition_drift` instead of applying edits.
- `hum stop api` stops a process and keeps its logs. To do work between runs: stop, work, then `hum start api`.
- `hum remove api` stops it and discards its state and logs. `hum remove --all` does this for every session in the scope. Neither edits the manifest.
- `hum signal api HUP` sends a signal. It never stops automatic restarts, even TERM or KILL; use `hum stop`.
- If `up` warns `removed_definition`, a process started from the manifest is no longer declared. Stop or remove it explicitly.

## Manifest notes

- `after` lists prerequisites. Each must declare `ready`. Dependents start only after their prerequisites are ready.
- Readiness: `match` (log regex), `exec` (command exits 0), `http` (GET returns 2xx), `tcp` (connection accepted), or `exit: 0` for setup steps that must finish first.
- `restart: on-failure` retries crashes after 1s, 2s, 4s, 8s, and 16s, then gives up. The default is `never`. While retrying, `up` reports `recovery_pending`. Retries reuse the old definition.
- `stop_grace` sets the wait between TERM and KILL.
- Leave `tty` off unless a tool needs a terminal. Prefer a non-interactive flag such as `--yes` or `CI=1`.

## Scope

hum picks the project from the current directory. Each worktree is its own project. Use `hum --project /path` to target another. `hum --global` targets machine-wide ad-hoc sessions. `hum list --all` lists every scope.
