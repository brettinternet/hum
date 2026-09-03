# hum design

## Scope and status

hum is a local process supervisor for humans and coding agents. A private
Unix-socket daemon owns named process groups and bounded output independently of
client lifetimes. The current product includes a strict project manifest and
idempotent manifest-backed launches while retaining an ad hoc command path for
operators. macOS and Linux are supported; Windows is not.

The daemon is the only process owner. CLI and future agent adapters resolve a
project definition, translate it to the daemon protocol, and never launch a
second supervisor or reconstruct shell text. Process names are scoped to the
nearest Git root; without a Git marker, the caller's working directory is the
project root.

## CLI

```text
hum serve [--daemon]
hum start <name>... [--no-wait] [--timeout DURATION] [--json]
hum up [--no-wait] [--timeout DURATION] [--json]
hum run <name> [--detach] [--json] [-- <command> [args...]]
hum list [--all] [--json]
hum status <name> [--json]
hum logs <name> [--stream stdout|stderr|both] [--tail N] [--after-cursor N]
           [--limit-bytes N] [--match REGEX] [--follow] [--json]
hum wait <name> [--after-cursor N] [--match REGEX] [--timeout DURATION] [--json]
hum restart <name>... [--json]
hum stop <name>... [--json]
hum shutdown [--stop-processes] [--json]
```

Human-readable output is the default. `start` and `up` JSON are newline-
delimited, with one launch result per name. Every result has `name`,
`outcome`, `source`, and `argv`; process identity, readiness, `ready_cursor`,
and error details are included when applicable. `up` emits lexical declaration
order and attempts every entry even after an individual failure. Its aggregate
exit precedence is request error 1, exited before readiness 3, timeout 2, then
success 0. Attached `run --json` still streams raw child output, and
`logs --json --follow` emits bounded NDJSON events.

`run <name>` without an argv uses the current manifest definition when the name
is declared and keeps attached-run semantics. Raw
`run <name> -- <command> [args...]` remains available for undeclared ad hoc
names only. A declared name cannot be occupied by a conflicting raw run.
`restart` uses the current manifest definition when one exists; it is not a
copy of an old command line.

## Canonical strict manifest

A project may commit exactly one YAML document named `hum.yaml` at its nearest
Git project root. Version 1 has a top-level `version: 1` and a `processes`
mapping keyed by safe process name:

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

Each entry has a non-empty string `argv` sequence. `cwd`, when present, is
root-relative. It is normalized to an absolute path under the project root;
lexical traversal, symlink-resolved escape, and missing launch directories are
errors. `ready.match` is a regular expression and `ready.timeout` is a positive
duration whose default is 30 seconds.

Parsing is strict and single-document. Unknown or duplicate keys, aliases and
merge keys, multiple documents, unsupported versions, invalid names, invalid
regular expressions or durations, empty or non-string argv elements, shell
text, absolute or escaping cwd values, and unsafe entries are rejected with
file and entry context. `hum.yml`, `hum.json`, `hum.toml`, and other alternate
manifest names are ignored. Definitions carry `source: manifest` and are
sorted lexically by name before the CLI consumes them.

The manifest contains process definitions only. It has no runtime settings,
dependency ordering, ports or HTTP health checks, automatic crash
restart/backoff, environment literals, or environment files.

## Start and readiness

`start` is an idempotent ensure-running operation. A stopped or never-started
manifest entry launches detached with the requesting client's working
directory and full environment. An already-running manifest incarnation is
left in place and returns `already_running`; concurrent ensures create at most
one child. `up` applies the same operation to every declaration in lexical
order, waits for readiness concurrently, and leaves successful processes
running even if another declaration fails. `--no-wait` returns after spawn.

