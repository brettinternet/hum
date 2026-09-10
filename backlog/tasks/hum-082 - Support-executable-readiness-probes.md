---
id: HUM-082
title: Support executable readiness probes
status: In Progress
assignee: []
created_date: '2026-09-10 20:35'
updated_date: '2026-09-10 20:56'
labels:
  - config
  - process
  - protocol
  - cli
  - integration
  - docs
dependencies: []
references:
  - 'https://f1bonacc1.github.io/process-compose/health/'
  - 'https://github.com/nc9/taskmux'
modified_files:
  - hum.schema.json
  - hum.example.yaml
  - README.md
  - docs/design.md
  - docs/coding-agents.md
  - internal/project/manifest.go
  - internal/project/manifest_test.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/app/relaunch_test.go
  - internal/orchestrate/orchestrate.go
  - internal/orchestrate/orchestrate_test.go
  - internal/daemon/wire_protocol.go
  - internal/daemon/wire_protocol_test.go
  - internal/cli/manifest.go
  - internal/cli/manifest_test.go
  - internal/cli/mcp.go
  - internal/cli/render.go
  - internal/cli/render_test.go
  - internal/cli/commands.go
  - internal/cli/help_contract_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/manifest_test.go
priority: high
type: feature
ordinal: 56800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a manifest process can declare a direct-argv readiness probe instead of an output matcher. Hum owns the probe for the current process incarnation and every start, up, restart, CLI, MCP, snapshot, and dependency-gating path observes one durable readiness result.

Context: output-only readiness is brittle for services that emit no stable startup line. Task and Just can already define portable health commands, so hum should execute those commands without absorbing task-runner-specific behavior. Failed attempts must not become retained process output, while a terminal readiness failure must preserve one bounded, actionable diagnostic.

Manifest contract: `ready` requires exactly one of `match` or `exec`. `exec` is a non-empty sequence of non-empty strings executed directly without a shell. `interval` is valid only with `exec`, must be a positive Go duration, and defaults to 1s. `timeout` remains a positive Go duration defaulting to 30s. An exec probe satisfies the existing requirement that every `after` dependency declare readiness.

Execution contract: run the first probe immediately after a successful process launch, then wait `interval` after each unsuccessful completion before retrying; attempts never overlap. Each attempt uses the supervised process working directory and launch environment. Exit 0 marks that incarnation ready at the attempt completion time, with no ready cursor. Nonzero exit or spawn failure schedules another attempt while the process remains active and the readiness deadline remains. Process exit, stop, restart, shutdown, supervisor close, or deadline expiry cancels any in-flight attempt and leaves no probe child running. Retain only the last failed-attempt exit or start error plus captured output capped by the existing supervisor max-line byte limit; never append probe output or per-attempt failures to the process output store.

Readiness and drift contract: snapshots retain the configured readiness method. Match readiness keeps its match and cursor behavior; exec readiness exposes argv and interval, and terminal launch results expose the bounded last-attempt diagnostic when present. Changing between match and exec or changing exec argv reports `readiness_exec` definition drift; timeout and interval remain wait policy and do not create drift. The shared orchestrator must observe durable exec readiness state rather than issuing an output wait, so CLI and MCP cannot disagree with the daemon.

Code map: parsing and after validation are in internal/project/manifest.go. Readiness state and output observation are in internal/app/app.go. Shared readiness waiting and drift comparison are in internal/orchestrate/orchestrate.go. protocol.ReadinessConfig, protocol.Readiness, and process snapshots cross internal/daemon/wire_protocol.go plus the CLI/MCP adapters. The private protocol version must advance because launch requests and snapshots gain fields. hum.schema.json currently requires `ready.match`.

Scope: implement the manifest and runtime contracts above; retain probe configuration across explicit restart and automatic relaunch; expose readiness configuration and terminal diagnostics through daemon, CLI, and MCP results; update schema, examples, help, human docs, and coding-agent docs. Preserve existing match readiness and launch outcome/exit-code semantics.

Non-goals: continuous liveness monitoring, restart-on-unhealthy behavior, built-in HTTP or TCP probes, shell command strings, task DAG execution, probe-specific environment overrides, arbitrary probe output retention, or changing crash-relaunch policy.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/project -run '^TestExecutableReadinessManifest$' -count=1 -v` exits 0 and prints PASS for match or exec exclusivity, non-empty exact argv, exec-only positive interval with a 1s default, the existing 30s timeout default, invalid interval/timeout rejection, and an `after` dependency satisfied by exec readiness.
- [ ] #2 `go test ./internal/app ./internal/orchestrate -run 'ExecutableReadiness' -count=1 -v` exits 0 and prints PASS for immediate first attempt, serial retry after interval, inherited cwd/environment, success, process exit, timeout, stop/restart/shutdown cancellation, automatic-relaunch incarnation isolation, no surviving probe child, one max-line-bounded last-attempt diagnostic, and no retained output per attempt.
- [ ] #3 `go test ./internal/protocol ./internal/daemon ./internal/cli ./internal/mcp -run 'ExecutableReadiness' -count=1 -v` exits 0 and prints PASS for the bumped wire contract, argv/interval/readiness-state round trips, start/up/restart and CLI/MCP result parity without an output-wait request for exec readiness, match-versus-exec and argv drift as `readiness_exec`, and unchanged match readiness behavior.
- [ ] #4 `go test ./integration -run '^TestExecutableReadiness$' -count=1 -v` exits 0 and prints PASS for a test-written probe executable that initially exits nonzero, succeeds after the supervised process creates a marker, and releases its `after` dependent only after success; no task, just, or shell executable is required.
- [ ] #5 `task ci` exits 0 after hum.schema.json, hum.example.yaml, README.md, docs/design.md, and docs/coding-agents.md document exact argv, immediate-first/1s-interval retry semantics, inherited cwd/environment, bounded terminal diagnostics, and the fact that readiness is startup gating rather than liveness monitoring.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation started in isolated worktree; claimed with worklease after selection by task backlog:next.
<!-- SECTION:NOTES:END -->
