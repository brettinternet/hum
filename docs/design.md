# hum design

## Product direction

`hum` is a local process supervisor for humans and coding agents that need shared, bounded access to long-running development processes and their output. The problem it solves: a developer starts `npm run dev` in a terminal and an agent working in the same repository cannot see that output, cannot tell whether the server is up, and cannot restart it, so the agent starts its own copy or asks the developer to paste logs. hum gives both parties one supervised set of named processes per project, with reads bounded for agent context windows.

The primary interface is one small CLI usable from any shell. A long-running local daemon owns child processes; clients communicate with it over a user-private Unix domain socket. macOS and Linux are the initial platforms.

Build the smallest complete vertical slice first: a foreground daemon, attached and detached `run`, `list`, bounded and followed `logs`, `stop`, and daemon `shutdown`. Then add detached daemon startup with automatic start from `run`. Only after the slice passes its built-binary integration tests add `status`, `wait`, `restart`, and the shipped agent skill. A project manifest and an MCP adapter are the next milestone, not the foundation.

## Planned CLI

```text
hum serve [--daemon]
hum run <name> [--detach] [--json] -- <command> [args...]
hum list [--all] [--json]
hum status <name> [--json]
hum logs <name> [--stream stdout|stderr|both] [--tail N] [--after-cursor N] [--limit-bytes N] [--grep REGEX] [--follow] [--json]
hum wait <name> [--after-cursor N] [--match REGEX] [--timeout DURATION] [--json]
hum restart <name> [--json]
hum stop <name>
hum shutdown [--stop-processes]
hum skill
```

Human-readable output is the default. Stable JSON is intended for agents. `logs --json --follow` emits newline-delimited JSON events. Live output is sent only to an attached `run` client or an explicit log follower; other commands receive bounded responses. Every error with an obvious next step names the command to run, in the style of `Start it with hum serve --daemon.`

## Architecture

- `cmd/hum/main.go` stays small: `main` calls a testable `run(context.Context, []string)`, reports errors to stderr, handles clean exits and SIGINT/SIGTERM/SIGHUP, and receives version and build time through ldflags.
- Use the latest compatible `urfave/cli` v3 release at the CLI edge.
- `internal/cli` builds commands, `internal/app` owns use cases, and `internal/daemon` adapts those services to the local protocol.
- `internal/process`, `internal/output`, and `internal/protocol` own process groups, bounded output, and wire types respectively.
- Keep interfaces narrow and prefer the Go standard library wherever practical.

## Process launch

The daemon starts the exact argv array directly, never reconstructed shell text. Process names are unique within the nearest Git root, where a `.git` directory or a `.git` file (a linked worktree) both count as a root; without one, the current working directory is the project root. Each process records its project root, PID, process-group ID, cwd, argv, environment, start time, state, exit status, and restart count.

The child runs with the `run` client's working directory and full environment, never the daemon's. The daemon may have been started hours earlier from a different shell with a different `PATH`, and version managers such as mise, nvm, and direnv rely on the caller's environment. The recorded environment exists only so `restart` can relaunch faithfully; no command returns it, because it commonly contains secrets.

Each child starts in its own Unix process group with stdin attached to `/dev/null`, so `npm run dev` and the tools it spawns can be signaled together and nothing blocks waiting for terminal input.

`run` is attached by default. The daemon owns the child while the CLI streams its stdout and stderr, preserving raw line content wherever possible. If the process exits while attached, the CLI returns its exit code. `run --detach` prints the process name and PID (or JSON) and returns immediately. Neither mode provides a PTY or arbitrary interactive stdin. Without a PTY most tools disable colors, which keeps captured logs clean for agents; humans who want colors while attached can set `FORCE_COLOR=1` in their shell. Starting a name that is already running fails and names the running PID.

## Output model

Each process has one ordered sequence of line-oriented output entries. An entry carries its stream (stdout or stderr), a timestamp, and the raw line text. The cursor is the entry's sequence number: it starts at 0, increases monotonically for the life of the process record (including across `restart`), and is never reused. `--stream` filters entries at read time, `--tail N` counts entries, and `--limit-bytes` bounds the response. One cursor space for both streams keeps `--stream both --after-cursor N` and `wait --after-cursor N` unambiguous.

