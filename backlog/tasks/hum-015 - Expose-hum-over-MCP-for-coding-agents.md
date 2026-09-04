---
id: HUM-015
title: Expose the project process lifecycle over MCP
status: Done
assignee:
  - '@brett'
created_date: '2026-09-02 20:13'
updated_date: '2026-09-04 01:16'
labels:
  - cli
  - protocol
  - docs
milestone: m-1
dependencies:
  - HUM-016
  - HUM-018
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
Outcome: an MCP-capable coding agent can bring up resolved development processes and observe, wait on, restart, stop, or bring down any existing project-managed process, including an ad hoc process the user launched through `hum run`, without opening a shell or knowing its underlying command.

Scope: a stdio MCP server command, `hum mcp`, exposing `start`, `up`, `down`, `list`, `status`, `logs`, `wait`, `restart`, and `stop`. Every tool requires an absolute existing `project_root` directory, resolved through the same nearest-Git-root-or-cwd-fallback rules as the CLI. Only `start` and `up` require names from explicit or discovered project definitions; they wait for readiness by default, accept the same no-wait and timeout inputs, and return the same states, source metadata, retained per-incarnation readiness outcomes, cursors, collision errors, and aggregate semantics as the CLI. Discovered definitions without readiness return `running_unverified` and are never reported ready. `list` and `status` carry the CLI `readiness` field.

With a live daemon, `list` merges resolved definitions with every runtime record in that project, labelling records created by raw CLI `hum run` as `ad_hoc`. `status`, `logs`, `wait`, and `stop` accept any existing runtime record name in the requested project, whether resolved or ad hoc; `down` stops every running record in the project with the CLI per-name results. `restart` resolves a declared or discovered name from the current project definition and uses the MCP server current environment; when no definition exists but a retained ad hoc record does, it relaunches that record exact argv, cwd, and recorded environment under the same name using the HUM-012 semantics. An evicted ad hoc record is not reconstructible and returns not found. Logs and wait expose bounded cursor-based inputs and outputs, wait defaults its cursor to the current launch cursor and its timeout to the CLI default, and no MCP response exposes a recorded environment.

The MCP adapter parses schemas and maps MCP errors, but every daemon interaction goes through the same internal daemon protocol client and automatic-start/version-replacement helper as the CLI. Only start/up may create a daemon; list resolves the project locally and reports definitions as stopped when no daemon exists; status/logs/wait/restart preserve the CLI unavailable-daemon errors and stop/down succeed with nothing running. The adapter never constructs `internal/app` services, process supervisors, or output stores in-process. Tool schemas and descriptions explain the resolved-launch versus existing-record boundary without relying on the shell skill. Document one-time registration with common coding agents, the required project-root argument, ad hoc CLI handoff, and the fact that daemon shutdown loses ad hoc definitions.

Non-goals: arbitrary-command MCP `run`, `init`, recreating an evicted ad hoc command, daemon `serve` or `shutdown` tools, Streamable HTTP transport, authentication, remote access, per-agent configuration mutation, manifest generation, shell execution, committed environment values, or any second supervisor core.

