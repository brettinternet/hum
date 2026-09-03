# hum design

## Product direction

`hum` is a local process supervisor for humans and coding agents that need shared, bounded access to long-running development processes and their output. The problem it solves: a developer starts `npm run dev` in a terminal and an agent working in the same repository cannot see that output, cannot tell whether the server is up, and cannot restart it, so the agent starts its own copy or asks the developer to paste logs. hum gives both parties one supervised set of named processes per project, with reads bounded for agent context windows.

The product contract is a resolved set of named development processes. Conventional projects need no hum-specific file: hum can discover one root `dev` task. Projects that need multiple processes, readiness, or explicit working directories commit one canonical `hum.yaml` manifest. Humans use one small CLI and coding agents use typed MCP tools or a short shell skill; both operate on the same resolved names. A long-running local daemon remains the only process owner, and every client communicates with it over a user-private Unix domain socket. macOS and Linux are the initial platforms.

Build the process-supervision foundation as a tested vertical slice: a foreground daemon, attached and detached `run`, `list`, bounded and followed `logs`, `stop`, and daemon `shutdown`; then detached daemon startup, `status`, `wait`, and `restart`. The first seamless product workflow follows immediately: add the YAML manifest and conservative default discovery, then expose their shared name-based operations through MCP and a shell-only agent skill. The raw `run -- <command>` form is an escape hatch, not the agent workflow.

## Planned CLI

```text
hum serve [--daemon]
hum run <name> [--detach] [--json] [-- <command> [args...]]
hum list [--all] [--json]
hum status <name> [--json]
hum logs <name> [--stream stdout|stderr|both] [--tail N] [--after-cursor N] [--limit-bytes N] [--match REGEX] [--follow] [--json]
hum wait <name> [--after-cursor N] [--match REGEX] [--timeout DURATION] [--json]
hum restart <name>... [--json]
hum stop <name>... [--json]
hum shutdown [--stop-processes] [--json]
hum start <name>... [--no-wait] [--timeout DURATION] [--json]
hum up [--no-wait] [--timeout DURATION] [--json]
hum down [--json]
hum init [--json]
hum skill
hum mcp
```

Human-readable output is the default. Stable JSON is intended for agents; every command that changes or reports state accepts `--json`. `logs --json --follow` emits newline-delimited JSON events. Live output is sent only to an attached `run` client or an explicit log follower; other commands receive bounded responses. Commands that take a process name accept several names and report one stable result per name. `--match REGEX` is the one spelling for a line regular expression: it filters `logs` and is the condition for `wait`.

Every error with an obvious next step names the command to run. The daemon is an implementation detail, so unavailable-daemon errors name a launch command rather than `serve`: a name with a resolved definition says `Nothing is running in this project. Start it with hum start <name>.`, an undefined name says `Nothing is running. Start a process with hum run <name> -- <command>.`, and `hum serve --daemon` appears only in `serve` help and operator documentation.

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

`run` is attached by default. The daemon owns the child while the CLI streams its stdout and stderr, preserving raw line content wherever possible. If the process exits while attached, the CLI returns its exit code. `run --detach` prints the process name and PID (or JSON) and returns immediately. Neither mode provides a PTY or arbitrary interactive stdin. Without a PTY most tools disable colors, which keeps captured logs clean for agents; humans who want colors while attached can set `FORCE_COLOR=1` in their shell. Starting a name that is already running fails, names the running PID, and suggests `hum logs <name> --follow` for watching it. Once project resolution defines a name, raw `run <name> -- <command>` is rejected so an ad hoc process cannot impersonate that contract; `run <name>` without argv remains valid and uses the resolved definition.

## Output model

Each process has one ordered sequence of line-oriented output entries. An entry carries its stream (stdout or stderr), a timestamp, and the raw line text. The cursor is the entry's sequence number: it starts at 0, increases monotonically for the life of the process record (including across `restart`), and is never reused. Every launch records its starting cursor. A manifest launch also records the readiness expression and, as output arrives, the first matching cursor for that incarnation. This small readiness state survives output eviction, resets on every relaunch, and is used only when the requested expression matches the recorded one; readiness therefore cannot be lost to bounded retention or leak across restarts. `--stream` and `--match` filter entries at read time, `--tail N` counts entries, and `--limit-bytes` bounds the response. One cursor space for both streams keeps `--stream both --after-cursor N` and `wait --after-cursor N` unambiguous.

