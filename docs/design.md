# devproc design

## Product direction

`devproc` is a local process supervisor for humans and coding agents that need shared, bounded access to long-running development processes and their output. The primary interface is one small CLI usable from any shell. A long-running local daemon owns child processes; clients communicate with it over a user-private Unix domain socket. macOS and Linux are the initial platforms.

Build the smallest complete vertical slice first: foreground and detached daemon startup, attached and detached `run`, `list`, bounded and followed `logs`, `stop`, and daemon `shutdown`. Add `status` and `wait` only after that slice passes its integration tests. MCP remains a future adapter, not the foundation.

## Planned CLI

```text
devproc serve [--daemon]
devproc run <name> [--detach] -- <command> [args...]
devproc list [--json]
devproc status <name> [--json]
devproc logs <name> [--stream stdout|stderr|both] [--tail N] [--after-cursor N] [--limit-bytes N] [--grep REGEX] [--follow] [--json]
devproc wait <name> --after-cursor N [--match REGEX] [--timeout DURATION] [--json]
devproc stop <name>
devproc shutdown [--stop-processes]
```

Human-readable output is the default. Stable JSON is intended for agents. `logs --json --follow` emits newline-delimited JSON events. Live output is sent only to an attached `run` client or an explicit log follower; other commands receive bounded responses.

## Architecture

- `cmd/devproc/main.go` stays small: `main` calls a testable `run(context.Context, []string)`, reports errors to stderr, handles clean exits and SIGINT/SIGTERM/SIGHUP, and receives version and build time through ldflags.
- Use the latest compatible `urfave/cli` v3 release at the CLI edge.
- `internal/cli` builds commands, `internal/app` owns use cases, and `internal/daemon` adapts those services to the local protocol.
- `internal/process`, `internal/output`, and `internal/protocol` own process groups, bounded output, and wire types respectively.
- Keep interfaces narrow and prefer the Go standard library wherever practical.

The daemon starts the exact argv array directly, never reconstructed shell text. Process names are unique within the nearest Git-root project, falling back to the current working directory. Each process records its PID, process-group ID, cwd, argv, start time, state, and exit status.

`run` is attached by default. The daemon owns the child while the CLI streams its stdout and stderr, preserving raw line content wherever possible. Ctrl+C sends SIGINT to the managed process group. If the process exits while attached, the CLI returns its exit code; if the CLI connection disappears without an explicit signal, the process continues. `run --detach` prints the process name and PID and returns immediately. Neither mode provides a PTY or arbitrary interactive stdin.

Stdout and stderr are captured separately in bounded in-memory ring buffers. Cursors are monotonically increasing byte positions. Reads default to at most 100 lines and 16 KiB and return truncation and next-cursor metadata.

`wait` checks retained output before blocking. It returns when matching output appears, the process exits, or the timeout expires, always returning a new cursor and an explicit matched, exited, or timed-out outcome.

## Daemon startup and client behavior

`serve` runs in the foreground and writes diagnostics to stderr. `serve --daemon` starts a fully detached process in a new session/process group without retaining the caller's standard streams. The original caller waits for a readiness handshake, then receives the daemon PID and socket path. Detached diagnostics go to a bounded or rotating `daemon.log` in the runtime directory.

Detached startup is idempotent. A startup lock prevents concurrent `serve --daemon` or automatic-start attempts from creating multiple daemons. Startup verifies live ownership before recovering stale PID or socket files and reports readiness failures to the original caller.

`run` automatically starts the detached daemon when no daemon is available, including when several `run` clients race. Read-only and control commands never start an empty daemon. `list`, `status`, `logs`, `wait`, `stop`, and `shutdown` instead fail concisely with `Start it with devproc serve --daemon.`

`logs --follow` first returns retained output using the same tail, cursor, stream, grep, and byte-limit rules as a bounded read, then delivers cursor-based events. Multiple followers are independent. Cancelling a follower leaves the managed process running. Delivery remains bounded per read; lagging followers receive explicit eviction information rather than causing unbounded buffering. Following remains available after the original attached `run` client disconnects.

## Configuration direction

Follow the useful separation in `../policydev/agent/config`: typed build options and runtime inputs, construction testable without CLI objects, platform-aware path helpers, and flags kept at the CLI boundary. Support arguments and environment variables where they improve shell and agent use.

Although `urfave/cli` supports YAML sources, persistent configuration files are explicitly outside the initial scope. Do not add YAML loading or allow CLI framework types to leak into application services.

## Security and lifecycle

The daemon protocol is internal newline-delimited JSON, including bounded streaming events for attached output and log followers. The socket lives below `$XDG_RUNTIME_DIR/devproc/`, falling back to a per-user directory below the OS temporary directory. Directory and socket permissions must exclude other users. The runtime directory also contains PID, startup-lock, and bounded/rotating daemon-log state.

Reject malformed or unsafe names, duplicate running names, invalid cursors and regular expressions, malformed protocol messages, and requests above server-side output limits with clear errors.

Each child starts in a Unix process group. Ctrl+C from an attached run sends SIGINT to that group. `stop` sends SIGTERM to one managed group, waits a bounded grace period, then sends SIGKILL if necessary. `shutdown` refuses while processes are running and lists their names; `shutdown --stop-processes` applies the same graceful termination sequence to every group before stopping the daemon. Managed processes survive ordinary client and follower disconnection.

## Verification

Unit tests cover ring eviction, cursor boundaries, truncation, stream filtering, regex matching, concurrent followers, cancellation, and bounded slow-client delivery. Integration tests cover foreground serve, detached readiness and terminal isolation, idempotent and concurrent automatic startup, stale PID/socket recovery, attached output and exit status, SIGINT forwarding, client disconnection, detached run, multiple followers, eviction, NDJSON follow output, stop, shutdown refusal, and graceful process-tree termination. Project gates run formatting, tests, static analysis, and built-binary smoke coverage on macOS and Linux.

## Initial limitations

No PTY or arbitrary interactive input, Windows support, authentication, remote access, web UI, persistent process history, plugin system, repository layer, dependency-injection framework, or premature persistence abstraction. Daemon restart loses buffered process history. Do not add launchd, systemd, login startup, or operating-system service installation yet.

## Future MCP adapter

A future stdio or Streamable HTTP MCP server may expose `list`, `status`, `logs`, `wait`, and `stop`. It must delegate to the same `internal/app` services as the CLI and add only transport translation, schemas, and MCP-specific error mapping. It must not bypass socket ownership, output bounds, project scoping, or security assumptions. No MCP dependency or server package belongs in the initial implementation.
