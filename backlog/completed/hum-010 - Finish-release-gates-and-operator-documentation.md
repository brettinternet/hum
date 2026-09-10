---
id: HUM-010
title: Finish release gates and operator documentation
status: Done
assignee:
  - '@brett'
created_date: '2026-09-02 17:07'
updated_date: '2026-09-03 18:26'
labels:
  - docs
  - tooling
  - integration
milestone: m-0
dependencies:
  - HUM-008
  - HUM-009
  - HUM-012
modified_files:
  - README.md
  - docs/design.md
  - Taskfile.dist.yaml
  - .github/workflows/ci.yaml
priority: medium
type: docs
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the supervisor foundation has one green macOS/Linux gate and concise documentation that a human or coding agent can use without learning internal protocol details.

Scope: installation and ldflags build examples; architecture and application-service boundary; complete foundation CLI help/examples; ordinary attached `run` workflow and its automatic daemon startup; foreground versus detached `serve`; attached versus detached process startup; read-only `logs --follow` and the human next-cursor trailer; `restart` by name; multi-name and idempotent `stop` versus shutting down the daemon; default shutdown refusal and `--stop-processes`; `list --all`; JSON/cursor, `wait` defaults and exit codes, and NDJSON-follow usage; client cwd and environment inheritance; no-daemon messages for non-launch commands that name a launch command rather than `serve`; version-mismatch behavior after upgrades; Unix socket/process-group security; output, completed-record, and daemon-log bounds; daemon-restart data loss; unsupported interactive programs and `FORCE_COLOR=1` for colored attached output; supported platforms; troubleshooting; removal of stale template directions. Explain that the next milestone adds one strict `hum.yaml` for precise definitions, conservative zero-config discovery of one conventional `dev` task when YAML is absent, resolved-definition `start` and `up` that wait for readiness by default plus `down` as their inverse, `hum init` scaffolding, visible readiness in `status` and `list`, a shell-only skill, and a stdio MCP adapter that reaches application services only through the daemon protocol client.

Non-goals: manifest, discovery, down, init, skill, or MCP implementation; PTY or arbitrary interactive input; Windows; auth; web UI; runtime configuration files; persistence; plugins; OS-service installation; launchd; systemd; login startup; repository abstractions; or DI frameworks.