Retained output is bounded per process by total bytes; the oldest entries are evicted first. A read whose cursor is older than the oldest retained entry returns from the oldest entry with explicit truncation metadata. Partial lines are flushed after a short idle interval or when they exceed a maximum line length, so progress output and unterminated prompts still become visible. Reads default to at most 100 entries and 16 KiB and return truncation and next-cursor metadata. Human-readable `logs` output ends with a one-line stderr trailer naming the next cursor and any truncation, so a shell caller can continue with `--after-cursor` without parsing JSON. System entries, such as a restart marker, are appended through the same sequence.

`wait` checks retained output before blocking. It returns when matching output appears, the process exits, or the timeout expires, always returning a new cursor and an explicit matched, exited, or timed-out outcome. `--after-cursor` defaults to the current incarnation's launch cursor, so the common case, "has the server said ready yet?", needs no bookkeeping and a line printed by an earlier incarnation can never satisfy a wait issued after `restart`. `--timeout` defaults to 30s so an agent tool call never blocks indefinitely. Without `--match`, `wait` waits for exit; waiting for declared readiness is `hum start <name>`, and `wait` help says so. Exit codes are stable for scripts and agents: 0 when the awaited condition happened (a match when `--match` is given, otherwise exit), 2 on timeout, 3 when the process exited before matching, and 1 for errors.

## Daemon startup and client behavior

`serve` runs in the foreground and writes diagnostics to stderr. `serve --daemon` starts a fully detached process in a new session/process group without retaining the caller's standard streams. The original caller waits for a readiness handshake, then receives the daemon PID and socket path. Detached diagnostics go to a bounded or rotating `daemon.log` in the runtime directory.

Detached startup is idempotent. A startup lock prevents concurrent `serve --daemon` or automatic-start attempts from creating multiple daemons. Startup verifies live ownership before recovering stale PID or socket files and reports readiness failures to the original caller.

Commands that launch resolved or ad hoc processes (`run`, `start`, and `up`) automatically start the detached daemon when none is available, including when clients race. Read-only and control commands never start an empty daemon. Without a daemon, project-aware `list` still reports resolved processes as stopped; `status`, `logs`, `wait`, and `restart` fail concisely with the unavailable-daemon message above; `stop` and `down` succeed because nothing is running; and `shutdown` succeeds with `No hum daemon is running.`

Every connection begins with a hello that carries the protocol version. The hello and shutdown message shapes are frozen so a newer client can always retire an idle older daemon. After a binary upgrade an older daemon may still be running: a mismatched client fails with the daemon's version and the shutdown command to run, except that launch commands (`run`, `start`, and `up`) replace a mismatched daemon automatically when it has no managed processes.

`list` shows the current project by default; `--all` shows every project with its root path. `shutdown` refuses while processes are running and lists each as `<project root>: <name>`.

`logs --follow` first returns retained output using the same tail, cursor, stream, match, and byte-limit rules as a bounded read, then delivers cursor-based events. Multiple followers are independent. Cancelling a follower leaves the managed process running. Delivery remains bounded per read; lagging followers receive explicit eviction information rather than causing unbounded buffering. Following remains available after the original attached `run` client disconnects and continues across `restart`.

## Signals

- Ctrl+C in an attached `run` sends SIGINT to the managed process group and stays attached; the CLI returns the exit code once the process exits. A second Ctrl+C requests the same graceful stop sequence as `hum stop` and waits for it.
- SIGTERM or SIGHUP to any client, including an attached `run` whose terminal closed or whose agent tool call timed out, detaches the client and leaves the managed process running.
- `stop` sends SIGTERM to each named group, waits a bounded grace period, then sends SIGKILL if necessary. Stopping a name that is not running succeeds and says so, so `stop` is as idempotent as `start`. `restart` runs the stop sequence and then relaunches the recorded argv, cwd, and environment under the same name, printing the new PID and launch cursor; the output sequence continues and records a system entry marking the restart.
- `down` applies the stop sequence concurrently to every running process in the current project, reports one result per name, and leaves the daemon and other projects' processes alone. It is the inverse of `up`; `shutdown` is the daemon-level operation.
- `shutdown --stop-processes` applies the stop sequence to every group before stopping the daemon. The daemon treats its own SIGTERM or SIGINT, including Ctrl+C on a foreground `serve`, the same way, so an intentional daemon exit never silently orphans a supervised process.

Managed processes survive ordinary client and follower disconnection.

## Agent interfaces

