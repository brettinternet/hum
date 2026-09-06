---
id: HUM-028
title: Follow logs from multiple resolved processes
status: Done
assignee: []
created_date: '2026-09-06 00:37'
updated_date: '2026-09-06 03:19'
labels:
  - cli
  - logs
  - human
  - json
  - docs
milestone: m-3
dependencies:
  - HUM-020
modified_files:
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/list_logs_test.go
  - internal/cli/surface_test.go
  - integration/logs_test.go
  - README.md
  - docs/design.md
priority: medium
type: feature
ordinal: 5700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: one hum logs invocation can read or continuously follow several project processes, including the complete declaration set that hum up resolves, so operators do not need one terminal per process.

CLI contract:

    hum logs web worker --follow
    hum logs --follow

NAME becomes optional and repeatable. One or more explicit names select exactly those durable sessions. Omitting names resolves the same current hum.yaml or zero-configuration declaration set as hum up, in lexical order; it does not include ad-hoc runtime sessions. The selection is captured when logs starts and does not change if hum.yaml changes later. If no names are supplied and no declaration resolves, return a clear user-facing error. The daemon does not retain provenance saying a process was launched specifically by up, so the no-name form follows the current resolved declaration set rather than a historical launch set.

Bounded and follow behavior: the plural interface applies both to bounded reads and --follow. Stream, tail, match, and byte-limit options apply independently to each selected process. Bounded results are emitted in selection order. Follow opens one durable follower per selected process and serializes events as they arrive; each follower retains the existing pre-launch, exit, wait, relaunch, down/up, removal, cancellation, and daemon-loss behavior. Ctrl+C closes every follower without signaling managed processes. --after-cursor remains process-local and therefore requires exactly one explicit NAME; aggregate use fails before contacting or starting the daemon with actionable guidance. Duplicate explicit names fail validation rather than attaching twice.

Rendering and compatibility: a single explicit NAME preserves existing human and JSON output byte-for-byte. Aggregate human output prefixes every emitted child or system entry with [NAME] and writes each prefix plus entry atomically so concurrent followers cannot interleave bytes. It does not otherwise rewrite the retained entry or invent a trailing newline. Aggregate JSON remains NDJSON with one existing event object per underlying event; every object carries its process name, with no new aggregate envelope or protocol schema. A failure isolated to one selected session is identified by name and does not discard already emitted output from other sessions; daemon loss or output failure cancels the whole aggregate and returns nonzero.

Docs: command help, README.md, and docs/design.md document plural and no-name selection, fixed declaration membership, per-process filters and limits, aggregate prefixes, process-local cursor restriction, Ctrl+C behavior, and the recommended hum up followed by hum logs --follow workflow.

Non-goals: adding --follow to hum up; persisting which command launched a process; following every project or daemon-wide ad-hoc session; dynamically adding declarations while following; a daemon-level aggregate protocol; globally ordered daemon timestamps or cursors; MCP follow support; changing retained output, single-name rendering, lifecycle, or process state.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — go test ./internal/cli -run "^TestLogsMultipleNames$" -count=1 -v exits 0 and prints --- PASS: TestLogsMultipleNames. It proves explicit bounded multi-name reads preserve argument order, apply stream/tail/match/limit independently per process, prefix every aggregate human entry atomically, emit named existing-schema NDJSON objects without an envelope, reject duplicates, and leave single-explicit-name human and JSON output unchanged.
- [x] #2 AC2 — go test ./integration -run "^TestLogsFollowMultipleProcesses$" -count=1 -v exits 0 and prints --- PASS: TestLogsFollowMultipleProcesses. With the built binary it starts at least two hum.yaml declarations through hum up, runs hum logs --follow with no names, observes distinctly prefixed output from both while they write concurrently, proves the fixed resolved selection excludes an ad-hoc session, follows each declaration across down and a later up, and proves Ctrl+C detaches all followers without stopping either child.
- [x] #3 AC3 — go test ./internal/cli -run "^TestLogsAggregateValidationAndLifecycle$" -count=1 -v exits 0 and prints --- PASS: TestLogsAggregateValidationAndLifecycle. It proves omitted names use the lexical hum up resolution set including zero-config resolution, no resolvable declarations return actionable guidance, --after-cursor requires exactly one explicit name before daemon startup, one follower ending does not corrupt other output, and daemon loss or writer failure cancels all followers and returns nonzero without goroutine or connection leaks.
- [x] #4 AC4 — go test ./internal/cli -run "^TestLogsAggregateDocs$" -count=1 -v exits 0 and prints --- PASS: TestLogsAggregateDocs. It proves hum logs --help, README.md, and docs/design.md describe NAME as optional/repeatable, hum up followed by hum logs --follow, fixed current-declaration selection, per-process filters and limits, [NAME] human prefixes, named NDJSON events, the process-local --after-cursor restriction, and unchanged single-name behavior.
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
1. Resolve and validate optional/repeatable log names before daemon startup, preserving the single-explicit-name path and using lexical declaration order when omitted.
2. Extend bounded rendering to process each selected session independently in selection order, with atomic [NAME] human prefixes and existing named JSON objects.
3. Fan in one durable follower per selected session with coordinated cancellation and isolated per-session errors while preserving lifecycle and Ctrl+C semantics.
4. Add focused CLI/integration/docs coverage and run every AC command plus task ci.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented optional/repeatable aggregate logs with lexical no-name declaration selection, per-process bounded reads, serialized multi-follower streaming, atomic human prefixes, and named NDJSON. Reviewer findings fixed: empty declaration sets now fail before daemon startup, and remove/recreate closes only the old follower while other followers continue.

AC#1 evidence — `go test ./internal/cli -run "^TestLogsMultipleNames$" -count=1 -v` exited 0 and printed `--- PASS: TestLogsMultipleNames`.
AC#2 evidence — `go test ./integration -run "^TestLogsFollowMultipleProcesses$" -count=1 -v` exited 0 and printed `--- PASS: TestLogsFollowMultipleProcesses`.
AC#3 evidence — `go test ./internal/cli -run "^TestLogsAggregateValidationAndLifecycle$" -count=1 -v` exited 0 and printed `--- PASS: TestLogsAggregateValidationAndLifecycle`.
AC#4 evidence — `go test ./internal/cli -run "^TestLogsAggregateDocs$" -count=1 -v` exited 0 and printed `--- PASS: TestLogsAggregateDocs`.

Independent verifier: PASS for AC1–AC4, allowed-path scope, no deleted/skipped/weakened tests, empty declarations, and remove/recreate lifecycle. `task ci` passed on implementation commit be7a5b0 and again in a clean detached worktree at merged main commit f88540c. Implementation touched only the seven declared paths. Review outcome: PASS after all findings were resolved.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented plural `hum logs` selection and no-name declaration resolution for bounded and follow modes, preserving single-name output. Aggregate human output is atomically name-prefixed; JSON remains named NDJSON; follower cancellation and isolated session lifecycle are covered. Verified all AC commands independently and passed `task ci` on merged main commit f88540c.
<!-- SECTION:FINAL_SUMMARY:END -->
