# hum

hum is a local development process supervisor for humans and coding agents.

The current CLI surface is intentionally limited to help and version output. Process and daemon commands (`serve`, `run`, `start`, `up`, `down`, `list`, `status`, `logs`, `wait`, `restart`, `stop`, `shutdown`, and `init`) are planned. See the [hum design](docs/design.md) for the intended behavior and delivery order.

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

Detached daemon diagnostics will use a bounded or rotating `daemon.log` in the private runtime directory. Concurrent starts will produce one daemon, and verified stale PID/socket files will be recovered safely. Only launch commands (`run`, `start`, `up`) auto-start it. If the daemon is unavailable, `status`, `logs`, `wait`, and `restart` will report that nothing is running and name the launch command to use instead of starting an empty daemon.

Follow logs without attaching process control:

```sh
hum logs api --follow
hum logs api --stream stderr --tail 100 --limit-bytes 16000 --json
hum logs api --after-cursor 2941 --limit-bytes 16000 --json
```

Following is read-only: Ctrl+C cancels only that follower, and multiple followers may observe the same process. `--json --follow` will emit newline-delimited JSON events. Reads and streaming delivery remain bounded, and a lagging follower is told when earlier output was evicted.

`hum stop api` will gracefully stop one managed process group, and `hum down` will stop every process in the current project. Daemon shutdown is distinct:

```sh
hum shutdown
hum shutdown --stop-processes
```

Default shutdown will refuse and list active process names. `--stop-processes` will send SIGTERM to every managed group, wait a bounded grace period, use SIGKILL only where necessary, and then stop the daemon. Launchd, systemd, login startup, and operating-system service installation are not part of this update.

## Development

See the [development guide](docs/development.md) for project setup, build
commands, checks, and commit conventions.