The primary agent interface is `hum mcp`, a stdio MCP server registered once with an agent. Its typed `start`, `up`, `down`, `list`, `status`, `logs`, `wait`, `restart`, and `stop` tools require an absolute existing directory and resolve it with the same nearest-Git-root rules as the CLI, so one server can safely serve multiple workspaces without relying on its startup directory. `start` and `up` operate only on explicit or discovered project definitions and auto-start the daemon. The MCP adapter uses the same daemon protocol client as the CLI; it never creates an in-process supervisor or bypasses daemon ownership. Tool responses preserve the CLI's bounds, cursors, outcomes, source metadata, and errors.

For shell-only agents, the binary embeds a one-screen skill file in the Agent Skills format. `hum skill` prints it for installation in the agent's normal skill location. The skill teaches the same resolved-project workflow: try `up` without inventing a command, use `list` to inspect the explicit or discovered definitions and their readiness, then inspect bounded logs, wait, restart, stop, and `down` by name. Missing `hum.yaml` is not an error when conventional discovery succeeds; when resolution reports ambiguity or no candidate, the skill asks the developer to run `hum init` and commit the result. The skill never asks the agent to derive or execute the underlying `npm run dev`-style command. A test keeps every command and flag named in the skill synchronized with the command tree.

## Configuration direction

Runtime settings are typed and resolved once at the CLI boundary: build metadata injected through ldflags, then command-line flags, then environment variables, then defaults. Construction is testable without CLI framework objects: a typed input struct feeds a constructor that validates and returns the configuration, and only the thin flag-reading adapter in `internal/cli` touches `urfave/cli`. Platform-aware path helpers choose the runtime directory. The environment variables are `HUM_RUNTIME_DIR` (overrides the runtime directory; integration tests use it for isolation), `HUM_STOP_GRACE` (SIGTERM-to-SIGKILL grace period, default 10s), `HUM_OUTPUT_BYTES` (retained output per process, default 4 MiB), and `HUM_COMPLETED_RECORDS` (completed records retained across projects, default 20).

Persistent configuration files for runtime settings are explicitly outside the initial scope. Do not allow CLI framework types to leak into application services. A committed per-project process manifest is different: it is project data, not tool configuration.

The only supported manifest is a single YAML document named `hum.yaml` at the discovered project root. Version 1 contains `version: 1` and a `processes` mapping keyed by safe process name. Each entry has a non-empty `argv` string sequence and may have a root-relative `cwd` and a `ready` mapping containing a required `match` regular expression plus an optional `timeout`. Parsing is strict: reject unknown or duplicate keys, aliases and merge keys, multiple documents, unsupported versions, shell text, empty argv elements, non-string command values, absolute working directories, and paths that escape the project root. Names are processed lexically for deterministic output. Do not also support JSON, TOML, `.yml`, or alternate manifest filenames.

`hum init` scaffolds the manifest so nobody writes it from memory. It refuses to overwrite an existing `hum.yaml`, runs the same discovery as `up`, and writes a version-1 document containing the single discovered definition with its exact argv, a comment naming the source, and a commented `ready` example. When discovery is ambiguous or finds nothing, it writes a commented template that lists every detected candidate by source and argv. The generated file always passes the strict parser, and `init` never launches a process or starts the daemon.

```yaml
version: 1
processes:
  web:
    argv: [bun, run, dev]
    cwd: web
    ready:
      match: "Local:"
      timeout: 30s
```

Resolved commands inherit the launching CLI or MCP server environment. Changing `cwd` does not activate per-directory shell hooks such as direnv, and a globally registered MCP server may have a different environment from an interactive terminal. Projects that require deterministic activation encode it in argv through their committed tool runner (for example, `mise exec -- ...`); version 1 deliberately does not commit secret environment values or shell text.

## Project definition resolution

Explicit configuration always wins. If `hum.yaml` exists, it is authoritative: invalid YAML fails visibly, and an empty `processes` mapping means the project defines no processes. Hum never falls back from a present manifest to inferred commands.

Without `hum.yaml`, hum conservatively looks for exactly one root `dev` task and normalizes it to the same internal process definition as YAML, named `dev`:

1. Discover project-level candidates through available task runners' native introspection: an exact local mise task named `dev` (`mise tasks --local --json`, run at the project root) and an exact Task task named `dev` (`task --dir <root> --list-all --json`). Skip a runner that is not installed. If exactly one candidate exists, use `mise run dev` or `task dev`. If both exist, return an ambiguity error listing both.
2. Only when no project-level candidate exists, use `package.json#scripts.dev`. Honor its `packageManager` field first. Otherwise select bun, pnpm, Yarn, or npm from exactly one supported lockfile family; conflicting lockfiles are an error, and no lockfile defaults to npm. Invoke it as runner argv such as `[bun, run, dev]`; never parse or execute the script body directly.
3. If no candidate exists, fail with an actionable suggestion to run `hum init`, add `hum.yaml`, or use ad hoc `hum run`. Never infer `start`, arbitrary package scripts, ports, readiness expressions, monorepo packages, or multiple processes.

