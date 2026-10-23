---
id: TASK-002
title: Prove the CAO gate wiring fails loudly
status: todo
type: chore
priority: high
labels: [loop]
rank: a04a062b2e43dafce
created: 2026-08-05T20:25:00Z
updated: 2026-09-02T17:01:00Z
---

Outcome: prove that a failing area check actually fails `task ci`, so a green gate means something. An area whose checks are stubs makes every downstream acceptance criterion unfalsifiable.

Scope: one temporary deliberate failure in an area check plus the recorded evidence in Implementation Notes.

Non-goals: leaving the failure committed, adding new areas, or changing cao/ scripts.

Modified-file contract: no production file changes; Implementation Notes only.

## Acceptance criteria

- [ ] task ci exits 0 on the unmodified tree.
- [ ] With one area check made to exit 1, task ci exits non-zero and names that area; the tree is restored afterwards and task ci exits 0 again.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] An independent verifier pass returned PASS for every acceptance criterion
- [ ] The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] No test was deleted, skipped, or weakened
- [ ] No protected gate file was modified unless the owner labelled this task tooling
- [ ] Committed on main with the task ID in the commit subject and a Task: trailer
