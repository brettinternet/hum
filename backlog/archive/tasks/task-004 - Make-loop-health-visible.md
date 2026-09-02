---
id: TASK-004
title: Make loop health visible
status: To Do
assignee: []
created_date: '2026-08-05 20:26'
updated_date: '2026-09-02 17:01'
labels:
  - loop
  - tooling
dependencies: []
modified_files:
  - scripts/loop-health.sh
  - Taskfile.dist.yaml
  - README.md
priority: medium
type: task
ordinal: 400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a local read-only command reports queue health and fails on stale work or backlog/code drift, so an unattended loop cannot silently rot.

Scope: scripts/loop-health.sh (new), one root Taskfile.dist.yaml target, and the loop documentation.

Non-goals: dashboards, notification services, remote queries, or any mutation of task state. The command is strictly read-only.

Modified-file contract: scripts/loop-health.sh, Taskfile.dist.yaml (protected; this task carries tooling), README.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task loop:health prints the DONE_LAST_7_DAYS, IN_PROGRESS, BLOCKED, and IN_PROGRESS_NO_RECENT_COMMIT sections from Backlog.md data and mutates nothing (backlog task list --json is byte-identical before and after).
- [ ] #2 The evaluator exits non-zero for an In Progress task older than the documented 24-hour threshold.
- [ ] #3 The evaluator exits non-zero for a Done task with an unchecked acceptance criterion, and for a blocked label with no BLOCKED(class) note.
- [ ] #4 The loop documentation names the command and both thresholds (7 days, 24 hours).
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
