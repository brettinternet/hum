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

| Purpose | Version | Source |
| --- | --- | --- |
| Development Go | 1.27.1 | `mise.toml` |
| Staticcheck | 2026.2.1 | `mise.toml` |
| Minimum supported Go | 1.22 | `go.mod` |

`task ci` uses the development pins. `task check:go-min` compiles and tests with Go 1.22.

To upgrade a development tool:

1. Change its exact version in `mise.toml`.
2. Run `mise install`.
3. Run `task ci`.

Go and Staticcheck can be upgraded separately. Raise the minimum Go version only when the support policy changes. Update `go.mod`, `task check:go-min`, and this table together. Then run both `task check:go-min` and `task ci`.

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

## Project checks

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
- `task ci` independently runs checks, tests, race-sensitive package tests, and the built-binary smoke test with Go 1.27.1 and Staticcheck 2026.2.1.

## Commit messages

Commits use [Conventional Commits](https://www.conventionalcommits.org/):

```text
<type>(<optional scope>)<optional !>: <description>
```

Choose the type that best describes the change:

- Common: `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`
- Also accepted: `perf`, `revert`, `style`

Keep scopes lowercase and omit them when they add no information. Mark breaking changes with `!` or a `BREAKING CHANGE:` trailer. Do not add task IDs or ticket references.

Examples:

```text
feat(cli): add process status command
fix(daemon): preserve buffered stderr on exit
docs: explain runtime configuration
```
