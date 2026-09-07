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

## Toolchain policy

`mise.toml` pins Go 1.27.1 and Staticcheck 2026.2.1. Ordinary `task ci` uses those pins. The `go 1.22` directive in `go.mod` is the minimum supported Go version, not the development toolchain; `task check:go-min` compiles and tests the source with Go 1.22 to keep that compatibility promise executable.

Upgrade either tool pin deliberately by changing its exact version in `mise.toml`, running `mise install`, and rerunning `task ci`; Go and Staticcheck pins may be upgraded independently. Raise the Go minimum only when the support policy changes: update the `go.mod` directive, the Go version in `task check:go-min`, and this policy together, then run both `task check:go-min` and `task ci`.

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
task fix:staged
task check:staged
task check
task check:go-min
task test
task ci
```

- `task fix:staged` formats staged Go files and re-stages the fixes.
- `task check:staged` runs the pre-commit formatter and staged secret scan.
- `task check` verifies Go formatting and runs `go vet ./...` and Staticcheck with the pinned development toolchain.
- `task check:go-min` compiles and tests the source with the Go 1.22 minimum.
- `task test` runs `go test ./...`.
- `task ci` runs checks, tests, race-sensitive package tests, and the built-binary smoke test with Go 1.27.1 and Staticcheck 2026.2.1. The pre-push hook runs this same gate.

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
