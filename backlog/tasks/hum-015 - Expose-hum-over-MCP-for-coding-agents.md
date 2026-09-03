---
id: HUM-015
title: Expose the project process lifecycle over MCP
status: To Do
assignee: []
created_date: '2026-09-02 20:13'
updated_date: '2026-09-03 02:51'
labels:
  - cli
  - protocol
  - docs
milestone: m-1
dependencies:
  - HUM-016
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
Outcome: an MCP-capable coding agent can bring up resolved development processes and observe, wait on, restart, or stop any existing project-managed process, including an ad hoc process the user launched through `hum run`, without opening a shell or knowing its underlying command.

Scope: a stdio MCP server command, `hum mcp`, exposing `start`, `up`, `list`, `status`, `logs`, `wait`, `restart`, and `stop`. Every tool requires an absolute existing `project_root` directory, resolved through the same nearest-Git-root-or-cwd-fallback rules as the CLI. Only `start` and `up` require names from explicit or discovered project definitions; they accept the same wait and timeout inputs and return the same states, source metadata, retained per-incarnation readiness outcomes, cursors, collision errors, and aggregate semantics as the CLI. Discovered definitions without readiness return `running_unverified` and are never reported ready.

With a live daemon, `list` merges resolved definitions with every runtime record in that project, labelling records created by raw CLI `hum run` as `ad_hoc`. `status`, `logs`, `wait`, and `stop` accept any existing runtime record name in the requested project, whether resolved or ad hoc. `restart` resolves a declared or discovered name from the current project definition and uses the MCP server's current environment; when no definition exists but a retained ad hoc record does, it relaunches that record's exact argv, cwd, and recorded environment under the same name using the HUM-012 semantics. An evicted ad hoc record is not reconstructible and returns not found. Logs and wait expose bounded cursor-based inputs and outputs, and no MCP response exposes a recorded environment.

The MCP adapter parses schemas and maps MCP errors, but every daemon interaction goes through the same internal daemon protocol client and automatic-start/version-replacement helper as the CLI. Only start/up may create a daemon; list resolves the project locally and reports definitions as stopped when no daemon exists; status/logs/wait/restart/stop preserve unavailable-daemon errors. The adapter never constructs `internal/app` services, process supervisors, or output stores in-process. Tool schemas and descriptions explain the resolved-launch versus existing-record boundary without relying on the shell skill. Document one-time registration with common coding agents, the required project-root argument, ad hoc CLI handoff, and the fact that daemon shutdown loses ad hoc definitions.

Non-goals: arbitrary-command MCP `run`, recreating an evicted ad hoc command, daemon `serve` or `shutdown` tools, Streamable HTTP transport, authentication, remote access, per-agent configuration mutation, manifest generation, shell execution, committed environment values, or any second supervisor core.

Modified-file contract: internal/mcp/, internal/cli/, integration/, internal/testutil/, go.mod, go.sum, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/mcp -run 'TestToolSchemas|TestToolValidation|TestErrorMapping' -count=1` exits 0 and proves the exact eight tools, absolute existing project_root validation, resolved-name validation only for start/up, existing-record validation for status/logs/wait/restart/stop, bounded logs/wait inputs, stable source-bearing result shapes, and typed unavailable/not-found errors.
- [ ] #2 `go test ./internal/mcp -run 'TestStartUp|TestObservationTools|TestAdHocProcessTools' -count=1` exits 0 with an injected daemon client and proves start/up auto-start and accept only resolved definitions, list merges resolved and ad hoc records, status/logs/wait/stop accept either record kind, resolved restart uses the current definition and server environment, retained ad hoc restart uses recorded argv/cwd/environment, evicted ad hoc restart is not found, and observation/control tools never create a daemon or expose recorded environment.
- [ ] #3 `go list -deps ./internal/mcp | rg '/internal/(app|process|output)$'` exits 1 and `go test ./internal/mcp -run TestNoInProcessSupervisor -count=1` exits 0, proving MCP uses the daemon client rather than constructing a second supervisor.
- [ ] #4 `go test ./integration -run TestMCPResolvedAndAdHocLifecycle -count=1` exits 0 and proves a stdio client can start explicit YAML and zero-config definitions, then discover and operate on an ad hoc process launched as `hum run transient --detach -- <fixture argv>`: bounded logs, status, wait, restart with unchanged argv, and stop all match corresponding CLI JSON while daemon restart makes the ad hoc definition unavailable.
- [ ] #5 `go test ./internal/cli -run TestMCPHelp -count=1` exits 0 and proves registration documentation/help names stdio transport, one-time agent registration, required project_root on every tool, the resolved start/up versus existing-record control boundary, ad hoc CLI handoff and daemon-loss limitation, deterministic argv-based environment activation for explicit definitions, and the absence of run/serve/shutdown MCP tools.
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
