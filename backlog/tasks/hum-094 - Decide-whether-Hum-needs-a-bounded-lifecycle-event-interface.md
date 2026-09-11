---
id: HUM-094
title: Decide whether Hum needs a bounded lifecycle event interface
status: To Do
assignee: []
created_date: '2026-09-11 16:27'
updated_date: '2026-09-11 17:02'
labels:
  - architecture
  - integration
  - events
  - product-boundary
milestone: m-5
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

## The question

HUM-092 initially refreshes its process picker by running `hum list --json` on open, after lifecycle actions, and when the user requests refresh. That may be entirely sufficient. The possible gap is between snapshots:

```text
t0      plugin runs list --json -> api is running
t0+10ms api exits
 t1     picker still shows running until its next refresh
```

A stale display is a normal UI concern if refresh is cheap and actions revalidate state. It becomes a correctness gap only if a real consumer must observe every transition, cannot safely act from a fresh snapshot, or must reconnect without losing events. Polling can also miss a fast `starting -> exited -> recovery_pending` sequence even though the final snapshot is correct.

Process-scoped `logs --follow`, `attach`, and bounded `wait` already cover known sessions. They do not provide one project-wide lifecycle feed for newly created, removed, or renamed sessions. This spike decides whether HUM-092 demonstrates a need for that feed.

## Evidence to collect

Use the shipped Herdr plugin and its tests to record:

- Which state changes the UI actually needs versus which are merely nice to animate.
- Whether open/action/manual refresh produces correct operations despite stale display state.
- Subprocess startup and polling cost at realistic process counts.
- Whether any required transition is missed rather than recoverable from the next snapshot.
- Multi-process and multi-project behavior.
- Cancellation and cleanup when a Herdr pane closes.
- What reconnect, cursor retention, truncation, and backpressure guarantees a stream would require.
- Whether existing `logs`, `wait`, or follow cursors can solve the demonstrated gap without a new API.

## Decision rubric

Record `Decision: defer` when on-demand/action-triggered refresh is correct, periodic refresh is acceptably cheap if desired, and no user workflow requires every intermediate transition. State the measurable threshold that would reopen the decision.

Record `Decision: proceed` only when HUM-092 demonstrates a concrete correctness or unacceptable-cost problem that existing commands cannot solve. In that case, document the smallest viable out-of-process contract and create a separate implementation draft. Do not implement it in this spike.

A possible shape for discussion, not a committed API, is a project-scoped retained event cursor with bounded reads:

```sh
hum events --after-cursor 42 --limit 100 --json
```

```json
{"schema_version":1,"cursor":43,"type":"state","name":"api","from":"running","to":"exited"}
```

A bounded wait could then wait for events after a cursor. Any proposal must explain retention/truncation, reconnect behavior, daemon-restart behavior, ordering, slow consumers, and why process snapshots are insufficient. An indefinite global stream, public daemon socket, or callback executing user code inside Hum is not an acceptable shortcut.

## Deliverable

Add a `Lifecycle event interface decision` section to docs/design.md with subsections `Evidence from HUM-092`, `Polling and race analysis`, and `Backpressure and cursor semantics`, followed by exactly `Decision: defer` or `Decision: proceed`. Include the evidence, rejected alternatives, objective reconsideration trigger, and any separately created follow-up draft ID.

The decision must distinguish operator UI convenience from correctness requirements. Any future contract remains local, bounded, versioned, and out-of-process.

## Non-goals

Implementing an event stream; changing CLI, MCP, daemon, or manifest behavior; public socket access; remote transport; lifecycle hooks; notifications; a general plugin API; or speculative support for consumers not represented by HUM-092.
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