Modified-file contract: internal/mcp/, internal/cli/, integration/, internal/testutil/, go.mod, go.sum, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go list -deps ./internal/mcp | rg '/internal/(app|process|output)$'` exits 1 and `go test ./internal/mcp -run TestNoInProcessSupervisor -count=1` exits 0, proving MCP uses the daemon client rather than constructing a second supervisor.
- [x] #2 `go test ./integration -run TestMCPResolvedAndAdHocLifecycle -count=1` exits 0 and proves a stdio client can start explicit YAML and zero-config definitions, then discover and operate on an ad hoc process launched as `hum run transient --detach -- <fixture argv>`: bounded logs, status, wait, restart with unchanged argv, and stop all match corresponding CLI JSON while daemon restart makes the ad hoc definition unavailable.
- [x] #3 `go test ./internal/cli -run TestMCPHelp -count=1` exits 0 and proves registration documentation/help names stdio transport, one-time agent registration, required project_root on every tool, the resolved start/up versus existing-record control boundary, ad hoc CLI handoff and daemon-loss limitation, deterministic argv-based environment activation for explicit definitions, and the absence of run/serve/shutdown MCP tools.
- [x] #4 `go test ./internal/mcp -run 'TestToolSchemas|TestToolValidation|TestErrorMapping' -count=1` exits 0 and proves the exact nine tools, absolute existing project_root validation, resolved-name validation only for start/up, existing-record validation for status/logs/wait/restart/stop, bounded logs/wait inputs with launch-cursor and timeout defaults, stable source-bearing result shapes including `readiness`, and typed unavailable/not-found errors.
- [x] #5 `go test ./internal/mcp -run 'TestStartUp|TestDown|TestObservationTools|TestAdHocProcessTools' -count=1` exits 0 with an injected daemon client and proves start/up auto-start, wait by default, and accept only resolved definitions, down stops every project record and returns per-name results, list merges resolved and ad hoc records, status/logs/wait/stop accept either record kind, resolved restart uses the current definition and server environment, retained ad hoc restart uses recorded argv/cwd/environment, evicted ad hoc restart is not found, and observation/control tools never create a daemon or expose recorded environment.
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
1. Add an internal/mcp stdio server with the exact nine typed tools, schema validation, stable structured results, MCP error mapping, and an injected protocol-shaped lifecycle backend that imports no supervisor packages.
2. Wire hum mcp in internal/cli through an adapter over the existing daemon.Client and daemon auto-start/version-replacement helpers, preserving resolved-definition versus existing-record semantics and ad hoc restart retention.
3. Add focused internal/mcp, internal/cli, and integration coverage for schemas, lifecycle semantics, stdio interoperability, and the no-supervisor dependency boundary.
4. Document agent registration, required absolute project_root, lifecycle boundaries, environment activation, and daemon-loss limits in README.md and docs/design.md.
5. Run each acceptance command, task ci, independent verification, record evidence, commit, merge to main, and clean the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1 PASS: `go list -deps ./internal/mcp | rg '/internal/(app|process|output)$'` exited 1 with no matches; `go test ./internal/mcp -run TestNoInProcessSupervisor -count=1` passed.
AC#2 PASS: `go test ./integration -run TestMCPResolvedAndAdHocLifecycle -count=1` passed, covering explicit, zero-config, and ad hoc lifecycle parity plus daemon-restart loss.
AC#3 PASS: `go test ./internal/cli -run TestMCPHelp -count=1` passed with the required registration, boundary, environment, and non-tool documentation.
AC#4 PASS: `go test ./internal/mcp -run 'TestToolSchemas|TestToolValidation|TestErrorMapping' -count=1` passed for nine schemas, validation, stable result metadata, and typed errors.
AC#5 PASS: `go test ./internal/mcp -run 'TestStartUp|TestDown|TestObservationTools|TestAdHocProcessTools' -count=1` passed for injected-client lifecycle behavior, readiness outcomes, ad hoc retention, and no-daemon controls.
Final commit 8fdf637: `task ci` passed after commit. `task check:staged` passed before commit. Independent verifier returned PASS for AC#1-AC#5; adversarial reviewer returned PASS with no remaining actionable defect. Diff is confined to README.md, docs/design.md, integration/, internal/cli/, and internal/mcp/; no tests were deleted, skipped, or weakened and no protected gate file changed.

Post-completion contract audit: commit 3854d38 adds all serialized terminal process fields (`exit`, `exit_code`, `exited_at`) to MCP output schemas and preserves the readiness expression recorded for the current process incarnation, avoiding waits on a changed manifest expression. Regression tests cover exited-process schema compatibility and the changed-match race. AC#4, AC#5, integration, `task ci`, and `task check:staged` passed; adversarial correction review returned PASS.

Final correction commit 3854d38: `task ci` passed after commit.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented `hum mcp` with nine daemon-backed stdio lifecycle tools, stable schemas and validation, per-incarnation readiness behavior, explicit/zero-config/ad hoc integration coverage, and registration/design documentation. Verified the final implementation and contract correction with every acceptance command, `task ci`, `task check:staged`, independent AC#1-AC#5 verification, and adversarial review.
<!-- SECTION:FINAL_SUMMARY:END -->
