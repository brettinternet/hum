---
id: HUM-085
title: Configure stop grace per manifest process
status: To Do
assignee: []
created_date: '2026-09-10 20:36'
updated_date: '2026-09-11 13:49'
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
  - internal/cli/mcp.go
  - internal/cli/render.go
  - internal/cli/render_test.go
  - internal/cli/status_test.go
  - internal/cli/list_logs_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/stop_shutdown_test.go
priority: medium
type: enhancement
ordinal: 59800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: each manifest process can override the daemon default SIGTERM-to-SIGKILL grace with a non-negative duration, and every lifecycle path uses the effective value retained for that process.

Context: databases, container wrappers, and development servers need materially different shutdown windows. One daemon-wide value either delays every stop or kills slower services prematurely. This is supervision policy, unlike arbitrary stop hooks or task-runner behavior.

Manifest contract: add optional flat `stop_grace` as a Go duration string. Omission means inherit the daemon configuration; explicit `0s` means send SIGKILL immediately after the TERM check and must remain distinguishable from omission. Reject bare `0`, malformed values, and negative durations. No CLI per-invocation override is added.

Runtime contract: resolve the override when a record is admitted and retain both the effective duration and whether it was inherited. Use that record value for explicit stop, down, restart, remove, daemon shutdown, replacement cleanup, automatic-relaunch cancellation, and the existing control-signal intent window. Concurrent records use their own values. Explicit restart with a current manifest definition adopts its stop_grace setting; automatic relaunch preserves the admitted setting. Records without an override, including ad-hoc and discovered records, keep the Supervisor default. Startup reclaim of an orphan runtime group keeps using the daemon default because runtime persistence remains unchanged.

Snapshot and drift contract: process snapshots expose canonical `stop_grace` plus `stop_grace_inherited`. Human `status` prints the duration and inherited marker; CLI JSON list/status and every MCP result containing a process expose the same two fields, while the compact human list table gains no column. Definition drift reports `stop_grace` when inheritance changes or two explicit values differ; two inherited records match regardless of the effective daemon default captured when each was admitted. Restart adoption clears that drift.

Code map: daemon configuration reaches app.Options.StopGrace in internal/daemon/server.go. internal/app/app.go currently uses one Supervisor stopGrace in Stop, waitForDone, shutdown cleanup, and control-intent expiry; records retain restart and readiness launch policy but not stop grace. project.Definition flows through CLI/MCP adapters, protocol StartRequest/RestartRequest, daemon request conversion, app records and snapshots, then orchestrate.DefinitionChangedFields and renderers. The private protocol version must advance for request and snapshot fields. hum.schema.json already has the reusable Go-duration pattern under readiness timeout.

Scope: implement manifest parsing, inherited-versus-explicit retention, all lifecycle uses, launch/restart/snapshot wire fields, drift and restart adoption, canonical human/JSON/MCP presentation, schema, example, and docs. Preserve the existing daemon default and TERM-then-KILL sequence.

Non-goals: custom stop commands, lifecycle hooks, per-invocation overrides, configurable signal sequences, platform expansion, changing daemon-wide configuration precedence, persisting per-process grace in the runtime record, or applying a manifest override during orphan-group reclaim.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/project -run '^TestStopGraceManifest$' -count=1 -v` exits 0 and prints PASS for omitted/inherited, positive, sub-second, and explicit `0s` values plus bare-zero, malformed, negative, wrong-type, and unknown-field rejection.
- [x] #2 `go test ./internal/app -run 'ProcessStopGrace' -count=1 -v` exits 0 and prints PASS with deterministic timers, proving independent record values govern stop, restart, remove, shutdown, replacement cleanup, control-signal intent, and automatic-relaunch cancellation; explicit zero is preserved, in-flight lifecycle operations retain their admitted value, and unconfigured records use the Supervisor default.
- [x] #3 `go test ./internal/protocol ./internal/daemon ./internal/orchestrate ./internal/cli ./internal/mcp -run 'ProcessStopGrace' -count=1 -v` exits 0 and prints PASS for the bumped launch/restart/snapshot wire contract, effective duration plus inherited marker, inheritance-aware `stop_grace` drift, restart adoption, canonical human status and CLI JSON list/status output, MCP process-result parity, and unchanged daemon-default behavior.
- [x] #4 `go test ./integration -run '^TestPerProcessStopGrace$' -count=1 -v` exits 0 and prints PASS with two manifest processes running the hum-fixture tree ignore-term mode, one explicit `0s` and one nonzero grace, proving independent TERM-to-KILL windows under `hum stop` and the reported snapshot values.
- [x] #5 `task ci` exits 0 after hum.schema.json, hum.example.yaml, README.md, docs/design.md, and docs/coding-agents.md document inheritance, explicit-zero behavior, restart/relaunch retention, user-visible snapshot fields, and orphan-reclaim use of the daemon default.
- [ ] #6 mise exec go -- go test -race -count=100 -shuffle=1789133877229139000 -run '^TestProcessStopGraceOperations/restart_remove_replacement_and_shutdown_use_record_values$' ./internal/app exits 0 without hanging, and failure cleanup cannot block on an unfired synthetic grace timer
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
- [x] Add manifest/protocol/runtime stop-grace fields with inheritance-aware admission, lifecycle, drift, and restart behavior. - [x] Update CLI/MCP snapshots and renderers, schema, examples, and documentation. - [x] Add focused unit and integration coverage for every lifecycle and presentation path. - [x] Run all acceptance commands and task ci, obtain independent verification, then commit and integrate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1 PASS — go test ./internal/project -run TestStopGraceManifest -count=1 -v exited 0 after final rebase. AC#2 PASS — go test ./internal/app -run ProcessStopGrace -count=1 -v exited 0 and exercised every required lifecycle path with deterministic timers. AC#3 PASS — go test ./internal/protocol ./internal/daemon ./internal/orchestrate ./internal/cli ./internal/mcp -run ProcessStopGrace -count=1 -v exited 0 after final rebase. AC#4 PASS — go test ./integration -run TestPerProcessStopGrace -count=1 -v exited 0 and observed distinct 0s and 500ms stop windows. AC#5 PASS — task ci exited 0 on final commit e23ad7a after one unrelated intermittent TestSupervisorGlobalScope race failure passed on focused rerun and the complete rerun. Independent verifier PASS — all AC1–AC5 passed with substantive coverage; no tests were deleted, skipped, or weakened. Modified-file deviation: internal/protocol/codec_test.go and internal/protocol/restart_policy_test.go were necessarily updated because adding request/snapshot fields and advancing the private protocol to v19 invalidated their all-fields fixture and version assertion.

Reopened from HUM-091 remote stress run https://github.com/brettinternet/hum/actions/runs/34605158013 at d33a5aa: macOS timed out after 10m in TestProcessStopGraceOperations/restart_remove_replacement_and_shutdown_use_record_values. The test failed to observe the synthetic 3s timer within its 1s polling window, then testSupervisor cleanup and the in-flight Shutdown both blocked because that timer was never fired. Replace the readiness timing assumption with explicit synchronization and make failure cleanup non-blocking; do not widen timeouts or retry assertions.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented per-process stop_grace inheritance and explicit-zero semantics across manifest admission, all lifecycle paths, protocol v19, drift/restart behavior, CLI/MCP snapshots, schema, examples, and docs. Verified every focused acceptance command, independent verifier PASS, and task ci on commit e23ad7a; fast-forward merged to main.
<!-- SECTION:FINAL_SUMMARY:END -->
