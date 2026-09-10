---
id: HUM-082
title: Support executable readiness probes
status: To Do
assignee: []
created_date: '2026-09-10 20:35'
updated_date: '2026-09-10 20:44'
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
  - internal/orchestrate/orchestrate.go
  - internal/orchestrate/orchestrate_test.go
  - internal/daemon/wire_protocol.go
  - internal/daemon/wire_protocol_test.go
  - internal/cli/manifest.go
  - internal/cli/manifest_test.go
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
Outcome: a manifest process can declare an exact-argv readiness command as an alternative to output matching. Hum retries the command at a bounded interval until it exits successfully, the process exits, cancellation occurs, or the existing readiness timeout expires. after dependencies, start, up, restart, CLI, and MCP observe the same readiness result.

Context: output-only readiness is brittle for services that emit no stable startup line. Task and Just can already define portable health commands, so hum should invoke those commands rather than absorb task-runner or protocol-specific health-check behavior. Failed attempts must not flood retained output; the final readiness state should retain a bounded, actionable last exit/error diagnostic.

Code map: the manifest model is internal/project/manifest.go (ReadyDefinition, parseReady; validateAfterGraph requires every after dependency to declare ready). The daemon-side tracker is readinessTracker in internal/app/app.go and today observes store appends only. protocol.ReadinessConfig and app.ReadinessConfig carry only Match and Timeout; internal/daemon/wire_protocol.go converts them (appReadinessConfigFromProtocol) and internal/cli/manifest.go and internal/mcp/tools.go copy Match/Timeout into start, up, and restart requests. orchestrate.DefinitionChangedFields compares only the readiness match string, so the process snapshot must expose the probe argv for drift detection. The hum.schema.json readiness definition currently requires match.

Scope: add a mutually exclusive exact-argv readiness form (ready.exec as a non-empty string sequence beside ready.match; exactly one of the two is required) plus ready.interval with validation and a default; cancellation and process-exit handling; definition-drift comparison of the probe argv; snapshots and results; schema and examples; human and agent documentation. An exec probe satisfies the after-graph readiness requirement. Preserve existing ready.match behavior and timeout semantics.

Non-goals: continuous liveness monitoring, restart-on-unhealthy behavior, built-in HTTP or TCP probes, shell command strings, task DAG execution, or changing crash-relaunch policy.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 go test ./internal/project -run "^TestExecutableReadinessManifest$" -count=1 -v exits 0 and prints PASS for valid exact argv, interval and timeout defaults, match/exec exclusivity (neither and both rejected), empty argv rejection, invalid interval or timeout rejection, and an after dependency satisfied by an exec probe.
- [ ] #2 go test ./internal/app ./internal/orchestrate -run "ExecutableReadiness" -count=1 -v exits 0 and prints PASS for retry-until-success, process exit, timeout, cancellation, dependency gating, restart readiness, bounded last-attempt diagnostics, and no retained output entry per failed attempt.
- [ ] #3 go test ./internal/protocol ./internal/daemon ./internal/cli ./internal/mcp -run "ExecutableReadiness" -count=1 -v exits 0 and prints PASS, proving the probe argv and interval round-trip through the wire protocol, CLI and MCP return equivalent readiness states, and definition drift identifies executable probe changes.
- [ ] #4 go test ./integration -run "^TestExecutableReadiness$" -count=1 -v exits 0 and prints PASS for a manifest whose readiness command is a test-written executable that exits nonzero until the supervised process creates a marker file, and whose after dependent starts only after the probe succeeds. No task or just binary may be required.
- [ ] #5 task ci exits 0 after hum.schema.json, hum.example.yaml, README.md, docs/design.md, and docs/coding-agents.md describe the exact-argv probe, its interval, and its non-liveness semantics.
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
