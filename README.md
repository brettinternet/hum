# hum

hum is a local development process supervisor for humans and coding agents.
The implemented foundation owns named processes and their bounded output through
a private local daemon. It supports macOS and Linux; Windows is not supported
by this foundation.

## Install and build

Install [mise](https://mise.jdx.dev/) and make it available on `PATH`. From a
fresh clone, run these commands at the repository root:

```sh
# `mise install` installs the project-managed Task and Go.
mise install
mise exec task -- task init
mise exec task -- task cli:build
```

`mise exec task -- task init` installs the declared toolchain, downloads Go modules, and installs
the hooks. `mise exec task -- task cli:build` uses the mise-managed Go toolchain and writes
`bin/hum`. The development binary reports `dev` and `unknown` build metadata:

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

These are the current foundation commands:

```text
hum serve [--daemon]
hum run NAME [--detach] [--json] -- COMMAND [ARGS...]
hum list [--all] [--json]
hum status NAME [--json]
hum logs NAME [--stream stdout|stderr|both] [--tail N] [--after-cursor N]
           [--limit-bytes N] [--match REGEX] [--follow] [--json]
hum wait NAME [--after-cursor N] [--match REGEX] [--timeout DURATION] [--json]
hum restart NAME... [--json]
hum stop NAME... [--json]
hum shutdown [--stop-processes] [--json]
```

Human-readable output is the default. `--json` emits stable JSON for
commands that support it. For `run`, stable JSON is available only with
`--detach`; attached `run --json` still streams the child's stdout and stderr
as raw output. JSON output is intended for scripts and agents; `logs --json
--follow` is newline-delimited JSON (NDJSON), with one event per line.

## Lifecycle walkthrough

The following is a copy/pasteable macOS/Linux shell session. It uses a fresh
temporary runtime directory, creates one second project for `list --all`, and
cleans up both directories at the end. Values shown as `<pid>` and `<cursor>`
are intentionally variable.

```sh
set -eu
runtime_dir="$(mktemp -d)"
other_dir="$(mktemp -d)"
export HUM_RUNTIME_DIR="$runtime_dir"
HUM="$(pwd)/bin/hum"
trap 'rm -rf "$runtime_dir" "$other_dir"' EXIT

# A foreground daemon owns the socket until it receives SIGINT.
"$HUM" serve >"$runtime_dir/foreground.log" 2>&1 &
foreground_pid=$!
while [ ! -S "$runtime_dir/hum.sock" ]; do sleep 0.05; done
"$HUM" list >/dev/null
cat "$runtime_dir/foreground.log"
# hum serve: listening on .../hum.sock (PID <pid>)
kill -INT "$foreground_pid"
wait "$foreground_pid"
# The foreground daemon exits and removes its socket, PID, and readiness files.

# --daemon is detached, reports readiness on stderr, and is idempotent.
"$HUM" serve --daemon 2>&1
# hum serve: listening on .../hum.sock (PID <pid>)
"$HUM" serve --daemon 2>&1
# The second invocation reports the existing daemon rather than starting one.

# Attached run streams the child's stdout/stderr and returns its exit status.
"$HUM" run attached -- /bin/sh -c \
  'printf "attached stdout\n"; printf "attached stderr\n" >&2'
# stdout contains: attached stdout
# stderr contains: attached stderr; the command exits 0

# Detached runs return immediately while the daemon keeps the process groups.
"$HUM" run api --detach -- /bin/sh -c \
  'printf "api-one\n"; printf "api-warning\n" >&2; printf "api-two\n"; sleep 30'
"$HUM" run worker --detach -- /bin/sh -c \
  'printf "worker-start\n"; sleep 30'
# Each command prints: started NAME (PID <pid>, cursor <cursor>)

# A process in another directory is visible only to --all.
(cd "$other_dir" && "$HUM" run other --detach -- /bin/sh -c 'sleep 30')

"$HUM" list
# Current-project rows include api and worker in state running, with PIDs.
"$HUM" list --all
# Rows include api/worker and the other project; --all prefixes rows with
# their project roots.

"$HUM" status api
# The human result includes name, project_root, pid, pgid, cwd, argv,
# started_at, state: running, exit_status: null, restart_count: 0, and
# next_cursor: <cursor>.

# A bounded human read writes raw entries to stdout and its cursor trailer to
# stderr. --tail selects final entries; --limit-bytes tightens the byte bound.
"$HUM" logs api --tail 2 --limit-bytes 15
# stdout contains at most two retained entries; stderr ends with:
# next cursor: <cursor> (more available)
# Pass that numeric cursor to --after-cursor on a later read.

# Human follow is read-only. This short process exits on its own so the example
# does not require an interactive Ctrl+C.
"$HUM" run follow-human --detach -- /bin/sh -c \
  'printf "human-first\n"; sleep 1; printf "human-last\n"; exit 0'
"$HUM" logs follow-human --follow --tail 1
# stdout receives retained/followed text; stderr reports next cursor: <cursor>.
# The follower exits when the managed process exits and does not control it.

# JSON follow emits one parseable object per line, including a terminal exit
# event. Output delivery remains bounded by the normal read limits.
"$HUM" run follow-json --detach -- /bin/sh -c \
  'printf "json-first\n"; sleep 0.1; printf "json-last\n"; exit 0'
"$HUM" logs follow-json --follow --json
# stdout is NDJSON with op=event/type=output lines and a final
# op=event/type=exit line; event cursor values vary.

`wait` without --match waits for process exit and uses a default timeout of
`30s`; an explicitly supplied `--timeout` must be at least `1ms`. A matching
line is an immediate successful outcome.
"$HUM" run ready --detach -- /bin/sh -c 'printf "ready\n"; sleep 0.2'
"$HUM" wait ready --match ready
# outcome: matched
# cursor: <cursor>

"$HUM" run exited --detach -- /bin/sh -c 'sleep 0.1; exit 0'
"$HUM" wait exited
# outcome: exited, followed by exit_code: 0; --timeout was omitted, so the
# 30s default applied (the command returned as soon as the process exited).

# Exercise the non-zero wait outcomes without stopping this shell session.
"$HUM" run timeout --detach -- /bin/sh -c 'sleep 2'
set +e
"$HUM" wait timeout --match never --timeout 50ms
wait_timeout_status=$?
"$HUM" wait exited --match never --timeout 1s
wait_exited_status=$?
"$HUM" wait missing
wait_error_status=$?
set -e
printf 'wait statuses: timeout=%s exited-before-match=%s request-error=%s\n' \
  "$wait_timeout_status" "$wait_exited_status" "$wait_error_status"
# timeout=2, exited-before-match=3, request-error=1. Matching output or an
# exit wait without --match returns 0. The human output still identifies the
# timed_out or exited outcome and its cursor.

"$HUM" restart api
# api restarted pid=<pid> restarts=1 launch_cursor=<cursor>; its recorded
# argv, cwd, and environment are relaunched under the same name, and its
# output cursor sequence continues.

# Names are processed in order. Repeating a name is safe and reports the
# second occurrence as not running.
"$HUM" stop api worker api
# api stopped
# worker stopped
# api not running

# `other` is still active, so default shutdown refuses and leaves the daemon
# available. The expected refusal is an error (exit 1) and lists active
# project-root/name pairs.
set +e
"$HUM" shutdown
shutdown_status=$?
set -e
printf 'shutdown status: %s\n' "$shutdown_status"
# shutdown status: 1

"$HUM" shutdown --stop-processes
# hum daemon shut down
# All remaining managed process groups were stopped.
# The socket, PID, and readiness files were removed; daemon.log and the startup
# lock remain until temporary-directory cleanup.
```

`logs --follow` never sends a signal to the managed process. Ctrl+C cancels
only that follower, so another follower or the process itself can continue.
For a follow that needs to be stopped before process exit, press Ctrl+C in the
follower's terminal.

## Daemon, projects, and configuration

`run` is the only current command that automatically starts a detached daemon
when one is unavailable. Both attached and `--detach` runs use that
run-only auto-start. Read and control commands never start an empty daemon.
`serve --daemon` is the explicit, idempotent daemon launcher.

Process names are scoped to the nearest Git root; without a Git marker, the
current working directory is the project root. The same name may therefore be
used by separate projects, while a duplicate name in one project is rejected.

Without a daemon, the current commands deliberately have different outcomes:

| Command | Result |
| --- | --- |
| `list` | exits successfully and prints `Nothing is running.` (`--json` returns an empty `processes` array) |
| `logs NAME`, `status NAME`, `wait NAME`, `restart NAME` | fail with `Nothing is running. Start a process with hum run <name> -- <command>.` |
| `stop NAME...` | exits successfully and prints `Nothing is running.` in human mode; `--json` emits one `not_running` result per supplied name |
| `shutdown` | exits successfully and prints `No hum daemon is running.` in human mode; `--json` returns `{"status":"not_running"}` |

The daemon uses a private Unix socket at `hum.sock` under the runtime
directory. Runtime selection is `--runtime-dir`, then `HUM_RUNTIME_DIR`, then
`$XDG_RUNTIME_DIR/hum`, then a per-user temporary directory. The directory is
mode `0700` and the socket and lifecycle files are mode `0600`; no remote
transport or network listener is involved. A detached daemon writes bounded
diagnostics to `daemon.log` in that directory.

The client sends the process's exact argv, its current working directory, and
its full environment at `run` time. The daemon therefore does not need to have
the caller's shell, `PATH`, version manager, or directory hooks. `restart`
reuses the recorded argv, cwd, and environment from the original launch;
environment values are never returned by `list` or `status`.

Each child has its own Unix process group and stdin connected to `/dev/null`.
`stop` sends SIGTERM to the whole group, waits the configured grace period,
then sends SIGKILL if necessary. Attached `run` sends SIGINT to the group on
the first Ctrl+C and uses the normal graceful stop sequence on a second;
SIGTERM or SIGHUP to an attached client detaches it without stopping the
process. A foreground `serve` shutdown is forced and stops managed groups.
There is no PTY and no arbitrary interactive-input channel. Tools that disable
color without a terminal can be run with an inherited override such as:

```sh
FORCE_COLOR=1 "$HUM" run colored -- npm run dev
```

The conservative defaults and bounds are:

| Setting | Default or bound |
| --- | --- |
| graceful stop (`--stop-grace` / `HUM_STOP_GRACE`) | `10s` |
| retained output (`--output-bytes` / `HUM_OUTPUT_BYTES`) | `4 MiB` per process; minimum `64 KiB` |
| completed records (`--completed-records` / `HUM_COMPLETED_RECORDS`) | `20`; valid range `1`–`1000` |
| one `logs` read | at most `100` entries and `16 KiB` unless overridden |
| `logs --limit-bytes` | byte bound for that read; `--tail N` selects final entries |
| one captured line | flushed/chunked at `64 KiB` |
| detached daemon `daemon.log` | bounded to `1 MiB` by default |
| `wait --timeout` | `30s` unless explicitly set to a duration of at least `1ms` |

Output cursors are monotonically increasing within a process record. A human
`logs` read prints `next cursor: N` on stderr; a stale cursor reports
truncation/eviction metadata instead of returning unbounded history.

`wait` reports `matched`, `exited`, or `timed_out` and always returns a cursor.
Its process exit statuses are `0` for a match or an exit wait without
`--match`, `2` for timeout, `3` when a process exits before a requested match,
and `1` for request/daemon errors. `--json` returns the same outcome and
cursor in one JSON object.

After a binary upgrade, a running daemon performs a protocol-version
handshake. A mismatched read/control client fails with both versions and the
next step `hum shutdown`. The frozen shutdown request remains allowed, so
`hum shutdown` can retire an idle older daemon. `run` can replace a mismatched
daemon automatically only when it has no managed processes; with active
processes it refuses and names `hum shutdown --stop-processes`. Explicit
`serve --daemon` reports the mismatch rather than silently replacing an
active daemon.

Daemon state is in memory. Restarting or replacing the daemon loses retained
output, process records, and restart history; the completed-records bound does
not make that state persistent. A daemon's normal forced shutdown also stops
its managed process groups.

## Future commands

`start`, `up`, `down`, `init`, `skill`, and `mcp` are future interfaces, not
part of this foundation. In particular, `down` does not exist; use
`hum stop NAME...` for named process groups and `hum shutdown` for the daemon.