Modified-file contract: README.md, docs/design.md, Taskfile.dist.yaml, .github/workflows/ci.yaml, template-only root configuration files justified in Implementation Notes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task ci` exits 0 and runs formatting, unit/integration tests, race-sensitive checks where configured, static analysis, and built-binary smoke coverage.
- [x] #2 Fresh-clone instructions build hum and every documented serve/run/list/status/logs/wait/restart/stop/shutdown example is exercised against a temporary runtime directory with the stated result.
- [x] #3 `go list -deps ./...` and dependency inspection show no MCP server/package/dependency, manifest loader, PTY, YAML loader, web, authentication, persistence, plugin, repository, or DI framework.
- [x] #4 `go test ./internal/cli -run TestLifecycleHelp` exits 0 and proves root and command help distinguish foreground/detached daemon execution, attached/detached process startup, read-only log following, restart, stopping one managed process, and both daemon shutdown modes.
- [x] #5 `README.md`, `docs/design.md`, and CLI help clearly distinguish foreground/detached daemon execution, attached/detached process startup, read-only following, restart, process stop, and daemon shutdown; they also document auto-start, no-daemon messages that name a launch command, version-mismatch handling, client environment inheritance, `wait` defaults and exit codes, macOS/Linux support, private socket permissions, process-group signals, output/completed-record/log bounds, daemon-restart data loss, and unsupported interactive input with the `FORCE_COLOR=1` workaround.
- [x] #6 `docs/design.md` names future strict hum.yaml definitions, conservative zero-config dev discovery, `down`, `hum init`, visible readiness, shell-only skill, and stdio/Streamable HTTP MCP transports; it maps resolved-definition start/up/down plus list/status/logs/wait/restart/stop through the existing daemon protocol client without defining a second supervisor.
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
1. Expand Taskfile.dist.yaml so task ci explicitly runs formatting/static analysis, all tests, race-sensitive packages, and built-binary integration smoke coverage while retaining the dual-platform workflow.
2. Replace the stale README with fresh-clone build/install guidance and an executable temporary-runtime transcript covering every implemented lifecycle command and documented operator behavior.
3. Tighten docs/design.md so the implemented supervisor foundation and future resolved-definition/discovery/MCP milestones are unambiguous, preserving the single daemon-protocol client boundary.
4. Improve CLI lifecycle help and add TestLifecycleHelp coverage in internal/cli/surface_test.go; justify this modified-file deviation because AC#4 and AC#5 explicitly require tested CLI help.
5. Run every acceptance command plus task ci, obtain independent verifier and adversarial review passes, fix findings, commit, merge to main, rerun task ci, record final evidence, and remove the worktree and branch.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1 evidence: `task ci` exited 0 on implementation commit 0e3805b and merged-main commit d354bb5. The gate ran gofmt verification, go vet, staticcheck, `go test ./...` (including the comprehensive built-binary integration suite), race tests for internal/output, internal/process, internal/app, and internal/daemon, `task cli:build`, and `go test ./cmd/hum -run TestBuiltBinaryIntegration -count=1` dedicated smoke coverage.
AC#2 evidence: `mise install && mise exec task -- task init && mise exec task -- task cli:build` exited 0 and built bin/hum. A temporary-runtime binary transcript exercised foreground/detached serve, attached/detached run, list/--all, status, bounded logs, human and NDJSON follow, wait outcomes/codes 0/2/3/1, restart, idempotent multi-name stop, shutdown refusal, and forced shutdown; every asserted result passed.
AC#3 evidence: `mise exec go -- go list -deps ./...` exited 0 and resolved only the standard library, hum packages, and github.com/urfave/cli/v3; no deferred MCP, manifest/YAML, PTY, web, auth, persistence, plugin, repository, or DI package is present.
AC#4 evidence: `mise exec go -- go test ./internal/cli -run TestLifecycleHelp -count=1` exited 0; rendered root and command help proved every required lifecycle distinction, including detached-only run JSON.
AC#5 evidence: `mise exec go -- go test ./internal/cli -run 'TestLifecycleHelp|TestWaitCLIValidation|TestAttachedRun|TestRestart' -count=1` exited 0; the temporary-runtime transcript passed all documented lifecycle outcomes, and final LSP diagnostics reported no issues.
AC#6 evidence: `git diff --check` exited 0 after docs/design.md marked strict hum.yaml, zero-config discovery, down, init, visible readiness, shell skill, and stdio/Streamable HTTP MCP as future and required reuse of the existing daemon protocol client without a second supervisor; ReleaseVerifier returned PASS.
Modified-file deviation: internal/cli/commands.go, root.go, surface_test.go, wait_test.go, serve_run_test.go, and restart_test.go are intentionally changed because AC#4 explicitly requires lifecycle help behavior and TestLifecycleHelp, while adversarial review required regression coverage for empty wait matches, attached run --json raw output, and multi-name restart fail-fast semantics. No test was deleted, skipped, or weakened. The task has the tooling label, authorizing Taskfile.dist.yaml gate changes; .github/workflows/ci.yaml already had macOS/Linux task ci jobs and did not need modification.
Independent verification: ReleaseVerifier returned PASS for AC#1 through AC#6 and all DoD checks. Final adversarial review: ReleaseReviewer returned PASS with no findings after all corrections.
Delivery: implementation commit 0e3805b merged to main as d354bb5; `task ci` passed on both commits.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Completed the supervisor-foundation release gate and operator documentation. `task ci` now makes formatting/static analysis, all tests, configured race packages, an explicit hum build, and built-binary smoke coverage visible; README/design and tested CLI help cover the implemented lifecycle and clearly isolate future resolved-process/MCP work. Fresh-clone build, the full temporary-runtime lifecycle transcript, focused help/regression tests, final branch and merged-main `task ci`, ReleaseVerifier AC#1-#6 PASS, and ReleaseReviewer PASS all completed. Implementation 0e3805b; merged as d354bb5.
<!-- SECTION:FINAL_SUMMARY:END -->
