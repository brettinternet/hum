---
id: HUM-034
title: Rewrite command help for humans
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 17:31'
labels:
  - cli
  - docs
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/root.go
  - internal/cli/commands.go
  - internal/cli/help_contract_test.go
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
Outcome: `hum --help` and every visible `hum <command> --help` provide concise operator help: one-line usage, a description of at most three sentences, one to three copy-pasteable examples, and a visible default or explicit omission meaning for every flag. The help for start, up, and wait each states its 0/1/2/3 exit behavior once. Contract prose remains in docs/design.md.

Scope: rewrite Usage, Description, ArgsUsage, and examples in internal/cli; add one table-driven help-contract test that walks the assembled visible command tree so newly added commands and flags inherit the same standard; update phrase pins without dropping coverage of README.md, docs/design.md, docs/coding-agents.md, or bundled skills.

Why now: root and up help currently read like internal specifications, contain no examples, and omit exit behavior. Help is the primary discovery surface for humans and agents using the CLI.

Non-goals: changing flags, behavior, exit codes, JSON output, or restructuring README.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestHelpContract$' -count=1 -v` exits 0 and prints PASS while walking every visible command and asserting a one-line usage, at most three description sentences, one to three examples, and a displayed default or explicit omission meaning for every flag.
- [ ] #2 `go test ./internal/cli -run '^TestHelpExitCodes$' -count=1 -v` exits 0 and prints PASS, proving start, up, and wait each document the exact 0/1/2/3 outcomes once and no other help block duplicates that contract.
- [ ] #3 `task cli:build && test "$(./bin/hum --help | wc -w | tr -d ' ')" -le 300 && test "$(./bin/hum up --help | sed -n '/DESCRIPTION:/,/OPTIONS:/p' | wc -w | tr -d ' ')" -le 120` exits 0.
- [ ] #4 `go test ./internal/cli -run 'Docs|Surface|Root|HelpContract' -count=1` exits 0 with all contract-level phrases still asserted in the canonical docs or skills.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Define concise usage, description, example, default, and exit-code copy for the root and every visible command.
2. Add a command-tree help contract test instead of hard-coded shell lists.
3. Re-pin documentation assertions and run focused help tests plus the final gate.
<!-- SECTION:PLAN:END -->
