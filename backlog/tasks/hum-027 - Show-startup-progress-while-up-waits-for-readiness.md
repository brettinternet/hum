---
id: HUM-027
title: Show startup progress while up waits for readiness
status: To Do
assignee: []
created_date: '2026-09-06 00:13'
labels:
  - cli
  - human
  - output
milestone: m-3
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/manifest_test.go
  - integration/manifest_test.go
  - docs/design.md
priority: medium
type: enhancement
ordinal: 4700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A multi-process hum up currently produces no terminal output until every readiness wait finishes. A slow or misconfigured process therefore looks hung, even while other processes become ready and retained logs already contain useful diagnostics. Operators need bounded, actionable startup feedback without changing durable supervision or turning up into an unbounded log follower.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Running go test ./integration -run TestUpStartupProgress passes and proves hum up reports each process launch or readiness transition before another process reaches its readiness timeout.
- [ ] #2 Running go test ./integration -run TestUpReadinessTimeoutDiagnostics passes and proves a timed-out readiness result names the process and tells the operator how to inspect its retained logs.
- [ ] #3 Running go test ./internal/cli passes and proves --json remains valid NDJSON without human progress text contaminating stdout.
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