Retained output is bounded per process by total bytes; the oldest entries are evicted first. A read whose cursor is older than the oldest retained entry returns from the oldest entry with explicit truncation metadata. Partial lines are flushed after a short idle interval or when they exceed a maximum line length, so progress output and unterminated prompts still become visible. Reads default to at most 100 entries and 16 KiB and return truncation and next-cursor metadata. System entries, such as a restart marker, are appended through the same sequence.

`wait` checks retained output before blocking. It returns when matching output appears, the process exits, or the timeout expires, always returning a new cursor and an explicit matched, exited, or timed-out outcome. `--after-cursor` defaults to 0 so the common case, "has the server said ready yet?", needs no bookkeeping. Exit codes are stable for scripts and agents: 0 when the awaited condition happened (a match when `--match` is given, otherwise exit), 2 on timeout, 3 when the process exited before matching, and 1 for errors.

## Daemon startup and client behavior

`serve` runs in the foreground and writes diagnostics to stderr. `serve --daemon` starts a fully detached process in a new session/process group without retaining the caller's standard streams. The original caller waits for a readiness handshake, then receives the daemon PID and socket path. Detached diagnostics go to a bounded or rotating `daemon.log` in the runtime directory.

Detached startup is idempotent. A startup lock prevents concurrent `serve --daemon` or automatic-start attempts from creating multiple daemons. Startup verifies live ownership before recovering stale PID or socket files and reports readiness failures to the original caller.

`run` automatically starts the detached daemon when no daemon is available, including when several `run` clients race. Read-only and control commands never start an empty daemon. `list`, `status`, `logs`, `wait`, `restart`, `stop`, and `shutdown` instead fail concisely with `Start it with hum serve --daemon.`

Every connection begins with a hello that carries the protocol version. The hello and shutdown message shapes are frozen so a newer client can always retire an idle older daemon. After a binary upgrade an older daemon may still be running: a mismatched client fails with the daemon's version and the shutdown command to run, except that `run` replaces a mismatched daemon automatically when it has no managed processes.

`list` shows the current project by default; `--all` shows every project with its root path. `shutdown` refuses while processes are running and lists each as `<project root>: <name>`.

`logs --follow` first returns retained output using the same tail, cursor, stream, grep, and byte-limit rules as a bounded read, then delivers cursor-based events. Multiple followers are independent. Cancelling a follower leaves the managed process running. Delivery remains bounded per read; lagging followers receive explicit eviction information rather than causing unbounded buffering. Following remains available after the original attached `run` client disconnects and continues across `restart`.

## Signals

- Ctrl+C in an attached `run` sends SIGINT to the managed process group and stays attached; the CLI returns the exit code once the process exits. A second Ctrl+C requests the same graceful stop sequence as `hum stop` and waits for it.
- SIGTERM or SIGHUP to any client, including an attached `run` whose terminal closed or whose agent tool call timed out, detaches the client and leaves the managed process running.
- `stop` sends SIGTERM to one managed group, waits a bounded grace period, then sends SIGKILL if necessary. `restart` runs that sequence and then relaunches the recorded argv, cwd, and environment under the same name, printing the new PID; the output sequence continues and records a system entry marking the restart.
- `shutdown --stop-processes` applies the stop sequence to every group before stopping the daemon. The daemon treats its own SIGTERM or SIGINT, including Ctrl+C on a foreground `serve`, the same way, so an intentional daemon exit never silently orphans a supervised process.

Managed processes survive ordinary client and follower disconnection.

## Agent onboarding

The binary embeds a one-screen skill file in the Agent Skills format (YAML frontmatter with `name` and `description`, then instructions). `hum skill` prints it, so `hum skill > .claude/skills/hum/SKILL.md` installs it for Claude Code and the equivalent path works for other agents. The skill tells an agent to start processes with `run --detach`, wait for readiness with `wait --match`, read bounded output with `logs`, `restart` after configuration changes, `stop` when done, and never run dev servers directly in its own shell. A test keeps every command and flag named in the skill in sync with the real command tree.

