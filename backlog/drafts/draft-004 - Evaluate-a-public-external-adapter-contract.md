---
id: DRAFT-004
title: Evaluate a public external-adapter contract
status: Draft
assignee: []
created_date: '2026-09-06 19:10'
labels:
  - architecture
  - integration
  - deferred
  - product-boundary
dependencies:
  - HUM-035
references:
  - DRAFT-003
  - docs/design.md
priority: low
type: spike
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Deferred outcome: after real integrations expose repeated friction, publish the smallest stable out-of-process contract needed by terminal UIs and other local development clients. Prefer existing CLI JSON/NDJSON and stdio MCP; add a primitive only when subprocess composition cannot express the requirement safely.

Why deferred: one speculative Herdr adapter is not evidence for a general extension system. Hum’s daemon protocol is intentionally private, and CLI/MCP orchestration already has known duplication tracked by HUM-035. Declaring a plugin ABI or public socket now would freeze internals and add compatibility, trust, and crash-isolation obligations without a second consumer.

Promotion trigger: at least two concrete external integrations need the same missing capability, the Herdr experiment has documented why current CLI/MCP composition is insufficient, and HUM-035 has removed semantic drift that a new adapter could otherwise repeat.

Potential narrow shapes: clarify and version selected CLI JSON/NDJSON envelopes, expose an explicit non-starting terminal handoff, or add a bounded lifecycle event stream. Choose only the demonstrated gap.

Non-goals: dynamically loaded Go plugins; plugin-defined manifest fields; lifecycle hooks; arbitrary daemon code execution; stabilizing the private socket wholesale; remote TCP control; authentication; custom schedulers, output stores, or readiness engines; a plugin marketplace.
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
