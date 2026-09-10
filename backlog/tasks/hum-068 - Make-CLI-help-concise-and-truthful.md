---
id: HUM-068
title: Make CLI help concise and truthful
status: To Do
assignee: []
created_date: '2026-09-10 01:51'
updated_date: '2026-09-10 06:01'
labels: []
dependencies:
  - HUM-064
  - HUM-066
modified_files:
  - internal/cli/commands.go
  - internal/cli/root.go
  - internal/cli/surface_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: docs
ordinal: 44700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Root and subcommand help lead with task-oriented usage, show only selectors a command supports, and move protocol/state-machine detail to durable documentation. Evidence (measured 2026-09-10 with `hum CMD --help | wc -w`): root 393 words; logs 436, up 329, run 318, wait 299, restart 280, mcp 267, input 246, attach 222, init 208 all exceed 200. Inherited `--project/-C` is advertised on mcp, serve, skill, shutdown, and completion although `rejectProjectOverride` rejects it on the first four and completion is scope-neutral. Scope: shorten command descriptions, expose scope flags only where valid, retain links or brief pointers to detailed design/coding-agent docs, and add help-contract tests. Non-goals: do not remove supported options, change command behavior, or duplicate full reference documentation in help text.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise exec go -- go test ./internal/cli -run "Test.*(Help|Surface|Scope)" -count=1` exits 0.
- [ ] #2 `mise exec go -- go test ./internal/cli -run TestHelpWordBudgets -count=1` exits 0 after proving root help is at most 250 words and every subcommand help is at most 200 words.
- [ ] #3 `mise exec go -- go test ./internal/cli -run TestHelpAdvertisesOnlySupportedScopeFlags -count=1` exits 0 after proving scope-neutral commands show no selectors and lifecycle commands show only supported selectors.
- [ ] #4 `mise exec go -- go test ./...` exits 0.
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

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: confirmed and re-measured; description updated with exact counts and the actual inherited-flag leak (--project on mcp/serve/skill/shutdown/completion). Kept dependencies on HUM-064 and HUM-066 because both change the help text this task budgets.
---
<!-- COMMENTS:END -->
