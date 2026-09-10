---
id: HUM-062
title: Stop implicit Mix execution and make discovery cancellable
status: To Do
assignee: []
created_date: '2026-09-10 01:49'
labels: []
dependencies: []
modified_files:
  - internal/project/resolver.go
  - internal/project/resolver_test.go
  - internal/cli/manifest.go
  - internal/cli/mcp.go
  - internal/cli/mcp_test.go
  - internal/mcp/server.go
  - internal/mcp/server_test.go
  - docs/design.md
priority: high
type: bug
ordinal: 38700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Implicit project discovery never evaluates repository `mix.exs`, and cancellation reaches every remaining command-backed discovery operation. Closing or cancelling `hum mcp` cannot be held open by a discovery subprocess. Evidence: `detectMix` runs `mix help --names`, and a controlled `mix.exs` created a sentinel during `hum list`. `mcpResolver.Resolve` discards its context, `runDiscoveryCommand` starts from `context.Background()`, and a blocking discovery command kept `hum mcp` alive for 8.1 seconds after EOF despite the documented two-second bound. Scope: replace Mix command introspection with fail-closed static detection or require an explicit manifest; thread context through manifest resolution and command execution; enforce one absolute MCP shutdown deadline; document the trust boundary. Non-goals: do not change explicit `hum.yaml`, process launch semantics, or the Task/mise discovery policy beyond making existing subprocesses cancellable.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise exec go -- go test -race ./internal/project ./internal/cli ./internal/mcp` exits 0.
- [ ] #2 A focused resolver/CLI regression command using a `mix.exs` that writes a sentinel exits 0 and leaves the sentinel absent for implicit list, status, completion, and init discovery paths.
- [ ] #3 A focused MCP regression command starts a blocking discovery subprocess, closes stdin or cancels the request, and exits 0 after proving `Serve` returns within two seconds and the subprocess is reaped.
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
