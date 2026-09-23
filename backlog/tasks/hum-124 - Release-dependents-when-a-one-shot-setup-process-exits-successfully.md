---
id: HUM-124
title: Release dependents when a one-shot setup process exits successfully
status: To Do
assignee: []
created_date: '2026-09-23 21:21'
updated_date: '2026-09-23 21:54'
labels:
  - config
  - process
  - contract
milestone: m-3
dependencies:
  - HUM-123
  - HUM-129
modified_files:
  - internal/project/manifest.go
  - internal/project/*_test.go
  - hum.schema.json
  - internal/protocol/protocol.go
  - internal/protocol/*_test.go
  - internal/orchestrate/orchestrate.go
  - internal/orchestrate/*_test.go
  - internal/app/app.go
  - internal/app/*_test.go
  - internal/cli/render.go
  - internal/cli/manifest.go
  - internal/cli/commands.go
  - internal/cli/*_test.go
  - internal/mcp/tools.go
  - internal/mcp/*_test.go
  - integration/*_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
  - docs/cli-json-v1.md
  - internal/skill/SKILL.md
  - plugins/hum/skills/hum/SKILL.md
priority: high
type: feature
ordinal: 7000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a manifest declaration can be a setup step (migrations, installs, codegen) whose exit status 0 releases its dependents, so `migrate` then `api` needs no `sh -c "migrate && exec api"` wrapper that merges two lifecycles and log streams. Today `ResultSatisfiesGate` (internal/orchestrate/orchestrate.go) requires a running, ready prerequisite and manifest validation (internal/project/manifest.go, "dependency %q must declare ready") requires every dependency to declare `ready`, so a completed step reports `exited_before_ready` and its dependents are skipped. Depends on HUM-123 only to serialize edits to the shared up scheduler; the selection closure from HUM-123 must include one-shot prerequisites.

Scope:
- Manifest: new readiness method `ready: {exit: 0}`. Only the integer 0 is accepted. It combines with `timeout` (default unchanged) and rejects `interval` exactly as `match` does. It satisfies the rule that a dependency must declare `ready`. hum.schema.json stays in sync with the parser.
- Semantics: the process becomes ready when it exits with status 0. A nonzero exit, signal, or operator stop before that is a failure that uses the existing early-exit handling, skip cascade with `blocked_by`, and exit code 3. `restart: on-failure` retries nonzero exits as today; a successful exit never relaunches.
- Convergence on `up`: a one-shot runs when it has no retained successful completion for its current definition, or when this invocation must launch at least one of its direct dependents. Otherwise its retained completion satisfies dependents without rerunning, so a fully converged `hum up` launches nothing, while `hum down` followed by `hum up` reruns it before relaunching dependents. A drifted completed record follows existing drift reporting. Explicit `hum start NAME` and `hum restart NAME` rerun it and wait for completion. Daemon replacement drops the record, so the next `up` runs it.
- Reporting: status, list, up results, CLI JSON, MCP, and events distinguish a successful completion from a crash. `hum up` exits 0 when a one-shot completes; interactive `up` prints its output and keeps following. Any new enum value is documented in docs/cli-json-v1.md under its existing compatibility rules.
- Readiness drift classification includes the exit method in every method pair.
- Update README, docs/design.md, docs/coding-agents.md, docs/cli-json-v1.md, and both skill copies; update the README Hum and pitchfork section so it no longer lists one-shot setup as a gap.

Non-goals: a separate `oneshot` or `kind` field; caching completion across daemon replacement; rerun on file change or schedule; liveness; changing `start` to pull prerequisites; accepting exit codes other than 0.

Implementation context (commit 465b774):
- Manifest: parseReady internal/project/manifest.go:535 accepts exactly one of match, exec, http, and tcp, and applies the interval rules. The `after` graph checks live in validateAfterGraph (:270), including "dependency %q must declare ready" at :294. The parsed value is ReadyDefinition (:79). hum.schema.json defines `ready` under $defs.process (:57). TestManifestSchemaContract (internal/project/manifest_schema_test.go:23) keeps the schema and parser in sync.
- Readiness config types, one per layer: project.ReadyDefinition; app.ReadinessConfig (internal/app/app.go:156, whose Method is inferred in validateReadinessConfig :60); protocol.ReadinessConfig and protocol.Readiness (internal/protocol/protocol.go:1117); orchestrate.ReadinessConfig (internal/orchestrate/orchestrate.go:58). The readiness state constants are duplicated in orchestrate.go:26-28, protocol.go:1130-1132, and app.go:180-182; keep them identical.
- Supervisor exit path: Supervisor.reconcile (app.go:2528). `unexpected` (:2555) already excludes exit status 0, so shouldRelaunch (:2566) never relaunches a successful exit. The startup_failure lifecycle event at :2574 fires when readiness is not ready at exit. For exit readiness, status 0 must set readiness to ready, and emit "ready", before that check. A nonzero exit still emits startup_failure.
- Orchestrate: WaitForReadiness (:579) classifies exited_before_ready (:606, :642, :651, :671, :715, :718). ResultSatisfiesGate (:840) checks outcome and readiness state, not whether the process is running. OrchestrateUp waits for readiness only when the started process is running (:1091). A one-shot that has already exited when the start result is observed must still be classified by its exit status, so handle that path explicitly. Ensure (:855) chooses between already_running and start. The convergence rule ("rerun only when a direct dependent must launch") needs a pass in OrchestrateUp that knows which dependents will launch before it decides the one-shot's action. Drift fields come from DefinitionChangedFields (:390, method list at :424); extend TestReadinessDriftAllMethodPairs (orchestrate_test.go:133).
- MCP enums: HUM-129 moves the readiness method list in internal/mcp/tools.go (:311, :379) to one shared list and adds a conformance test that fails until `exit` is added to it.
- CLI rendering: outcome switches in internal/cli/render.go at :137, :1035, :1072, and :1234.
- JSON compatibility: docs/cli-json-v1.md defines how new enum values are added.

Test templates:
- integration: TestManifestHTTPReadiness (integration/manifest_test.go:35) for a method-specific end-to-end test, and TestUpOrderedStack (:659) for ordering.
- orchestrate: the readiness table in TestOrchestrateUp (orchestrate_test.go:176).
- One-shot children: the fixture binary internal/testutil/cmd/hum-fixture (modes listed at main.go:18-40) has no plain "exit N" mode. Use exact argv such as `/bin/sh -c 'touch marker; exit 0'`, as existing tests do, or add a fixture mode.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/project -run "^TestManifestReadyExit$" -count=1 -v` exits 0; it accepts `ready: {exit: 0}` with and without `timeout`, rejects nonzero, non-integer, `interval`, and combination with another method, and validates `after` on an exit-ready dependency; `go test ./internal/project -run "Schema" -count=1` also exits 0, proving hum.schema.json matches the parser.
- [ ] #2 AC2 — `go test ./internal/orchestrate -run "^TestExitReadiness" -count=1 -v` exits 0; cases prove exit 0 releases dependents; nonzero exit, signal, and stop skip dependents with `blocked_by`; a retained success whose direct dependents are all running and ready launches nothing; a retained success with a dependent to launch reruns the one-shot first; a drifted completed record reports `definition_drift`; readiness drift covers the exit method in all method pairs.
- [ ] #3 AC3 — `go test ./integration -run "^TestOneShotPrerequisite$" -count=1 -v` exits 0 against the built binary; with migrate (writes a marker, exits 0) before api (ready match), `hum up --detach --json` exits 0 and the marker exists before api launches; `hum status migrate --json` reports the successful completion; a second `hum up --json` launches nothing; after `hum down`, `hum up` runs migrate again; changing migrate to exit 3 makes `hum up` exit 3 with api skipped.
- [ ] #4 AC4 — `go test ./internal/cli ./internal/mcp -run "ExitReadiness" -count=1 -v` exits 0; human status and up output plus CLI JSON and MCP results show the completion distinctly from a crash, and `hum restart migrate` reruns it and waits for exit.
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
