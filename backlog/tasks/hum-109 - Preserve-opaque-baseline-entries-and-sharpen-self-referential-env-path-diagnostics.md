---
id: HUM-109
title: >-
  Preserve opaque baseline entries and sharpen self-referential env path
  diagnostics
status: To Do
assignee: []
created_date: '2026-09-12 16:58'
labels: []
dependencies: []
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
- [ ] #1 `mise exec go -- go test ./internal/project -run '^TestEnvironmentCompositionContract$' -count=1` exits 0 with a case proving a baseline containing an entry with no `=` and an entry with an empty key survives composition unchanged when a process configures `env`, in the same relative position the no-op path preserves.
- [ ] #2 `mise exec go -- go test ./internal/project -run '^TestEnvironmentFileContract$' -count=1` exits 0 with a case asserting `files: ["."]` and `files: ["sub/.."]` fail with a message naming the path and stating it is not a file, and not claiming it escapes the project root.
- [ ] #3 `task cli:check && task test` exits 0.
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
