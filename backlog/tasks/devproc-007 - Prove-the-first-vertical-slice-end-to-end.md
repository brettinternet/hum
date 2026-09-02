---
id: DEVPROC-007
title: Prove the first vertical slice end to end
status: To Do
assignee: []
created_date: '2026-09-02 17:06'
labels:
  - integration
  - tooling
  - security
milestone: m-0
dependencies:
  - DEVPROC-006
modified_files:
  - integration/
  - internal/testutil/
  - Taskfile.dist.yaml
  - .github/workflows/ci.yaml
priority: high
type: task
ordinal: 700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the initial serve/run/list/logs/stop slice is exercised through the built binary before status or wait work may begin.

Scope: deterministic fixture processes; daemon lifecycle harness; exact-argv launch; incremental separate stdout/stderr reads; bounded cursor continuation and truncation; regex and stream filtering; observed exit status through list data; client disconnect/reconnect; duplicate-name and malformed-input errors; stopping a child/grandchild process tree on macOS and Linux.

Non-goals: implementing status or wait, benchmarks, persistence, PTY, Windows, or MCP.

Modified-file contract: integration/, internal/testutil/, Taskfile.dist.yaml, .github/workflows/ci.yaml.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./integration -run TestVerticalSlice -count=1` exits 0 after starting a real daemon and proves run, list, incremental bounded stdout/stderr logs, exit observation, and stop.
- [ ] #2 `go test ./integration -run TestReconnect -count=1` exits 0 and proves the daemon and managed child survive a client disconnect and accept a later client.
- [ ] #3 `go test ./integration -run TestStopTree -count=1` exits 0 and proves no spawned child or grandchild remains after stop, including the SIGKILL fallback fixture.
- [ ] #4 The macOS and Linux CI jobs each run `task ci` and the built-binary integration suite successfully.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
- [ ] #7 Committed on main with the task ID in the commit subject and a Task: trailer
<!-- DOD:END -->
