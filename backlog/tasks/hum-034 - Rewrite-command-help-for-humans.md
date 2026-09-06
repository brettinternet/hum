---
id: HUM-034
title: Rewrite command help for humans
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - cli
  - docs
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/root.go
  - internal/cli/commands.go
  - internal/cli/surface_test.go
  - internal/cli/after_docs_test.go
  - internal/cli/restart_policy_docs_test.go
  - internal/cli/terminal_control_docs_test.go
  - internal/cli/root_test.go
  - docs/design.md
priority: high
type: enhancement
ordinal: 11700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum --help` and every `hum <command> --help` read as concise operator help: a one-line usage, a description of at most three sentences, an `Examples:` block with one to three copy-pasteable commands, defaults shown on every flag, and the exit codes of `up`, `start`, and `wait` listed once in their help. Contract-level prose (readiness gates, drift and recovery outcomes, terminal-control stripping, TTY lease rules) lives only in docs/design.md, which already states it.

Why now: the root DESCRIPTION is a ~250-word specification paragraph and `hum up --help` is a ~400-word paragraph in contract language ("reports lexical skipped results with direct blocked_by names"). The 2026-09-06 audit found zero Examples sections in 17 commands, exit codes documented nowhere in the CLI, and rated this the largest ergonomics gap.

Scope: rewrite Usage, Description, and ArgsUsage strings in internal/cli/root.go and internal/cli/commands.go; re-pin the help assertions in the docs tests to the new concise wording without removing any assertion about README.md, docs/design.md, docs/coding-agents.md, or the skills, which keep the full contract phrases.

Non-goals: changing any flag, behavior, exit code, or JSON output; rewriting README.md (separate task).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:build && ./bin/hum --help | wc -w` prints a number at or below 300.
- [ ] #2 `for c in serve init mcp skill run start up down list status logs wait input restart stop remove shutdown; do ./bin/hum $c --help | grep -c 'Examples:'; done` prints 1 for every command, and `./bin/hum up --help | sed -n '/DESCRIPTION:/,/OPTIONS:/p' | wc -w` prints a number at or below 120.
- [ ] #3 `./bin/hum up --help | grep -c 'exit'` and `./bin/hum wait --help | grep -c 'exit'` each print at least 1, describing the 0/1/2/3 exit codes.
- [ ] #4 `go test ./internal/cli -run 'Docs|Surface|Root' -count=1` exits 0 with help phrase pins updated to the new wording and every README, design, and skill assertion intact.
- [ ] #5 `task ci` exits 0.
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
