---
id: HUM-148
title: Discover supervised process ports on demand
status: To Do
assignee: []
created_date: '2026-10-10 01:33'
labels:
  - cli
  - mcp
  - process
  - contract
dependencies: []
priority: medium
type: feature
ordinal: 32000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: humans and agents can ask which local network endpoints a named Hum process is actually using, without parsing logs or trusting configured ports. Launchers often spawn the actual server as a child, so inspecting only the initial PID is insufficient.

Scope:
- Add opt-in `hum status NAME --ports`, including `--json`, and an equivalent optional input on the existing MCP status tool. Require a single named process; ordinary status/list behavior and output remain unchanged.
- Return a current snapshot of TCP listening sockets and bound UDP sockets, with transport, local address, port, and owning PID. Preserve IPv4/IPv6 and wildcard addresses rather than inventing a browser URL. Exclude outbound TCP connections.
- Inspect the whole currently owned supervision group (Unix process group / Windows Job Object), including descendants after the leader exits. Never attribute sockets from unrelated processes, other Hum sessions, or a reused PID to the requested process.
- Support macOS, Linux, and Windows without requiring administrator privileges. Report inspection unavailable, permission denied, or partial results explicitly; an empty successful result must mean no matching sockets were observed, not that inspection failed. Document snapshot races and any platform limitations. Missing/stopped process behavior must stay consistent with existing status semantics.
- Inspection is read-only, bounded, cancellable, and performed only when requested. It must not block supervision or scan sockets during ordinary status calls. Results belong to the current launch, not a prior incarnation.

Non-goals: continuous monitoring, background polling, port history/events, outbound connection tracking, port allocation/reservation, conflict resolution, reverse proxying, URL inference, readiness changes, manifest configuration, or a new UI. Read-only discovery does not change decision-001 excluding port allocation and proxying.

Modified-file contract: Go implementation and focused tests under internal/process/, internal/app/, internal/daemon/, internal/protocol/, internal/cli/, and internal/mcp/; minimal shared fixtures under internal/testutil/; one built-binary feature scenario under integration/; README.md, docs/design.md, docs/cli-json-v1.md, and docs/coding-agents.md. go.mod/go.sum only if a platform inspection dependency is justified. No plugin, manifest schema, CI, task-runner, or other protected gate changes. Record and justify any expansion before making it.

Testing: follow docs/development.md#tests; implement before adding tests. Critical failures to guard are reporting another process or launch as the socket owner, missing child-server listeners, treating inspection failure as no ports, and accidentally performing inspection on ordinary status requests. Test semantic rules at their owner, adapter behavior with existing fakes, and one real-child integration scenario. No external network service or fixed port is required. No known task dependencies; next action is to inspect existing lifecycle identity and status request paths before selecting the platform inspection mechanism.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `mise exec go -- go test ./internal/process ./internal/app -run Ports -count=1 -v` exits 0 with named tests executed (not no-tests-to-run). Real local socket fixtures and owner-level tests prove TCP listeners and bound UDP endpoints are attributed to owned group members, including a child server and descendants after leader exit; unrelated sockets and outbound TCP connections are excluded. Identity changes/restarts cannot return stale launch ownership. Run the native fixture tests on macOS, Linux, and Windows and record each host and result; cross-compilation alone is not evidence.
- [ ] #2 AC2 — `mise exec go -- go test ./internal/process ./internal/app ./internal/daemon -run Ports -count=1 -v` exits 0 with named tests executed. Tests distinguish successful empty discovery from denied/unavailable/partial inspection, retain valid partial results with a diagnostic, and prove cancellation and ordinary status bypass using controlled fakes/counters rather than timing thresholds. Inspection does not change process lifecycle state.
- [ ] #3 AC3 — `mise exec go -- go test ./internal/cli ./internal/mcp ./internal/protocol -run Ports -count=1 -v` exits 0 with named tests executed. Adapter and serialization tests cover single-name opt-in validation, request forwarding, human output, JSON and MCP schema-valid endpoint/inspection-state output, and unchanged default status output. Non-running and missing processes preserve existing status semantics without inspecting unrelated PIDs.
- [ ] #4 AC4 — `mise exec go -- go test ./integration -run "^TestStatusPorts$" -count=1 -v` exits 0 and runs TestStatusPorts against the built binary. An isolated Hum runtime starts a launcher whose child binds an ephemeral loopback TCP listener and UDP socket; `status NAME --ports --json` reports both endpoints and the child PID, not a listener belonging to an unrelated fixture. Use shared runtime/condition-wait helpers and parallel-safe fixtures.
- [ ] #5 AC5 — `task cli:build`, `bin/hum status --help`, and `mise exec go -- go test ./internal/cli ./internal/mcp -run "TestHelpContract|TestDocs" -count=1 -v` exit 0; help exposes --ports and structural docs checks pass. README and design/JSON/MCP references explain opt-in usage, group scope, snapshot limitations, and empty versus unavailable results. `task cli:test` passes on macOS/Linux and `task windows:test` passes on Windows, with native results recorded before completion.
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
