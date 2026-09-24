---
id: HUM-125
title: Stop dependents before prerequisites in down through one shared implementation
status: Done
assignee: []
created_date: '2026-09-23 21:21'
updated_date: '2026-09-24 12:39'
labels:
  - cli
  - mcp
  - process
  - reviewed
milestone: m-3
dependencies: []
modified_files:
  - internal/orchestrate/orchestrate.go
  - internal/orchestrate/*_test.go
  - internal/cli/commands.go
  - internal/cli/*_test.go
  - internal/mcp/tools.go
  - internal/mcp/*_test.go
  - integration/down_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: enhancement
ordinal: 14000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum down` and MCP `down` stop declared processes in reverse `after` order, so a prerequisite is stopped only after every active dependent stop request has completed, while unrelated processes and ad-hoc or undeclared records stop concurrently. CLI and MCP share one implementation. Today the adapters disagree: CLI `downCommand` (internal/cli/commands.go) stops everything concurrently, MCP `down` (internal/mcp/tools.go) stops sequentially in lexical order, and docs/design.md says "`down` remains concurrent rather than reverse ordered". Stopping a database at the same moment as its API fills the API retained output with connection errors, which is exactly what agents read next.

Scope:
- internal/orchestrate computes stop waves from the selected manifest `after` graph restricted to active records and runs each wave concurrently through an injected stop operation. Both adapters call it.
- A failed dependent stop does not block its prerequisites; each name still reports its own result.
- Results stay one per name in lexical order with the existing statuses and shapes. A missing manifest yields an empty graph (everything concurrent), matching current CLI behavior; invalid-manifest behavior is unchanged.
- Update the `down` help text, the MCP `down` description, README, docs/design.md, and docs/coding-agents.md where they describe down ordering.

Non-goals: ordering for `stop NAME...`, `shutdown --stop-processes`, or `remove --all`; new flags; per-process stop timeouts beyond existing `stop_grace`; result shape changes.

Implementation context (commit 465b774):
- CLI: downCommand internal/cli/commands.go:2380-2509 lists records and merges manifest declarations (mergeManifestProcesses). It then starts one goroutine per active name, each on its own daemon connection (daemonClient inside the worker), and a start barrier releases all stops together. Results are stopResult{Name, Status, Message} with the statuses stopped, not_running, and error. A name is stopped when app.IsActiveState or processNeedsRestartControl is true. `hum --global down` loads no manifest (:2416-2421), so its graph is empty and all stops form one wave.
- MCP: Server.down internal/mcp/tools.go:1686-1724 uses one client and stops names sequentially in lexical order. Results are stopResult{Name, State, Error}. Its predicate also stops records in State "starting". Keep each adapter's current predicate and result type; this task changes stop order only.
- Concurrency: daemon.Client serializes requests on one connection (internal/daemon/client.go:29 `mu`), and a stop request blocks until the process exits. Stops within one wave therefore need one connection each, as the CLI already does. MCP obtains connections from s.opts.ClientFactory (tools.go:651).
- Shared code: put the wave computation and a runner that stops each wave concurrently through an injected `stop(ctx, name) error` in internal/orchestrate. The graph covers declared definitions only; ad-hoc and undeclared records have no edges. A prerequisite waits only for its active dependents, so a prerequisite whose dependents are all inactive stops in the first wave.
- Docs and help to update: docs/design.md:393 ("`down` remains concurrent rather than reverse ordered."), the down help at commands.go:160-172 ("concurrently"), and the MCP down description at tools.go:486.

Existing tests that must keep passing: TestDownStopsProcessesConcurrentlyWithIndependentConnections (internal/cli/down_test.go:286); it has no `after`, so it still runs as one concurrent wave. Also the other tests in internal/cli/down_test.go, TestDown (internal/mcp/tools_test.go:1702), and TestDownWorkflow (integration/down_test.go:52). The helpers newDownTestChild (down_test.go:351, which has delay and stopErr knobs), downTestSupervisor (:385), downTestServer (:406), and downStartProcess (:436) can drive CLI ordering tests. For AC3, order lifecycle `exit` events by cursor, not by timestamp.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `go test ./internal/orchestrate -run "^TestDownOrder$" -count=1 -v` exits 0; for db <- api <- web plus independent worker and one ad-hoc record, the first wave is {adhoc, web, worker}, then api, then db; in a diamond a prerequisite waits for all dependents; inactive records are skipped without delaying later waves; a dependent stop error still lets its prerequisite stop.
- [x] #2 AC2 — `go test ./internal/cli -run "^TestDownStopsDependentsFirst$" -count=1 -v` and `go test ./internal/mcp -run "^TestDownStopsDependentsFirst$" -count=1 -v` both exit 0; each records the order of stop requests and the two adapters produce the same wave order and identical lexical result lists.
- [x] #3 AC3 — `go test ./integration -run "Down" -count=1 -v` exits 0 and includes a built-binary case where `hum events --kind lifecycle --json` shows web exiting before api is stopped and api exiting before db is stopped.
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
Implementation 48d9b30 (merged to main by fast-forward): shared reverse-after stop waves for CLI/MCP, preserving result shapes and independent stop connections. AC#1: go test ./internal/orchestrate -run "^TestDownOrder$" -count=1 -v PASS (chain, diamond, inactive, failure, ad-hoc). AC#2: go test ./internal/cli -run "^TestDownStopsDependentsFirst$" -count=1 -v PASS; go test ./internal/mcp -run "^TestDownStopsDependentsFirst$" -count=1 -v PASS (same waves, lexical results). AC#3: go test ./integration -run "Down" -count=1 -v PASS (built binary lifecycle exit cursors). Independent verifier PASS AC1-AC3; one review finding was test factory concurrent append race, fixed with mutex in internal/mcp/tools_test.go; go test -race ./internal/mcp -run "^(TestRestartPolicyMCP|TestDownStopsDependentsFirst|TestDown)$" -count=1 PASS. task check:staged PASS. task ci PASS on code commit 48d9b30 after an initial unrelated TestAttachStreamsBurstWithoutAborting burst timeout; isolated rerun PASS, full gate rerun PASS including go test -race ./... . Diff limited to declared paths; no test deleted/skipped/weakened or protected gate file modified. Next step: finalize provider record and clean up worktree.

Review: no findings. Wave computation, failure release, per-stop connections, and lexical results verified. Explicit --file down intentionally keeps runtime-only semantics (README, docs/design.md, manifest_test), so it does not parse the selected manifest for ordering. No follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented shared reverse-after down stop waves for CLI and MCP; all three acceptance tests and independent verification passed, task ci passed on 48d9b30, merged to main.
<!-- SECTION:FINAL_SUMMARY:END -->
