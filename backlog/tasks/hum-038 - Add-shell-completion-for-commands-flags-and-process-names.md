---
id: HUM-038
title: 'Add shell completion for commands, flags, and process names'
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - cli
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/root.go
  - internal/cli/commands.go
  - internal/cli/completion.go
  - internal/cli/completion_test.go
  - README.md
  - docs/design.md
priority: medium
type: feature
ordinal: 15700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum completion bash|zsh|fish` prints an installable completion script, and completion of subcommands, flags, and NAME positions works: NAME candidates come from the current project's resolved declarations merged with runtime records (the same set `hum list --all` shows) without starting a daemon.

Why now: urfave/cli v3 ships completion support but `EnableShellCompletion` is never set, so interactive discovery of commands and process names is entirely manual. Every comparable supervisor (pm2, overmind, process-compose, docker compose) completes names.

Scope: enable urfave shell completion, add the visible `completion` command, add ShellComplete callbacks for commands taking NAME..., document installation in README.md.

Non-goals: PowerShell, installers that edit shell rc files.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestCompletion' -count=1 -v` exits 0 and prints PASS for bash, zsh, and fish script generation and for NAME completion listing declared and running names without starting a daemon.
- [ ] #2 `task cli:build && ./bin/hum completion zsh | grep -c hum` prints at least 1, and `./bin/hum --help | grep -c completion` prints 1.
- [ ] #3 `task ci` exits 0.
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
