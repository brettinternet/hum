---
id: HUM-056
title: Report a dead group leader whose descendant holds the inherited output pipes
status: To Do
assignee: []
created_date: '2026-09-07 14:42'
updated_date: '2026-09-07 14:42'
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
- [ ] #1 go test ./internal/app -run '^TestSnapshotDistinguishesSurvivingDescendants$' -count=1 -v exits 0 and prints PASS, proving a record whose recorded leader has exited while its process group remains alive reports a state distinguishable from a live-leader running record and never presents the dead leader pid as live.
- [ ] #2 go test ./integration -run '^TestSignalledLeaderWithSurvivingDescendant$' -count=1 -v exits 0 and prints PASS, proving hum status --json and hum list --json for a group whose leader was signalled while a descendant ignores that signal stop reporting state running with a dead pid, and that the reported state changes once the descendant exits.
- [ ] #3 rg -n 'descendant' docs/design.md prints at least one line documenting what is reported while only descendants of an exited leader remain.
- [ ] #4 task ci exits 0.
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
