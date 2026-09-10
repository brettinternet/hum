---
id: HUM-085
title: Configure stop grace per manifest process
status: To Do
assignee: []
created_date: '2026-09-10 20:36'
updated_date: '2026-09-10 20:38'
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
  - internal/config/config.go
  - internal/config/config_test.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/app/relaunch_test.go
  - internal/orchestrate/orchestrate.go
  - internal/orchestrate/orchestrate_test.go
  - internal/daemon/runtime.go
  - internal/daemon/runtime_test.go
  - internal/cli/manifest.go
  - internal/cli/manifest_test.go
  - internal/cli/commands.go
  - internal/cli/status_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/stop_shutdown_test.go
  - integration/restart_test.go
  - integration/relaunch_test.go
priority: medium
type: enhancement
ordinal: 59800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: each manifest process may override the hum daemon default SIGTERM-to-SIGKILL grace period with a positive or zero duration, and every explicit stop, down, restart, removal, shutdown, and replacement or recovery path consistently uses the effective value for that process.

Context: databases, container wrappers, and development servers have materially different shutdown times. A single daemon-wide grace period either delays every stop or kills slower services prematurely. This is process supervision policy, unlike arbitrary stop hooks or task-runner behavior.

Scope: add a flat stop_grace manifest field that falls back to daemon configuration when omitted; validate and expose the effective policy; retain it with launch specifications across restart and automatic relaunch; include it in active definition-drift detection and restart adoption; update schema, examples, status, JSON, MCP, and documentation where supervision policy is shown.

Non-goals: custom stop commands, lifecycle hooks, per-invocation overrides, configurable signal sequences, platform expansion, or changing the existing SIGTERM then SIGKILL behavior.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 go test ./internal/config -run "^TestManifestProcessStopGrace$" -count=1 -v exits 0 and prints PASS for omitted fallback, positive and explicit-zero values, invalid duration rejection, schema parity, and round-trip preservation.
- [ ] #2 go test ./internal/app -run "ProcessStopGrace" -count=1 -v exits 0 and prints PASS, proving each record uses its own effective grace for stop, restart, remove, shutdown, and automatic-relaunch cancellation while other records retain their own values.
- [ ] #3 go test ./internal/orchestrate ./internal/cli ./internal/mcp -run "ProcessStopGrace" -count=1 -v exits 0 and prints PASS for definition drift, restart adoption, snapshots, human and JSON output, MCP parity, and unchanged daemon-default behavior.
- [ ] #4 go test ./integration -run "^TestPerProcessStopGrace$" -count=1 -v exits 0 and prints PASS with two manifest processes demonstrating independent zero and nonzero SIGTERM-to-SIGKILL windows.
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
