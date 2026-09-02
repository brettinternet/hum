---
id: DEVPROC-015
title: Expose devproc over MCP for coding agents
status: To Do
assignee: []
created_date: '2026-09-02 20:13'
labels:
  - cli
  - protocol
  - docs
milestone: m-1
dependencies:
  - DEVPROC-010
modified_files:
  - internal/mcp/
  - internal/cli/
  - go.mod
  - go.sum
  - README.md
  - docs/design.md
priority: medium
type: feature
ordinal: 1200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: coding agents that speak MCP can list, inspect, read, wait on, restart, and stop devproc processes through typed tools that delegate to the existing application services.

Scope: a stdio MCP server command (`devproc mcp`) exposing list, status, logs, wait, restart, and stop tools with JSON schemas mirroring the CLI JSON output and bounds; transport translation, schemas, and MCP-specific error mapping only; project scoping from the server's cwd; documentation for registering the server with common coding agents. Update docs/design.md and README.md.

Non-goals: Streamable HTTP transport (follow-up), starting the daemon or processes from MCP, `run` or `serve` tools, authentication, or any second core.

Modified-file contract: internal/mcp/, internal/cli/, go.mod, go.sum, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/mcp` exits 0 and proves each tool delegates to internal/app services, applies the same bounds as the CLI, and maps typed errors to MCP errors.
- [ ] #2 With a daemon and fixture process running, an MCP client session over stdio lists the tools, calls logs with a cursor, and receives bounded output identical to `devproc logs --json`.
- [ ] #3 `go list -deps ./internal/app ./internal/daemon ./internal/output ./internal/process` contains no MCP package, proving the adapter stays at the edge.
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
