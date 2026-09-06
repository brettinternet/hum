---
id: HUM-047
title: Make attached run usable for one-shot commands
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 19:10'
labels:
  - cli
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/serve_run_test.go
  - internal/cli/run_args_test.go
  - internal/cli/flag_alias_test.go
  - internal/cli/surface_test.go
  - docs/design.md
  - README.md
priority: medium
type: enhancement
ordinal: 24700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum run NAME --once -- COMMAND` attaches to one incarnation, returns as soon as that incarnation exits, and propagates its exit code; signal exits map to 128+signal. The default attached run keeps the durable follow-across-launches-until-Ctrl+C behavior. Attached launch and reattach accept `--tail N` to bound retained replay before live output; `--tail 0` suppresses retained replay.

Scope: add command-local `--once` and `--tail N` flags to run, including its post-NAME option parser, help, human behavior, exit mapping, tests, README.md, and docs/design.md. `--once` is valid only for attached mode and conflicts with `--detach`; tail must be non-negative, affects only client replay, and never changes daemon retention. A fast process that has already exited by the time attachment is established returns promptly with that incarnation exit rather than waiting for another launch.

Why now (observed 2026-09-06): `timeout 4 hum run build -- sh -c 'exit 2'` never returns because output ends with `build waiting for next launch`, so run cannot be a CI or script step. Reattaching can also dump a large stale replay before live output.

Non-goals: changing detached run, changing the default durable attached mode, changing `logs --follow`, or changing retained-output storage.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestRunOnceExitCode$' -count=1 -v` exits 0 and prints PASS for child exit 0 and nonzero codes, signal-to-128+signal mapping, a child that exits before attachment completes, and no wait for a subsequent launch.
- [ ] #2 `go test ./internal/cli -run '^TestRunAttachTail$' -count=1 -v` exits 0 and prints PASS for exact final-N replay in source order, `--tail 0` suppressing replay while preserving live output, and no daemon retention mutation.
- [ ] #3 `go test ./internal/cli -run '^TestRunOnceParsingAndDefaults$' -count=1 -v` exits 0 and prints PASS for pre-NAME and post-NAME flag positions, actionable rejection of `--once --detach` and negative tails before daemon contact, unchanged detach behavior, and unchanged durable attachment across stop/relaunch when `--once` is absent.
- [ ] #4 `go test ./internal/cli -run '^TestRunOnceDocs$' -count=1 -v` exits 0 and prints PASS, proving help, README.md, and docs/design.md document copy-pasteable one-shot and bounded-reattach examples.
- [ ] #5 `task ci` exits 0.
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
1. Extend the run flag surface and special post-NAME parser with `--once` and `--tail` validation.
2. Bound initial replay and terminate one-incarnation attachment with exact exit propagation while preserving default durable following.
3. Cover fast exits, signals, replay, conflicts, help, docs, and unchanged default behavior.
<!-- SECTION:PLAN:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: maintainer
created: 2026-09-06 19:10
---
Product-boundary review: archive this active task. Its bounded replay need is consolidated into HUM-055 (`hum attach --tail N`). Its one-incarnation exit behavior remains preserved as deferred DRAFT-002 and should be promoted only with evidence of a development-session use case that direct shell, Task, or Just execution cannot satisfy.
---
<!-- COMMENTS:END -->
