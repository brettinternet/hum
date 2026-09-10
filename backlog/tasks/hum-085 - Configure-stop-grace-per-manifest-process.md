---
id: HUM-085
title: Configure stop grace per manifest process
status: To Do
assignee: []
created_date: '2026-09-10 20:36'
updated_date: '2026-09-10 20:45'
labels:
  - config
  - process
  - protocol
  - cli
  - integration
  - docs
dependencies: []
references:
  - 'https://f1bonacc1.github.io/process-compose/launcher/'
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
  - internal/daemon/server.go
  - internal/daemon/wire_protocol.go
  - internal/daemon/wire_protocol_test.go
  - internal/cli/manifest.go
  - internal/cli/manifest_test.go
  - internal/cli/render.go
  - internal/cli/status_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/stop_shutdown_test.go
priority: medium
type: enhancement
ordinal: 59800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: each manifest process may override the hum daemon default SIGTERM-to-SIGKILL grace period with a positive or zero duration, and every explicit stop, down, restart, removal, shutdown, and replacement or recovery path consistently uses the effective value for that process.

Context: databases, container wrappers, and development servers have materially different shutdown times. A single daemon-wide grace period either delays every stop or kills slower services prematurely. This is process supervision policy, unlike arbitrary stop hooks or task-runner behavior.

Code map: the daemon-wide grace is internal/config Config.StopGrace from --stop-grace or its environment variable (DefaultStopGrace 10s, explicit zero accepted) and reaches the Supervisor as a single app.Options.StopGrace used by Stop, waitForDone, and the control-signal grace timer in internal/app/app.go. The manifest model is internal/project/manifest.go. Per-process supervision policy already flows end to end for restart: project.Definition, then internal/cli/manifest.go and internal/mcp/tools.go, then protocol.StartRequest and RestartRequest, then internal/daemon/server.go building app.StartRequest and RestartOptions, then the record and app.Process snapshot, then protocol.Process via internal/daemon/wire_protocol.go, then orchestrate.DefinitionChangedFields, then the status cell in internal/cli/render.go. Follow the same path for stop_grace. hum.schema.json already defines a Go duration pattern (readiness timeout) that accepts 0s but not bare 0. internal/daemon/runtime.go persists only scope, root, name, PIDs, and start identity per group.

Scope: add a flat stop_grace manifest field (Go duration string; explicit zero written as 0s) that falls back to the daemon configuration when omitted; validate it and expose the effective policy in snapshots; retain it with the launch specification across restart and automatic relaunch; include it in active definition-drift detection and restart adoption; update schema, examples, status human and JSON output, MCP, and documentation where supervision policy is shown.

Non-goals: custom stop commands, lifecycle hooks, per-invocation overrides, configurable signal sequences, platform expansion, changing the existing SIGTERM then SIGKILL behavior, or persisting per-process grace in the runtime record; orphan-group reclaim at daemon startup keeps using the daemon default.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 go test ./internal/project -run "^TestStopGraceManifest$" -count=1 -v exits 0 and prints PASS for omitted (nil, daemon fallback), positive, and explicit 0s values, invalid and negative duration rejection, and round-trip preservation.
- [ ] #2 go test ./internal/app -run "ProcessStopGrace" -count=1 -v exits 0 and prints PASS, proving each record uses its own effective grace for stop, restart, remove, shutdown, control-signal grace, and automatic-relaunch cancellation while other records and records without an override retain the Supervisor default.
- [ ] #3 go test ./internal/protocol ./internal/daemon ./internal/orchestrate ./internal/cli ./internal/mcp -run "ProcessStopGrace" -count=1 -v exits 0 and prints PASS for wire round-trip, a stop_grace definition-drift field, restart adoption, snapshots, human and JSON status output, MCP parity, and unchanged daemon-default behavior.
- [ ] #4 go test ./integration -run "^TestPerProcessStopGrace$" -count=1 -v exits 0 and prints PASS with two manifest processes running the hum-fixture tree ignore-term mode, one with stop_grace 0s and one with a nonzero grace, demonstrating independent SIGTERM-to-SIGKILL windows under hum stop.
- [ ] #5 task ci exits 0 after hum.schema.json, hum.example.yaml, README.md, docs/design.md, and docs/coding-agents.md document fallback and explicit-zero semantics.
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
