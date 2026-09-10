# Development

## Prerequisites

Install [mise](https://mise.jdx.dev/) and make it available on your `PATH`. Project-managed versions of Go, Staticcheck, Task, Lefthook, gitleaks, govulncheck, and Backlog.md are declared in `mise.toml`.

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
| govulncheck | 1.8.0 | `mise.toml` |
| Minimum supported Go | 1.22 | `go.mod` |

`task ci` uses the development pins. `task check:go-min` compiles and tests with Go 1.22.

Dependabot proposes weekly Go module and GitHub Actions updates. Workflow actions remain pinned to immutable commit SHAs with their major version in a comment. To upgrade project tools, run `task setup:upgrade`, review the exact version changes in `mise.toml`, and run `task ci`.

Go and Staticcheck can be upgraded separately. Raise the minimum Go version only when the support policy changes. Update `go.mod`, `task check:go-min`, and this table together. Then run both `task check:go-min` and `task ci`.

## Build

Build the CLI with:

```sh
task cli:build
```

The build writes `bin/hum`. Runtime configuration is resolved by the CLI before the daemon is
started. In particular, `HUM_STOP_GRACE=0s` is an explicit immediate-escalation setting rather
than a request for the ten-second default. Hum-created runtime directories are mode 0700;
pre-existing directories retain their operator-managed mode and are rejected only when writable
by group or other users.

The current executable supports:

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
task coverage
task security
task ci
```

- `task fix:staged` formats staged Go files and re-stages the fixes.
- `task check:staged` runs the pre-commit formatter and staged secret scan.
- `task check` verifies Go formatting and runs `go vet ./...` and Staticcheck with the pinned development toolchain.
- `task check:go-min` compiles and tests the source with the Go 1.22 minimum.
- `task test` runs `go test ./...`.
- `task coverage` runs all tests with repository-wide coverage and prints the per-function report. Its coverage profile is written outside the repository at `/tmp/hum-coverage.out`.
- `task security` scans the full Git history with gitleaks and runs `govulncheck ./...`; govulncheck fails only for vulnerabilities reachable from project code.
- `task ci` independently runs the security gate, checks, tests, race-sensitive package tests, and the built-binary smoke test with Go 1.27.1 and Staticcheck 2026.2.1.

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
