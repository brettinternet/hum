---
id: HUM-132
title: Fold the cmd/hum built-binary test into the integration suite
status: Done
assignee: []
created_date: '2026-09-23 21:53'
updated_date: '2026-09-24 12:42'
labels:
  - tooling
  - integration
  - reviewed
milestone: m-4
dependencies: []
modified_files:
  - cmd/hum/integration_test.go
  - integration/*_test.go
  - Taskfile.dist.yaml
  - docs/development.md
priority: low
type: task
ordinal: 7000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: hum has one built-binary test suite, integration/, instead of two. cmd/hum/integration_test.go is 1,178 lines holding one sequential test, TestBuiltBinaryIntegration (:176). It has its own binary build, JSON structs (integrationProcess and related types at :26-68), and process and follower helpers (:589-837). These duplicate the TestMain build in integration/main_test.go:16-49 and that suite's helpers, so every CLI output change must be fixed in both places. The test runs in 1.5s, so this is a maintenance fix, not a speed fix. `task smoke` (Taskfile.dist.yaml:88-96) runs only this test. CI (.github/workflows/ci.yaml:55 and :127) runs `task security check test smoke`, so the test runs twice: once in `go test ./...` and once in smoke.

Owner exception (2026-09-23): this task may delete cmd/hum/integration_test.go after the named integration test asserts each phase below. It may not remove anything else. Its Definition of Done replaces the default no-deletion rule with a scoped one.

Phase map from cmd/hum/integration_test.go to the integration/ test that owns each phase. Add any missing assertion to the owner. The map comes from a read-only survey, so check each assertion, not only the topic.
| Phase | Owner |
|---|---|
| :203-275 daemon autostart; repeating `serve --daemon` reports the same live daemon | lifecycle_test.go:397 TestAutomaticStartup; lifecycle_test.go:299 TestDetachedServe |
| :277-339 foreground serve; attached run preserves stdout, stderr, argv, and exit 7 | lifecycle_test.go:236 TestForegroundServe; run_reconnect_test.go:66 TestAttachedRunForegroundLifecycle |
| :341-382 detached JSON run, JSON list metadata, bounded log tail and cursor | run_reconnect_test.go:347 TestDetachedRun |
| :383-488 NDJSON `logs --follow` with initial and delayed output; follower survives stop until detach | logs_test.go:133 TestLogFollowers; logs_test.go:407 TestNDJSONFollow |
| :490-537 stopping already-stopped and missing names reports not_running | stop_shutdown_test.go:62 TestStopTree |
| :539-581 shutdown refuses while a process is active; forced shutdown stops the group and ends foreground serve | stop_shutdown_test.go:182 TestShutdown |

Keep cmd/hum/machine_output_v1_test.go (TestBuiltCLIMachineOutputV1, which holds the only JSON v1 contract checks) and cmd/hum/main_test.go. integration/ helpers are defined per file, for example runitProcessRecord (run_reconnect_test.go:51) and logsitProcess (logs_test.go:58). Reuse the owner file's helpers; do not port cmd/hum's.

Smoke: change the smoke target to run `go test ./integration -run '^(TestAutomaticStartup|TestDetachedServe|TestForegroundServe|TestAttachedRunForegroundLifecycle|TestDetachedRun|TestNDJSONFollow|TestStopTree|TestShutdown)$' -count=1` and `go test ./cmd/hum -run '^TestBuiltCLIMachineOutputV1$' -count=1`, keeping the existing cli:build and cli:man deps and the dist/hum.1 check. If the smoke wording changes, update docs/development.md:94. This task edits Taskfile.dist.yaml, so it carries the tooling label.

Windows: HUM-119 and HUM-120 list ./cmd/hum in WINDOWS_PACKAGES and cmd/hum/*_test.go in their modified files. Those still exist after this task, and HUM-120 has fewer tests to port.

Non-goals: merging other integration tests; changing the CI job layout.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `test ! -e cmd/hum/integration_test.go && ! rg -n TestBuiltBinaryIntegration --glob "!backlog/**" .` exits 0.
- [x] #2 AC2 — `rg -n "go test ./integration -run" Taskfile.dist.yaml` exits 0 and `task smoke` exits 0.
- [x] #3 AC3 — `go test ./integration ./cmd/hum -count=1` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No protected gate file was modified unless the owner labelled this task tooling
- [x] #6 cmd/hum/integration_test.go was deleted only after Implementation Notes mapped each of its assertions to the integration test that now holds it; no other test was deleted, skipped, or weakened
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Compare each built-binary phase assertion with its mapped integration owner and add missing checks.
2. Route smoke to those owners plus the machine-output contract, then remove the duplicate test.
3. Run focused tests, independent verification, and task ci; commit, merge to main, and finalize provider state.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Pre-deletion assertion mapping: autostart child liveness/socket and repeat serve exact socket/PID/status/log -> TestAutomaticStartup + TestDetachedServe; foreground daemon stdout/readiness and attached raw stdout/stderr/argv/nonzero exit -> TestForegroundServe + TestAttachedRunForegroundLifecycle; detached JSON process and list PID/cwd/argv/state -> TestDetachedRun; bounded tail/stream/byte limit/next cursor and NDJSON replay/delayed output -> TestLogFollowers + TestNDJSONFollow; follower survives stop and detaches -> TestLogsFollowMultipleProcesses + TestReconnect (both existing, beyond smoke subset); stopped/missing multi-name stop ordering -> TestStopTree (added); refusal lists live processes and JSON active_processes; force kills PID/group and foreground serve exits -> TestShutdown (added JSON/serve assertions). Focused changed tests passed.

Commit 13f4d1f. AC#1: test ! -e cmd/hum/integration_test.go && ! rg -n TestBuiltBinaryIntegration --glob !backlog/** . exited 0. AC#2: rg -n go.test.integration.-run Taskfile.dist.yaml and task smoke exited 0 (selected integration and JSON v1 tests). AC#3: go test ./integration ./cmd/hum -count=1 exited 0. task ci exited 0 on commit 13f4d1f including race and smoke. An initial integration attempt hit a transient MCP next_cursor race and passed on rerun; initial task ci timed out in unchanged TestAttachStreamsBurstWithoutAborting under concurrent load, then passed twice including on final commit. Independent verifier PASS for AC1-3 and DoD #1,#3-6; missing AC evidence lines were its only finding and are now recorded. Review found no item-scoped defects. No other tests deleted or weakened; diff limited to declared paths; no protected gate modified. Next: merge 13f4d1f into main and finalize.

Exact AC#2 command: rg -n "go test ./integration -run" Taskfile.dist.yaml (exit 0); task smoke (exit 0). Exact AC#1 command: test ! -e cmd/hum/integration_test.go && ! rg -n TestBuiltBinaryIntegration --glob "!backlog/**" . (exit 0).

Review: coverage map and smoke verified on main (task smoke passes). Fixed the MCP next_cursor flake noted during implementation: TestMCPResolvedAndAdHocLifecycle waited only for the first stderr line, so idle-flushed partial fragments could advance next_cursor between MCP and CLI snapshots; it now waits for both partials (09d44ea, 15x pass). Other noted flake tracked by HUM-134. No further follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Consolidated built-binary lifecycle smoke into integration and retained JSON v1 checks; added missing stop and shutdown assertions. Commit 13f4d1f merged into main. Focused suites and task ci passed; independent verifier passed all acceptance criteria.
<!-- SECTION:FINAL_SUMMARY:END -->
