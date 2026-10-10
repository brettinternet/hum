---
id: HUM-148
title: Discover supervised process TCP listeners on demand
status: In Progress
assignee: []
created_date: '2026-10-10 01:33'
updated_date: '2026-10-10 04:34'
labels:
  - cli
  - mcp
  - process
  - contract
  - tooling
dependencies: []
priority: medium
type: feature
ordinal: 32000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: humans and agents can ask which TCP endpoints a named Hum process is actually listening on, without parsing logs or trusting configured ports. This meets the decision-001 revisit trigger: in parallel worktrees, dev servers (Vite, Next, Astro) silently fall back to the next free port so the manifest `PORT` is wrong; launchers (turbo, foreman, `bin/dev`) start several servers as children; some servers print nothing or bind port 0; and bounded log retention can evict the startup banner. The listener is usually a descendant, so inspecting only the leader PID is insufficient.

Scope:
- Append a dated "Revision: read-only port observation" to decision-001 naming that trigger and restating that allocation, `$PORT` injection, proxying, and URL inference remain non-goals. Align README.md and docs/design.md non-goal text with it.
- Add opt-in `hum status NAME --ports` (with `--json`) and an optional `ports` boolean on the existing MCP status tool. Require exactly one name; default status/list output and behavior stay unchanged and never inspect sockets.
- On Unix, return a current snapshot of TCP listening sockets held by members of the owned group: one entry per socket with `transport: tcp`, local address, port, and the sorted PIDs of every group member holding it. Inherited sockets (Node cluster, gunicorn/uvicorn workers, reloaders) share one entry; separate IPv4/IPv6 binds are separate entries. Preserve wildcard and literal addresses; never build a URL. Exclude non-listening TCP, UDP, and Unix-domain sockets; `transport` keeps the schema additive.
- Group scope: Unix processes whose PGID is the recorded PGID, and Windows Job Object members (existing `jobProcessIDs`), including descendants after the leader exits. Never attribute sockets of unrelated processes, other Hum sessions, a prior launch, or a reused PID; re-confirm membership after reading sockets so a member that exits mid-snapshot cannot be replaced by a reused PID. Processes that left the group (setsid/daemonizers, container runtimes and port forwarders such as Docker Desktop or OrbStack) are not reported; document that an empty result does not prove the service is unreachable.
- Mechanisms, without cgo (release builds use CGO_ENABLED=0) and without new go.mod dependencies:
  - Linux: `/proc/PID/fd` socket inodes joined with that PID's `/proc/PID/net/tcp` and `tcp6` LISTEN rows (namespace-correct).
  - macOS: `/usr/sbin/lsof` by absolute path (never PATH or a shell), `-nP -a -p PIDS -iTCP -sTCP:LISTEN -F` field output, bounded by the request context and an output byte cap. Add a fuzz target for the field parser following the existing internal/protocol and internal/output fuzz tests.
  - Windows: iphlpapi `GetExtendedTcpTable` with `TCP_TABLE_OWNER_PID_LISTENER`, filtered to identity-verified Job Object members. User-approved exception: report API binding-owner PIDs, not all inherited socket holders, with an explicit partial diagnostic (including empty results). The API has no socket identity; preserve separate rows rather than merging same-endpoint binds as shared sockets. Document this platform limitation.
- No administrator privileges. Report inspection `unavailable` (for example lsof missing), `denied`, or `partial` explicitly with a diagnostic, retaining valid partial results; a successful empty result means no listeners were observed. Missing and stopped processes keep existing status semantics and are not inspected.
- Inspection is read-only, bounded, cancellable, performed only when requested, and runs outside supervision locks so it cannot block lifecycle work. Results are tied to the current launch identity.

Non-goals: UDP and Unix-domain sockets, outbound connections, aggregate or cross-process port lookup (DRAFT-005), listener-based readiness (DRAFT-004), continuous monitoring, background polling, port history/events, allocation/reservation, conflict resolution, reverse proxying, URL inference, readiness changes, manifest configuration, or a new UI.

Modified-file contract: Go implementation and focused tests under internal/process/, internal/app/, internal/daemon/, internal/protocol/, internal/cli/, and internal/mcp/; minimal shared fixtures under internal/testutil/; one built-binary feature scenario under integration/; the decision-001 file under backlog/decisions/ (append-only revision); README.md, docs/design.md, docs/cli-json-v1.md, and docs/coding-agents.md. go.mod/go.sum are not expected to change; justify any change in Implementation Notes. No plugin, manifest schema, CI, task-runner, or other protected gate changes. Record and justify any expansion before making it.

Testing: follow docs/development.md#tests; implement before adding tests. Critical failures to guard: reporting another process or launch as the socket holder, missing child-server listeners, treating inspection failure as no listeners, and inspecting on ordinary status requests. Test semantic rules at their owner, adapter behavior with existing fakes, and one real-child integration scenario. No external network service or fixed port is required.

