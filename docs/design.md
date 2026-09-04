# hum design

## Scope

hum is a local process supervisor for humans and coding agents. A private
Unix-socket daemon owns named process groups and bounded output independently of
clients. CLI and MCP clients resolve project definitions and send exact argv to
that daemon; they never create another supervisor or reconstruct shell text.

Process names are scoped to the nearest Git root, or the caller's working
directory when no Git marker exists. macOS and Linux are supported; Windows is
not.

## CLI

```text
hum init [--json]
hum serve [--daemon]
hum start <name>... [--no-wait] [--timeout DURATION] [--json]
hum up [--no-wait] [--timeout DURATION] [--json]
hum down [--json]
hum run <name> [--detach] [--json] [-- <command> [args...]]
hum list [--all] [--json]
hum status <name> [--json]
hum logs <name> [--stream stdout|stderr|both] [--tail N] [--after-cursor N]
           [--limit-bytes N] [--match REGEX] [--follow] [--json]
hum wait <name> [--after-cursor N] [--match REGEX] [--timeout DURATION] [--json]
hum restart <name>... [--json]
hum stop <name>... [--json]
hum remove <name>... [--json]
hum shutdown [--stop-processes] [--json]
hum mcp
```

Short aliases are command-local except the global help and version aliases.
Long options remain canonical in documentation, scripts, output, and errors.
Combined short options are unsupported; MCP fields have no aliases.

| Alias | Long option | Commands |
| --- | --- | --- |
| `-h` | `--help` | global |
| `-v` | `--version` | global |
| `-j` | `--json` | all supporting commands |
| `-d` | `--daemon`, `--detach` | `serve`, `run` |
| `-t` | `--timeout` | `start`, `up`, `wait` |
| `-a` | `--all` | `list` |
| `-s` | `--stream` | `logs` |
| `-n` | `--tail` | `logs` |
| `-c` | `--after-cursor` | `logs`, `wait` |
| `-b` | `--limit-bytes` | `logs` |
| `-m` | `--match` | `logs`, `wait` |
| `-f` | `--follow` | `logs` |

`--no-wait`, `--stop-processes`, `--runtime-dir`, `--stop-grace`,
`--output-bytes`, and `--completed-records` remain long-only.

Human-readable output is the default. JSON process snapshots include `name`,
`source`, and `argv`, plus identity, readiness, cursors, and errors when
applicable. `start` and `up` emit one NDJSON launch result per name. `up` uses
lexical declaration order, attempts every entry, and applies this exit-code
precedence: request error (1), early exit (3), timeout (2), success (0).
Attached `run --json` still streams raw child output; `logs --json --follow`
emits bounded NDJSON events.

### Command semantics

`init` resolves the project and zero-config candidates without launching or
starting the daemon. It exclusively creates `hum.yaml`: one discovered
candidate produces a definition; none or several produce a commented, valid
template. Existing paths and resolution or write errors exit 1. Output includes
the path, `generated` or `template` outcome, and `hum up` as the next command;
JSON also includes candidates.

`start` idempotently ensures a named session is running. It relaunches retained
stopped records; retained ad hoc records reuse their exact argv, cwd, and
environment, while resolved records use the current definition and client
environment. Concurrent starts create at most one child. `up` does the same only
for current resolved definitions, waits concurrently, and leaves successful
children running after other failures. `--no-wait` returns after spawn.

`run <name>` uses an existing resolved definition or attaches to an existing
running or stopped session. `run <name> -- <command>...` creates an ad hoc
session or replaces a stopped session's retained ad hoc launch spec; it keeps
existing conflict rules while running. A missing zero-config candidate permits
the ad hoc form; malformed, ambiguous, and introspection failures do not. A
resolved name cannot be occupied by a conflicting ad hoc run.

`restart` uses the current resolved definition and client environment. If only
a retained ad hoc record exists, it reuses its exact argv, cwd, and environment.
Daemon replacement loses ad hoc definitions, so evicted records cannot be
restarted.

`list` merges current definitions with all project runtime records. Without a
daemon it reports resolved definitions as stopped. `status`, `logs`, `wait`,
`restart`, `stop`, and `remove` operate on resolved and ad hoc records in the
project. `stop` preserves the durable session; `remove` stops its child, closes
followers, and discards runtime launch state and output without editing
`hum.yaml`.

`down` is the project-scoped inverse of `up`. It concurrently stops every
running project record, includes declared-but-absent names as `not_running`, and
returns one name-sorted `stopped`, `not_running`, or `error` result per name.
It never starts or shuts down the daemon, affects other projects, or deletes
records. With no daemon or names it succeeds with
`Nothing is running in this project.` Any stop error exits 1.

`shutdown` controls daemon lifetime across projects. It refuses while any
process is active unless `--stop-processes` is given.

## Definitions and resolution

### Manifest

The nearest Git project root may contain one authoritative `hum.yaml`. A valid
empty manifest resolves to no definitions; an invalid manifest is an error.
Discovery occurs only when the file is absent. Alternate filenames are ignored.

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

Each entry requires a safe name and a non-empty string argv. Optional `cwd` is
root-relative and must exist and remain beneath the root after lexical and
symlink resolution. `ready.match` is a regular expression; `ready.timeout` is a
positive duration defaulting to 30 seconds.

