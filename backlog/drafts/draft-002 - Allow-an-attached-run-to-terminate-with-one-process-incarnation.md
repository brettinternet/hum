---
id: DRAFT-002
title: Allow an attached run to terminate with one process incarnation
status: Draft
assignee: []
created_date: '2026-09-06 19:10'
labels:
  - cli
  - deferred
  - product-boundary
dependencies: []
references:
  - HUM-047
  - HUM-055
modified_files:
  - internal/cli/
  - README.md
  - docs/design.md
priority: low
type: enhancement
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Deferred outcome: optionally let an attached `hum run` return when one supervised incarnation exits and propagate that exit status. This preserves the use case recorded in HUM-047 without keeping finite command and CI behavior in the active product queue.

Why deferred: Hum is being kept as a dev-only bridge for long-running processes that outlive individual shells and agents. Optimizing `hum run` as a finite build, test, script, or CI step overlaps Task, Just, and direct shell execution and weakens that boundary. The valid bounded-replay portion of HUM-047 has moved to HUM-055 as `hum attach --tail N`.

Promotion trigger: demonstrate a development-only workflow where a process must remain daemon-owned and independently observable while one client needs exactly one incarnation’s exit status, and where invoking the finite command directly cannot satisfy the requirement. Promotion must explicitly exclude becoming a general task-execution or CI API.

Potential shape if promoted: an opt-in one-incarnation mode on attached run, exact exit and signal mapping, fast-exit race handling, no change to the default durable follow-across-launches behavior, and no MCP arbitrary-command expansion.

Non-goals unless separately reconsidered: task graphs, CI orchestration, caching, shell command strings, output artifacts, or changing daemon retention.
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
