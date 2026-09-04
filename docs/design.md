# hum design

## Scope and status

hum is a local process supervisor for humans and coding agents. A private
Unix-socket daemon owns named process groups and bounded output independently of
client lifetimes. The current product includes a strict project manifest,
bounded zero-config resolution for one conventional `dev` entrypoint,
idempotent resolved-definition launches, an ad hoc command path for operators,
and an MCP stdio adapter. macOS and Linux are supported; Windows is not.

The daemon is the only process owner. CLI and MCP adapters resolve a project
definition, translate it to the daemon protocol, and never launch a second
supervisor or reconstruct shell text. Process names are scoped to the nearest
Git root; without a Git marker, the caller's working directory is the project
root.

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
hum shutdown [--stop-processes] [--json]
hum mcp
```

### Stable CLI flag aliases

Aliases are command-local (with `-h/--help` and `-v/--version` as global
aliases). Combined-short parsing is unsupported; each short spelling must be
provided as its own option. The long spelling remains canonical and is
preferred in primary commands, narrative, scripts, workflow examples, and
error messages. MCP fields have no aliases.

| Command scope | Alias | Canonical long option |
| --- | --- | --- |
| Global | `-h` | `--help` |
| Global | `-v` | `--version` |
| `init` | `-j` | `--json` |
| `serve` | `-d` | `--daemon` |
| `start` | `-t` | `--timeout` |
| `start` | `-j` | `--json` |
| `up` | `-t` | `--timeout` |
| `up` | `-j` | `--json` |
| `down` | `-j` | `--json` |
| `run` | `-d` | `--detach` |
| `run` | `-j` | `--json` |
| `list` | `-a` | `--all` |
| `list` | `-j` | `--json` |
| `status` | `-j` | `--json` |
| `logs` | `-s` | `--stream` |
| `logs` | `-n` | `--tail` |
| `logs` | `-c` | `--after-cursor` |
| `logs` | `-b` | `--limit-bytes` |
| `logs` | `-m` | `--match` |
| `logs` | `-f` | `--follow` |
| `logs` | `-j` | `--json` |
| `wait` | `-c` | `--after-cursor` |
| `wait` | `-m` | `--match` |
| `wait` | `-t` | `--timeout` |
| `wait` | `-j` | `--json` |
| `restart` | `-j` | `--json` |
| `stop` | `-j` | `--json` |
| `shutdown` | `-j` | `--json` |

These options remain long-only and have no aliases: `--no-wait`,
`--stop-processes`, `--runtime-dir`, `--stop-grace`, `--output-bytes`, and
`--completed-records`.

Human-readable output is the default. `start` and `up` JSON are newline-
delimited, with one launch result per name. Every result has `name`, `outcome`,
`source`, and `argv`; process identity, readiness, `ready_cursor`, and error
details are included when applicable. `up` emits lexical declaration order and
attempts every entry even after an individual failure. Its aggregate exit
precedence is request error 1, exited before readiness 3, timeout 2, then
success 0. Attached `run --json` still streams raw child output, and
`logs --json --follow` emits bounded NDJSON events. A definition without a
readiness expression, including every discovered definition, reports
`running_unverified` and is never reported ready.
`hum init` is a non-launching manifest scaffold operation. It resolves the
nearest project root and the same zero-config candidates as `up`, then writes
only when `hum.yaml` is absent. Human output has `path:`, `outcome:`, and
`next_command: hum up` lines; `--json` emits `path`, `outcome`, `next_command`,
and `candidates`. Successful outcomes are `generated` for one candidate and
`template` for no or ambiguous candidates. Existing manifests and root,
configuration, introspection, or write errors exit 1.

`run <name>` without an argv uses the current resolved project definition when
the name resolves and keeps attached-run semantics; this includes discovered
`dev` and requires strict resolution, so a no-candidate error occurs before
daemon startup. Raw `run <name> -- <command> [args...]` remains available only
for names with no resolved definition. When discovery yields `NoCandidate`, this
explicit-argv form intentionally treats it as no local definition and may start
the daemon for an ad hoc run; malformed, ambiguous, or introspection errors still
propagate before startup. A resolved name cannot be occupied by a conflicting
raw run. `restart` uses the current resolved definition when one exists; it is
not a copy of an old command line.

### Project-scoped `down`

`down` is the project-scoped inverse of `up`. It resolves the caller's current
project root, uses the existing project-scoped `List` operation to collect active
resolved and ad hoc records, and, when a daemon is available, loads current
manifest declarations so each declared name without a runtime record is included
as `not_running`. It then applies the existing named `Stop` operation concurrently
to every running name, with an independent daemon connection per stop because a
single `Client` serializes round trips.

`down` never autostarts or shuts down the daemon, never signals a process outside
the current project, and leaves runtime records intact. The daemon remains
available for later commands. Results are deterministic and name-sorted, with
exactly one result per name and status `stopped`, `not_running`, or `error`;
errors include `message`. Human output is one `<name> stopped`, `<name> not
running`, or `<name> error: <message>` line per result. `--json` emits the
existing newline-delimited `stopResult` objects. Any real stop error sets exit
status 1; otherwise exit status is 0.

`down` is successful and does not create runtime state when no daemon is
available; its human output is exactly `Nothing is running in this project.`.
When a daemon is available but there are no runtime records and no declarations,
it returns the same message. If declarations exist without runtime records, each
declared name is emitted as `not_running`. This differs from `stop <name>...`,
which targets only explicitly named groups, and from `shutdown`, which controls
daemon lifetime: ordinary `shutdown` refuses while any process is active, while
`shutdown --stop-processes` stops all managed groups across projects before
terminating the daemon. Neither shutdown mode is used by `down`.

## MCP stdio adapter

`hum mcp` speaks MCP JSON-RPC over stdin/stdout only. Register it once with a
coding agent as a direct executable (`/absolute/path/to/hum` with argument
`mcp`); the agent does not need a shell skill. Every tool request requires
`project_root` as an absolute existing directory. The caller supplies the
nearest Git project root, or the requested directory when no Git marker exists,
using the same root rule as the CLI.

The adapter exposes exactly nine tools:
`start`, `up`, `down`, `list`, `status`, `logs`, `wait`, `restart`, and `stop`.
`start` and `up` are resolved-definition operations: names must come from the
current explicit manifest or zero-config resolution. They wait for readiness by
default and accept `no_wait` and `timeout`, returning CLI states, source and
`argv` metadata, retained per-incarnation readiness outcomes, cursors,
collision errors, and `up` aggregate semantics. Discovered definitions without
readiness return `running_unverified` and are never reported ready.

`list` merges resolved definitions with every runtime record in the project.
`status`, `logs`, `wait`, `restart`, and `stop` control existing records in the
requested project, whether resolved or `ad_hoc`; `down` stops every running
record and returns CLI per-name results. `restart` uses the current resolved
definition and MCP server environment when one exists. Otherwise, a retained
ad hoc record is relaunched with its exact recorded argv, cwd, and environment.
Logs and wait use bounded cursor-based inputs and outputs; wait defaults to the
current launch cursor and CLI timeout. Recorded environments never appear in
MCP responses.

Only `start` and `up` may create or replace a daemon. Without a daemon, `list`
reports resolved definitions as stopped; `status`, `logs`, `wait`, and `restart`
preserve unavailable-daemon errors, while `stop` and `down` succeed with
nothing running. A detached CLI `hum run transient --detach -- ...` creates
the `ad_hoc` record that MCP can list and control. Daemon shutdown or
replacement loses retained ad hoc definitions; an evicted ad hoc record cannot
be reconstructed and restart returns not found.

The MCP package receives a protocol-shaped backend/client. CLI wiring injects
the existing daemon client and automatic-start/version-replacement helper, so
the adapter constructs no `internal/app` services, process supervisors, or
output stores in-process and never creates a second supervisor. MCP has no
`run`, `serve`, or `shutdown` tool and no HTTP transport, authentication, or
remote access.

For an explicit manifest definition that needs environment activation, commit a
runner or wrapper and put it in the definition's exact argv:

```yaml
processes:
  web:
    argv: [./tools/run-with-project-env, bun, run, dev]