Parsing is strict and single-document. Unknown or duplicate keys, YAML aliases
or merges, unsupported versions, invalid names, regexes or durations, unsafe
cwd, empty/non-string argv, and shell text are errors with file and entry
context. Definitions are name-sorted and carry `source: manifest`.

The manifest defines processes only: no runtime settings, dependencies, ports,
HTTP checks, restart policy, or environment values/files. Projects needing
environment activation must commit a runner and put it in argv; CLI and MCP do
not activate mise, nvm, direnv, or shell hooks.

### Zero-config discovery

Without `hum.yaml`, hum inspects supported root-level conventions without
executing task bodies or launching candidates. Exactly one candidate resolves
to `dev`, rooted at the project, with no inferred readiness:

| Source | Required `dev` entry | argv |
| --- | --- | --- |
| Mise | local task | `mise run dev` |
| Task | task | `task dev` |
| Just | public recipe | `just dev` |
| Make | literal, non-pattern target | `make dev` |
| package.json | script | `<manager> run dev` |
| deno.json(c) | task | `deno task dev` |
| composer.json | script | `composer run-script dev` |
| bin/dev | executable file | `./bin/dev` |
| Mix | introspection confirms `phx.server` | `mix phx.server` |

Command-backed sources are skipped when their executable is unavailable.
No candidates produce a typed `NoCandidateError`; several produce an
`AmbiguityError` listing all sources. Malformed configuration and failed or
malformed required introspection produce typed `ConfigurationError` and
`IntrospectionError`. All wrap their sentinel and work with `errors.As`.

For package.json, `packageManager` selects bun, pnpm, yarn, or npm (ignoring an
optional version suffix) and rejects other or non-string values. Otherwise the
runner comes from exactly one lockfile family: Bun (`bun.lock`, `bun.lockb`),
pnpm (`pnpm-lock.yaml`), Yarn (`yarn.lock`), or npm (`package-lock.json`,
`npm-shrinkwrap.json`). Multiple files in one family are allowed; conflicting
families are errors. With no lockfile, npm is used.

Discovery does not scan nested packages or infer language/framework commands
(except confirmed `mix phx.server`), Docker Compose, multiple processes, ports,
readiness, or dependencies. It never tries commands to see what succeeds.

Strict definition commands (`up`, `start`, and argv-free `run`) resolve before
daemon startup. Ad hoc `run` alone treats `NoCandidate` as no definition.
Control commands also treat it as no definition and may access existing runtime
records. Every other resolution error propagates before daemon control.

## Readiness and output

The daemon launches exact argv directly, without an implicit shell. Each child
has a Unix process group and stdin at `/dev/null`. `stop` and `down` send SIGTERM,
wait a bounded grace period, then send SIGKILL. Client and follower disconnects
do not stop children.

A launch records its readiness expression, launch cursor, and first matching
cursor even if nobody is waiting. Configured processes move from `starting` to
`ready`; the retained state survives output eviction. Relaunch resets readiness
at a new launch cursor, so old output cannot satisfy it. Definitions without
`ready`, including all discovered definitions, report `running_unverified` and
are never reported ready. A CLI timeout overrides the manifest timeout.

Each durable named session has one cursor sequence across stdout, stderr, and
incarnations. Entries contain stream, timestamp, raw text, and cursor.
Byte-bounded retention reports eviction explicitly; a live pre-launch or stopped
follower reserves its session from completed-record eviction. Attached `run` and
`logs --follow` may start before the first launch, return retained output, print
exit/wait/launch boundaries, and remain open across stop/start and down/up until
Ctrl+C, removal, or transport loss. Ctrl+C detaches only the observer. `wait`
without an explicit cursor waits for the next incarnation when stopped or
unlaunched and remains bounded (30 seconds by default). Exited and ad hoc
records omit readiness.

## Daemon and environments

One daemon serves each private runtime directory at `hum.sock`. `serve --daemon`,
`run`, `start`, and `up` use a startup lock and readiness handshake.
Read-only commands do not start an empty daemon. Foreground daemon exit and
`shutdown --stop-processes` stop all managed groups.

The launching client supplies cwd and its full environment. Manifest `cwd`
changes only the child directory; discovered definitions use the project root.
Resolved restarts use the current argv, cwd, readiness, and requesting client's
environment, so definition edits take effect.

## MCP adapter

`hum mcp` serves JSON-RPC over stdin/stdout. Every request requires an absolute,
existing `project_root` chosen by the same root rule as the CLI. It exposes ten
tools: `start`, `up`, `down`, `list`, `status`, `logs`, `wait`, `restart`,
`stop`, and `remove`.

The tools share CLI definition, readiness, cursor, collision, and aggregate
semantics. Only `start` and `up` may create or replace a daemon. Without one,
`list` reports stopped definitions; `stop` and `down` succeed; the other control
tools return unavailable-daemon errors. Recorded environments are never
returned.

MCP exposes no follow or other unbounded operation; agents use bounded `wait`
and `logs`. The adapter receives a protocol-shaped daemon client and constructs
no app services, supervisors, or output stores in-process. It has no `run`,
`serve`, or `shutdown`, HTTP transport, authentication, remote access, or arbitrary-command
tool.

## Non-goals

The foundation has no PTY or arbitrary interactive input, automatic crash
restart/backoff, remote transport, authentication, web UI, persistent process
history, plugin system, OS service installation, or environment literals/files.
The runtime directory contains only the socket, PID/startup state, and bounded
daemon diagnostics.
