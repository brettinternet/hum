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
hum serve [--daemon]
hum start NAME... [--no-wait] [--timeout DURATION] [--json]
hum up [--no-wait] [--timeout DURATION] [--json]
hum run NAME [--detach] [--json] [-- COMMAND [ARGS...]]
hum list [--all] [--json]
hum status NAME [--json]
hum logs NAME [--stream stdout|stderr|both] [--tail N] [--after-cursor N]
           [--limit-bytes N] [--match REGEX] [--follow] [--json]
hum wait NAME [--after-cursor N] [--match REGEX] [--timeout DURATION] [--json]
hum restart NAME... [--json]
hum stop NAME... [--json]
hum shutdown [--stop-processes] [--json]
```

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

Start and up JSON are newline-delimited: one object is emitted for each name
with `name`, `outcome`, `source`, and `argv`, plus process identity,
`readiness`, `ready_cursor`, or `error` when applicable. Successful ready
launches report `started` or `already_running`; a definition without a
readiness expression, including every discovered definition, reports
`running_unverified` and is never reported ready. Up attempts every resolved
definition even when one fails. Its aggregate exit status is 1 for a request
error, 3 for an exit before readiness, 2 for a timeout, and 0 otherwise.

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

The manifest contains process definitions only. Runtime settings, dependency
ordering, ports and HTTP health checks, automatic crash restart/backoff, and
environment literals or environment files are deliberately not manifest
features.

## Zero-config `dev` resolution

When `hum.yaml` is absent, hum inspects every supported root-level convention
without launching a development process and collects all qualifying candidates.
There is no precedence among sources: resolution succeeds only with exactly one
candidate. No candidates produce a typed, actionable `NoCandidateError` (wrapping
`ErrNoCandidate`) that names `hum.yaml` and every supported convention;
multiple candidates produce an `AmbiguityError` (wrapping `ErrAmbiguous`) that
lists every qualifying source. A malformed root file produces a
`ConfigurationError` (wrapping `ErrConfiguration`), and malformed native
machine-readable output or a failed required introspection produces an
`IntrospectionError` (wrapping `ErrIntrospection`), rather than being ignored
or falling through. These errors are `errors.As`-compatible. If a
command-backed runner is unavailable on `PATH`, that detector is skipped.

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

For deterministic MCP execution, commit a tool runner or wrapper in the
repository and put that runner in the manifest argv. For example:

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
`stop` sends SIGTERM to the group, waits the bounded grace period, and then
sends SIGKILL if needed. Clients and log followers may disconnect without
orphaning or stopping a managed process. `shutdown` refuses while processes are
active unless `--stop-processes` is supplied.

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
commands to see which succeeds. It also does not include `down`, manifest
generation (`init`), a shell-only skill, an MCP server, automatic crash
restart/backoff, environment literals/files, or arbitrary-command MCP tools.
Use `hum stop NAME...` for named process groups and `hum shutdown` for daemon
lifetime control.
