---
id: HUM-150
title: Find which Hum process holds a TCP port with hum list --port
status: To Do
assignee: []
created_date: '2026-10-10 02:13'
updated_date: '2026-10-16 17:11'
labels:
  - cli
  - mcp
  - process
  - contract
  - json
dependencies:
  - HUM-148
modified_files:
  - internal/app/app.go
  - internal/app/ports_test.go
  - internal/protocol/protocol.go
  - internal/protocol/ports_test.go
  - internal/daemon/server.go
  - internal/daemon/client.go
  - internal/daemon/server_test.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/mcp.go
  - internal/cli/status_ports_test.go
  - internal/mcp/tools.go
  - internal/mcp/ports_test.go
  - internal/mcp/tools_test.go
  - integration/status_ports_test.go
  - >-
    backlog/decisions/decision-001 -
    Hum-stays-a-process-API-no-port-allocation-proxying-or-shell-level-conveniences.md
  - README.md
  - docs/design.md
  - docs/cli-json-v1.md
  - docs/coding-agents.md
  - examples/worktrees/README.md
  - internal/skill/SKILL.md
  - plugins/hum/skills/hum/SKILL.md
priority: medium
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: after a launch fails with "address already in use", a person or agent runs one command, `hum list --port 3000` (or MCP `list` with `port: 3000`), and learns which Hum-supervised process in any project or in the global scope is listening on that TCP port, plus the selector needed to act on it. A port held by no Hum process gets a clear "no Hum process" answer. Every process Hum could not inspect is listed, so an incomplete search never looks like a clean miss.

Why: every Git worktree is its own project, and the worktrees example hashes branch names to ports, so two checkouts can collide (examples/worktrees/README.md). Today the answer takes a platform-specific lsof/netstat recipe plus mapping PIDs back to Hum names by hand, which fails whenever the listener is a descendant rather than the recorded leader PID. HUM-148/HUM-149 already inspect one named group with identity checks; this composes that inspection across running groups.

Decisions (owner-approved 2026-10-16; reviewer may overturn):
1. Surface: a `--port PORT` flag on `hum list` and an integer `port` input (1-65535) on the MCP `list` tool. A port is machine-wide, so the lookup always covers every scope, as `--all` does; a project-only lookup would answer "not held" for the main use case. `--all` is accepted and redundant; `--global` is rejected as it is with `--all`. MCP rejects `port` with `scope: global` or `all: false`. No `status --port` and no new command.
2. Mechanism: fan the existing per-group inspection (the `GetPortsScoped` path; platform inspectors unchanged) out over every running record in every scope, snapshotting records under one read lock and inspecting outside it, with a small concurrency bound (for example 4) and a whole-lookup deadline (a named constant, for example 5s) on top of the existing 2s per-group deadline. Prefer a dedicated private daemon operation over overloading `ListRequest`, so ordinary list stays inspection-free by construction. Rejected: a system-wide reverse lookup (one `lsof -iTCP:PORT`, a /proc scan, or a TCP table read mapped back to groups) needs new code on three platforms; revisit only if measured lookups are too slow.
3. Result: holders are running records whose inspected listeners include PORT. Each holder carries the existing `ports` object with `listeners` filtered to PORT. A record is reported as incomplete, with scope, project root, name, inspection state, and diagnostic, when its result was `denied`, `unavailable`, or `partial` without a match, when the whole-lookup deadline cut it off, or when its launch changed during inspection. Windows inspection is always `partial`, so non-matching Windows records are always incomplete; that is the existing honest contract, not a bug.
4. Output. Human output reuses the `list --all` project grouping and selectors, adding a LISTENING column with only matching endpoints and holder PIDs:

```console
$ hum list --port 3000
Project: /Users/me/app/.worktrees/a (hum --project /Users/me/app/.worktrees/a)
NAME  STATE    PID   LISTENING
web   running  4182  127.0.0.1:3000 (pid 4190)
```

