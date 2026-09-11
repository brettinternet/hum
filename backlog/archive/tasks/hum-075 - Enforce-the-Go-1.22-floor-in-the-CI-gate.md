---
id: HUM-075
title: Enforce the Go 1.22 floor in the CI gate
status: To Do
assignee: []
created_date: '2026-09-10 17:54'
updated_date: '2026-09-11 17:57'
labels:
  - tooling
dependencies: []
modified_files:
  - Taskfile.dist.yaml
  - docs/development.md
  - .github/workflows/ci.yaml
priority: medium
type: chore
ordinal: 51700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: A go.mod `go` directive or dependency bump that raises the minimum supported Go version fails `task ci` instead of merging silently. Evidence (2026-09-10 review of HUM-070): `task ci` runs security, check, test, race, and smoke but not `check:go-min`; docs/development.md lists `check:go-min` as a separate manual step. Commit 2dda323 added weekly Dependabot gomod updates, and `go mod tidy` on an x/sys update to v0.48.0 would rewrite the `go 1.22` directive to 1.26; nothing in CI would notice. Commit 3987427 already pinned `check:go-min` to `GOTOOLCHAIN=local`, so the check itself is now fail-closed when run. Scope: wire a Go 1.22 floor check into `task ci` (either the existing full `check:go-min` test run or a cheaper `go build ./... && go vet ./...` variant under `GOTOOLCHAIN=local mise exec go@1.22`), decide and document which, and confirm CI runtime remains acceptable. Non-goals: do not raise the minimum Go version, change Dependabot cadence, or alter unrelated ci steps.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task ci` exits 0 and its log includes a step executed with Go 1.22 under `GOTOOLCHAIN=local`.
- [ ] #2 With go.mod temporarily edited to `go 1.26`, `task ci` exits non-zero at the floor step; the edit is reverted afterwards and `git status --short` is clean.
- [ ] #3 `rg -n "go-min" Taskfile.dist.yaml docs/development.md` shows the ci wiring and matching documentation.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Superseded 2026-09-10: the owner decided to drop the Go 1.22 floor rather than gate it. hum ships only as prebuilt release binaries built with the pinned toolchain and its module path `hum` is not importable, so no one builds it with an older Go. The go.mod directive now tracks the pinned Go minor (1.27), `task check:go-min` and its docs were removed, and x/sys, x/term, and testify were updated to their latest releases. No CI floor gate is needed because `task ci` already runs with the pinned toolchain.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-11 17:57
---
Archived 2026-09-11 during backlog cleanup: the task was superseded (the Go floor was dropped in favour of tracking the pinned toolchain), so none of its acceptance criteria apply. Archived rather than completed so the Done set only contains work whose criteria were met.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Closed without a CI gate: the Go 1.22 floor was dropped in favour of tracking the pinned toolchain, removing the need for a separate floor check.
<!-- SECTION:FINAL_SUMMARY:END -->
