---
id: HUM-027
title: Show startup progress while up waits for readiness
status: To Do
assignee: []
created_date: '2026-09-06 00:13'
updated_date: '2026-09-06 05:08'
labels:
  - cli
  - human
  - output
  - docs
milestone: m-3
dependencies:
  - HUM-026
modified_files:
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/manifest_test.go
  - integration/manifest_test.go
  - docs/design.md
priority: medium
type: enhancement
ordinal: 4700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: human `hum up` reports bounded startup progress while it waits, so operators can see which declarations launched, became ready, failed, or timed out without following child output. Existing final summaries remain on stdout.

Why now: `up` currently waits for every readiness result before writing anything. A slow or misconfigured declaration therefore looks hung even when other processes launched or became ready, and the timeout result does not point operators to the retained diagnostics already owned by hum.

Scope, activation and streams: progress is enabled only for CLI `hum up` in default human mode when readiness waiting is enabled. Progress uses stderr; the existing full per-declaration human results remain on stdout after all declarations settle and remain lexical by name. `hum up --json` suppresses progress entirely, keeps stderr empty on success, and emits exactly the existing final NDJSON objects on stdout. `hum up --no-wait`, CLI `start`, and MCP `up` keep their existing output and timing behavior.

Scope, bounded progress contract: each declaration emits one newline-terminated progress line as soon as its launch, observation, error, or dependency-blocked result is known. A declaration that enters `starting` emits one additional line when it becomes ready, exits, or times out, for a maximum of two progress lines per declaration. Waiting lines are `hum up: NAME: started; waiting for readiness` or `hum up: NAME: already running; waiting for readiness`. Immediate terminal lines are `hum up: NAME: started; ready`, `hum up: NAME: already running; ready`, `hum up: NAME: started; readiness unverified`, `hum up: NAME: already running; readiness unverified`, `hum up: NAME: error: MESSAGE`. Dependency-blocked terminal lines preserve HUM-026’s distinction: `hum up: NAME: skipped (blocked by A, B); existing process running`, `hum up: NAME: skipped (blocked by A, B); existing process exited`, or `hum up: NAME: skipped (blocked by A, B); not launched`. Readiness terminal lines are `hum up: NAME: ready`, `hum up: NAME: exited before readiness; inspect retained logs: hum logs NAME`, and `hum up: NAME: readiness timed out; inspect retained logs: hum logs NAME`.

Scope, concurrency and compatibility: progress order reflects actual transition completion and is intentionally not lexical. Concurrent writers serialize complete lines without ANSI control sequences or cursor rewriting. HUM-026 dependency-gated declarations report progress only when actually launched or when finalized as skipped. Final stdout result shape, lexical ordering, aggregate exit precedence, readiness timeouts, and successful child lifetime are unchanged.

Docs: CLI help and docs/design.md document when progress appears, stderr versus stdout, the two-line bound, temporal progress ordering, and timeout/early-exit log guidance.

Non-goals: streaming or tailing child output; copying retained diagnostics into `up`; spinners, terminal detection, or interactive rendering; JSON or MCP progress events; progress for `start` or `up --no-wait`; daemon, protocol, readiness, dependency scheduling, result schema, or exit-code changes; Windows-specific behavior.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/cli -run "^TestUpHumanProgress$" -count=1 -v` exits 0 and prints `--- PASS: TestUpHumanProgress`. Against deterministic fake-client barriers it proves every declaration emits its exact initial human stderr line as soon as its launch, observation, error, or skipped result is known; only `starting` declarations emit a second exact ready, exited, or timeout line; output is bounded to two newline-terminated lines per declaration; concurrent transitions never interleave bytes; progress follows completion time rather than lexical order; and the final detailed human stdout summaries remain lexical and unchanged.
- [ ] #2 AC2 — `go test ./integration -run "^TestUpStartupProgress$" -count=1 -v` exits 0 and prints `--- PASS: TestUpStartupProgress`. With the built binary, a fast-ready declaration, and a gated never-ready declaration, it observes started/waiting progress and the fast ready transition on stderr while `hum up` is still running and before the other readiness timeout, proves child output is not copied into progress, and then observes the unchanged lexical stdout summaries.
- [ ] #3 AC3 — `go test ./integration -run "^TestUpReadinessTimeoutDiagnostics$" -count=1 -v` exits 0 and prints `--- PASS: TestUpReadinessTimeoutDiagnostics`. With the built binary it proves timeout and early-exit progress each name the declaration and print `hum logs NAME`, the timeout invocation exits 2 with its existing final result, the early-exit invocation preserves exit 3, and the advertised logs command reads retained child diagnostics without `up` streaming them.
- [ ] #4 AC4 — `go test ./internal/cli -run "^TestUpProgressOutputModes$" -count=1 -v` exits 0 and prints `--- PASS: TestUpProgressOutputModes`. It proves `up --json` writes no progress to stderr and stdout remains exactly one parseable, unchanged NDJSON result per declaration in lexical order; `up --no-wait`, human and JSON `start`, aggregate exit precedence, and final human stdout rendering remain unchanged.
- [ ] #5 AC5 — `go test ./internal/cli -run "^TestUpProgressDocs$" -count=1 -v` exits 0 and prints `--- PASS: TestUpProgressDocs`. It proves `hum up --help` and docs/design.md describe human-only stderr progress, unchanged final stdout and JSON behavior, the maximum of two lines per declaration, temporal progress order, no child-output streaming, and `hum logs NAME` guidance for timeout and early exit.
- [ ] #6 AC6 — `go test ./internal/cli -run "^TestUpProgressBlockedExistingState$" -count=1 -v` exits 0 and prints `--- PASS: TestUpProgressBlockedExistingState`. It proves dependency-blocked progress exactly distinguishes a retained running record, a retained exited record, and no record while preserving sorted direct blockers and the two-line bound.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
- [ ] T1 — Add a serialized, human-only `up` progress renderer and emit launch plus terminal readiness transitions from the HUM-026 scheduler without changing final result collection.
- [ ] T2 — Add deterministic CLI and built-binary coverage for prompt progress, concurrency, exact bounded lines, retained-log diagnostics, and unchanged output modes.
- [ ] T3 — Update CLI help and the design contract, then verify every focused acceptance command and the project gate.
<!-- SECTION:PLAN:END -->
