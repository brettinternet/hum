---
id: DRAFT-005
title: Find which Hum process holds a TCP port across worktrees
status: Draft
assignee: []
created_date: '2026-10-10 02:13'
labels:
  - cli
  - mcp
  - process
  - contract
dependencies:
  - HUM-148
priority: low
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: when a launch fails with "address already in use" in one worktree, a human or agent can ask which Hum-supervised process (in any scope) is listening on that port, instead of running platform-specific lsof/netstat recipes and mapping PIDs back to Hum names.

Idea: opt-in aggregate inspection, for example `hum list --all --ports` and/or `hum status --port 3000` (exact surface TBD), reusing HUM-148 group TCP-listener inspection across every running owned group, plus an equivalent MCP input. Output names scope, process name, PID(s), and endpoint. A port held by a non-Hum process reports "not held by a Hum process", without identifying foreign processes.

Open questions before refinement: surface (list flag versus status selector); bounds on inspecting many groups (total deadline, partial results per group); whether cross-scope inspection needs the same privacy rules as `list --all`.

Non-goals: allocation, conflict resolution, killing the holder, monitoring.

Depends on HUM-148.
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
