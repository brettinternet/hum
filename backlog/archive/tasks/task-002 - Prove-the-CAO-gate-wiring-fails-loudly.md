---
id: TASK-002
title: Prove the CAO gate wiring fails loudly
status: To Do
assignee: []
created_date: '2026-08-05 20:25'
updated_date: '2026-09-02 17:01'
labels:
  - loop
dependencies: []
priority: high
type: chore
ordinal: 200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: prove that a failing area check actually fails `task ci`, so a green gate means something. An area whose checks are stubs makes every downstream acceptance criterion unfalsifiable.

Scope: one temporary deliberate failure in an area check plus the recorded evidence in Implementation Notes.

Non-goals: leaving the failure committed, adding new areas, or changing cao/ scripts.

Modified-file contract: no production file changes; Implementation Notes only.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task ci exits 0 on the unmodified tree.
- [ ] #2 With one area check made to exit 1, task ci exits non-zero and names that area; the tree is restored afterwards and task ci exits 0 again.
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
