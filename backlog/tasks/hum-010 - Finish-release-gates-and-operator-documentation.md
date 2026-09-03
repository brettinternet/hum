---
id: HUM-010
title: Finish release gates and operator documentation
status: To Do
assignee: []
created_date: '2026-09-02 17:07'
updated_date: '2026-09-03 03:18'
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
- [ ] #1 `task ci` exits 0 and runs formatting, unit/integration tests, race-sensitive checks where configured, static analysis, and built-binary smoke coverage.
- [ ] #2 Fresh-clone instructions build hum and every documented serve/run/list/status/logs/wait/restart/stop/shutdown example is exercised against a temporary runtime directory with the stated result.
- [ ] #3 `go list -deps ./...` and dependency inspection show no MCP server/package/dependency, manifest loader, PTY, YAML loader, web, authentication, persistence, plugin, repository, or DI framework.
- [ ] #4 `go test ./internal/cli -run TestLifecycleHelp` exits 0 and proves root and command help distinguish foreground/detached daemon execution, attached/detached process startup, read-only log following, restart, stopping one managed process, and both daemon shutdown modes.
- [ ] #5 `README.md`, `docs/design.md`, and CLI help clearly distinguish foreground/detached daemon execution, attached/detached process startup, read-only following, restart, process stop, and daemon shutdown; they also document auto-start, no-daemon messages that name a launch command, version-mismatch handling, client environment inheritance, `wait` defaults and exit codes, macOS/Linux support, private socket permissions, process-group signals, output/completed-record/log bounds, daemon-restart data loss, and unsupported interactive input with the `FORCE_COLOR=1` workaround.
- [ ] #6 `docs/design.md` names future strict hum.yaml definitions, conservative zero-config dev discovery, `down`, `hum init`, visible readiness, shell-only skill, and stdio/Streamable HTTP MCP transports; it maps resolved-definition start/up/down plus list/status/logs/wait/restart/stop through the existing daemon protocol client without defining a second supervisor.
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
