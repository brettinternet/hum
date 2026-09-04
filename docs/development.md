# Development

## Prerequisites

Install [mise](https://mise.jdx.dev/) and make it available on your `PATH`. Project-managed versions of Go, Staticcheck, Task, Lefthook, gitleaks, and Backlog.md are declared in `mise.toml`.

## Setup

Run from the repository root:

```sh
mise install
task init
```

`task init` installs the project toolchain, downloads dependencies, and installs the Git hooks.

## Build

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

For a release or locally labelled build:

```sh
mkdir -p bin
mise exec go -- go build \
  -ldflags "-X main.buildVersion=1.2.3 -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o bin/hum ./cmd/hum
```

## Project gates

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
