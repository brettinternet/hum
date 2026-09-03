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

`hum.yaml` is the canonical project definition. `start` ensures the requested
manifest names are running; `up` ensures every declaration is running in
lexical name order. Both commands start a detached daemon when needed and wait
for declared readiness by default. `--no-wait` returns after spawn, and
`--timeout` overrides the manifest or 30-second readiness timeout.

Start and up JSON are newline-delimited: one object is emitted for each name
with `name`, `outcome`, `source`, and `argv`, plus process identity,
`readiness`, `ready_cursor`, or `error` when applicable. Successful ready
launches report `started` or `already_running`; declarations without a
readiness expression report `running_unverified`. Up attempts every
declaration even when one fails. Its aggregate exit status is 1 for a request
error, 3 for an exit before readiness, 2 for a timeout, and 0 otherwise.

`run NAME` without a command uses the manifest definition when `NAME` is
declared and retains attached-run streaming semantics. A raw
`run NAME -- COMMAND [ARGS...]` is available for ad hoc names only; a declared
name cannot be occupied by a conflicting raw run. Ad hoc runs are labelled
`ad_hoc` in JSON list output. Human-readable output remains the default, while
`--json` is intended for scripts and agents. Attached `run --json` still
streams raw child stdout and stderr; `logs --json --follow` is NDJSON.

## Canonical `hum.yaml` v1

A project definition, when present, is exactly one file named `hum.yaml` at
its nearest Git project root. Version 1 is a strict single YAML document:

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
# Declared entries include source=manifest and argv. Running ad hoc entries,
# if any, are merged and labelled source=ad_hoc.

"$HUM" logs web --tail 20
"$HUM" wait web --match "Listening|Local:"
"$HUM" restart web --json
# Restart uses the current manifest argv/cwd/readiness and the requesting
# client's environment. The output cursor sequence remains monotonic.

"$HUM" stop web worker
"$HUM" shutdown --stop-processes
```

Without a daemon, `list --json` reads the local manifest and reports each
declaration as stopped with `source=manifest` and its argv; it does not create
runtime state. With a daemon, it merges those declarations with ad hoc records.
`status`, `logs`, `wait`, and `restart` use the resolved manifest definition
when one exists. If no daemon is available for a resolved name they report
`Nothing is running in this project. Start it with hum start <name>.`; undefined
names retain the ad hoc `hum run` guidance.

Readiness is tracked for every manifest launch, even when no client is waiting.
The first matching cursor belongs to the current incarnation and remains
satisfied after bounded output evicts the matching line. A relaunch resets
readiness and starts a new incarnation cursor. Running manifest records expose
`readiness=starting`, `ready` with `ready_cursor`, or
`running_unverified`; exited records and ad hoc records omit readiness.

A matching `wait` is still useful for an explicit later condition. It searches
retained output from the current launch cursor, returns `matched`, `exited`, or
`timed_out` with a cursor, and never uses an old incarnation's output to satisfy
a new launch.

## Working directory and environment

The daemon launches the exact argv supplied by the client or manifest and
never evaluates shell text. A manifest `cwd` controls the child working
directory only; changing cwd does **not** activate shell hooks from mise, nvm,
direnv, or another interactive shell. The requesting client's full environment
is used for a launch and for a manifest restart; environment values are never
returned by list or status.

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
`hum.sock`. `run`, `start`, and `up` automatically start a detached daemon when
none is available. Read-only commands do not start an empty daemon. Process
names are scoped to the nearest Git root; without a Git marker, the current
working directory is the project root. A duplicate name in one project is
rejected, while the same name may be used by separate projects.

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

The manifest workflow intentionally does not include zero-configuration
discovery, `down`, manifest generation (`init`), a shell-only skill, an MCP
server, dependency ordering, ports or HTTP checks, automatic crash restart or
backoff, environment literals/files, or arbitrary-command MCP tools. Those are
separate product decisions. Use `hum stop NAME...` for named process groups and
`hum shutdown` for daemon lifetime control.
