# hum

hum is a local development process supervisor for humans and coding agents. It
owns named process groups and bounded output through one private local daemon.
The daemon keeps processes alive when a client disconnects, and every launch
uses an exact argv sequence rather than reconstructed shell text. macOS and
Linux are supported; Windows is not supported by this implementation.

## Install and build

Install [mise](https://mise.jdx.dev/) and make it available on `PATH`. From a
fresh clone, run:

```sh
mise install
mise exec task -- task init
mise exec task -- task cli:build
```

The build writes `bin/hum` and reports `dev` and `unknown` build metadata:

```sh
./bin/hum --help
./bin/hum --version
```

Release or locally labelled builds can inject the variables in
`cmd/hum/main.go` with Go linker flags:

```sh
mkdir -p bin
mise exec go -- go build \
  -ldflags "-X main.buildVersion=1.2.3 -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o bin/hum ./cmd/hum
```

## Command surface

```text
hum init [--json]
hum serve [--daemon]
hum start NAME... [--no-wait] [--timeout DURATION] [--json]
hum up [--no-wait] [--timeout DURATION] [--json]
hum down [--json]
hum run NAME [--detach] [--json] [-- COMMAND [ARGS...]]
hum list [--all] [--json]
hum status NAME [--json]
hum logs NAME [--stream stdout|stderr|both] [--tail N] [--after-cursor N]
           [--limit-bytes N] [--match REGEX] [--follow] [--json]
hum wait NAME [--after-cursor N] [--match REGEX] [--timeout DURATION] [--json]
hum restart NAME... [--json]
hum stop NAME... [--json]
hum shutdown [--stop-processes] [--json]
hum mcp
hum skill
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

Project definitions are resolved at the nearest Git project root. A present
`hum.yaml` is authoritative: valid YAML supplies exactly its declarations,
including zero declarations for an empty `processes` mapping; invalid YAML is a
resolution error and never falls back to discovery. Only an absent `hum.yaml`
enables bounded zero-config discovery, which must find exactly one `dev`
candidate. `start` ensures the requested resolved names are running; `up`
ensures every resolved definition is running in lexical name order. Both
commands start a detached daemon when needed and wait for declared readiness
by default. `--no-wait` returns after spawn, and `--timeout` overrides the
manifest or 30-second readiness timeout.
`hum init` resolves the same nearest project root and discovery candidates as
`up`, but it never launches a candidate or starts a daemon. With exactly one
candidate it writes `hum.yaml` with outcome `generated`; with no candidate or
ambiguous candidates it writes a strict-parser-valid commented template with
outcome `template`. The template has exactly one commented example entry,
explains why no definition was generated, and lists every detected source and
exact argv. An existing `hum.yaml` is never overwritten and is reported as an
error with exit status 1. Human output includes `path:`, `outcome:`, and
`next_command: hum up`; `--json` emits `path`, `outcome`, `next_command`, and
`candidates` fields. Init never infers readiness, cwd, or multiple processes.

Start and up JSON are newline-delimited: one object is emitted for each name
with `name`, `outcome`, `source`, and `argv`, plus process identity,
`readiness`, `ready_cursor`, or `error` when applicable. Successful ready
launches report `started` or `already_running`; a definition without a
readiness expression, including every discovered definition, reports
`running_unverified` and is never reported ready. Up attempts every resolved
definition even when one fails. Its aggregate exit status is 1 for a request
error, 3 for an exit before readiness, 2 for a timeout, and 0 otherwise.
`down` is the project-scoped inverse of `up`: it lists active resolved and ad hoc
records in the current project, includes declared manifest names with no runtime
record as `not_running` when a daemon is available, and applies the existing
named `stop` operation concurrently to every running name. It never starts a
daemon, shuts one down, touches another project, or removes runtime records.
Results are name-sorted and contain exactly one `stopped`, `not_running`, or
`error` result per name; errors include a message. Human output is one
`<name> stopped`, `<name> not running`, or `<name> error: <message>` line per
result, while `--json` emits the existing newline-delimited `stopResult` objects.
A real stop error makes exit status 1; otherwise exit status is 0. Without a
daemon, `down` succeeds without creating runtime state and human output is exactly
`Nothing is running in this project.` When a daemon has no runtime records and no
declarations, it returns the same message; declarations without runtime records
are emitted as `not_running`.

`run NAME` without a command uses the current resolved project definition when
`NAME` resolves (including discovered `dev`) and retains attached-run streaming
semantics; it requires strict resolution, so a no-candidate error occurs before
daemon startup. A raw `run NAME -- COMMAND [ARGS...]` is available only for names
with no resolved definition. When discovery has no candidate, this explicit-argv
form treats `NoCandidate` as no local definition and may start the daemon for the
ad hoc run; malformed, ambiguous, or introspection failures still abort before
startup. A resolved name cannot be occupied by a conflicting raw run. Ad hoc
runs are labelled `ad_hoc` in JSON list output.
Human-readable output remains the default, while `--json` is intended for
scripts and agents. Attached `run --json` still streams raw child stdout and
stderr; `logs --json --follow` is NDJSON.

## MCP for coding agents

`hum mcp` is an MCP server on stdio: it reads MCP JSON-RPC from stdin and
writes responses to stdout. Register it once with an agent by pointing the
agent directly at the `hum` executable; an agent does not need a shell skill
to use it. Use an absolute executable path in these examples:

Claude Code:

```sh
claude mcp add --transport stdio hum -- /absolute/path/to/hum mcp
```

Codex CLI:

```sh
codex mcp add hum -- /absolute/path/to/hum mcp
```

Cursor (or another client using an `mcpServers` JSON configuration):

```json
{
  "mcpServers": {
    "hum": {
      "command": "/absolute/path/to/hum",
      "args": ["mcp"]
    }
  }
}
```

These are one-time registrations. Register the executable directly, not as
`sh -c ...`; do not bake a project path into the registration. Every tool call
requires `project_root`, an absolute existing directory. Pass the nearest Git
project root, or the requested directory when no Git marker exists, using the
same root rule as the CLI.

The server exposes exactly nine tools:
`start`, `up`, `down`, `list`, `status`, `logs`, `wait`, `restart`, and `stop`.
`start` and `up` accept only names from the current explicit `hum.yaml` or
zero-config resolved definitions. They wait for readiness by default and
accept `no_wait` and `timeout`; launch results retain CLI states, source and
`argv` metadata, per-incarnation readiness and cursors, collision errors, and
`up` aggregate semantics. A definition without readiness, including discovered
`dev`, returns `running_unverified` and is never reported `ready`.

`list` merges resolved definitions with every runtime record in the project.
`status`, `logs`, `wait`, `restart`, and `stop` target an existing record in
that project, whether resolved or `ad_hoc`; only `start` and `up` are
definition-only. `restart` uses the current resolved definition and the MCP
server's current environment, or relaunches a retained ad hoc record with its
recorded exact `argv`, cwd, and environment when no definition exists. `down`
stops every running record and returns the CLI per-name results. `logs` and
`wait` expose bounded cursor-based inputs and outputs; `wait` defaults to the
current launch cursor and the CLI timeout. No MCP response exposes a recorded
environment.

Only `start` and `up` may create or replace a daemon. Without a daemon,
`list` reports resolved definitions as stopped; `status`, `logs`, `wait`, and
`restart` preserve the CLI unavailable-daemon errors, while `stop` and `down`
succeed with nothing running. A detached CLI command such as
`hum run transient --detach -- ./tools/transient` is handed off through its
runtime record and appears to MCP as `source: ad_hoc`. A daemon shutdown or
replacement loses retained ad hoc definitions; an evicted ad hoc record cannot
be reconstructed and its restart returns not found.

MCP has no `run`, `serve`, or `shutdown` tool, and provides no HTTP transport,
authentication, or remote access. It is an adapter over hum's existing daemon
client and startup/version-replacement helper, not an in-process supervisor or
output store; it never creates a second supervisor.

For an explicit manifest definition that needs environment activation, commit a
runner or wrapper and put it in the definition's exact `argv`:

```yaml
processes:
  web:
    argv: [./tools/run-with-project-env, bun, run, dev]
```

The runner owns activation deterministically. CLI and MCP then use the same
argv without shell hooks, environment literals, or environment files.

### Shell-only skill fallback

MCP via `hum mcp` is the primary integration for coding agents. If MCP is
unavailable, `hum skill` prints the versioned embedded Agent Skills file
byte-for-byte for installation into an agent's normal skill location; it does
not prescribe an agent-specific installer or configure an agent automatically.

## Canonical `hum.yaml` v1

Resolution is presence-sensitive. If `hum.yaml` exists at the nearest Git
project root, it is authoritative: a valid file supplies exactly its
declarations, including an empty `processes` mapping, and an invalid file is a
resolution error. Neither case falls back to discovery. When the file is
absent, hum uses the bounded zero-config rules described below.

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

`processes` is keyed by safe process names. Each `argv` is a non-empty sequence
of non-empty strings. `cwd`, when present, is root-relative; hum normalizes it
to an absolute path under the project root and rejects lexical or symlink
escapes and missing directories. `ready.match` is a regular expression and
`ready.timeout` is a positive duration; the default is 30 seconds.

The parser rejects unknown or duplicate keys, aliases and merge keys, multiple
documents, unsupported versions, shell-text commands, invalid names, invalid
regular expressions and durations, empty or non-string argv values, absolute or
escaping cwd values, and alternate files such as `hum.yml`, `hum.json`, or
`hum.toml`. Declaration order in the file does not affect behavior: names and
`up` results are lexical.
`hum init` uses this strict format for its first draft. A single candidate is
written under its discovered name with the exact discovery argv, a source
comment, and a commented `ready` example containing `match` and `timeout`.
No-candidate and ambiguous results instead contain one commented example entry
plus the reason and every detected candidate's source and argv. Comments are
not executable definitions: the generated and template files remain valid
version-1 documents, and init never infers readiness, cwd, or multiple
processes. Init refuses any existing `hum.yaml` (including a dangling symlink
or other non-regular path) without changing it.

The manifest contains process definitions only. Runtime settings, dependency
ordering, ports and HTTP health checks, automatic crash restart/backoff, and
environment literals or environment files are deliberately not manifest
features.

## Zero-config `dev` resolution

When `hum.yaml` is absent, hum inspects every supported root-level convention
without launching a development process and collects all qualifying candidates.
No candidates produce a typed, actionable `NoCandidateError` (wrapping
`ErrNoCandidate`) that names `hum.yaml`, every supported convention, and
`hum init` as the way to create a draft; multiple candidates produce an
`AmbiguityError` (wrapping `ErrAmbiguous`) that lists every qualifying source
and directs the user to `hum init` for a commented template. A malformed root
file produces a `ConfigurationError` (wrapping `ErrConfiguration`), and
malformed native machine-readable output or a failed required introspection
produces an `IntrospectionError` (wrapping `ErrIntrospection`), rather than
being ignored or falling through. These errors are `errors.As`-compatible.
If a command-backed runner is unavailable on `PATH`, that detector is skipped.
`hum init` uses these same strict discovery errors and preserves them without
writing a file when configuration or introspection fails.

Each candidate is normalized to the one name `dev`, an absolute cwd equal to
the project root, no readiness expression, and the exact argv shown here:

| Source | Qualifying declaration | Exact argv |
| --- | --- | --- |
| `mise` | Local Mise task named exactly `dev`, reported by `mise tasks --local --json` | `[mise, run, dev]` |
| `task` | Task task named exactly `dev`, reported by `task --dir <root> --list-all --json` | `[task, dev]` |
| `just` | Public Just recipe named exactly `dev`, reported by a JSON dump of root `justfile`, `Justfile`, or `.justfile` | `[just, dev]` |
| `make` | Literal, non-pattern `dev` target in root `Makefile`, `makefile`, or `GNUmakefile`, parsed conservatively | `[make, dev]` |
| `package_json` | Exact root `package.json` `scripts.dev` entry | `[bun, run, dev]`, `[pnpm, run, dev]`, `[yarn, run, dev]`, or `[npm, run, dev]` |
| `deno_json` | Exact root `deno.json` or `deno.jsonc` `tasks.dev` entry | `[deno, task, dev]` |
| `composer_json` | Exact root `composer.json` `scripts.dev` entry | `[composer, run-script, dev]` |
| `bin_dev` | Executable root `bin/dev` | `[./bin/dev]` |
| `mix` | Root `mix.exs` for which Mix introspection confirms the `phx.server` task is available | `[mix, phx.server]` |

For `package_json`, a present `packageManager` value must name `bun`, `pnpm`,
`yarn`, or `npm` (an optional `@...` suffix is ignored for runner selection)
and overrides lockfile detection; unsupported or non-string values are typed
configuration errors. Without that field, the root lockfile family is selected
from Bun (`bun.lock` or `bun.lockb`), pnpm (`pnpm-lock.yaml`), Yarn
(`yarn.lock`), or npm (`package-lock.json` or `npm-shrinkwrap.json`). Exactly
one family may be present; multiple files within one family are fine, while
conflicting families are a configuration error. With no lockfile family, npm is
the default, and the selected runner is the first argv element. Hum never
parses or executes package, Composer, Deno, Just, Make, or Mix task bodies
during resolution and never probes by starting likely commands.

The same resolved `dev` definition powers `up`, `start dev`, `run dev` without
argv, `restart dev`, and project-aware `list` and `status`. Launch output
includes source and argv, for example
`started dev (package_json: bun run dev)`, and `list`/`status` expose the same metadata.
A discovered launch succeeds immediately after spawn as `running_unverified`
under the default wait; it is never upgraded to `ready`, and no readiness,
port, or dependency is inferred. For strict definition-required `up`, `start`,
and argv-free `run`, resolution runs before daemon startup; ambiguity, malformed
discovery input, and no-candidate errors therefore never start a daemon.
Explicit-argv ad-hoc `run NAME -- ...` treats `NoCandidate` as no local
definition and may start the daemon, but still propagates ambiguity, malformed,
and introspection errors.

## Initializing a manifest

Use `hum init` when a project needs an explicit manifest instead of relying on
zero-config discovery:

```sh
cd /path/to/project
hum init
# path: /path/to/project/hum.yaml
# outcome: generated
# next_command: hum up
```

With exactly one supported `dev` candidate, init writes a version-1 definition
with that candidate's exact name and argv, a comment naming its source, and a
commented readiness example (`match` and `timeout`). With no candidate or more
than one candidate, init writes a strict-parser-valid commented template with
exactly one example entry, a reason no definition was generated, and every
detected source plus exact argv. It does not run candidate bodies, infer
readiness or cwd, combine multiple processes, or start a daemon.

`hum init --json` emits one object with `path`, `outcome`, `next_command`, and
`candidates`; `next_command` is always `hum up`. Outcomes are `generated` or
`template` on successful writes. If `hum.yaml` already exists at the nearest
root, init refuses to overwrite it (including dangling symlinks and other
non-regular paths), exits 1, and reports the existing path. Discovery,
configuration, introspection, root, and write errors also exit 1 without a
successful file.

## Manifest workflow

The following is a copy/pasteable macOS/Linux session. Values shown as `<pid>`
and `<cursor>` are intentionally variable:

```sh
set -eu
runtime_dir="$(mktemp -d)"
project_dir="$(mktemp -d)"
export HUM_RUNTIME_DIR="$runtime_dir"
HUM="$(pwd)/bin/hum"
trap 'rm -rf "$runtime_dir" "$project_dir"' EXIT

cat >"$project_dir/hum.yaml" <<'YAML'
version: 1
processes:
  web:
    argv: [bun, run, dev]
    ready:
      match: "Local:"
      timeout: 30s
  worker:
    argv: [./bin/worker]
YAML

cd "$project_dir"
"$HUM" up --json
# NDJSON includes web outcome=started, readiness=ready, and
# worker outcome=running_unverified; each object includes source=manifest and argv.

"$HUM" status web --json
# The process object reports readiness=ready and ready_cursor=<cursor> without waiting.

"$HUM" list --json
# Resolved entries include source and argv. Running ad hoc entries, if any, are
# merged and labelled source=ad_hoc.

"$HUM" logs web --tail 20
"$HUM" wait web --match "Listening|Local:"
"$HUM" restart web --json
# Restart uses the current resolved argv/cwd/readiness and the requesting
# client's environment. The output cursor sequence remains monotonic.

"$HUM" stop web worker
"$HUM" down --json
# Stops every active process in this project and leaves the daemon running.
"$HUM" shutdown --stop-processes
```

Without a daemon, `list --json` resolves the local project and reports every
resolved definition as stopped with its source and `argv`; it does not create
runtime state. With a daemon, it merges resolved definitions with ad hoc
records. `status`, `logs`, `wait`, and `restart` use the current resolved
definition when one exists, including a discovered `dev`. If no daemon is
available for a resolved name they report `Nothing is running in this project.
Start it with hum start <name>.`; undefined names retain the ad hoc `hum run`
guidance. When no local candidate exists, `list`, `status`, `logs`, `wait`, and
`restart` treat `NoCandidate` as no local definition and retain their daemon
control semantics: an existing daemon is still queried or controlled, while
these commands do not start one merely for inspection or control. They still
propagate ambiguity, malformed-input, and introspection errors before daemon
control.

Readiness is tracked for every manifest launch, even when no client is waiting.
The first matching cursor belongs to the current incarnation and remains
satisfied after bounded output evicts the matching line. A relaunch resets
readiness and starts a new incarnation cursor. Running manifest records expose
`readiness=starting`, `ready` with `ready_cursor`, or `running_unverified`;
running discovered records expose only `running_unverified`; discovered
definitions are never reported ready. Exited records and ad hoc records omit
readiness.

A matching `wait` is still useful for an explicit later condition. It searches
retained output from the current launch cursor, returns `matched`, `exited`, or
`timed_out` with a cursor, and never uses an old incarnation's output to satisfy
a new launch.

## Working directory and environment

The daemon launches the exact argv supplied by the client or resolved
definition and never evaluates shell text. A manifest `cwd` controls the child
working directory only; a discovered definition always uses the absolute
project root as its cwd. Changing cwd does **not** activate shell hooks from
mise, nvm, direnv, or another interactive shell. The requesting client's full
environment is used for a launch and for a manifest or discovered restart;
environment values are never returned by list or status.

For deterministic MCP execution of an explicit manifest definition, commit a
tool runner or wrapper in the repository and put that runner in the manifest
argv. For example:

```yaml
processes:
  web:
    argv: [./tools/run-with-project-env, bun, run, dev]
```

The committed runner owns any required environment activation explicitly.
This keeps MCP and CLI launches identical without relying on the daemon's
shell, startup directory, or mutable interactive hooks. Do not replace this
with a shell command string or an `env` literal in the manifest.

## Daemon, projects, and output

There is one daemon per runtime directory, using a private Unix socket at
`hum.sock`. `start`, `up`, and argv-free `run` automatically start a detached daemon when
needed, but only after strict project-definition resolution succeeds; their
no-candidate, ambiguity, malformed-input, or introspection errors occur before
daemon startup. An explicit-argv ad-hoc `run NAME -- ...` may start the daemon
after `NoCandidate` is treated as no local definition, while ambiguity, malformed,
or introspection errors still abort before startup. Read-only commands do not
start an empty daemon.
Process names are scoped to the nearest Git root; without a Git marker, the
current working directory is the project root. A duplicate name in one project
is rejected, while the same name may be used by separate projects.

Each child has its own Unix process group and stdin connected to `/dev/null`.
`stop` and `down` send SIGTERM to the group, wait the bounded graceful-stop
period, and send SIGKILL if needed. Clients and log followers may disconnect
without orphaning or stopping a managed process. `shutdown` refuses while
processes are active unless `--stop-processes` is supplied.

Captured stdout and stderr share one monotonically increasing cursor sequence
per process record. Retention is bounded per process; stale cursors return
truncation metadata instead of unbounded history. `logs --follow` first returns
bounded retained output and then cursor-based events. It never signals the
managed process when the follower is cancelled.

Runtime defaults are intentionally conservative:

| Setting | Default or bound |
| --- | --- |
| graceful stop (`--stop-grace` / `HUM_STOP_GRACE`) | `10s` |
| retained output (`--output-bytes` / `HUM_OUTPUT_BYTES`) | `4 MiB` per process; minimum `64 KiB` |
| completed records (`--completed-records` / `HUM_COMPLETED_RECORDS`) | `20`; valid range `1`–`1000` |
| one `logs` read | at most `100` entries and `16 KiB` unless overridden |
| one captured line | flushed/chunked at `64 KiB` |
| `wait --timeout` | `30s` unless explicitly set to at least `1ms` |

The runtime directory defaults to `--runtime-dir`, then `HUM_RUNTIME_DIR`, then
`$XDG_RUNTIME_DIR/hum`, then a per-user temporary directory. It is private and
contains the socket, lifecycle files, bounded daemon diagnostics, and startup
lock. Daemon state is in memory: replacing or restarting the daemon loses
retained output and process history.

## Current non-goals

Zero-config discovery is intentionally bounded. It does not include
language-level guesses such as bare `go run` or `cargo run`, framework launch
commands other than a confirmed `mix phx.server`, Docker Compose inference,
scanning workspace packages or nested manifests, combining several inferred
processes, inferring ports, readiness, or dependencies, or executing candidate
commands to see which succeeds. It also does not include arbitrary-command MCP
tools, MCP HTTP transport, authentication, or remote access, automatic crash
restart/backoff, or environment literals/files.
`hum down` is the project-scoped inverse of `hum up`: it stops every active
resolved or ad hoc process in the current project, preserves the daemon and
runtime records, and leaves other projects untouched. Use `hum stop NAME...` for
selected named groups. Use `hum shutdown` for daemon lifetime control; ordinary
shutdown refuses while processes are active, while `hum shutdown --stop-processes`
stops all managed groups before the daemon exits.
