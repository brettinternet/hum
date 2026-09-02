---
id: DEVPROC-010
title: Finish release gates and operator documentation
status: To Do
assignee: []
created_date: '2026-09-02 17:07'
updated_date: '2026-09-02 20:05'
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

Scope: installation and ldflags build examples; architecture and application-service boundary; complete CLI help/examples; ordinary attached `run` workflow and its automatic daemon startup; foreground versus detached `serve`; attached versus detached process startup; read-only `logs --follow`; stopping one managed process versus shutting down the daemon; default shutdown refusal and `--stop-processes`; JSON/cursor and NDJSON-follow usage for agents; unavailable-daemon guidance for non-run commands; Unix socket/process-group security; output and daemon-log bounds; daemon-restart data loss; unsupported interactive programs; supported platforms; troubleshooting; removal of stale template directions. Explain how a future stdio or Streamable HTTP MCP server delegates to the existing application services.

Non-goals: MCP implementation or dependency, PTY or arbitrary interactive input, Windows, auth, web UI, configuration files, persistence, plugins, OS-service installation, launchd, systemd, login startup, repository abstractions, or DI frameworks.

Modified-file contract: README.md, docs/design.md, Taskfile.dist.yaml, .github/workflows/ci.yaml, template-only root configuration files justified in Implementation Notes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task ci` exits 0 and runs formatting, unit/integration tests, race-sensitive checks where configured, static analysis, and built-binary smoke coverage.
- [ ] #2 Fresh-clone instructions build devproc and every documented serve/run/list/status/logs/wait/stop/shutdown example is exercised against a temporary runtime directory with the stated result.
- [ ] #3 `go list -deps ./...` and dependency inspection show no MCP server/package/dependency, PTY, YAML loader, web, authentication, persistence, plugin, repository, or DI framework.
- [ ] #4 `README.md`, `docs/design.md`, and CLI help clearly distinguish foreground/detached daemon execution, attached/detached process startup, read-only following, process stop, and daemon shutdown; they also document auto-start, unavailable-daemon guidance, macOS/Linux support, private socket permissions, process-group signals, bounded output/logging, daemon-restart data loss, and unsupported interactive input.
- [ ] #5 `docs/design.md` names stdio and Streamable HTTP as future MCP transports and maps list/status/logs/wait/stop directly to internal/app services without defining a second core.
- [ ] #6 `go test ./internal/cli -run TestLifecycleHelp` exits 0 and proves root and command help distinguish foreground/detached daemon execution, attached/detached process startup, read-only log following, stopping one managed process, and both daemon shutdown modes.
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
