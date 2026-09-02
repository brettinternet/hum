---
id: TASK-003
title: 'Configure backlog statuses, labels, and definition-of-done defaults'
status: To Do
assignee: []
created_date: '2026-08-05 20:26'
updated_date: '2026-09-02 17:01'
labels:
  - loop
dependencies: []
modified_files:
  - backlog/config.yml
priority: high
type: task
ordinal: 300
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: backlog/config.yml enumerates every label the queue actually uses and the definition-of-done defaults that a new task inherits. Without a parking label an autonomous loop selects owner-only work, fails, and either thrashes or marks it Done falsely.

Scope: backlog/config.yml.

Non-goals: creating milestones, re-prioritising existing tasks, or changing selection logic in cao/scripts/tracker.sh.

Modified-file contract: backlog/config.yml.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 labels enumerates the taxonomy in use: lane labels, gate labels (human, device, credential), and loop-state labels (blocked, escalated, container, tooling, deferred). No task carries a label outside the list.
- [ ] #2 backlog task create "probe" shows the definition-of-done defaults, each of which names a command or an inspection rather than an opinion.
- [ ] #3 backlog task list --plain and backlog milestone list both exit 0 after the change.
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
