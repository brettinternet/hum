---
id: TASK-004
title: Make loop health visible
status: todo
type: task
priority: medium
labels: [loop, tooling]
rank: a0acb90f6bf3a9a36
files: [scripts/loop-health.sh, Taskfile.dist.yaml, README.md]
created: 2026-08-05T20:26:00Z
updated: 2026-09-02T17:01:00Z
---

Outcome: a local read-only command reports queue health and fails on stale work or backlog/code drift, so an unattended loop cannot silently rot.

Scope: scripts/loop-health.sh (new), one root Taskfile.dist.yaml target, and the loop documentation.

Non-goals: dashboards, notification services, remote queries, or any mutation of task state. The command is strictly read-only.

Modified-file contract: scripts/loop-health.sh, Taskfile.dist.yaml (protected; this task carries tooling), README.md.

## Acceptance criteria

- [ ] task loop:health prints the DONE_LAST_7_DAYS, IN_PROGRESS, BLOCKED, and IN_PROGRESS_NO_RECENT_COMMIT sections from Backlog.md data and mutates nothing (backlog task list --json is byte-identical before and after).
- [ ] The evaluator exits non-zero for an In Progress task older than the documented 24-hour threshold.
- [ ] The evaluator exits non-zero for a Done task with an unchecked acceptance criterion, and for a blocked label with no BLOCKED(class) note.
- [ ] The loop documentation names the command and both thresholds (7 days, 24 hours).

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] An independent verifier pass returned PASS for every acceptance criterion
- [ ] The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] No test was deleted, skipped, or weakened
- [ ] No protected gate file was modified unless the owner labelled this task tooling
- [ ] Committed on main with the task ID in the commit subject and a Task: trailer
