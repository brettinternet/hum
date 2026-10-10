---
id: HUM-149
title: Show TCP listeners automatically in named CLI status
status: To Do
assignee: []
created_date: '2026-10-10 15:32'
labels: []
dependencies: []
ordinal: 33000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: hum status NAME shows the running service’s observed TCP listeners without requiring discovery of --ports. Applies to human output and --json.

Scope:
- Make named CLI status request the existing bounded, cancellable, read-only listener snapshot by default. Reuse the current inspector and rendering; preserve addresses, ports, holder PIDs, inspection states, and diagnostics.
- Keep aggregate hum status and list lightweight: no socket inspection or new ports column. Keep MCP status opt-in through ports:true and the daemon/protocol request contract unchanged.
- Retain --ports as a backward-compatible redundant flag for named status; retain its current single-name validation. Help and examples should teach hum status NAME rather than require the flag.
- Treat inspection as best-effort: denied, partial, unavailable, and successful empty results remain distinct; inspection failure must not turn otherwise successful status into a command failure. Preserve missing, stopped, and unlaunched status behavior without socket inspection.
- Update CLI human/JSON documentation and examples to distinguish automatic named CLI inspection from opt-in MCP inspection. Explicitly document the change to default named JSON output.

Non-goals: aggregate port discovery, a new flag or configuration setting, MCP default changes, inspector/platform redesign, polling, readiness, port allocation, URL inference, cross-worktree lookup, and container port forwarding.

Modified-file contract: internal/cli/ implementation and focused tests; existing integration/status_ports_test.go scenario (or the existing file containing TestStatusPorts); README.md, docs/design.md, docs/cli-json-v1.md, docs/coding-agents.md. Other paths require a recorded justification before expanding scope. No dependency, CI, task-runner, schema, or platform-inspector changes are expected.

Testing: implement first and follow docs/development.md#tests. Extend existing coverage rather than duplicating it. Specific regressions to catch: named status silently omitting listeners, aggregate status or MCP unexpectedly inspecting sockets, inspection failure becoming a command error, and stopped/missing status semantics changing. Supersede only the old CLI opt-in expectation explicitly changed by this task; retain ownership and exclusion assertions.

Dependency: follows completed HUM-148; no unfinished prerequisite. Next action: trace internal/cli/commands.go named status request and internal/cli/status_ports_test.go, then switch the CLI default without changing the daemon/MCP defaults.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 mise exec go -- go test ./internal/cli -run "Ports|Status" -count=1 -v exits 0 with named tests executed. Named human and JSON status include inspection without --ports; legacy --ports remains accepted; aggregate status does not inspect. Missing/stopped/unlaunched semantics remain unchanged; unsuccessful inspection is distinct from empty and does not fail otherwise successful status.
- [ ] #2 mise exec go -- go test ./internal/app ./internal/daemon ./internal/mcp -run Ports -count=1 -v exits 0 with named tests executed, preserving explicit protocol/MCP opt-in, bounded cancellation, failure states, and default bypass behavior.
- [ ] #3 mise exec go -- go test ./integration -run "^TestStatusPorts$" -count=1 -v exits 0 and runs the existing real-child scenario using status NAME --json without --ports. It reports the child’s ephemeral TCP listener and still excludes the unrelated listener.
- [ ] #4 task cli:build and bin/hum status --help exit 0; help teaches automatic named inspection and describes --ports as redundant compatibility syntax. mise exec go -- go test ./internal/cli ./internal/mcp -run "TestHelpContract|TestDocs" -count=1 -v exits 0 with named tests executed; README and CLI/JSON/MCP docs consistently distinguish defaults and retain snapshot limitations.
- [ ] #5 task cli:test exits 0 with no skipped, deleted, or weakened regression checks; existing assertions about CLI opt-in are updated only to reflect the explicitly changed named-status contract.
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
