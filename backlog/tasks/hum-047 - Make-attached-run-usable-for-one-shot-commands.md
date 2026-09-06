---
id: HUM-047
title: Make attached run usable for one-shot commands
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - cli
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/serve_run_test.go
  - docs/design.md
  - README.md
priority: medium
type: enhancement
ordinal: 24700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum run NAME --once -- COMMAND` (name to be settled: `--once` or `--wait`) attaches, returns when the current incarnation exits, and propagates its exit code (signal exits map to 128+signal); the default attached run keeps today's durable follow-until-Ctrl+C behavior. Reattaching with `hum run NAME` accepts `--tail N` to skip the retained replay.

Why now (observed 2026-09-06): `timeout 4 hum run build -- sh -c 'exit 2'` never returns (output ends with `build waiting for next launch`), so run cannot be a CI or script step, and reattaching dumped ~190 stale lines before live output.

Non-goals: changing detached run, changing follower semantics for logs --follow.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestRunOnceExitCode$' -count=1 -v` exits 0 and prints PASS: the CLI exits with the child's code and stops following.
- [ ] #2 `go test ./internal/cli -run '^TestRunAttachTail$' -count=1 -v` exits 0 and prints PASS.
- [ ] #3 `task ci` exits 0.
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
