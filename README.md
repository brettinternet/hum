# hum

hum is a local development process supervisor for humans and coding agents. This repository is currently the Go CLI bootstrap: it provides help and version output plus reproducible project tooling and gates.

The current CLI surface is intentionally limited to help and version output. Process and daemon commands (`serve`, `run`, `list`, `status`, `logs`, `wait`, `stop`, and `shutdown`) are planned. See the [hum design](docs/design.md) for the intended behavior and delivery order.

## Planned lifecycle

The ordinary workflow will require no separate daemon command:

```sh
hum run api -- ./bin/api
```

`run` will start the detached daemon if needed, ask the daemon to own the process, and remain attached to its stdout and stderr. Ctrl+C will send SIGINT to the managed process group. If the CLI disconnects without an explicit signal, the process will continue. PTY and arbitrary interactive-input support are outside the initial design.

Use detached process startup when no terminal should remain attached:

```sh
hum run api --detach -- ./bin/api
```

This will print the process name and PID and return immediately.

Daemon execution is separate from process attachment:

```sh
hum serve           # foreground; diagnostics on stderr
hum serve --daemon  # detached and idempotent; prints PID and socket after readiness
```

Detached daemon diagnostics will use a bounded or rotating `daemon.log` in the private runtime directory. Concurrent starts will produce one daemon, and verified stale PID/socket files will be recovered safely. `run` is the only command that auto-starts it. If the daemon is unavailable, `list`, `status`, `logs`, `wait`, `stop`, and `shutdown` will suggest `hum serve --daemon` instead of starting an empty daemon.

Follow logs without attaching process control:

```sh
hum logs api --follow
hum logs api --stream stderr --tail 100 --limit-bytes 16000 --json
hum logs api --after-cursor 2941 --limit-bytes 16000 --json
```

Following is read-only: Ctrl+C cancels only that follower, and multiple followers may observe the same process. `--json --follow` will emit newline-delimited JSON events. Reads and streaming delivery remain bounded, and a lagging follower is told when earlier output was evicted.

`hum stop api` will gracefully stop one managed process group. Daemon shutdown is distinct:

```sh
hum shutdown
hum shutdown --stop-processes
```

Default shutdown will refuse and list active process names. `--stop-processes` will send SIGTERM to every managed group, wait a bounded grace period, use SIGKILL only where necessary, and then stop the daemon. Launchd, systemd, login startup, and operating-system service installation are not part of this update.

## Prerequisite

Install [mise](https://mise.jdx.dev/) and make it available on your `PATH`. Project-managed versions of Go, Staticcheck, Task, Lefthook, gitleaks, and Backlog.md are declared in `mise.toml`.

## Bootstrap

Run from the repository root:

```sh
mise install
task init
```

`task init` installs the project toolchain, downloads dependencies, and installs the Git hooks.

Run the project gates with:

```sh
task check
task test
task ci
```

- `task check` verifies Go formatting and runs `go vet ./...` and Staticcheck.
- `task test` runs `go test ./...`.
- `task ci` runs both the check and test gates.

## Commit messages

Commits use [Conventional Commits](https://www.conventionalcommits.org/):

```text
<type>(<optional scope>)<optional !>: <description>
```

Use `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, or `chore` for
most changes. `perf`, `revert`, and `style` are also accepted when they describe
the change precisely. Keep the scope lowercase and omit it when it adds no
information. Describe breaking changes with `!` or a `BREAKING CHANGE:` trailer.
Commit messages do not need task IDs or ticket references.

Examples:

```text
feat(cli): add process status command
fix(daemon): preserve buffered stderr on exit
docs: explain runtime configuration
```

Build the CLI with:

```sh
task cli:build
```

The build writes `bin/hum`. The current executable supports:

```sh
./bin/hum --help
./bin/hum --version
```

`--help` displays the current command usage. The default development build reports `hum version dev (built unknown)`; release builds inject version and build-time metadata through Go linker flags.
