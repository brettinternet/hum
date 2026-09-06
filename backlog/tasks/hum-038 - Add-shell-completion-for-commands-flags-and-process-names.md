---
id: HUM-038
title: 'Add shell completion for commands, flags, and process names'
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 17:42'
labels:
  - cli
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/root.go
  - internal/cli/commands.go
  - internal/cli/completion.go
  - internal/cli/completion_test.go
  - internal/cli/help_contract_test.go
  - README.md
  - docs/design.md
priority: medium
type: feature
ordinal: 15700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum completion bash|zsh|fish` prints an installable completion script. Subcommands and flags complete from the assembled command tree; NAME positions complete the same merged declaration/runtime set as project-scoped `hum list`, never records from other projects. Completion never starts a daemon.

Scope: enable urfave shell completion, add the visible completion command, add NAME callbacks to commands that accept names, and document opt-in installation for bash, zsh, and fish. When the daemon is absent, declarations still complete. Manifest or daemon errors yield no candidates and no diagnostic so shell completion remains quiet and side-effect-free.

Why now: command and process-name discovery is manual even though the CLI framework supplies completion support. This creates avoidable friction for frequently repeated lifecycle commands.

Non-goals: PowerShell, editing shell rc files, cross-project name completion, fuzzy ranking, or daemon startup.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestCompletionScripts$' -count=1 -v` exits 0 and prints PASS for valid non-empty bash, zsh, and fish scripts plus visible completion help.
- [ ] #2 `go test ./internal/cli -run '^TestNameCompletion$' -count=1 -v` exits 0 and prints PASS, proving declared and same-project runtime names are merged, deduplicated, sorted, and offered only at NAME positions while records from other projects are excluded.
- [ ] #3 `go test ./internal/cli -run '^TestCompletionIsQuietAndInert$' -count=1 -v` exits 0 and prints PASS, proving no daemon is started, declarations complete when no daemon exists, and daemon/manifest failures produce no candidates, stdout diagnostics, or stderr diagnostics.
- [ ] #4 `task cli:build && ./bin/hum completion zsh | grep -q hum && ./bin/hum --help | grep -q completion` exits 0, and `go test ./internal/cli -run '^TestCompletionDocs$' -count=1` exits 0 with copy-pasteable README.md installation commands for all three shells.
- [ ] #5 `go test ./internal/cli -run '^TestHelpContract$' -count=1 -v` exits 0 and prints PASS with completion and every visible shell child satisfying the existing usage, description, examples, and flag-default contract.
- [ ] #6 `task ci` exits 0.
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
1. Add the completion command and framework-driven command/flag generation.
2. Implement quiet, project-scoped NAME candidate resolution without daemon startup.
3. Cover all shells, absent/error paths, installation docs, and help visibility.
<!-- SECTION:PLAN:END -->
