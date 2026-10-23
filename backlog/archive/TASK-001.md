---
id: TASK-001
title: Bootstrap the CAO backlog loop
status: todo
type: chore
priority: high
labels: [loop, tooling]
rank: a018acb90f6bf3a9a
files: [cao/Taskfile.yaml, README.md]
created: 2026-08-05T20:25:00Z
updated: 2026-08-05T20:25:00Z
---

Outcome: a fresh clone can install CAO, start its local server, and enable the backlog poller and reaper schedules with one documented command.

Scope: cao/Taskfile.yaml targets and the setup instructions in README.md.

Non-goals: product features, remote providers, pushing, opening pull requests, or changing the selection rules in cao/scripts/tracker.sh.

Modified-file contract: cao/Taskfile.yaml, README.md.

## Acceptance criteria

- [ ] task cao:install exits 0 and `cao profile validate` accepts all three profiles in cao/agents/.
- [ ] task cao:up exits 0, and `cao schedule list` shows backlog-poller and reaper enabled.
- [ ] task cao:status prints "CAO runtime health: OK" while the server is running, and exits non-zero when the server is stopped.
- [ ] task cao:down exits 0 and leaves no cao-server process listening on CAO_API_PORT.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] An independent verifier pass returned PASS for every acceptance criterion
- [ ] The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] No test was deleted, skipped, or weakened
- [ ] No protected gate file was modified unless the owner labelled this task tooling
- [ ] Committed on main with the task ID in the commit subject and a Task: trailer
