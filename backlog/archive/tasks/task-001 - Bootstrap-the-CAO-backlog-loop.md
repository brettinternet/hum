---
id: TASK-001
title: Bootstrap the CAO backlog loop
status: To Do
assignee: []
created_date: '2026-08-05 20:25'
labels:
  - loop
  - tooling
dependencies: []
modified_files:
  - cao/Taskfile.yaml
  - README.md
priority: high
type: chore
ordinal: 100
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a fresh clone can install CAO, start its local server, and enable the backlog poller and reaper schedules with one documented command.

Scope: cao/Taskfile.yaml targets and the setup instructions in README.md.

Non-goals: product features, remote providers, pushing, opening pull requests, or changing the selection rules in cao/scripts/tracker.sh.

Modified-file contract: cao/Taskfile.yaml, README.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cao:install exits 0 and `cao profile validate` accepts all three profiles in cao/agents/.
- [ ] #2 task cao:up exits 0, and `cao schedule list` shows backlog-poller and reaper enabled.
- [ ] #3 task cao:status prints "CAO runtime health: OK" while the server is running, and exits non-zero when the server is stopped.
- [ ] #4 task cao:down exits 0 and leaves no cao-server process listening on CAO_API_PORT.
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