No task dependencies. Next action: trace the named status path (cli/mcp -> app -> daemon -> protocol) and where launch identity is checked, then implement the Linux procfs inspector behind a platform-neutral process-package function.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `mise exec go -- go test ./internal/process ./internal/app -run Ports -count=1 -v` exits 0 with named tests executed (not no-tests-to-run). Real local socket fixtures and owner-level tests prove TCP listeners are attributed to owned group members, including a child server, on Unix a socket shared by two members (one entry, both PIDs), and descendants after leader exit; unrelated listeners, outbound TCP connections, and UDP sockets are excluded. Identity changes/restarts cannot return stale launch ownership. Run the native fixture tests on macOS, Linux, and Windows and record each host and result; cross-compilation alone is not evidence. On Windows, verify owner-only Job Object results with an explicit partial diagnostic: GetExtendedTcpTable exposes the binding PID, not every inherited holder or a unique socket identity; same-endpoint binds must not be merged as if proven shared.
- [x] #2 AC2 — `mise exec go -- go test ./internal/process ./internal/app ./internal/daemon -run "Ports|Fuzz" -count=1 -v` exits 0 with named tests executed, including the lsof field-parser fuzz seed corpus. Tests distinguish successful empty discovery from denied/unavailable/partial inspection, retain valid partial results with a diagnostic, and prove cancellation and ordinary status bypass using controlled fakes/counters rather than timing thresholds. Inspection does not change process lifecycle state.
- [x] #3 AC3 — `mise exec go -- go test ./internal/cli ./internal/mcp ./internal/protocol -run Ports -count=1 -v` exits 0 with named tests executed. Adapter and serialization tests cover single-name opt-in validation, request forwarding, human output, JSON and MCP schema-valid endpoint/inspection-state output, and unchanged default status output. Non-running and missing processes preserve existing status semantics without inspecting.
- [x] #4 AC4 — `mise exec go -- go test ./integration -run "^TestStatusPorts$" -count=1 -v` exits 0 and runs TestStatusPorts against the built binary. An isolated Hum runtime starts a launcher whose child binds an ephemeral loopback TCP listener; `status NAME --ports --json` reports that endpoint with the child PID and omits a listener held by an unrelated fixture. Use shared runtime/condition-wait helpers and parallel-safe fixtures.
- [ ] #5 AC5 — `task cli:build`, `bin/hum status --help`, and `mise exec go -- go test ./internal/cli ./internal/mcp -run "TestHelpContract|TestDocs" -count=1 -v` exit 0; help exposes --ports and structural docs checks pass. decision-001 has the dated revision; README and design/JSON/MCP references explain opt-in usage, group scope, escaped-process and container limitations, snapshot races, and empty versus unavailable results. `task cli:test` passes on macOS/Linux and `task windows:test` passes on Windows, with native results recorded before completion.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed for implementation in the primary main checkout. Preserve the pre-existing task refinements and unrelated draft files. User authorized commit and push; no PR or new branch requested.

User approved Windows owner-only results with an explicit partial diagnostic. Microsoft MIB_TCPROW_OWNER_PID documents dwOwningPid as the PID issuing the context bind, not every inherited holder; no socket identity exists in this table. Scope/AC1 amended accordingly, preserving Unix all-holder requirements. Source: https://learn.microsoft.com/en-us/windows/win32/api/tcpmib/ns-tcpmib-mib_tcprow_owner_pid. Existing tests will retain ownership/exclusion assertions; Windows expected state changes to partial per this approved contract.

Owner approved scope expansion: update mise.toml Go pin and docs/development.md toolchain table from 1.27.1 to 1.27.2, fixing eight reachable standard-library advisories blocking task ci. These two paths are added to the modified-file contract; no CI/task/gate logic or dependencies are changed. Initial task ci security failure recorded in /tmp/hum-148-final-ci/gate.log. Independent remaining gates: task check and task test passed; task race failed TestDownStopsDependentsFirst daemon-alive assertion, under investigation.

Owner approved a compatible source-built Staticcheck pin after 2026.2.1 failed Go 1.27.2 unified-v5 export data (also with a fresh GOCACHE). Upstream f1838cc308e5cfbb38d91cfc973355611845997e updates x/tools to support v5; pin its Go module pseudo-version in mise.toml via a tool backend alias and document it in docs/development.md. Same Staticcheck task/check set; no gate bypass. This is within the already approved two-file tooling expansion.

AC#2: macOS mise exec go -- go test ./internal/process ./internal/app ./internal/daemon -run "Ports|Fuzz" -count=1 -v PASS; named process/app/daemon tests, denied/available/unavailable/partial outcomes, controlled cancellation and bypass, restart race, lsof parser fuzz seeds. AC#3: mise exec go -- go test ./internal/cli ./internal/mcp ./internal/protocol -run Ports -count=1 -v PASS (also rechecked in combined Ports|Fuzz|TestHelpContract|TestDocs run). AC#4: mise exec go -- go test ./integration -run "^TestStatusPorts$" -count=1 -v PASS with real launcher child and unrelated-listener exclusion. AC#1/AC#5 pending final native Linux/Windows CI; macOS full suite passed. Full local gate PASS: TMPDIR=/tmp/hum-148-final-ci GOFLAGS="-p=1 -count=1" task ci, log /tmp/hum-148-final-ci/gate-compatible.log. Isolated TMPDIR avoids existing protocol-20 daemon; serial package scheduling reduces existing integration timing sensitivity without skipping or modifying any test. Go patch and source-built Staticcheck committed as 9c904da. Independent verifier reconciliation pending; no second general review requested. Unix shared socket and separate SO_REUSEPORT native fixture confirms lowercase lsof d identity; parser now resets per descriptor. Windows owner-only diagnostic follows the owner-approved scope. Unrelated pre-existing drafts remain untracked and excluded.
<!-- SECTION:NOTES:END -->
