# devproc

devproc is a local development process supervisor for humans and coding agents. This repository is currently the Go CLI bootstrap: it provides help and version output plus reproducible project tooling and gates.

The current CLI surface is intentionally limited to help and version output. Process and daemon commands (`serve`, `run`, `list`, `status`, `logs`, `wait`, and `stop`) are planned. See the [devproc design](docs/design.md) for the intended behavior and delivery order.

## Prerequisite

Install [mise](https://mise.jdx.dev/) and make it available on your `PATH`. Project-managed versions of Go, Task, Lefthook, gitleaks, and Backlog.md are declared in `mise.toml`.

## Bootstrap

Run from the repository root:

```sh
mise install
task init
```

`task init` installs the project toolchain and dependencies and installs the Git hooks.

Run the project gates with:

```sh
task check
task test
task ci
```

- `task check` runs Go formatting checks and `go vet ./...`.
- `task test` runs `go test ./...`.
- `task ci` runs both the check and test gates.

Build the CLI with:

```sh
task cli:build
```

The build writes `bin/devproc`. The current executable supports:

```sh
./bin/devproc --help
./bin/devproc --version
```

`--help` displays the current command usage. `--version` prints `devproc version dev` for the default development build.
