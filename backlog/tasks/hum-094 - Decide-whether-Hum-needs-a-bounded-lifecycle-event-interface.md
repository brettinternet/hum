---
id: HUM-094
title: Decide whether Hum needs a bounded lifecycle event interface
status: To Do
assignee: []
created_date: '2026-09-11 16:27'
labels:
  - architecture
  - integration
  - events
  - product-boundary
dependencies:
  - HUM-092
  - HUM-093
references:
  - HUM-092
  - HUM-093
  - docs/design.md
modified_files:
  - docs/design.md
priority: medium
type: spike
ordinal: 66800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the first shipped external adapter provides concrete evidence for a documented decision to either keep composing existing CLI operations or pursue the smallest bounded lifecycle event interface. Hum does not add an event API merely because polling is aesthetically undesirable.

Scope: use HUM-092's Herdr plugin and HUM-093's public machine-output contract to evaluate whether `list --json`, `status --json`, bounded `logs`/`wait`, and process-scoped follow/attach operations can observe the state transitions an external local client actually needs. Record measured subprocess/polling behavior, stale-state and missed-transition risks, multi-process and multi-project requirements, cancellation, cursor/reconnect semantics, and backpressure. If existing composition is sufficient, explicitly defer an event interface and state the evidence threshold for reconsideration. If it is insufficient, document the demonstrated gap and the minimum out-of-process event contract; create a separate implementation draft rather than implementing it in this spike.

The decision belongs in docs/design.md and must distinguish operator UI convenience from correctness requirements. Any future contract remains local, bounded, versioned, and outside the daemon address space.

Non-goals: implementing an event stream; changing CLI, MCP, daemon, or manifest behavior; public socket access; remote transport; lifecycle hooks; notifications; a general plugin API; or speculative support for consumers not represented by HUM-092.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `python3 -m unittest discover -s plugins/herdr -p 'test_*.py' -v` exits 0 and reproduces the external adapter scenarios used as evidence in the decision.
- [ ] #2 AC2 — `rg -n 'Lifecycle event interface decision|Evidence from HUM-092|Polling and race analysis|Backpressure and cursor semantics|Decision: (defer|proceed)' docs/design.md` exits 0 and the matched section records concrete evidence, the chosen decision, and the objective threshold for revisiting it.
- [ ] #3 AC3 — `rg -n 'private.*daemon|bounded|versioned|out-of-process' docs/design.md` exits 0 and the decision preserves Hum's private daemon boundary and states the minimum constraints for any future contract.
- [ ] #4 AC4 — `git diff --check && task check` exits 0; the source diff is limited to docs/design.md, and any recommended implementation is represented only by a separately created provider draft.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->
