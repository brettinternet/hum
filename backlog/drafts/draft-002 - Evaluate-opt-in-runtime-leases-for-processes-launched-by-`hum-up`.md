---
id: DRAFT-002
title: Evaluate opt-in runtime leases for processes launched by `hum up`
status: Draft
assignee: []
created_date: '2026-09-15 20:08'
labels:
  - process
  - daemon
  - product-boundary
dependencies: []
type: spike
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Long-lived development processes started through `hum up` can be forgotten after an agent or person finishes working, leaving services running for hours. Explore whether Hum should offer an opt-in, daemon-enforced maximum lifetime while preserving the current default of running until explicitly stopped. The leading shape is a launch-scoped lease such as `hum up --stop-after 8h`, rather than a generic `--timeout` that could be confused with readiness and wait timeouts. A lease would likely apply only to processes newly launched by that invocation, survive automatic crash relaunches, reset on an explicit restart, expire through the normal graceful-stop path, and expose an explicit expiry reason. These are hypotheses to refine, not settled requirements. Compare this with keeping lifetime enforcement outside Hum through platform-native command wrappers or external automation. Determine how a lease should interact with already-running processes, partial or failed `up` launches, dependencies, manual start/restart, daemon crash recovery, system suspend, global scope, JSON/MCP/event contracts, and manifest-defined versus invocation-only configuration. Stopping processes when displays turn off is related motivation but should remain a separate question. Display sleep or lock is an unreliable proxy for abandonment, can interrupt intentionally unattended work, and is platform-specific. Prefer external OS or Herdr automation unless concrete workflows justify first-class integration. This draft is intentionally not implementation-ready. Refinement should produce an explicit outcome, scope, non-goals, modified-file contract, and locally executable acceptance evidence before promotion.
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
