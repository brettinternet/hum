---
id: HUM-026
title: Order up launches by declared readiness dependencies
status: Done
assignee:
  - '@brett'
created_date: '2026-09-05 15:14'
updated_date: '2026-09-06 05:45'
labels:
  - config
  - cli
  - docs
milestone: m-3
dependencies:
  - HUM-025
modified_files:
  - internal/project/
  - internal/cli/
  - internal/mcp/
  - internal/skill/
  - internal/testutil/
  - integration/
  - plugins/hum/skills/hum/SKILL.md
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: feature
ordinal: 3700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a declared process can list the declared processes that must become ready before it launches, so `hum up` brings a database, queue, API, and web server up in dependency order without hand-written `start` and `wait` sequences. Independent processes continue to launch and wait concurrently.

Why now: readiness already establishes when one process is usable. Applying it to manifest-local launch ordering closes the main multi-process orchestration gap while keeping the daemon unaware of project configuration and avoiding a general workflow engine.

Scope, manifest: `after` is an optional per-process list of declared process names in `hum.yaml`, defaulting to an empty `After []string`. Every name must be unique in that list, exist in the same manifest, differ from the owning entry, and declare `ready`; a definition without `ready` can only be `running_unverified` and cannot satisfy a gate. Unknown names, duplicates, self-reference, cycles, dependencies without readiness, non-list values, and non-string elements fail with file, process, and indexed-field context such as `process "web".after[1]`. Validation detects the full manifest graph, including cycles longer than two. Definition copy/clone paths copy `After` without slice aliasing. Discovered definitions always have empty `After`. `init` templates include an inert commented `after: [db]` example and remain valid.

Scope, up scheduler: ordering is resolved and enforced by CLI and MCP clients because they own project definitions and send exact launch requests; daemon behavior and the private protocol remain unchanged. `up` starts every zero-dependency definition concurrently. A dependent is considered only after all direct prerequisites have terminal results. A prerequisite satisfies its gate only when this invocation observes it as `started` or `already_running` with readiness `ready`; an already-ready running process satisfies immediately without relaunch. When all direct prerequisites satisfy, the dependent launches. Its own readiness timeout begins only when it launches or is first observed already running, so roots overlap and total wall time is bounded by the sum of timeouts along the DAG's critical path rather than one global timeout.

Scope, blocked results: if any direct prerequisite finishes as request `error`, `exited_before_ready`, `timed_out`, or `skipped`, the dependent is not launched and returns `outcome: skipped`. JSON/MCP add `blocked_by: []string`, present only for skipped results and containing every direct unsatisfied prerequisite sorted by name. Cascades name the direct blocker: if db blocks api and api blocks web, web reports `blocked_by:["api"]`. The scheduler waits for every direct prerequisite result before finalizing the array. Human output is `NAME: skipped (blocked by A, B)`.

Scope, aggregate and output compatibility: every launchable definition is attempted and successful children remain running. `skipped` is a per-process outcome, not a new process exit code: because every skip has an included causal result, aggregate CLI precedence stays request error 1, `exited_before_ready` 3, `timed_out` 2, success 0. CLI human and NDJSON results remain serialized in lexical definition-name order after all nodes settle; MCP returns its array in the same lexical order. Concurrency therefore changes launch timing, not stable presentation order.

Scope, no-wait and other commands: CLI `up --no-wait` and MCP `up` with `no_wait:true` reject the whole request before daemon creation/contact whenever any definition has a non-empty `after`, even if its prerequisites are already ready. `start NAME...` launches exactly the explicitly named processes concurrently under its current semantics and never adds or waits for transitive prerequisites; MCP `start` remains singular with the same rule. `restart`, `stop`, `down`, and `remove` are unchanged, and `down` remains concurrent rather than reverse dependency order. JSON/status snapshots do not expose `after`; it is manifest-local orchestration state.

Scope, restart-policy composition: this task follows HUM-025. If a prerequisite incarnation exits before readiness, this `up` records `exited_before_ready` and skips its dependents even when `restart: on-failure` schedules a successor. One bounded `up` invocation does not follow automatic successors; after recovery, an operator or agent reruns `up`, which observes the ready prerequisite and launches the blocked nodes.

