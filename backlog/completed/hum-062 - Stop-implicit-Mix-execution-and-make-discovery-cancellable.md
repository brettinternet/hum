---
id: HUM-062
title: Stop implicit Mix execution and make discovery cancellable
status: Done
assignee: []
created_date: '2026-09-10 01:49'
updated_date: '2026-09-10 06:36'
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
- [x] #1 `mise exec go -- go test -race ./internal/project ./internal/mcp && mise exec go -- go test ./internal/cli` exits 0.
- [x] #2 `mise exec go -- go test ./internal/project ./internal/cli -run "Test.*MixDiscoveryDoesNotExecuteProjectCode" -count=1` exits 0 and its sentinel assertions prove implicit list, status, completion, and init paths did not evaluate `mix.exs`.
- [x] #3 `mise exec go -- go test ./internal/mcp ./internal/cli -run "Test.*DiscoveryCancellation" -count=1` exits 0 after proving MCP EOF/request cancellation returns within two seconds and reaps the blocked discovery subprocess.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1 PASS — `mise exec go -- go test -race ./internal/project ./internal/mcp && mise exec go -- go test ./internal/cli` exited 0.
AC#2 PASS — `mise exec go -- go test ./internal/project ./internal/cli -run "Test.*MixDiscoveryDoesNotExecuteProjectCode" -count=1` exited 0; sentinel assertions covered list, status, completion, and init.
AC#3 PASS — `mise exec go -- go test ./internal/mcp ./internal/cli -run "Test.*DiscoveryCancellation" -count=1` exited 0; tests covered MCP EOF/request cancellation under two seconds and subprocess reaping.
DoD evidence — independent verifier returned PASS for AC#1–#3; `task ci` exited 0; no tests were deleted, skipped, or weakened; no protected gate files changed. Scope deviation: `integration/zero_config_test.go` was updated because the existing required `task ci` fixture encoded the removed `mix help --names` introspection contract; it now declares a literal Phoenix dependency and rejects discovery-time Mix execution.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: confirmed. `detectMix` (internal/project/resolver.go:1017) runs `mix help --names`, which evaluates `mix.exs`; `runDiscoveryCommand` (:251) derives from `context.Background()`; `mcpResolver.Resolve` (internal/cli/mcp.go:58) discards ctx. Other sources verified non-executing so the Mix-only scope is justified: `task --dir X --list-all --json` did not evaluate a `sh:` var sentinel, just is called with `--dump` (no backtick evaluation), and mise config is trust-gated. docs/design.md:664 and docs/coding-agents.md:32 document the two-second MCP shutdown bound referenced by AC#3.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Replaced Mix command introspection with conservative static Phoenix dependency detection, propagated MCP cancellation into command-backed discovery, and bounded all MCP shutdown joins by one deadline. Added sentinel, cancellation, subprocess-reaping, and integration coverage; all acceptance commands and task ci pass.
<!-- SECTION:FINAL_SUMMARY:END -->
