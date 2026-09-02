---
id: DEVPROC-015
title: Expose the declared process lifecycle over MCP
status: To Do
assignee: []
created_date: '2026-09-02 20:13'
updated_date: '2026-09-02 20:29'
labels:
  - cli
  - protocol
  - docs
milestone: m-1
dependencies:
  - DEVPROC-014
modified_files:
  - internal/mcp/
  - internal/cli/
  - integration/
  - internal/testutil/
  - go.mod
  - go.sum
  - README.md
  - docs/design.md
priority: high
type: feature
ordinal: 1200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: an MCP-capable coding agent can bring up, observe, wait on, restart, and stop declared development processes through typed tools, without opening a shell or knowing any underlying command.

Scope: a stdio MCP server command, `hum mcp`, exposing `start`, `up`, `list`, `status`, `logs`, `wait`, `restart`, and `stop`. Every tool requires an absolute existing `project_root` directory, which resolves through the same nearest-Git-root-or-cwd-fallback rules as the CLI; process tools also require a manifest process name. Start/up accept the same wait and timeout inputs and return the same per-process states, retained per-incarnation readiness outcomes, cursors, bounds, collision errors, and aggregate semantics as the CLI. Logs and wait expose bounded cursor-based inputs and outputs. The stdio server's current environment is sent on start/up and manifest-driven restart; documentation explains that cwd does not activate shell hooks and deterministic project activation belongs in manifest argv.

The MCP adapter parses schemas and maps MCP errors, but every daemon interaction goes through the same internal daemon protocol client and automatic-start/version-replacement helper as the CLI. Only start/up may create a daemon; list reads `hum.json` locally and reports declarations as stopped when no daemon exists; the remaining tools preserve unavailable-daemon errors. The adapter never constructs `internal/app` services, process supervisors, or output stores in-process. Tool schemas and descriptions are sufficient for operation without the shell skill. Document one-time registration with common coding agents and the required project-root argument.

Non-goals: arbitrary-command `run`, daemon `serve` or `shutdown` tools, Streamable HTTP transport, authentication, remote access, per-agent configuration mutation, manifest generation, shell execution, committed environment values, or any second supervisor core.

Modified-file contract: internal/mcp/, internal/cli/, integration/, internal/testutil/, go.mod, go.sum, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/mcp -run 'TestToolSchemas|TestToolValidation|TestErrorMapping'` exits 0 and proves the exact eight tools, absolute existing project_root validation with CLI-equivalent root discovery, manifest-name validation, bounded logs/wait inputs, stable CLI-equivalent result shapes, and typed MCP error mapping.
- [ ] #2 `go test ./internal/mcp -run 'TestStartUp|TestObservationTools'` exits 0 with an injected daemon protocol client and proves start/up perform automatic daemon startup and pass server environment, all process operations use the resolved project root, readiness survives output eviction without crossing incarnations, list works from the manifest without a daemon, and status/logs/wait/restart/stop never create one.
- [ ] #3 `go list -deps ./internal/mcp | rg '/internal/(app|process|output)$'` exits 1 and `go test ./internal/mcp -run TestNoInProcessSupervisor` exits 0, proving MCP uses the daemon client rather than constructing a second supervisor.
- [ ] #4 `go test ./integration -run TestMCPManifestLifecycle -count=1` exits 0 and proves a stdio client can start a declared process from no daemon, wait for retained current-incarnation readiness, read bounded cursor logs, restart with the server environment, stop it, and receive responses equivalent to the corresponding CLI JSON.
- [ ] #5 `go test ./internal/cli -run TestMCPHelp` exits 0 and proves registration documentation/help names stdio transport, one-time agent registration, required project_root on every tool, deterministic argv-based environment activation, and the absence of run/serve/shutdown tools.
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