With no holder it prints `No Hum process is listening on TCP port 3000.` Incomplete records follow, one line each with name, selector, state, and diagnostic. Exit 0 whether or not a holder is found; errors keep current exit codes. JSON uses the aggregate snapshot shape (`processes` holds only holders) plus an optional top-level `incomplete` array: an additive cli-json-v1 change with no schema_version bump. MCP structuredContent mirrors it.
5. Boundaries: never identify, signal, or describe a non-Hum process. Ordinary `list`, `list --all`, and aggregate status still never inspect sockets. Append a dated revision to decision-001 allowing explicit aggregate inspection for port lookup only. Update README "Ports", docs/design.md "Port observation", docs/cli-json-v1.md, docs/coding-agents.md, the worktrees example README (collision recovery), and both bundled skills.

MCP budget (measured with hum 0.17.1 on 2026-10-16): tools/list is 38,569 of 40,000 bytes and 16,893 of 17,000 without output schemas (internal/mcp/tools_budget_test.go). Pay for the new input and output schema text by shortening existing descriptions; do not raise either budget.

Non-goals: identifying or killing foreign holders; port allocation, conflict resolution, or free-port suggestions; UDP, Unix sockets, or outbound connections; address filtering or several ports per query; container port forwarding; monitoring or caching; `status --port`; changes to platform inspectors or the named-status contract.

Modified-file contract: the frontmatter list. Tests stay in the listed files; `internal/mcp/tools_test.go` changes only to add the new fake-client method. Record a justification before touching anything else.

Testing: implement first; follow docs/development.md#tests. Regressions to catch: an incomplete inspection reported as "no Hum process" (a false negative an agent acts on); plain list or aggregate status starting to inspect sockets; one stalled inspector holding the whole lookup; a foreign PID leaking into output. Use controlled fake inspectors and counters, not wall-clock thresholds.

Next action: add the fan-out beside `GetPortsScoped` (internal/app/app.go:2807), then wire it through the daemon dispatch (internal/daemon/server.go:960 area), `Client` (internal/daemon/client.go:315 area), `listCommand` (internal/cli/commands.go:1008), `mcpDaemonClient` (internal/cli/mcp.go:168), and the MCP list tool (internal/mcp/tools.go:457, :510, :1589).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 mise exec go -- go test ./internal/app -run 'PortLookup' -count=1 -v exits 0 with named tests executed: with controlled fake inspectors across two project scopes and the global scope, the lookup returns exactly the running records whose listeners include the port, with listeners filtered to it; terminal and stopped records are never inspected; denied, unavailable, and non-matching partial results and a launch that changes during inspection are reported incomplete, never as a clean miss; a blocking inspector cannot hold the lookup past the whole-lookup deadline and uninspected records are reported incomplete; a counter shows concurrent inspections never exceed the bound.
- [ ] #2 mise exec go -- go test ./internal/protocol ./internal/daemon ./internal/cli ./internal/mcp -run 'PortLookup' -count=1 -v exits 0 with named tests executed: the private request and response round-trip port, holders, and incomplete records; hum list --port validates 1-65535 before daemon contact and rejects --global; human output shows grouped holders with the LISTENING column, the no-holder message, and incomplete lines; JSON has processes and incomplete; plain list, list --all, and aggregate status send no port lookup; MCP list accepts port, rejects it with scope global or all false before daemon contact, and returns holders and incomplete in structuredContent.
- [ ] #3 mise exec go -- go test ./integration -run '^TestListPortLookup$' -count=1 -v exits 0 on Unix with the built binary: projects A and B share one runtime and B runs a child listening on an ephemeral TCP port; hum -C A list --port PORT --json exits 0 and reports B's process with B's project_root, the child PID, and the endpoint; a port held only by a listener the test owns outside Hum returns exit 0, empty processes, and no PID of that listener anywhere in the output.
- [ ] #4 task cli:build && bin/hum list --help exits 0 and shows --port. rg -n 'list --port' README.md docs/design.md docs/cli-json-v1.md docs/coding-agents.md examples/worktrees/README.md internal/skill/SKILL.md plugins/hum/skills/hum/SKILL.md prints at least one line from each file. decision-001 ends with a dated revision allowing explicit port-holder lookup. mise exec go -- go test ./internal/cli ./internal/mcp -run 'TestHelpContract|TestDocs|TestToolsListBudget' -count=1 -v exits 0 with the budgets unchanged.
- [ ] #5 task cli:test exits 0 with no test skipped, deleted, or weakened.
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
