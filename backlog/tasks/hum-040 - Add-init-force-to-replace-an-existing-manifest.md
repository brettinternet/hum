---
id: HUM-040
title: Add init --force to replace an existing manifest
status: Done
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-07 05:38'
labels:
  - cli
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/init.go
  - internal/cli/init_test.go
  - internal/cli/flag_alias_test.go
  - internal/project/init.go
  - internal/project/init_test.go
  - docs/design.md
priority: low
type: enhancement
ordinal: 17700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum init --force` atomically replaces an existing regular hum.yaml and reports outcome replaced in both human and JSON output. Without --force, the existing file and refusal remain byte-for-byte unchanged.

Scope: resolve and render the complete replacement before touching hum.yaml; create a mode-0600 temporary file in the same directory, sync and close it, atomically rename it over the destination, and remove the temporary file on every failure. Refuse symlink, directory, device, and other non-regular destinations. Discovery, rendering, write, sync, close, or rename failure must leave the original manifest byte-identical.

Why now: regeneration currently requires manual deletion, which creates an unnecessary missing-manifest window and risks losing a working configuration.

Non-goals: merging, backups, following symlinks, preserving comments, or cross-filesystem replacement.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/project -run '^TestInitManifestForceReplace$' -count=1 -v` exits 0 and prints PASS, proving same-directory mode-0600 temp creation, complete write/sync/close before atomic rename, replacement content, and no leftover temp file.
- [x] #2 `go test ./internal/project -run '^TestInitManifestForcePreservesOriginalOnFailure$' -count=1 -v` exits 0 and prints PASS for discovery, render, write, sync, close, and rename failures plus symlink and non-regular targets, with the original bytes unchanged and no temporary file left behind.
- [x] #3 `go test ./internal/cli -run '^TestInitForce$' -count=1 -v` exits 0 and prints PASS for human and JSON outcome replaced, unchanged refusal/output without --force, help text, and actionable non-regular-target errors.
- [x] #4 `task ci` exits 0.
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
1. Add --force plumbing and a project-layer atomic replacement path that fails closed.
2. Preserve the existing no-force path and stable human/JSON output.
3. Exercise success, every pre-rename failure class, non-regular targets, cleanup, and final gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed for implementation in an isolated worktree on 2026-09-07.

Implementation commit 0432dfc; merged to main as 698edf1.
Review: fixed successful-publish cleanup so a concurrently recreated temporary pathname is not deleted; added regression coverage.
AC#1 PASS — `go test ./internal/project -run '^TestInitManifestForceReplace$' -count=1 -v` exited 0 and printed PASS.
AC#2 PASS — `go test ./internal/project -run '^TestInitManifestForcePreservesOriginalOnFailure$' -count=1 -v` exited 0 and printed PASS.
AC#3 PASS — `go test ./internal/cli -run '^TestInitForce$' -count=1 -v` exited 0 and printed PASS.
AC#4 PASS — `task ci` exited 0 on rebased implementation commit 0432dfc; implementation tree is unchanged in merge commit 698edf1.
Independent verifier: PASS for AC#1–AC#4; confirmed exactly the seven declared files changed and no tests were deleted, skipped, or weakened.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added `hum init --force` with fail-closed regular-target validation and same-directory mode-0600 write/sync/close/atomic-rename replacement. Added human/JSON `replaced` output, preserved no-force behavior, documented the flag, and covered success, cleanup, failure, and non-regular targets. All focused acceptance tests and `task ci` passed; independent verification passed every criterion.
<!-- SECTION:FINAL_SUMMARY:END -->
