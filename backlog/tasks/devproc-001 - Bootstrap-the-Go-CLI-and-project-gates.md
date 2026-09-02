---
id: DEVPROC-001
title: Bootstrap the Go CLI and project gates
status: Done
assignee: []
created_date: '2026-09-02 17:05'
updated_date: '2026-09-02 20:14'
labels:
  - cli
  - config
  - tooling
milestone: m-0
dependencies: []
modified_files:
  - go.mod
  - go.sum
  - cmd/devproc/
  - internal/cli/
  - mise.toml
  - Taskfile.dist.yaml
  - .taskfiles/
  - lefthook.yaml
  - .github/workflows/ci.yaml
  - .gitignore
  - .editorconfig
  - README.md
  - AGENTS.md
priority: high
type: feature
ordinal: 100
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a fresh clone builds a versioned devproc binary and the template server/client/web assumptions are replaced by real Go formatting, testing, vetting, static analysis, and build tasks.

Scope: initialize the Go module; add the latest compatible urfave/cli v3 dependency; create a small cmd/devproc main that calls a testable run(context.Context, []string), reports errors on stderr, handles clean exits and SIGINT/SIGTERM/SIGHUP, shows help when no command is supplied, and accepts version/build-time ldflags; update mise/Task tooling for Go.

Non-goals: daemon startup, managed processes, YAML configuration, PTY, Windows, MCP, a web UI, or product commands beyond root help/version.

Modified-file contract: go.mod, go.sum, cmd/devproc/, mise.toml, Taskfile.dist.yaml and .taskfiles/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task init && task cli:build` exits 0 and produces a runnable bin/devproc; `./bin/devproc --version` prints a version line.
- [x] #2 `go run -ldflags '-X main.buildVersion=1.2.3 -X main.buildTime=2026-09-02T12:00:00Z' ./cmd/devproc --version` exits 0 and prints version 1.2.3 plus the injected build time; `go run ./cmd/devproc` exits 0 and prints concise command help.
- [x] #3 `task cli:check && task cli:test` exits 0 and runs gofmt verification, go vet, staticcheck, and Go tests rather than template echo commands.
- [x] #4 `go test ./cmd/devproc -run TestRun` proves injected args/context and clean help, error, and cancellation-exit behavior without spawning hidden daemon work.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented in commit e055f55 (feat: bootstrap Go CLI and project tooling) before the loop picked this task up; closed during backlog refinement on 2026-09-02 after re-running every criterion. The acceptance criteria originally named `task server:*` targets; the Taskfile exposes them as `cli:build`, `cli:check`, and `cli:test`, and the criteria were corrected to match.

AC#1: `task init && task cli:build` exited 0; `./bin/devproc --version` printed `devproc version dev (built unknown)`.
AC#2: `go run -ldflags '-X main.buildVersion=1.2.3 -X main.buildTime=2026-09-02T12:00:00Z' ./cmd/devproc --version` printed `devproc version 1.2.3 (built 2026-09-02T12:00:00Z)`; `go run ./cmd/devproc` printed NAME/USAGE help and exited 0.
AC#3: `task ci` (cli:check then cli:test) exited 0 running gofmt -l, `go vet ./...`, `staticcheck ./...`, and `go test ./...` (devproc/cmd/devproc and devproc/internal/cli ok).
AC#4: `go test ./cmd/devproc -run TestRun -v` passed TestRunNoArgsShowsHelp, TestRunVersion, TestRunInvalidCommandReturnsError, and TestRunCanceledContextReturnsCancellation; main.go installs signal.NotifyContext for SIGINT/SIGTERM/SIGHUP and spawns nothing.

Independent verifier pass (2026-09-02, run against a clean main checkout of the same tree): PASS on all four criteria with the same commands and evidence.

DoD deviations: the bootstrap commit also touched internal/cli/, lefthook.yaml, .github/workflows/ci.yaml, .gitignore, .editorconfig, README.md, and AGENTS.md, so the modified-file list was widened to match; its subject lacks the task ID and Task: trailer, so DoD#7 stays unchecked rather than rewriting main history.
<!-- SECTION:NOTES:END -->
