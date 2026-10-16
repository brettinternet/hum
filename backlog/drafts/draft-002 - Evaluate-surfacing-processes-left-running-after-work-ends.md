---
id: DRAFT-002
title: Evaluate surfacing processes left running after work ends
status: Draft
assignee: []
created_date: '2026-09-15 20:08'
updated_date: '2026-10-16 17:12'
labels:
  - process
  - cli
  - product-boundary
dependencies: []
type: spike
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Problem: processes started with `hum up` or `hum run --detach` outlive the person or agent that started them, and parallel worktrees multiply them. This draft started as a launch-scoped lease (`hum up --stop-after 8h`); refinement on 2026-10-16 moved it toward making abandoned processes visible instead.

Evidence (2026-10-16, hum 0.17.1, isolated runtime directory): `hum list --all` shows name, state, and PID but not how long a process has run; JSON already has `started_at`. After the project directory is deleted, `list --all` still shows that project with no hint the root is gone, and `hum --project <deleted root> down` still stops its processes. The Worktrunk config in examples/worktrees/wt.toml has a `pre-start` hook but no `pre-remove` hook that runs `hum down`.

Leading direction (observation only, no timers):
1. Show uptime for running processes in human `hum list` output.
2. Mark a project group whose root no longer exists in `list --all`, in human output and JSON, keeping the `hum --project ROOT down` selector.
3. Add a `pre-remove` hook running `hum down` to examples/worktrees/wt.toml and document the lifecycle-hook pattern. This is docs-only and could ship alone.

Deferred: a daemon-enforced lease. It is time-based scheduling, which decision-001 lists as a non-goal, and it needs answers for crash relaunch, explicit restart, sleep and suspend, daemon crash recovery, dependencies, the global scope, and the JSON, MCP, and event contracts. External options already exist: a supervised reaper such as `hum --global run reaper -- sh -c 'sleep 28800; hum --project ROOT down'`, or OS or Herdr automation. Wrapping argv in `timeout 8h` is a poor substitute: its exit status 124 looks like a crash, so `restart: on-failure` relaunches it. Reconsider a lease only with a concrete workflow that items 1-3 do not fix.

Rejected: stopping processes automatically when their project root disappears (deleting a directory is not a stop request; reporting it is enough), and stopping on display sleep or screen lock (an unreliable abandonment signal that is platform-specific and interrupts intentionally unattended work; use OS or Herdr automation).

Open questions before promotion: uptime in default `list` or only with `--full` or `--all`; the JSON field name for a missing root; the cost and Windows behavior of checking each root (one stat per root per list); whether aggregate `status` or `doctor` should also flag missing roots; whether items 1-3 are one task or three.

This draft is not implementation-ready. Promotion needs an explicit outcome, scope, non-goals, modified-file contract, and locally executable acceptance evidence.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->
