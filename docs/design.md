# devproc design

## Product direction

`devproc` is a local process supervisor for humans and coding agents that need shared, bounded access to long-running development processes and their output. The primary interface is one small CLI usable from any shell. A long-running local daemon owns child processes; clients communicate with it over a user-private Unix domain socket. macOS and Linux are the initial platforms.

Build the smallest complete vertical slice first: daemon startup, `run`, `list`, bounded `logs`, and `stop`. Add `status` and `wait` only after that slice passes its integration tests. MCP remains a future adapter, not the foundation.

## Planned CLI

```text
devproc serve
devproc run <name> -- <command> [args...]
devproc list [--json]
devproc status <name> [--json]
devproc logs <name> [--stream stdout|stderr|both] [--tail N] [--after-cursor N] [--limit-bytes N] [--grep REGEX] [--json]
devproc wait <name> --after-cursor N [--match REGEX] [--timeout DURATION] [--json]
devproc stop <name>
```

Human-readable output is the default. Stable JSON is intended for agents. Output is always requested explicitly; the daemon never injects logs proactively.

## Architecture

- `cmd/devproc/main.go` stays small: `main` calls a testable `run(context.Context, []string)`, reports errors to stderr, handles clean exits and SIGINT/SIGTERM/SIGHUP, and receives version and build time through ldflags.
- Use the latest compatible `urfave/cli` v3 release at the CLI edge.
- `internal/cli` builds commands, `internal/app` owns use cases, and `internal/daemon` adapts those services to the local protocol.
- `internal/process`, `internal/output`, and `internal/protocol` own process groups, bounded output, and wire types respectively.
- Keep interfaces narrow and prefer the Go standard library wherever practical.

The daemon starts the exact argv array directly, never reconstructed shell text. Process names are unique within the nearest Git-root project, falling back to the current working directory. Each process records its PID, process-group ID, cwd, argv, start time, state, and exit status.

Stdout and stderr are captured separately in bounded in-memory ring buffers. Cursors are monotonically increasing byte positions. Reads default to at most 100 lines and 16 KiB and return truncation and next-cursor metadata.

`wait` checks retained output before blocking. It returns when matching output appears, the process exits, or the timeout expires, always returning a new cursor and an explicit matched, exited, or timed-out outcome.

## Configuration direction

Follow the useful separation in `../policydev/agent/config`: typed build options and runtime inputs, construction testable without CLI objects, platform-aware path helpers, and flags kept at the CLI boundary. Support arguments and environment variables where they improve shell and agent use.

Although `urfave/cli` supports YAML sources, persistent configuration files are explicitly outside the initial scope. Do not add YAML loading or allow CLI framework types to leak into application services.

## Security and lifecycle

The daemon protocol is internal newline-delimited JSON. The socket lives below `$XDG_RUNTIME_DIR/devproc/`, falling back to a per-user directory below the OS temporary directory. Directory and socket permissions must exclude other users.

Reject malformed or unsafe names, duplicate running names, invalid cursors and regular expressions, malformed protocol messages, and requests above server-side output limits with clear errors.

Each child starts in a Unix process group. `stop` sends SIGTERM to the group, waits a short grace period, then sends SIGKILL if necessary. Managed processes survive client disconnection.

## Verification

Unit tests cover ring eviction, cursor boundaries, truncation, stream filtering, and regex matching. Integration tests start a real daemon and fixture process, read incremental stdout and stderr, observe exit status, reconnect after client disconnection, and stop a spawned child process tree. Project gates run formatting, tests, static analysis, and built-binary smoke coverage on macOS and Linux.

## Initial limitations

No PTY or interactive programs, Windows support, authentication, remote access, web UI, persistent process history, proactive log delivery, plugin system, repository layer, dependency-injection framework, or premature persistence abstraction. Daemon restart loses buffered process history.

## Future MCP adapter

A future stdio or Streamable HTTP MCP server may expose `list`, `status`, `logs`, `wait`, and `stop`. It must delegate to the same `internal/app` services as the CLI and add only transport translation, schemas, and MCP-specific error mapping. It must not bypass socket ownership, output bounds, project scoping, or security assumptions. No MCP dependency or server package belongs in the initial implementation.
