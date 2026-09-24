# Development

## Prerequisites

Install [mise](https://mise.jdx.dev/) and make it available on your `PATH`. Project-managed versions of Go, git-cliff, Staticcheck, Task, Lefthook, gitleaks, govulncheck, and Backlog.md are declared in `mise.toml`.

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

`task ci` uses the development pins. hum ships as prebuilt release binaries and its module path is
not importable, so the only supported build toolchain is the pinned one: the `go.mod` directive
tracks the pinned Go minor and there is no separate minimum supported Go version.

Dependabot proposes weekly Go module and GitHub Actions updates. Workflow actions remain pinned to immutable commit SHAs with their major version in a comment. To upgrade project tools, run `task setup:upgrade`, review the exact version changes in `mise.toml`, and run `task ci`.

Go and Staticcheck can be upgraded separately. When raising the Go pin to a new minor, update the `go.mod` directive and this table together, then run `task ci`.

## Build

Build the CLI with:

```sh
task cli:build
```

The build writes `bin/hum`. Runtime configuration is resolved by the CLI before the daemon is
started. In particular, `HUM_STOP_GRACE=0s` is an explicit immediate-escalation setting rather
than a request for the ten-second default. Hum-created runtime directories are mode 0700;
pre-existing directories retain their operator-managed mode and are rejected when writable
by group or other users or owned by another user. Daemon clients and the daemon refuse a
socket peer running as another user.

The current executable supports:

```sh
./bin/hum --help
./bin/hum --version
```

`--help` displays the current command usage. The default development build reports `hum dev (built unknown)`; release builds inject version, commit, and build-time metadata through Go linker flags.

## Release

Push the release commit to `main` and wait for its CI workflow to start, then tag that exact commit and push the tag. The release workflow waits for the newest `main` push CI run for the tagged commit and publishes only when it succeeds. It generates the GitHub release notes from conventional commits, then regenerates and commits `CHANGELOG.md` to `main`. If CI fails or no matching run exists, push a fix and create a new tag for the fixed commit.

Run `task changelog` to preview or regenerate the changelog locally. Features, fixes, performance changes, refactors, documentation, and reverts are included; test, CI, backlog, and maintenance commits are omitted.

For a release or locally labelled build:

```sh
mkdir -p bin
mise exec go -- go build \
  -ldflags "-X main.buildVersion=1.2.3 -X main.buildCommit=$(git rev-parse HEAD) -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o bin/hum ./cmd/hum
```

## Project checks

```sh
task changelog
task fix:staged
task check:staged
task check
task test
task coverage
task security
task stress
task ci
```

- `task fix:staged` formats staged Go files and re-stages the fixes.
- `task check:staged` runs the pre-commit formatter and staged secret scan.
- `task check` verifies changelog generation and Go formatting, then runs `go vet ./...` and Staticcheck with the pinned development toolchain.
- `task test` runs `go test ./...`.
- `task coverage` runs all tests with repository-wide coverage and prints the per-function report. Its coverage profile is written outside the repository at `/tmp/hum-coverage.out`.
- `task security` scans the full Git history with gitleaks and runs `govulncheck ./...`; govulncheck fails only for vulnerabilities reachable from project code.
- `task stress` repeatedly runs race-enabled daemon and child-process tests with shuffled ordering. It is intentionally separate from `task ci` and runs daily on Linux and macOS through `.github/workflows/stress.yaml`; the workflow also supports manual dispatch.
- `task ci` independently runs the security gate, checks, tests, race-sensitive package tests, and release-output smoke tests with Go 1.27.1 and Staticcheck 2026.2.1. The smoke step builds the binary and `hum(1)` manual before exercising the integration lifecycle and CLI JSON v1 contract tests against the built binary. GitHub Actions preserves those gates on Linux and macOS while running each OS's race tests concurrently with its other checks, restoring a per-job Go build and module cache keyed by OS, Go version, and `go.sum`; `GOFLAGS=-count=1` keeps test results from being reused.

## Windows verification and packaging

On a Windows host, `task windows:test` runs `go test -count=1 ./...`, including the integration suite and platform-specific Windows fixtures. The `Go CI (Windows)` job runs this target on `windows-latest`. After explicit approval to publish a test branch, push the worktree HEAD to `windows/<task-id>` and run `task windows:watch` on macOS/Linux to wait for the matching CI run. A local cross-compile does not replace this native check.

On macOS/Linux, `task windows:package:smoke` cross-builds the same `hum-<version>-windows-x64.zip` as the release job, checks that it contains `hum.exe`, and verifies its SHA-256 line in `dist/checksums.txt`. Set `VERSION`, `BUILD_TIME`, and `BUILD_COMMIT` for a labelled build. The release workflow calls `task windows:package`, regenerates checksums for tarballs and the zip, and uploads all of them. Windows installation is manual with PowerShell `Expand-Archive`; `install.sh` remains Unix-only.

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
