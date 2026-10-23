---
id: TASK-006
title: Choose the notification channel for escalations
status: todo
type: task
priority: medium
labels: [loop, human, credential]
rank: a10f6bf3a9a37849e
created: 2026-08-05T20:26:00Z
updated: 2026-08-05T20:26:00Z
---

Owner-only. cao/scripts/notify.sh is best-effort Pushover and stays silently disabled without both PUSHOVER_TOKEN and PUSHOVER_USER_KEY. Decide whether this project uses Pushover, a different channel, or no notifications at all, then record the decision and provide the credentials out of band.

This task carries human and credential: the loop must never select it.

Non-goals: committing any credential.

## Acceptance criteria

- [ ] The chosen channel is recorded in the loop documentation, and either both credentials exist in the untracked .env or the decision to run without notifications is recorded.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] An independent verifier pass returned PASS for every acceptance criterion
- [ ] The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] No test was deleted, skipped, or weakened
- [ ] No protected gate file was modified unless the owner labelled this task tooling
- [ ] Committed on main with the task ID in the commit subject and a Task: trailer