A manifest launch records its readiness expression, launch cursor, and the
first matching output cursor for that incarnation even when no client is
waiting. A configured process is `starting` until its expression matches and
then `ready`; readiness is retained after bounded output evicts the matching
line. A relaunch resets readiness at the new launch cursor, while the output
store and monotonically increasing cursor sequence continue. Old output or old
satisfied state cannot satisfy a new incarnation. A declaration without
`ready` succeeds as `running_unverified`.

The CLI timeout overrides the declaration timeout. Waiting first checks the
recorded state, then subscribes from the current launch cursor, so an
already-running process that matched earlier still succeeds immediately.
`status` and `list` expose readiness without waiting: running manifest records
show `starting`, `ready` with `ready_cursor`, or `running_unverified`; exited
records and ad hoc records omit readiness. `logs` and explicit `wait` operate
on the same current record and bounded cursor stream; a requested match never
uses an earlier incarnation.

## Project-aware list, status, and errors

Without a daemon, `list` reads the local `hum.yaml` and reports every
declaration as stopped with `source: manifest` and its `argv`; it does not
create runtime state. With a daemon, it merges declarations and runtime
records. Manifest records carry `source: manifest`; raw `run` records carry
`source: ad_hoc`. Every JSON process snapshot includes `argv` and, when
present, readiness fields.

When a resolved name has no available daemon, `status`, `logs`, `wait`, and
`restart` say:

```text
Nothing is running in this project. Start it with hum start <name>.
```

Undefined names retain the ad hoc guidance to use `hum run <name> -- <command>`.
A failed `up` still reports one result for every declaration, and successful
children remain supervised.

## Process and output model

The daemon starts the exact argv array directly. It never parses shell text or
uses a shell as an implicit command interpreter. Each child has its own Unix
process group and stdin attached to `/dev/null`; `stop` sends SIGTERM to the
group, waits a bounded grace period, and then sends SIGKILL if necessary.
Clients and log followers may disconnect without stopping the managed process.

Each process has one ordered output sequence shared by stdout and stderr. A
line entry carries its stream, timestamp, raw text, and cursor. Cursors increase
monotonically for the lifetime of the process record, including across
restarts. Retention is bounded by bytes; stale reads return explicit eviction
metadata rather than unbounded history. `logs --follow` starts with the same
bounded retained read and then delivers cursor-based events without signaling
the process when the follower is cancelled.

`serve --daemon`, `run`, `start`, and `up` use an idempotent startup lock and
readiness handshake. Read-only commands do not start an empty daemon. A
foreground daemon or `shutdown --stop-processes` stops its managed groups;
ordinary shutdown refuses while any process remains active. Runtime state is
in memory, so replacing a daemon loses retained output and process history.

## Working directory and deterministic environments

The requesting client supplies cwd and environment at launch. A manifest cwd
sets only the child's working directory; it does **not** activate shell hooks
from mise, nvm, direnv, or another interactive shell. `restart` resolves the
current manifest argv/cwd/readiness and uses the requesting client's current
environment, so edits to the committed definition take effect.

For deterministic MCP execution, a project that needs environment activation
commits a tool runner or wrapper and puts it in manifest argv:

```yaml
processes:
  web:
    argv: [./tools/run-with-project-env, bun, run, dev]
```

The committed runner explicitly owns activation. CLI and MCP launches then use
the same argv without depending on the daemon's shell, startup directory, or
mutable interactive hooks. Environment literals and environment files remain
out of the manifest format.

## Current non-goals and future surfaces

This change does not add zero-configuration discovery, `down`, manifest
generation (`init`), a shell-only skill, an MCP server, dependency ordering,
ports or HTTP health checks, automatic crash restart/backoff, environment
literals/files, or arbitrary-command MCP tools. These may be separate product
surfaces; they must continue to reuse the daemon boundary and exact argv model.

The foundation also intentionally has no PTY, arbitrary interactive input,
remote transport, authentication, web UI, persistent process history, plugin
system, or operating-system service installation. The private runtime directory
contains the socket, PID/startup state, and bounded daemon diagnostics.
