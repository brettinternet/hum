---
id: HUM-080
title: Add stress coverage and stabilize shutdown teardown
status: Done
assignee:
  - '@pi'
created_date: '2026-09-10 20:35'
updated_date: '2026-09-11 02:12'
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
- [x] #1 task --summary stress shows mise exec go -- go test -race -count=5 -shuffle=on -failfast ./integration ./internal/cli ./internal/daemon ./internal/app ./internal/mcp.
- [x] #2 mise exec actionlint -- actionlint .github/workflows/stress.yaml exits 0; the workflow has schedule and workflow_dispatch triggers, contents: read permissions, Linux and macOS jobs, 60-minute job timeouts, and SHA-pinned actions.
- [x] #3 mise exec go -- go test -race -count=100 -run 'TestStopTreatsESRCHAsExited|TestStopTreatsSignalErrorFollowedByExitAsStopped|TestShutdownProcessTrees|TestShutdownCompletesAfterCanceledContext' ./internal/app exits 0.
- [x] #4 rg -n 'stress' Taskfile.dist.yaml .github/workflows/ci.yaml matches only Taskfile.dist.yaml and task ci exits 0 with no test deleted, skipped, or weakened.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
- [x] Add the opt-in stress task and scheduled workflow
- [x] Fix the shutdown EPERM teardown race and add deterministic regression coverage
- [x] Run final task ci and actionlint checks, record evidence, and complete the item
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation landed locally on main in 7b89f30. Focused shutdown regression verification passed before its conflict-free rebase onto current main. The original broad stress qualification was split into HUM-086 through HUM-091; those tasks retain the reproduced failures and investigation evidence.

AC#1 PASS — `task --summary stress` printed `mise exec go -- go test -race -count=5 -shuffle=on -failfast ./integration ./internal/cli ./internal/daemon ./internal/app ./internal/mcp`.
AC#2 PASS — `mise exec actionlint -- actionlint .github/workflows/stress.yaml` exited 0; inspection confirmed schedule and workflow_dispatch triggers, contents: read, Linux/macOS jobs, 60-minute timeouts, and SHA-pinned actions.
AC#3 PASS — `mise exec go -- go test -race -count=100 -run 'TestStopTreatsESRCHAsExited|TestStopTreatsSignalErrorFollowedByExitAsStopped|TestShutdownProcessTrees|TestShutdownCompletesAfterCanceledContext' ./internal/app` passed (`ok hum/internal/app`).
AC#4 PASS — `rg -n 'stress' Taskfile.dist.yaml .github/workflows/ci.yaml` matched only Taskfile.dist.yaml; final `task ci` passed on commit 9f66c8d. No test was deleted, skipped, or weakened.
Independent verifier run 1396220a-37eb-4510-aa4a-adb3958137b5 returned PASS for AC#1-#4 and DoD#1, #3-#6. Its missing-evidence-lines finding was resolved by this update. Implementation commits 7b89f30 and 9f66c8d touch only the four declared implementation paths; backlog provider-state commits are metadata. Task is labeled tooling, and no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added the opt-in race/shuffle stress task and daily/manual Linux/macOS workflow, normalized terminally reconciled macOS EPERM teardown races while preserving genuine signal failures, and added deterministic TERM/KILL regression coverage. Verified with actionlint, 100 race repetitions of shutdown regressions, independent AC review, and final task ci on 9f66c8d.
<!-- SECTION:FINAL_SUMMARY:END -->
