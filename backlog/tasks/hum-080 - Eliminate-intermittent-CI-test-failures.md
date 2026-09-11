---
id: HUM-080
title: Add stress coverage and stabilize shutdown teardown
status: To Do
assignee: []
created_date: '2026-09-10 20:35'
updated_date: '2026-09-11 01:51'
labels:
  - tooling
  - integration
  - cli
dependencies: []
references:
  - .github/workflows/ci.yaml
  - internal/cli/list_logs_test.go
  - internal/mcp/tools_test.go
  - integration/manifest_test.go
  - internal/daemon/daemon_test.go
  - internal/app/app_test.go
modified_files:
  - Taskfile.dist.yaml
  - .github/workflows/stress.yaml
  - internal/app/app.go
  - internal/app/app_test.go
priority: high
type: bug
ordinal: 52600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the repository has an opt-in race/shuffle stress command, a daily/manual two-OS stress workflow, and shutdown no longer reports macOS EPERM when the same process incarnation has already reached terminal reconciliation.

Scope:
- Add task stress for integration, CLI, daemon, app, and MCP packages.
- Add a scheduled and workflow-dispatch GitHub Actions stress workflow for Linux and macOS.
- Normalize teardown-race EPERM only after terminal reconciliation, while preserving genuine signal failures.
- Add deterministic TERM and KILL regression coverage.

Non-goals: fixing every flake surfaced by broad stress runs; proving three consecutive stress runs; remote workflow qualification; or rebasing Dependabot. Those are owned by HUM-086 through HUM-091.

Modified files: Taskfile.dist.yaml, .github/workflows/stress.yaml, internal/app/app.go, internal/app/app_test.go.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task --summary stress shows mise exec go -- go test -race -count=5 -shuffle=on -failfast ./integration ./internal/cli ./internal/daemon ./internal/app ./internal/mcp.
- [ ] #2 mise exec actionlint -- actionlint .github/workflows/stress.yaml exits 0; the workflow has schedule and workflow_dispatch triggers, contents: read permissions, Linux and macOS jobs, 60-minute job timeouts, and SHA-pinned actions.
- [ ] #3 mise exec go -- go test -race -count=100 -run 'TestStopTreatsESRCHAsExited|TestStopTreatsSignalErrorFollowedByExitAsStopped|TestShutdownProcessTrees|TestShutdownCompletesAfterCanceledContext' ./internal/app exits 0.
- [ ] #4 rg -n 'stress' Taskfile.dist.yaml .github/workflows/ci.yaml matches only Taskfile.dist.yaml and task ci exits 0 with no test deleted, skipped, or weakened.
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
- [x] Add the opt-in stress task and scheduled workflow
- [x] Fix the shutdown EPERM teardown race and add deterministic regression coverage
- [ ] Run final task ci and actionlint checks, record evidence, and complete the item
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation landed locally on main in 7b89f30. Focused shutdown regression verification passed before its conflict-free rebase onto current main. The original broad stress qualification was split into HUM-086 through HUM-091; those tasks retain the reproduced failures and investigation evidence.
<!-- SECTION:NOTES:END -->
