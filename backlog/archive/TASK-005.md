---
id: TASK-005
title: Make the CAO loop complete a backlog task end to end
status: todo
type: chore
priority: high
labels: [loop]
rank: a0de12818acb90f6a
created: 2026-08-05T20:26:00Z
updated: 2026-09-02T17:01:00Z
---

Outcome: prove the whole lifecycle unattended — the poller selects a ready task, the implementer and the independent verifier each write a valid artifact, finalization commits code and task together, runs task ci on the attempt commit, and fast-forwards main.

Scope: an end-to-end run against one small real task plus the recorded evidence. Fix whatever the run exposes in cao/scripts/.

Non-goals: adding features to the orchestrator that no failure justifies; pushing; opening a pull request.

Modified-file contract: cao/scripts/** as required by observed failures, plus Implementation Notes.

## Acceptance criteria

- [ ] A single dispatched task reaches Done with .cao/work/<attempt>/final.json outcome "pass" and a delivery checkpoint at .cao/state/tasks/<ID>/checkpoint.json with stage "integrated".
- [ ] main fast-forwarded to the attempt commit, whose subject carries the task ID and whose body carries a Task: trailer.
- [ ] The successful attempt worktree and cao/<attempt> branch are gone; a deliberately failed attempt keeps both as recovery evidence.
- [ ] git status --porcelain in the primary checkout is unchanged by the run, apart from ignored .cao/ state.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] An independent verifier pass returned PASS for every acceptance criterion
- [ ] The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] No test was deleted, skipped, or weakened
- [ ] No protected gate file was modified unless the owner labelled this task tooling
- [ ] Committed on main with the task ID in the commit subject and a Task: trailer
