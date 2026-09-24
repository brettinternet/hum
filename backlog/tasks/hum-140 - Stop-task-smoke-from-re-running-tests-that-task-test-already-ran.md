---
id: HUM-140
title: Stop task smoke from re-running tests that task test already ran
status: To Do
assignee: []
created_date: '2026-09-24 22:52'
updated_date: '2026-09-24 22:59'
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
- [ ] #1 AC1 — `awk '/^  smoke:/{f=1;next} /^  [a-z][a-z:]*:$/{f=0} f' Taskfile.dist.yaml | rg -q "go test"` exits 1 (smoke contains no go test line), and `task smoke` exits 0.
- [ ] #2 AC2 — `rg -n "smoke step" docs/development.md` exits 0 and the matched paragraph no longer says smoke runs integration tests.
- [ ] #3 AC3 — `task ci` exits 0.
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