```

The runner owns activation deterministically. CLI and MCP use the same argv;
they do not activate mise, nvm, direnv, or other shell hooks, and the manifest
has no environment literals or files.

## Canonical strict manifest

A project may commit exactly one YAML document named `hum.yaml` at its nearest
Git project root. If this file is present, it is authoritative: a valid
version-1 document supplies exactly its declarations, including an empty
`processes` mapping, while an invalid document is a resolution error. Neither
case falls back to discovery. Only when `hum.yaml` is absent does zero-config
resolution apply. Version 1 has a top-level `version: 1` and a `processes`
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
`hum init` generates only this strict version-1 shape and uses exclusive
creation, so it never overwrites an existing `hum.yaml`, including a dangling
symlink or other non-regular path. For one candidate, the file contains that
candidate under its discovered name with its exact argv, a source comment, and
a commented `ready` example with `match` and `timeout`. For no or ambiguous
candidates, the file is a strict-parser-valid commented template with exactly
one example entry, a reason no definition was generated, and every detected
source plus exact argv. Init never executes candidate bodies, starts a daemon,
or infers readiness, cwd, or multiple processes.

## Bounded zero-config resolution

When `hum.yaml` is absent, resolution inspects every supported root-level
convention without launching a development process and collects all qualifying
candidates: resolution succeeds only with exactly one candidate. No candidates
return a typed, actionable `NoCandidateError` (wrapping `ErrNoCandidate`) that
names `hum.yaml`, the supported conventions, and `hum init` as draft guidance;
multiple candidates return an `AmbiguityError` (wrapping `ErrAmbiguous`) listing
every qualifying source and directing the user to `hum init` for a commented
template. A malformed root file produces a `ConfigurationError` (wrapping
`ErrConfiguration`), and malformed native machine-readable output or a failed
required introspection produces an `IntrospectionError` (wrapping
`ErrIntrospection`), not an absent source or a reason to fall through. These
errors are `errors.As`-compatible. A command-backed runner whose executable is
unavailable on `PATH` is skipped. Init preserves these strict discovery errors
and does not write a successful file when they occur.

Every candidate is normalized to the single name `dev`, an absolute cwd equal
to the project root, no readiness expression, and one of these exact argv
arrays:

| Source | Qualifying declaration | Exact argv |
| --- | --- | --- |
| `mise` | Exact local Mise task `dev` from `mise tasks --local --json` | `[mise, run, dev]` |
| `task` | Exact Task task `dev` from `task --dir <root> --list-all --json` | `[task, dev]` |
| `just` | Exact public Just recipe `dev` from a JSON dump of root `justfile`, `Justfile`, or `.justfile` | `[just, dev]` |
| `make` | Literal, non-pattern `dev` target in root `Makefile`, `makefile`, or `GNUmakefile`, parsed conservatively | `[make, dev]` |
| `package_json` | Exact root `package.json` `scripts.dev` entry | `[bun, run, dev]`, `[pnpm, run, dev]`, `[yarn, run, dev]`, or `[npm, run, dev]` |
| `deno_json` | Exact root `deno.json` or `deno.jsonc` `tasks.dev` entry | `[deno, task, dev]` |
| `composer_json` | Exact root `composer.json` `scripts.dev` entry | `[composer, run-script, dev]` |
| `bin_dev` | Executable root `bin/dev` | `[./bin/dev]` |
| `mix` | Root `mix.exs` whose Mix introspection confirms `phx.server` | `[mix, phx.server]` |

For `package_json`, a present `packageManager` value must name `bun`, `pnpm`,
`yarn`, or `npm` (an optional `@...` suffix is ignored for runner selection)
and overrides lockfile detection; unsupported or non-string values are typed
configuration errors. Without that field, the root lockfile family is selected
from Bun (`bun.lock` or `bun.lockb`), pnpm (`pnpm-lock.yaml`), Yarn
(`yarn.lock`), or npm (`package-lock.json` or `npm-shrinkwrap.json`). Exactly
one family may be present; multiple files within one family are fine, while
conflicting families are a configuration error. With no lockfile family, npm is
the default, and the selected runner is the first argv element. Resolution
never parses or executes package, Composer, Deno, Just, Make, or Mix task
bodies and never probes by starting likely commands.

The resolved definition powers `up`, `start dev`, `run dev` without argv,
`restart dev`, and project-aware `list` and `status`. Launch output and list
or status output echo the source and argv, for example
`started dev (package_json: bun run dev)`. A discovered launch succeeds
immediately after spawn as `running_unverified` under the default wait and is
never reported ready; readiness, ports, and dependencies are not inferred.
For strict definition-required `up`, `start`, and argv-free `run`, resolution
happens before daemon startup; ambiguity, malformed discovery input, and
no-candidate errors therefore do not start a daemon. Explicit-argv ad-hoc
`run NAME -- ...` treats `NoCandidate` as no local definition and may start the
daemon, but still propagates ambiguity, malformed, and introspection errors.

## Start and readiness

`start` is an idempotent ensure-running operation. A stopped or never-started
manifest entry launches detached with the requesting client's working directory
and full environment. An already-running manifest incarnation is left in place
and returns `already_running`; concurrent ensures create at most one child. A
discovered `dev` uses its resolved root cwd and exact argv. `up` applies the
same operation to every declaration in lexical order, waits for readiness
concurrently, and leaves successful processes running even if another
declaration fails. `--no-wait` returns after spawn.

A manifest launch records its readiness expression, launch cursor, and the
first matching output cursor for that incarnation even when no client is
waiting. A configured process is `starting` until its expression matches and
then `ready`; readiness is retained after bounded output evicts the matching
line. A relaunch resets readiness at the new launch cursor, while the output
store and monotonically increasing cursor sequence continue. Old output or old
satisfied state cannot satisfy a new incarnation. A declaration without
`ready`, including every discovered definition, succeeds as
`running_unverified`; discovered definitions are never reported ready.

The CLI timeout overrides the declaration timeout. Waiting first checks the
recorded state, then subscribes from the current launch cursor, so an
already-running process that matched earlier still succeeds immediately.
`status` and `list` expose readiness without waiting: running manifest records
show `starting`, `ready` with `ready_cursor`, or `running_unverified`; running
discovered records show `running_unverified` only. Exited records and ad hoc
records omit readiness. `logs` and explicit `wait` operate on the same current
record and bounded cursor stream; a requested match never uses an earlier
incarnation.

## Project-aware list, status, and errors

Without a daemon, `list` resolves the local project and reports every resolved
definition as stopped with its source and `argv`; it does not create runtime
state. With a daemon, it merges resolved definitions and runtime records.
Manifest records carry `source: manifest`; discovered records carry their
stable discovery source; raw `run` records carry `source: ad_hoc`. Every JSON
process snapshot includes `argv` and, when present, readiness fields.

When a resolved name has no available daemon, `status`, `logs`, `wait`, and
`restart` say:

```text
Nothing is running in this project. Start it with hum start <name>.
```

Undefined names retain the ad hoc guidance to use `hum run <name> -- <command>`.
When no local candidate exists, `list`, `status`, `logs`, `wait`, and `restart`
treat `NoCandidate` as no local definition and retain their daemon-control
semantics: they query or control an existing daemon; with no daemon, `list`
reports no resolved entries and the named commands return their undefined-name
or unavailable-daemon guidance without starting one. Ambiguity, malformed
discovery input, and introspection errors still propagate before daemon control.
A failed `up` still reports one result for every declaration, and successful
children remain supervised.

## Process and output model

The daemon starts the exact argv array directly. It never parses shell text or
uses a shell as an implicit command interpreter. Each child has its own Unix
process group and stdin attached to `/dev/null`; `stop` and `down` send SIGTERM
to the group, wait the bounded grace period, and then send SIGKILL if necessary.
Clients and log followers may disconnect without stopping the managed process.

Each process has one ordered output sequence shared by stdout and stderr. A
line entry carries its stream, timestamp, raw text, and cursor. Cursors increase
monotonically for the lifetime of the process record, including across
restarts. Retention is bounded by bytes; stale reads return explicit eviction
metadata rather than unbounded history. `logs --follow` starts with the same
bounded retained read and then delivers cursor-based events without signaling
the process when the follower is cancelled.

There is one daemon per runtime directory, using a private Unix socket at
`hum.sock`.

`serve --daemon`, `run`, `start`, and `up` use an idempotent startup lock and
readiness handshake. For `start`, `up`, and argv-free `run`, strict definition
resolution happens before automatic daemon startup; those resolution errors
never start a daemon. Explicit-argv ad-hoc `run NAME -- ...` may start after
`NoCandidate` is treated as no local definition, while malformed, ambiguous, or
introspection errors still abort before startup. Read-only commands do not start
an empty daemon. `stop` and `down` preserve daemon lifetime and only stop their
selected processes. A foreground daemon or `shutdown --stop-processes` stops its
managed groups; ordinary shutdown refuses while any process remains active.

## Working directory and deterministic environments

The requesting client supplies cwd and environment at launch. A manifest cwd
sets only the child's working directory; a discovered definition always sets
the child's cwd to the absolute project root. Neither form activates shell
hooks from mise, nvm, direnv, or another interactive shell. `restart` resolves
the current definition's argv/cwd/readiness and uses the requesting client's
current environment, so edits to a committed definition or changes in the
discovered root are respected.

For deterministic MCP execution of an explicit manifest definition, a project
that needs environment activation commits a tool runner or wrapper and puts it
in manifest argv:

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

Zero-config discovery is intentionally bounded. It does not include
language-level guesses such as bare `go run` or `cargo run`, framework launch
commands other than a confirmed `mix phx.server`, Docker Compose inference,
scanning workspace packages or nested manifests, combining several inferred
processes, inferring ports, readiness, or dependencies, or executing candidate
commands to see which succeeds. It also does not include a shell-only skill,
arbitrary-command MCP tools, MCP HTTP transport, authentication, remote access,
automatic crash restart/backoff, or environment literals/files.
`down` is part of the current CLI surface, not a discovery feature; it reuses the
daemon boundary and exact argv model described above rather than defining another
supervisor.

The foundation also intentionally has no PTY, arbitrary interactive input,
remote transport, authentication, web UI, persistent process history, plugin
system, or operating-system service installation. The private runtime directory
contains the socket, PID/startup state, and bounded daemon diagnostics.
