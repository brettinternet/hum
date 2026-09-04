---
id: HUM-017
title: Add consistent short aliases for common CLI flags
status: Done
assignee: []
created_date: '2026-09-03 02:43'
updated_date: '2026-09-04 05:04'
labels:
  - cli
  - docs
milestone: m-1
dependencies:
  - HUM-014
  - HUM-018
  - HUM-019
modified_files:
  - internal/cli/
  - README.md
  - docs/design.md
priority: medium
type: enhancement
ordinal: 1300
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: frequently typed Hum options have predictable one-letter aliases while every long form remains canonical in help, documentation, scripts, and error messages.

Scope: define command-local aliases with identical parsing, defaults, validation, output, and exit behavior: `-h/--help` and `-v/--version`; `-j/--json` wherever JSON exists, including `stop`, `shutdown`, `down`, and `init`; `serve -d/--daemon`; `run -d/--detach`; `list -a/--all`; `logs -s/--stream`, `-n/--tail`, `-c/--after-cursor`, `-b/--limit-bytes`, `-m/--match`, and `-f/--follow`; `wait -c/--after-cursor`, `-m/--match`, and `-t/--timeout`; and `start`/`up` `-t/--timeout`. `--match` carries the same `-m` on `logs` and `wait` because it is one concept. Help shows the short and long spelling together and examples prefer readable long forms except where demonstrating aliases.

Keep uncommon configuration options long-only. Keep consequential `shutdown --stop-processes` and behavior-changing `--no-wait` long-only so neither can be triggered by an opaque single letter. Reject duplicate or conflicting aliases when assembling the command tree; do not add alternate aliases, combined-short parsing rules, or aliases to MCP fields.

Modified-file contract: internal/cli/, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/cli -run TestFlagAliases -count=1` exits 0 and proves the complete command tree exposes exactly the documented command-local aliases, has no collisions, keeps `--stop-processes`, `--no-wait`, and uncommon configuration flags long-only, and renders short/long pairs together in help.
- [x] #2 `go test ./internal/cli -run TestFlagAliasParity -count=1` exits 0 and table-tests every alias against its long form with the same parsed value, validation result, output mode, daemon request, and exit code.
- [x] #3 `go test ./internal/cli -run TestLifecycleHelp -count=1` exits 0 and `README.md` plus `docs/design.md` document the stable alias table while retaining long options in primary examples.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed for implementation in an isolated worktree on 2026-09-03.

AC#1 — `go test ./internal/cli -run TestFlagAliases -count=1` exited 0 (`ok hum/internal/cli`).
AC#2 — `go test ./internal/cli -run TestFlagAliasParity -count=1` exited 0 (`ok hum/internal/cli`).
AC#3 — `go test ./internal/cli -run TestLifecycleHelp -count=1` exited 0 (`ok hum/internal/cli`).
Final gate — `task ci` passed on commit d7896fb, including vet, staticcheck, all Go tests, race tests, build, and binary smoke test.
Independent verifier — PASS for AC#1, AC#2, AC#3, modified-file scope, no weakened tests, documentation completeness, and rejected alternate run spellings.
Review — final actionable parser finding fixed; exact `-d`/`-j` remain accepted while bare, double-dash-short, combined, and single-dash-long alternates reject.
Modified files — README.md, docs/design.md, internal/cli/commands.go, internal/cli/root.go, internal/cli/flag_alias_test.go; all are within the declared contract.

Correction reopened: synthetic alias action capture proves parsing but not real command output, request, validation, and exit parity. Adding stub-backed semantic coverage before final completion.

AC#2 correction — `go test ./internal/cli -run TestFlagAliasParity -count=1` exited 0 after adding real NewRootCommand action coverage for list, status, logs, wait, serve, run, init, start, up, down, restart, stop, and shutdown. The focused test also passed three consecutive runs.
Semantic evidence — wait captures and compares the real WaitRequest; logs uses isolated discriminating stream/tail/cursor/byte/match/follow cases; run inspects daemon process identity/argv/state; manifest timeout aliases produce timed_out/exit 2 within a 2s bound; lifecycle JSON actions compare decoded results, exact validation errors, output, and exits.
Cleanup evidence — detached serve registers unconditional shutdown cleanup and waits for the parsed PID to exit; the leaked verifier process from the deficient test was explicitly terminated and subsequent verifier runs observed no new orphan.
Independent verifier — PASS for corrected AC#2 on the final test implementation. Independent reviewer — PASS after all discriminating, timeout, validation, and cleanup findings were fixed.
Final gate — `task ci` passed on rebased final commit 82f8f83, including vet, staticcheck, all Go tests, race tests, build, and binary smoke test.

Final parity refinement reopened: tail must assert the selected retained entry and follow must observe output produced after subscription while the process is live.

Final AC#2 refinement — tail parity now compares the complete selected entry against the exact final retained entry. Follow parity uses a gated producer, baseline cursor, retained probe, asynchronous follower, first-event connection proof, post-subscription release, event-envelope checks, terminal exit-event checks, exact post-cursor texts, and a bounded context.
Verification — `go test ./internal/cli -run TestFlagAliasParity -count=1` exited 0; the focused suite passed three consecutive runs; `go test ./internal/cli -count=1` exited 0. Independent verifier PASS and independent reviewer PASS after cursor/subscription/exit findings were fixed.
Final gate — `task ci` passed on final commit 42cd90f, including vet, staticcheck, all tests, race tests, build, and binary smoke test.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented and merged HUM-017 in commits d7896fb, 82f8f83, and 42cd90f. The complete CLI alias surface has collision rejection, exact run parsing, canonical documentation, exhaustive parser parity, and real/stub semantic parity for requests, validation, output, daemon behavior, tail selection, live follow subscription, and exit codes. `task ci` passed on final commit 42cd90f; independent verification and review passed.
<!-- SECTION:FINAL_SUMMARY:END -->
