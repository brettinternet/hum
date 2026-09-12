---
id: HUM-109
title: >-
  Preserve opaque baseline entries and sharpen self-referential env path
  diagnostics
status: Done
assignee: []
created_date: '2026-09-12 16:58'
updated_date: '2026-09-12 17:40'
labels: []
dependencies: []
modified_files:
  - internal/project/environment.go
  - internal/project/environment_test.go
  - internal/project/manifest.go
priority: low
type: bug
ordinal: 81800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: close two low-severity HUM-108 review findings in `internal/project/environment.go`.

1. Active composition drops inherited baseline entries that are not `KEY=VALUE`. `splitEnvEntry` returns false when the entry has no `=` or an empty key, so those entries vanish from the composed environment whenever `environment`/`env` is configured. HUM-108 states inherited names outside the configured-key grammar are preserved during active composition. Default/no-op composition already copies the baseline verbatim, so the loss is limited to configured targets.

2. `resolveEnvironmentFile` reports `environment file "." escapes the project root` for a path that resolves to the manifest directory itself (`.`, `./`, `sub/..`). The path does not escape the root; it is not a file. The diagnostic should say so.

Scope: `internal/project/environment.go` and `internal/project/environment_test.go`.
Non-goals: any change to the file grammar, bounds, path containment rules, composition order, or the response-privacy boundary. Diagnostics must keep naming only key/path/line and never a value.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `mise exec go -- go test ./internal/project -run '^TestEnvironmentCompositionContract$' -count=1` exits 0 with a case proving a baseline containing an entry with no `=` and an entry with an empty key survives composition unchanged when a process configures `env`, in the same relative position the no-op path preserves.
- [x] #2 `mise exec go -- go test ./internal/project -run '^TestEnvironmentFileContract$' -count=1` exits 0 with a case asserting `files: ["."]` and `files: ["sub/.."]` fail with a message naming the path and stating it is not a file, and not claiming it escapes the project root.
- [x] #3 `task cli:check && task test` exits 0.
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
Implementation commit 61258a8 (fix(project): preserve opaque environment entries).

AC#1 — mise exec go -- go test ./internal/project -run "^TestEnvironmentCompositionContract$" -count=1 exited 0; active composition preserves no-equals and empty-key baseline entries unchanged at their baseline positions.

AC#2 — mise exec go -- go test ./internal/project -run "^TestEnvironmentFileContract$" -count=1 exited 0; real manifest parsing for files ["."] and files ["sub/.."] names each path as not a file and does not claim an escape.

AC#3 — task cli:check && task test exited 0.

Final gate — task ci exited 0 on commit 61258a8, including security, checks, tests, race tests, and smoke tests.

Independent verification — verifier reran all three acceptance commands and task ci on commit 61258a8 and returned PASS for AC#1, AC#2, and AC#3.

Modified-file deviation — internal/project/manifest.go was required in addition to the two scoped environment files because manifest parsing intercepted self-referential paths before resolveEnvironmentFile; the change only corrects that diagnostic. No test was deleted, skipped, or weakened, and no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Preserved opaque inherited environment entries during active composition and corrected self-referential environment file paths to report that they are not files. All focused checks, full tests, task ci, and independent verification passed on commit 61258a8.
<!-- SECTION:FINAL_SUMMARY:END -->
