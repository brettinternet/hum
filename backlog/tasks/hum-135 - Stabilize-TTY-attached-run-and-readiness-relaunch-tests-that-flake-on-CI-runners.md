---
id: HUM-135
title: >-
  Stabilize TTY, attached-run, and readiness-relaunch tests that flake on CI
  runners
status: To Do
assignee: []
created_date: '2026-09-24 12:39'
labels:
  - cli
  - testing
dependencies: []
priority: medium
type: bug
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: CI stops failing intermittently in three tests that pass locally (including under -race with -count=5) but failed on GitHub runners during the HUM-117..125 review (run 35999701233, commit 86b26b2, whose Unix-effective change was orchestrate-only): TestTTYCLI (internal/cli/tty_test.go:90, macOS race: replacement TTY run printed only the attach banner), TestAttachedRunForegroundLifecycle/SIGHUP_detaches_and_retained_logs_remain_readable (macOS race, run_reconnect_test.go:238: retained logs empty, next cursor 0), and TestExecutableReadinessAutomaticRelaunchIncarnationIsolation (internal/app/app_test.go:1229, Linux: probe pid file had fewer than 2 pids within 2s). HUM-122 notes also record a transient macOS TestTTYCLI race failure.

Scope: for each test, find out whether it is a fixed wall-clock wait racing runner load or a real ordering bug; make waits progress-based or event-driven, and fix product code if the failure is real.

Non-goals: TestAttachStreamsBurstWithoutAborting (HUM-134); weakening, skipping, or deleting assertions.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 go test -race ./internal/cli -run '^(TestTTYCLI|TestAttachedRunForegroundLifecycle)$' -count=20 exits 0
- [ ] #2 go test ./internal/app -run '^TestExecutableReadinessAutomaticRelaunchIncarnationIsolation$' -count=50 -cpu 1,2 exits 0
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