## Configuration direction

Runtime settings are typed and resolved once at the CLI boundary: build metadata injected through ldflags, then command-line flags, then environment variables, then defaults. Construction is testable without CLI framework objects: a typed input struct feeds a constructor that validates and returns the configuration, and only the thin flag-reading adapter in `internal/cli` touches `urfave/cli`. Platform-aware path helpers choose the runtime directory. The environment variables are `HUM_RUNTIME_DIR` (overrides the runtime directory; integration tests use it for isolation), `HUM_STOP_GRACE` (SIGTERM-to-SIGKILL grace period, default 10s), and `HUM_OUTPUT_BYTES` (retained output per process, default 4 MiB).

Persistent configuration files for runtime settings are explicitly outside the initial scope. Do not add YAML loading for them or allow CLI framework types to leak into application services. A committed per-project process manifest is different: it is project data, not tool configuration, and is planned for the next milestone.

## Security and lifecycle

The daemon protocol is internal newline-delimited JSON, including bounded streaming events for attached output and log followers. The socket lives below `$XDG_RUNTIME_DIR/hum/`, falling back to a per-user directory below the OS temporary directory. Directory and socket permissions must exclude other users. The runtime directory also contains PID, startup-lock, and bounded/rotating daemon-log state.

Reject malformed or unsafe names, duplicate running names, invalid cursors and regular expressions, malformed protocol messages, and requests above server-side output limits with clear errors. Never return a recorded environment over the protocol.

## Verification

Unit tests cover ring eviction, cursor boundaries, truncation, stream filtering, regex matching, partial-line flushing, concurrent followers, cancellation, and bounded slow-client delivery. Integration tests cover foreground serve, detached readiness and terminal isolation, idempotent and concurrent automatic startup, stale PID/socket recovery, version-mismatch handling, client environment inheritance, attached output and exit status, SIGINT forwarding and detach-on-SIGTERM, client disconnection, detached run, multiple followers, eviction, NDJSON follow output, stop, restart, shutdown refusal, and graceful process-tree termination. Project gates run formatting, tests, static analysis, and built-binary smoke coverage on macOS and Linux.

## Delivery order

1. Bootstrap the CLI and gates (done).
2. Typed configuration and paths; bounded output store; project-scoped process supervision.
3. Private NDJSON daemon protocol over the Unix socket.
4. CLI commands over a foreground daemon: `serve`, `run`, `list`, `logs`, `stop`, `shutdown`.
5. Detached daemon, automatic start from `run`, startup locking, stale recovery, version handshake.
6. Built-binary integration suite on macOS and Linux.
7. `status`, `wait`, `restart`, and the shipped agent skill, each gated on the integration suite.
8. Release gates and operator documentation.

## Initial limitations

No PTY or arbitrary interactive input, Windows support, authentication, remote access, web UI, persistent process history, plugin system, repository layer, dependency-injection framework, or premature persistence abstraction. Daemon restart loses buffered process history. Do not add launchd, systemd, login startup, or operating-system service installation yet.

## Next milestone: project manifest and agent adapters

The vertical slice still asks a human to type `hum run web -- npm run dev` and asks an agent to reverse-engineer that command from `package.json`. A committed manifest at the project root removes both barriers: it declares named processes as argv arrays with an optional cwd and an optional readiness pattern. With it, `hum start web` (or `hum up`) launches by name and can block until the readiness pattern matches, `run <name>` without argv uses the declared command, `list` shows declared-but-stopped processes, and `restart` prefers the manifest so edits to it take effect. The manifest keeps exact-argv execution; it is not a Procfile of shell text.

A stdio or Streamable HTTP MCP server may expose `list`, `status`, `logs`, `wait`, `restart`, and `stop`. It must delegate to the same `internal/app` services as the CLI and add only transport translation, schemas, and MCP-specific error mapping. It must not bypass socket ownership, output bounds, project scoping, or security assumptions. No MCP dependency or server package belongs in the initial implementation.
