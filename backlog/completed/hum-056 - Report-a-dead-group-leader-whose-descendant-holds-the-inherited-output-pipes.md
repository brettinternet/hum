---
id: HUM-056
title: Report a dead group leader whose descendant holds the inherited output pipes
status: Done
assignee: []
created_date: '2026-09-07 14:42'
updated_date: '2026-09-07 16:10'
labels:
  - process
  - cli
dependencies: []
modified_files:
  - internal/process/process.go
  - internal/app/app.go
  - internal/app/app_test.go
  - integration/lifecycle_test.go
  - docs/design.md
priority: medium
type: bug
ordinal: 33700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: hum status and hum list stop reporting state running with a dead pid indefinitely when the recorded group leader has exited but a descendant still holds the inherited stdout/stderr pipes.

Scope: internal/process completion accounting and the state it reports through internal/app snapshots. The original process group remains the lifecycle barrier; this task is about what the operator is shown while that barrier has not been reached, not about changing the barrier or reaping the descendant.

Why now (observed 2026-09-07 during the post-completion review of HUM-048): signalling a group leader with TERM while a descendant ignores it leaves hum status reporting state running with a pid that is gone, with no indication that only descendants remain. internal/process/process.go waits on groupGone as the completion barrier, which is deliberate and predates this window, so the reporting gap is the defect, not the barrier.

Non-goals: changing the group completion barrier, reaping or killing surviving descendants, altering hum signal semantics, or changing exit-status reporting for a leader that exits normally.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 go test ./internal/app -run '^TestSnapshotDistinguishesSurvivingDescendants$' -count=1 -v exits 0 and prints PASS, proving a record whose recorded leader has exited while its process group remains alive reports a state distinguishable from a live-leader running record and never presents the dead leader pid as live.
- [x] #2 go test ./integration -run '^TestSignalledLeaderWithSurvivingDescendant$' -count=1 -v exits 0 and prints PASS, proving hum status --json and hum list --json for a group whose leader was signalled while a descendant ignores that signal stop reporting state running with a dead pid, and that the reported state changes once the descendant exits.
- [x] #3 rg -n 'descendant' docs/design.md prints at least one line documenting what is reported while only descendants of an exited leader remain.
- [x] #4 task ci exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Expose a race-safe leader-exited observation from internal/process without changing the existing process-group Done/Wait barrier.
2. Derive an app snapshot-only descendants state while the leader is gone but the group remains, clearing the dead leader PID and retaining the PGID.
3. Add focused app and end-to-end lifecycle regressions for the intermediate state and final transition.
4. Document the operator-visible descendants state, run all acceptance commands and task ci, then obtain independent review and verification.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented in d59cfcf. The process layer now distinguishes a reaped leader only when a kernel process-group probe confirms surviving members; app snapshots report state descendants with PID 0 and retained PGID while preserving the original group barrier. Active-state predicates preserve CLI, daemon, MCP, and orchestration lifecycle control, requiring protocol v14.

AC#1 evidence: go test ./internal/app -run '^TestSnapshotDistinguishesSurvivingDescendants$' -count=1 -v — PASS at d59cfcf.
AC#2 evidence: go test ./integration -run '^TestSignalledLeaderWithSurvivingDescendant$' -count=1 -v — PASS at d59cfcf.
AC#3 evidence: rg -n 'descendant' docs/design.md — PASS; lines 232 and 234 document descendants reporting and terminal transition.
AC#4 evidence: task ci — PASS at d59cfcf, including gofmt, vet, staticcheck, all tests, race tests, build, and smoke.

Review: adversarial review found false fast-exit classification plus lifecycle-control and protocol-compatibility regressions; all were fixed before commit. Independent verifier returned PASS for AC#1-#4 at d59cfcf and confirmed no deleted, skipped, or weakened tests.

Modified-file deviation: internal/cli/commands.go, internal/daemon/server.go, internal/mcp/tools.go, internal/mcp/tools_test.go, internal/orchestrate/orchestrate.go, internal/orchestrate/orchestrate_test.go, internal/protocol/protocol.go, internal/protocol/protocol_test.go, and internal/protocol/restart_policy_test.go are required to treat descendants as an active lifecycle slot across existing control surfaces and to bump/test the private protocol version for the new wire-visible state. No protected gate files changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented surviving-descendant reporting: status/list now expose state descendants with PID 0 and retained PGID until the original process group ends. Preserved control behavior across CLI, daemon, MCP, and orchestration, bumped the private protocol to v14, and verified all acceptance checks plus task ci at d59cfcf. Independent verifier: PASS.
<!-- SECTION:FINAL_SUMMARY:END -->
