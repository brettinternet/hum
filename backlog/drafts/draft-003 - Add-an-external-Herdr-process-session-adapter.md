---
id: DRAFT-003
title: Add an external Herdr process-session adapter
status: Draft
assignee: []
created_date: '2026-09-06 19:10'
labels:
  - herdr
  - integration
  - deferred
dependencies:
  - HUM-055
references:
  - HUM-055
priority: low
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Deferred outcome: Herdr can present Hum-managed development sessions in its own panes without Hum depending on Herdr or learning pane, layout, workspace, or focus concepts. A process picker uses `hum list --json`; read-only panes run `hum logs NAME --follow`; interactive panes run the non-starting `hum attach NAME`; lifecycle actions invoke existing CLI commands.

Why deferred: the Hum-side primitive is tracked by HUM-055, while the adapter belongs in Herdr and should be validated there. Building it now would guess at Herdr UX and could create another in-core orchestration path before the existing CLI/MCP duplication is consolidated.

Promotion trigger: HUM-055 exists and a concrete Herdr workflow identifies an owner repository, UI entry point, and locally executable acceptance path. Start with subprocess composition against the public CLI; do not consume Hum’s private daemon protocol.

Integration boundary: Herdr owns discovery UI, pane creation, labels, focus, and terminal lifecycle. Hum owns supervised processes, retained output, lifecycle state, and the exclusive TTY input lease. The UI must label existing `hum run NAME` behavior as Start/attach, never Attach, because it may launch a stopped process. Agents continue using bounded MCP logs, wait, and input rather than terminal screen scraping.

Non-goals: a Hum-side Herdr command; tmux; raw socket access; remote transport; arbitrary terminal control through MCP; or a general plugin API.
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
