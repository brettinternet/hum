# Development

## Setup

Install [mise](https://mise.jdx.dev/), then from the repository root:

```sh
mise install
task init      # toolchain, dependencies, Git hooks
```

`mise.toml` pins Go, git-cliff, Staticcheck, Task, Lefthook, gitleaks, govulncheck, and Backlog.md.

## Build and run

```sh
task cli:build          # writes bin/hum
./bin/hum --help
./bin/hum --version     # hum dev (built unknown)
```

Release builds inject version, commit, and build time through linker flags:

```sh
mkdir -p bin
mise exec go -- go build \
  -ldflags "-X main.buildVersion=1.2.3 -X main.buildCommit=$(git rev-parse HEAD) -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o bin/hum ./cmd/hum
```

## Checks

Use the smallest check that covers your change.

| Task | What it does |
| --- | --- |
| `task fix:staged` | format staged Go files and re-stage them |
| `task check:staged` | pre-commit formatter plus a secret scan of staged files; run before committing |
| `task test` | `go test ./...` |
| `task check` | changelog generation, Go formatting, `go vet ./...`, Staticcheck |
| `task coverage` | all tests with coverage; profile at `/tmp/hum-coverage.out` |
| `task security` | gitleaks over full history, plus `govulncheck ./...` (fails only on reachable vulnerabilities) |
| `task stress` | repeated race-enabled daemon and child-process tests in shuffled order |
| `task changelog` | preview or regenerate `CHANGELOG.md` |
| `task ci` | the full gate: security, checks, tests, race tests, and release smoke tests |

The `task ci` smoke step builds the binary and `hum(1)` manual page, checks the manual page, and
runs the built binary once with `--version`. Integration and CLI JSON v1 contract tests run in
`task test`. GitHub Actions runs the same gates on Linux and macOS, with race tests in parallel,
a Go build and module cache keyed by OS, Go version, and `go.sum`, and `GOFLAGS=-count=1` so test
results are never reused. `task stress` is not part of `task ci`; it runs daily on Linux and macOS
(`.github/workflows/stress.yaml`) and on manual dispatch.

## Toolchain

| Tool | Version |
| --- | --- |
| Go | 1.27.1 |
| Staticcheck | 2026.2.1 |
| govulncheck | 1.8.0 |

Hum ships as prebuilt binaries and its module is not importable, so the pinned Go is the only
supported toolchain. The `go.mod` directive tracks the pinned Go minor.

- To upgrade tools, run `task setup:upgrade`, review the changes in `mise.toml`, and run `task ci`.
- When raising the Go minor, update `go.mod` and the table above together.
- Dependabot proposes weekly Go module and GitHub Actions updates. Actions stay pinned to commit
  SHAs with the major version in a comment.

## Windows

| Where | Command | Checks |
| --- | --- | --- |
| Windows host | `task windows:test` | `go test -count=1 ./...`, including integration and Windows fixtures; CI runs it as `Go CI (Windows)` on `windows-latest` |
| macOS/Linux | `task windows:watch` | waits for the CI run of a pushed `windows/<task-id>` branch (push only with explicit approval) |
| macOS/Linux | `task windows:package:smoke` | cross-builds `hum-<version>-windows-x64.zip`, checks it contains `hum.exe`, verifies its line in `dist/checksums.txt` |

A local cross-compile does not replace the native check. Set `VERSION`, `BUILD_TIME`, and
`BUILD_COMMIT` for a labelled package. The release workflow runs `task windows:package`, regenerates
checksums for the tarballs and zip, and uploads them all.

## Release

1. Push the release commit to `main` and wait for its CI run to start.
2. Tag that exact commit and push the tag.
3. The release workflow waits for that commit's newest `main` CI run and publishes only if it
   passes. It writes release notes from conventional commits, then regenerates and commits
   `CHANGELOG.md` to `main`.

If CI fails or no matching run exists, push a fix and tag the new commit. The changelog includes
features, fixes, performance, refactors, docs, and reverts; it omits test, CI, backlog, and
maintenance commits.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```text
<type>(<optional scope>)<optional !>: <description>
```

```text
feat(cli): add process status command
fix(daemon): preserve buffered stderr on exit
docs: explain runtime configuration
```

- Types: `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, plus `perf`, `revert`,
  and `style`.
- Keep scopes lowercase; omit them when they add nothing.
- Mark breaking changes with `!` or a `BREAKING CHANGE:` trailer.
- Do not add task IDs or ticket references.
