---
name: hum
description: Manage and inspect project dev processes with hum MCP tools, falling back to the hum CLI when MCP is unavailable.
---

# hum

hum runs a project's dev processes under a shared daemon. Use the hum MCP tools. Pass the absolute project directory as `project_root` on every call. Use the `hum` CLI only when MCP is unavailable; run `hum skill` for the CLI guide.

## Rules

- Start processes through hum. Never run dev commands such as npm, bun, yarn, or pnpm directly. There is no arbitrary-command tool by design.
- Read logs before changing a failing process.
- Call `down` only when the developer asks to stop everything.

## Start

- `up` starts every declared process in `after` order and waits until all are ready. Pass `names: ["api"]` to start `api` and its prerequisites.
- `start` starts one process without its prerequisites.
- Both wait for readiness. Set `no_wait` to return right after launch, or `timeout_ms` to change the limit.
- hum reads `.hum.yaml`, then `hum.yaml`, or the file given in `manifest`. It never merges them. With no manifest, tools return `manifest_missing`; run `hum init` to draft `hum.yaml` from project files.

If `up` reports `exited_before_ready` or `timed_out`, dependents are `skipped` with the failed prerequisite in `blocked_by`. Read its logs, fix it, then call `up` again.

## Inspect

- `list` shows processes. `list` with `port: 3000` finds Hum processes on that port across every worktree. Check any `incomplete` entries before treating a miss as clear.
- `status` shows state, restart counters, and TCP listeners.
- `events` shows recent starts, exits, and failures. Narrow with `names`, `failed`, or `since_ms`.

## Logs

- `logs` with `tail: 100` reads recent output.
- Set `stream` to `stderr`, or `system` for supervision events.
- `since_ms` reads a time window.
- `match` with `context: 5` shows matches with surrounding lines.
- To page forward, pass the returned `next` as `after` with its `session_id`. `after` requires `session_id`. A stale ID returns `cursor_stale`.

Matching ignores ANSI color codes. Output stays raw.

## Wait and input

- `wait` with `match` waits for new output. Without `match`, it waits for exit. It defaults to 30s (`timeout_ms`) and works before the process starts.
- To answer a TTY prompt: `wait` for the prompt, `input` with `text: "y\n"`, then `wait` again to confirm. No newline is added for you. Use `base64` for raw bytes. Input is sent once and is never retried.

## Change

- After editing a definition, call `restart`. `up` and `start` report `definition_drift` instead of applying edits.
- `stop` stops a process and keeps its logs. To do work between runs: `stop`, work, then `start`.
- `remove` stops it and discards its state and logs. `all: true` does this for every session in the scope. Neither edits the manifest.
- `signal` sends a signal. It never stops automatic restarts, even TERM or KILL; use `stop`.
- If `up` warns `removed_definition`, a process started from the manifest is no longer declared. Stop or remove it explicitly.

## Manifest notes

- `after` lists prerequisites. Each must declare `ready`. Dependents start only after their prerequisites are ready.
- Readiness: `match` (log regex), `exec` (command exits 0), `http` (GET returns 2xx), `tcp` (connection accepted), or `exit: 0` for setup steps that must finish first.
- `restart: on-failure` retries crashes after 1s, 2s, 4s, 8s, and 16s, then gives up. The default is `never`. While retrying, `up` reports `recovery_pending`. Retries reuse the old definition.
- `stop_grace` sets the wait between TERM and KILL.
- Leave `tty` off unless a tool needs a terminal. Prefer a non-interactive flag such as `--yes` or `CI=1`.

## Scope

Each worktree is its own project; pass its own `project_root`. Set `scope: "global"` and omit `project_root` for machine-wide ad-hoc sessions. `list` with `all: true` lists every scope.
