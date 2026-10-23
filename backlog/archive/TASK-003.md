---
id: TASK-003
title: Configure backlog statuses, labels, and definition-of-done defaults
status: todo
type: task
priority: high
labels: [loop]
rank: a07b5f9d4d1bc2502
files: [backlog/config.yml]
created: 2026-08-05T20:26:00Z
updated: 2026-09-02T17:01:00Z
---

Outcome: backlog/config.yml enumerates every label the queue actually uses and the definition-of-done defaults that a new task inherits. Without a parking label an autonomous loop selects owner-only work, fails, and either thrashes or marks it Done falsely.

Scope: backlog/config.yml.

Non-goals: creating milestones, re-prioritising existing tasks, or changing selection logic in cao/scripts/tracker.sh.

Modified-file contract: backlog/config.yml.

## Acceptance criteria

- [ ] labels enumerates the taxonomy in use: lane labels, gate labels (human, device, credential), and loop-state labels (blocked, escalated, container, tooling, deferred). No task carries a label outside the list.
- [ ] backlog task create "probe" shows the definition-of-done defaults, each of which names a command or an inspection rather than an opinion.
- [ ] backlog task list --plain and backlog milestone list both exit 0 after the change.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] An independent verifier pass returned PASS for every acceptance criterion
- [ ] The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] No test was deleted, skipped, or weakened
- [ ] No protected gate file was modified unless the owner labelled this task tooling
- [ ] Committed on main with the task ID in the commit subject and a Task: trailer
