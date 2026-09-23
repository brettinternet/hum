---
id: HUM-132
title: Fold the cmd/hum built-binary test into the integration suite
status: To Do
assignee: []
created_date: '2026-09-23 21:53'
updated_date: '2026-09-23 21:53'
labels:
  - tooling
  - integration
milestone: m-4
dependencies: []
modified_files:
  - cmd/hum/integration_test.go
  - integration/*_test.go
  - Taskfile.dist.yaml
  - docs/development.md
priority: low
type: task
ordinal: 15000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: hum has one built-binary test suite, integration/, instead of two. cmd/hum/integration_test.go is 1,178 lines holding one sequential test, TestBuiltBinaryIntegration (:176). It has its own binary build, JSON structs (integrationProcess and related types at :26-68), and process and follower helpers (:589-837). These duplicate the TestMain build in integration/main_test.go:16-49 and that suite's helpers, so every CLI output change must be fixed in both places. The test runs in 1.5s, so this is a maintenance fix, not a speed fix. `task smoke` (Taskfile.dist.yaml:88-96) runs only this test. CI (.github/workflows/ci.yaml:51 and :121) runs `task security check test smoke`, so the test runs twice: once in `go test ./...` and once in smoke.

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

Smoke: change the smoke target to run `go test ./integration -run '^(TestAutomaticStartup|TestDetachedServe|TestForegroundServe|TestAttachedRunForegroundLifecycle|TestDetachedRun|TestNDJSONFollow|TestStopTree|TestShutdown)$' -count=1` and `go test ./cmd/hum -run '^TestBuiltCLIMachineOutputV1$' -count=1`, keeping the existing cli:build and cli:man deps and the dist/hum.1 check. If the smoke wording changes, update docs/development.md:93. This task edits Taskfile.dist.yaml, so it carries the tooling label.

Windows: HUM-119 and HUM-120 list ./cmd/hum in WINDOWS_PACKAGES and cmd/hum/*_test.go in their modified files. Those still exist after this task, and HUM-120 has fewer tests to port.

Non-goals: merging other integration tests; changing the CI job layout.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `test ! -e cmd/hum/integration_test.go && ! rg -n TestBuiltBinaryIntegration --glob "!backlog/**" .` exits 0.
- [ ] #2 AC2 — `rg -n "go test ./integration -run" Taskfile.dist.yaml` exits 0 and `task smoke` exits 0.
- [ ] #3 AC3 — `go test ./integration ./cmd/hum -count=1` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No protected gate file was modified unless the owner labelled this task tooling
- [ ] #6 cmd/hum/integration_test.go was deleted only after Implementation Notes mapped each of its assertions to the integration test that now holds it; no other test was deleted, skipped, or weakened
<!-- DOD:END -->