`hum up` and `hum start dev` use the inferred definition; other names require `hum.yaml`. `list` reports the source as `manifest`, `mise`, `task`, or `package_json`, and shows each resolved entry's argv so the first `hum up` in a repository is never a surprise; `start` and `up` echo the same thing on launch, as in `started dev (package_json: bun run dev)`. `restart` resolves the definition again so edits to the selected source take effect. A discovered definition has no readiness contract: the default wait succeeds after spawn with the explicit outcome `running_unverified`, never `ready`. Malformed detector output, source ambiguity, and no candidate are typed configuration errors with candidate-specific guidance, not invitations for an agent to guess.

`start` is an idempotent ensure-running operation: an already-running resolved launch is reported without replacement, while a stopped or never-started entry launches from the current definition and caller environment. A running ad hoc record that collides with a newly resolved name is a conflict, not `already_running`. `up` applies that behavior to every entry, continues after individual failures, and returns one stable result per name. Both wait for readiness by default because that is what every caller wants; `--no-wait` returns after spawn. While waiting, every requested running incarnation with readiness configuration is checked against its retained per-incarnation readiness state and then new output, so an already-running process still converges on readiness while stale output from an earlier incarnation cannot match. Definitions without readiness configuration return `running_unverified` immediately after spawn, so the default costs nothing for discovered processes. Waits use an explicit CLI timeout, then the manifest timeout, then a bounded default. `up` starts entries in lexical order, waits concurrently, and reports aggregate failure without stopping successful processes. Restart refreshes the resolved argv, cwd, and requesting client's environment.

Readiness is visible without waiting. `status` and `list` report a `readiness` field for each running resolved process: `ready` with the matching cursor, `starting` while a configured expression has not yet matched, or `running_unverified` when the definition has no readiness. Exited processes and ad hoc records carry no readiness. This answers the most common agent question, "is the server up?", from state the daemon already keeps.

## Security and lifecycle

The daemon protocol is internal newline-delimited JSON, including bounded streaming events for attached output and log followers. The socket lives below `$XDG_RUNTIME_DIR/hum/`, falling back to a per-user directory below the OS temporary directory. Directory and socket permissions must exclude other users. The runtime directory also contains PID, startup-lock, and bounded/rotating daemon-log state.

Completed records retain buffered output and the launch environment only while restart remains available. The daemon evicts the oldest completed records beyond a small configured bound and clears their environment on eviction, preventing unbounded retention of secret-bearing environments and output. Active records are never evicted.

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
7. `status`, `wait`, and `restart`, each gated on the integration suite.
8. Release gates and operator documentation for the supervisor foundation.
9. The committed YAML manifest and idempotent `start`/`up` workflow with visible readiness.
10. `down` as the project-scoped inverse of `up`.
11. Conservative zero-config discovery normalized into the same process definitions.
12. `hum init` scaffolding of the manifest from discovery.
13. The MCP adapter and shell-only agent skill over resolved process names.
14. Consistent short flag aliases.

## Initial limitations

No PTY or arbitrary interactive input, Windows support, authentication, remote access, web UI, persistent process history, plugin system, repository layer, dependency-injection framework, or premature persistence abstraction. Daemon restart loses buffered process history. Do not add launchd, systemd, login startup, or operating-system service installation yet.

## Next milestone: seamless project processes and agent adapters

The vertical slice still asks a human to type `hum run web -- npm run dev` and asks an agent to reverse-engineer that command from `package.json`. Conservative discovery removes that barrier for conventional repositories: `hum up` finds one unambiguous root `dev` task and reports it as running but unverified. Projects add `hum.yaml` only when they need named processes, readiness, or working-directory control, and `hum init` writes the first draft from what discovery found. The manifest declares exact argv, working directory, and readiness once; it is not a Procfile of shell text.

A stdio MCP server exposes resolved-definition `start` and `up` plus `down`, `list`, `status`, `logs`, `wait`, `restart`, and `stop`. It uses the same daemon protocol client as the CLI and adds only transport translation, schemas, and MCP-specific error mapping. Every tool names its absolute project root; launch tools carry the MCP server's environment to the daemon and perform the same resolution, automatic startup, and version handling as the CLI. Arbitrary-command `run`, daemon `serve`, and daemon `shutdown` are deliberately absent. A future Streamable HTTP transport may expose the same tools, but no MCP dependency or server package belongs in the supervisor-foundation milestone.
