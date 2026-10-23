---
id: TASK-007
title: Add a first real client check and test
status: todo
type: task
priority: medium
labels: [client]
rank: a159721ed7e75346c
files: [client/package.json]
created: 2026-08-05T20:26:00Z
updated: 2026-08-05T20:38:00Z
---

Outcome: replace the placeholder client check/test with a real one, so client:check and client:test entering task ci proves something. An area whose gate is echo makes every acceptance criterion in that area unfalsifiable.

Scope: client/ configuration and one genuine test.

Non-goals: application features, server changes, or CI workflow changes.

Modified-file contract: client/package.json, one new client test file.

## Acceptance criteria

- [ ] task client:check exits 0 and actually lints; introducing a lint error makes it exit non-zero.
- [ ] task client:test runs at least one real assertion; breaking the asserted behavior makes task ci exit non-zero.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] An independent verifier pass returned PASS for every acceptance criterion
- [ ] The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] No test was deleted, skipped, or weakened
- [ ] No protected gate file was modified unless the owner labelled this task tooling
- [ ] Committed on main with the task ID in the commit subject and a Task: trailer

## Notes

CAO attempt task-007.artifact.203850 failed (repository_head_changed): primary checkout is not on main. Recovery branch: cao/task-007.artifact.203850
