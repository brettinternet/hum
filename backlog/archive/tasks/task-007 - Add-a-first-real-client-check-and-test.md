---
id: TASK-007
title: Add a first real client check and test
status: To Do
assignee: []
created_date: '2026-08-05 20:26'
updated_date: '2026-08-05 20:38'
labels:
  - client
dependencies: []
modified_files:
  - client/package.json
priority: medium
type: task
ordinal: 700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: replace the placeholder client check/test with a real one, so client:check and client:test entering task ci proves something. An area whose gate is echo makes every acceptance criterion in that area unfalsifiable.

Scope: client/ configuration and one genuine test.

Non-goals: application features, server changes, or CI workflow changes.

Modified-file contract: client/package.json, one new client test file.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task client:check exits 0 and actually lints; introducing a lint error makes it exit non-zero.
- [ ] #2 task client:test runs at least one real assertion; breaking the asserted behavior makes task ci exit non-zero.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
CAO attempt task-007.artifact.203850 failed (repository_head_changed): primary checkout is not on main. Recovery branch: cao/task-007.artifact.203850
<!-- SECTION:NOTES:END -->
