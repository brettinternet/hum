---
id: DEVPROC-001
title: Bootstrap the Go CLI and project gates
status: To Do
assignee: []
created_date: '2026-09-02 17:05'
updated_date: '2026-09-02 20:05'
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
  - mise.toml
  - Taskfile.dist.yaml
  - .taskfiles/
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
- [ ] #1 `task init && task server:build` exits 0 and produces a runnable devproc binary.
- [ ] #2 `go run -ldflags '-X main.buildVersion=1.2.3 -X main.buildTime=2026-09-02T12:00:00Z' ./cmd/devproc --version` exits 0 and prints version 1.2.3 plus the injected build time; `go run ./cmd/devproc` exits 0 and prints concise command help.
- [ ] #3 `task server:check && task server:test` exits 0 and runs gofmt verification, go vet, static analysis, and Go tests rather than template echo commands.
- [ ] #4 `go test ./cmd/devproc -run TestRun` proves injected args/context and clean help, error, and signal-exit behavior without spawning hidden daemon work.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->
