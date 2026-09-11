---
id: HUM-080
title: Eliminate intermittent CI test failures
status: To Do
assignee: []
created_date: '2026-09-10 20:35'
updated_date: '2026-09-11 01:42'
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
  - integration/
  - internal/cli/
  - internal/daemon/
  - internal/app/
  - internal/mcp/
  - internal/testutil/
  - docs/development.md
priority: high
type: bug
ordinal: 52600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Seven of the 25 most recent CI runs on main (2026-09-07 to 2026-09-10) failed on tests unrelated to the pushed change and were followed by a same-day patch to that one test:
- 34134814742: internal/cli TestFlagAliasParityReadCommands/wait_request_output_and_exit/short (Linux, normal)
- 34373521282: internal/cli TestShutdown/--stop-processes_waits_for_graceful_process-tree_termination (macOS, normal)
- 34510364048 and 34519110364: internal/mcp TestLogsSince/live_daemon_boundary_and_composition (macOS, normal)
- 34515155227: internal/cli TestInputCommand (macOS, race) and internal/app TestControlSignalSurvivorResumesOnFailureRestart (Linux, race)
- 34520139200: integration TestNDJSONFollow (macOS, normal) and integration TestManifestWorkflow, outcome "exited_before_ready" instead of "started" (Linux, normal)
- 34522056130: internal/daemon TestControlSignalDaemonRoundTripSuppressesRestart (both, race)
Dependabot PR #1 (actions/checkout 7.0.1) is red on the same TestLogsSince and TestManifestWorkflow failures. Commits 17359b5, f1e74ab, 88e63b1, and d6cb6fb each fixed one symptom; nothing reproduces flakes before they reach CI.

Outcome: intermittent failures are reproduced locally with a stress loop, fixed at the synchronization root cause, and guarded by a scheduled stress workflow so new flakes surface before they block a push or a Dependabot PR.

Scope:
- Add `task stress` to Taskfile.dist.yaml: `mise exec go -- go test -race -count=5 -shuffle=on -failfast ./integration ./internal/cli ./internal/daemon ./internal/app ./internal/mcp` (the packages that spawn daemons or child processes). It stays out of `task ci`.
- Re-run each test listed above with `-race -count=20` while `task stress` runs in another shell (loaded-machine condition, which is what CI runners look like). Record for each whether it is already fixed by the commits above or still fails, with the failure text, in Implementation Notes.
- Fix remaining failures by replacing timing assumptions with observable synchronization (readiness files, cursors, process-gone waits, explicit handshakes). Widening a timeout or retrying an assertion is not a fix. If a flake exposes a production race, fix it and call it out in the notes.
- Add .github/workflows/stress.yaml: `schedule` (daily) plus `workflow_dispatch`; one job per OS running `task stress`; `timeout-minutes: 60`; `permissions: contents: read`; actions pinned to commit SHAs like the existing workflows.
- After fixes land, re-run CI for Dependabot PR #1 (`gh pr comment 1 --body "@dependabot rebase"`) and report its result in the notes.

Non-goals: deleting, skipping, or auto-retrying tests; adding `-count` retries to `task ci`; running stress on every push; changing production behaviour except for a demonstrated race.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task stress` exits 0 on three consecutive invocations at the candidate revision; the three wall times are recorded in Implementation Notes.
- [ ] #2 With `task stress` running in a second shell, `mise exec go -- go test -race -count=20 -run 'TestLogsSince|TestManifestWorkflow|TestNDJSONFollow|TestControlSignalDaemonRoundTripSuppressesRestart|TestControlSignalSurvivorResumesOnFailureRestart|TestInputCommand|TestShutdown|TestFlagAliasParityReadCommands' ./integration ./internal/cli ./internal/daemon ./internal/app ./internal/mcp` exits 0.
- [ ] #3 `gh workflow run stress.yaml` then `gh run watch $(gh run list --workflow stress.yaml --limit 1 --json databaseId --jq '.[0].databaseId') --exit-status` exits 0 with both OS jobs successful.
- [ ] #4 `rg -n 'stress' Taskfile.dist.yaml .github/workflows/ci.yaml` matches only the Taskfile (stress is not part of the push gate), and `task ci` exits 0 with no test deleted, skipped, or weakened.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-09-11 smallest-diff checkpoint: retained only task stress, the scheduled stress workflow, and the shutdown EPERM teardown-race fix with deterministic TERM/KILL regression coverage. Reverted separable fixes and recorded them as HUM-086 through HUM-090. Commit bf2884c. Final bounded verification: task check:staged PASS; mise exec go -- go test -race -count=100 -run 'TestStopTreatsESRCHAsExited|TestStopTreatsSignalErrorFollowedByExitAsStopped|TestShutdownProcessTrees|TestShutdownCompletesAfterCanceledContext' ./internal/app PASS; rg -n 'stress' Taskfile.dist.yaml .github/workflows/ci.yaml matched only Taskfile.dist.yaml; git diff --cached --check PASS. Earlier broader candidate (before shrinking) passed task ci and three stress runs, but those results do not attest the final smallest diff. Remaining HUM-080 blockers: AC#1 and AC#2 are not established on bf2884c because stress exposed separable flakes now tracked in HUM-086 through HUM-090; AC#3 requires the workflow on remote main and was not run because no push was authorized. Dependabot PR #1 was not rebased for the same reason. internal/process/ was outside the declared modified-file list during exploration but is absent from the finalized diff.

Integration correction: rebase onto current main rewrote bf2884c as 7b89f30; main was fast-forwarded locally to 7b89f30. No post-rebase tests were run per user instruction.
<!-- SECTION:NOTES:END -->
