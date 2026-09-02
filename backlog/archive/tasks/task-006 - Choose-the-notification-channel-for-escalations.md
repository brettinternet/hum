---
id: TASK-006
title: Choose the notification channel for escalations
status: To Do
assignee: []
created_date: '2026-08-05 20:26'
labels:
  - loop
  - human
  - credential
dependencies: []
priority: medium
type: task
ordinal: 600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-only. cao/scripts/notify.sh is best-effort Pushover and stays silently disabled without both PUSHOVER_TOKEN and PUSHOVER_USER_KEY. Decide whether this project uses Pushover, a different channel, or no notifications at all, then record the decision and provide the credentials out of band.

This task carries human and credential: the loop must never select it.

Non-goals: committing any credential.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The chosen channel is recorded in the loop documentation, and either both credentials exist in the untracked .env or the decision to run without notifications is recorded.
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
