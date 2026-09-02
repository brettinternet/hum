---
id: DEVPROC-010
title: Finish release gates and operator documentation
status: To Do
assignee: []
created_date: '2026-09-02 17:07'
labels:
  - docs
  - tooling
  - integration
milestone: m-0
dependencies:
  - DEVPROC-008
  - DEVPROC-009
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
Outcome: the initial devproc release has one green macOS/Linux gate and concise documentation that a human or coding agent can use without learning internal protocol details.

Scope: installation and ldflags build examples; architecture and application-service boundary; complete CLI examples; JSON/cursor usage for agents; Unix socket and process-group security model; output bounds; daemon lifecycle; interactive-program limitation; supported platforms; troubleshooting; final removal of stale template web/client/Compose directions. Add a short design note explaining how a future stdio or Streamable HTTP MCP server would expose list, status, logs, wait, and stop by delegating to the existing application services.

Non-goals: MCP implementation or dependency, PTY, Windows, auth, web UI, configuration files, persistence, plugins, repository/DI abstractions, or new product behavior.

Modified-file contract: README.md, docs/design.md, Taskfile.dist.yaml, .github/workflows/ci.yaml, template-only root configuration files justified in Implementation Notes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task ci` exits 0 and runs formatting, unit/integration tests, race-sensitive checks where configured, static analysis, and built-binary smoke coverage.
- [ ] #2 Fresh-clone instructions build devproc and every documented serve/run/list/status/logs/wait/stop example is exercised against a temporary runtime directory with the stated result.
- [ ] #3 `go list -deps ./...` and dependency inspection show no MCP server/package/dependency, PTY, YAML loader, web, authentication, persistence, plugin, repository, or DI framework.
- [ ] #4 `README.md` and `docs/design.md` explicitly document macOS/Linux support, private socket permissions, process-group stop behavior, bounded in-memory output, daemon-restart data loss, and unsupported interactive programs.
- [ ] #5 `docs/design.md` names stdio and Streamable HTTP as future MCP transports and maps list/status/logs/wait/stop directly to internal/app services without defining a second core.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
- [ ] #7 Committed on main with the task ID in the commit subject and a Task: trailer
<!-- DOD:END -->
