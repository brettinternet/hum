---
id: HUM-140
title: Stop task smoke from re-running tests that task test already ran
status: Done
assignee: []
created_date: '2026-09-24 22:52'
updated_date: '2026-09-24 23:47'
labels:
  - tooling
dependencies: []
modified_files:
  - Taskfile.dist.yaml
  - docs/development.md
priority: medium
type: chore
ordinal: 24000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `task ci` and CI run each Go test once per mode, and `task smoke` checks only what `task test` cannot: that the release build and manual page generate.

Today smoke (Taskfile.dist.yaml:85-97) depends on cli:build (writes bin/hum) and cli:man (writes dist/hum.1), checks `test -s dist/hum.1`, and then runs `go test ./integration -run <8 named tests>` and `go test ./cmd/hum -run ^TestBuiltCLIMachineOutputV1$`. Neither test command uses bin/hum. The integration TestMain (integration/main_test.go:17-40) builds its own binary from source into a temp directory, and so does the cmd/hum test. Every smoke test also runs in `go test ./...`. `task ci` (Taskfile.dist.yaml:27-35) runs test before smoke, and the Linux and macOS CI jobs run `task security check test smoke` (.github/workflows/ci.yaml:55 and the macOS job). So these 9 tests run twice per job with the same source, the same binary build, and `GOFLAGS=-count=1`.

HUM-132 introduced the current smoke list when it folded the cmd/hum built-binary test into integration. It kept smoke running named tests to preserve the old shape, not because the second run checks anything new.

Change:
1. In Taskfile.dist.yaml, remove the two `go test` lines from smoke. Keep the cli:build and cli:man deps and the `test -s dist/hum.1` check. Add `bin/hum --version` so the release build is at least executed.
2. Update the smoke description in docs/development.md (the paragraph starting "The `task ci` smoke step" near :47) to say it builds the binary and manual page and runs the built binary once. Integration and CLI JSON v1 contract tests run in `task test`.
3. Do not change ci.yaml. It still runs `task test smoke`.

Non-goals: dropping `task test` from `task ci` in favour of race alone; changing the CI job layout; changing any test.

Unrelated failures: if `task ci` or a package run fails in a test this task did not touch, rerun that package once. If the rerun passes, record both runs in Implementation Notes and continue; if it fails again, stop and report it. Do not fix unrelated tests in this task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `awk '/^  smoke:/{f=1;next} /^  [a-z][a-z:]*:$/{f=0} f' Taskfile.dist.yaml | rg -q "go test"` exits 1 (smoke contains no go test line), and `task smoke` exits 0.
- [x] #2 AC2 — `rg -n "smoke step" docs/development.md` exits 0 and the matched paragraph no longer says smoke runs integration tests.
- [x] #3 AC3 — `task ci` exits 0.
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
1. Replace duplicate smoke test invocations with built binary version invocation; clarify the docs. 2. Run smoke and full ci; review diff and obtain independent acceptance verification. 3. Commit implementation, merge into main, run final gate and record evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1 — In hum-140-smoke, awk smoke block | rg -q "go test" exited 1; task smoke exited 0 (generated dist/hum.1, built and ran bin/hum --version: hum dev (built unknown)). AC#2 — rg -n "smoke step" docs/development.md exited 0 (line 47); paragraph says integration and CLI JSON v1 contracts run in task test, not smoke. AC#3 — task ci exited 0 in the implementation worktree (security, check, test, race, smoke). Independent verifier: PASS AC1 and AC2 directly; PASS AC3 on parent gate evidence and inspection of ci task. Diff limited to Taskfile.dist.yaml and docs/development.md; no tests deleted, skipped, or weakened; tooling label permits gate change. task check:staged passed. Next: commit implementation, merge to main, run task ci on final commit, finalize task and cleanup worktree.

Integrated implementation commit e4b6eff into main via wt merge --no-commit --no-rebase --no-remove; task ci exited 0 on main at e4b6eff (security, check, test, race, smoke). Review outcome: independent verifier PASS for AC#1 and AC#2 by execution; PASS for AC#3 from parent task ci evidence plus independent inspection of the ci task; initial DoD incompleteness (no final commit or notes at review time) now addressed. No remaining blockers; next: record Done and commit task state, confirm final gate, remove owned worktree.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Removed duplicate smoke test runs; smoke builds and executes bin/hum --version and checks the generated manual. task smoke and task ci passed; independent verifier passed AC1–AC3. Implementation e4b6eff merged to main.
<!-- SECTION:FINAL_SUMMARY:END -->