Docs: docs/design.md removes dependencies from the manifest exclusions, documents validation, gating, skipped/blocked results, stable ordering, unchanged exit precedence, and no-wait rejection. README.md includes a real three-process example. docs/coding-agents.md, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` tell agents to prefer `up` over sequencing starts when `after` is declared and to rerun `up` after an automatically recovering prerequisite.

Non-goals: dependencies on ad hoc or discovered definitions; `start` pulling in prerequisites; reverse-order `down`; continuous health propagation; restarting/stopping dependents when a prerequisite exits or relaunches; following an automatic successor within the same `up`; port, HTTP, or command health gates; daemon-side dependency state or protocol changes; per-edge timeouts; exposing `after` in runtime snapshots; Windows.

Scope, blocked existing sessions: before finalizing a dependency-blocked node, CLI and MCP make a read-only observation of its retained record. The result remains `outcome: skipped` with sorted direct `blocked_by`; when a record exists it also includes `existing_state: running|exited` and the applicable snapshot-backed PID, launch, readiness, restart, relaunch, and `next_launch_at` fields. Human output separately says `existing process running`, `existing process exited`, or `not launched`. Observation never stops, starts, restarts, or otherwise mutates the record. A skipped result never satisfies a downstream gate, even when its retained record is running. Definition-drift classification and recovery policy remain HUM-029 and HUM-030.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `go test ./internal/project -run "^TestAfterManifest$" -count=1 -v` exits 0 and prints `--- PASS: TestAfterManifest`. It proves absent/empty `after` becomes empty `After []string`; valid lists preserve names; unknown, duplicate, self, dependency-without-ready, non-list, non-string, and cycles of lengths 2, 3, and longer fail with file/process/index context; discovered definitions always have empty `After`; copies do not alias the source slice; and generated/template manifests remain valid with the inert commented example.
- [x] #2 AC2 — `go test ./internal/cli -run "^TestUpOrdersByAfter$" -count=1 -v` and `go test ./internal/mcp -run "^TestUpOrdersByAfter$" -count=1 -v` both exit 0 and print the corresponding named PASS line. Against fake daemon clients they prove independent roots launch concurrently; a node waits for all direct prerequisites and launches only when each invocation result is started/already-running and ready; already-ready processes satisfy without relaunch; per-process timeout begins at its own launch/observation; request error, exited-before-ready, timeout, and skipped cascade without a launch; `blocked_by` includes all and only sorted direct blockers; CLI human/NDJSON and MCP arrays stay lexical; aggregate exit precedence remains 1/3/2/0 with no skipped exit code; CLI `start NAME...` launches only requested names concurrently and MCP start remains singular; and no-wait rejection occurs before any fake daemon call.
- [x] #3 AC3 — `go test ./integration -run "^TestUpOrderedStack$" -count=1 -v` exits 0 and prints `--- PASS: TestUpOrderedStack`. With the built binary and a manifest of db, api (`after: [db]`), and web (`after: [api]`) whose fixtures record launch/readiness times, it proves each launch follows prerequisite readiness, independent roots overlap, `hum up` exits 0 with three lexical success results, and a second run is idempotent. Failure cases prove db exit before readiness returns exit 3 with api/web skipped and not launched, multiple failed roots produce complete sorted direct blockers, and an `on-failure` db successor is not followed by the same invocation but a later `hum up` launches the blocked nodes after db is ready.
- [x] #4 AC4 — `go test ./internal/cli ./internal/skill -run "^TestAfterDocs$" -count=1 -v` exits 0 and prints both named PASS lines. README.md, docs/design.md, docs/coding-agents.md, CLI help, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` document `after` validation, readiness gates and per-process timing, `skipped` plus sorted direct `blocked_by`, stable lexical output and unchanged exit precedence, pre-contact no-wait rejection, explicit-only start, concurrent down, and rerunning up after automatic prerequisite recovery; docs/design.md no longer lists dependencies as a manifest exclusion.
- [x] #5 AC5 — `go test ./internal/cli ./internal/mcp -run "^TestUpReportsBlockedExistingState$" -count=1 -v` exits 0 and prints a named PASS line for both packages. It proves blocked running and exited records remain untouched and are distinguished from an absent record in human, CLI NDJSON, and MCP results; no lifecycle request is sent for the blocked node; sorted direct blockers and aggregate exit precedence remain unchanged; and a skipped node with a running record still blocks its dependent.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
- [x] T1 — Add strict manifest graph decoding/validation and reusable, non-aliasing dependency metadata.
- [x] T2 — Add client-side DAG scheduling for CLI and MCP with concurrent roots, per-node readiness waits, complete blocker propagation, and stable result order.
- [x] T3 — Observe and render retained state for dependency-blocked nodes without changing skipped gating.
- [x] T4 — Preserve start/down/protocol behavior, reject no-wait before daemon contact, and prove HUM-025 recovery composition.
- [x] T5 — Update operator/agent documentation and prove ordered and blocked stacks with the built binary.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit dca091c implements ordered readiness dependencies and blocked retained-state reporting.
AC#1 PASS — go test ./internal/project -run "^TestAfterManifest$" -count=1 -v.
AC#2 PASS — go test ./internal/cli -run "^TestUpOrdersByAfter$" -count=1 -v; go test ./internal/mcp -run "^TestUpOrdersByAfter$" -count=1 -v.
AC#3 PASS — go test ./integration -run "^TestUpOrderedStack$" -count=1 -v.
AC#4 PASS — go test ./internal/cli ./internal/skill -run "^TestAfterDocs$" -count=1 -v.
AC#5 PASS — go test ./internal/cli ./internal/mcp -run "^TestUpReportsBlockedExistingState$" -count=1 -v.
DoD evidence — task ci passed after commit dca091c; independent reviewer returned no findings; independent verifier passed AC1-AC5 and confirmed no tests were deleted, skipped, or weakened. All implementation paths are within the declared modified-file contract; no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented manifest after validation and client-side readiness DAG scheduling for CLI/MCP, including stable skipped blockers, retained existing-state snapshots, timeout/incarnation fencing, no-wait rejection, integration coverage, and operator/agent documentation. All AC1-AC5 commands and task ci pass; independent review and verification pass.
<!-- SECTION:FINAL_SUMMARY:END -->
