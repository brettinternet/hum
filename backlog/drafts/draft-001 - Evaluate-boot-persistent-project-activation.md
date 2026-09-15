---
id: DRAFT-001
title: Evaluate boot-persistent project activation
status: Draft
assignee: []
created_date: '2026-09-15 19:29'
labels: []
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Context: `restart: on-failure` is bounded crash recovery within the current daemon lifetime. Bringing processes back after an OS reboot is boot activation, not another restart policy. It would require OS integration such as a launchd or systemd user service, durable and explicit project enablement, enable/disable and operator-stop semantics, and defined behavior for missing or moved worktrees, changed manifests, login timing, and unavailable environment or credentials.

Direction: do not overload the manifest `restart` field. Keep boot start and OS-service installation as current non-goals. The smallest near-term answer is documentation for an operator-owned native user service that invokes exact argv such as `hum --project /absolute/project up --detach`.

Revisit only when representative users need Hum-owned cross-reboot activation and external service configuration is materially insufficient. Before implementation, decide whether Hum should merely generate service definitions or own durable registration and lifecycle state across macOS and Linux.
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
