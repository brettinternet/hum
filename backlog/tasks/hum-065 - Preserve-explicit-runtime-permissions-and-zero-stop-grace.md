---
id: HUM-065
title: Preserve explicit runtime permissions and zero stop grace
status: To Do
assignee: []
created_date: '2026-09-10 01:50'
labels: []
dependencies: []
modified_files:
  - internal/daemon/runtime.go
  - internal/daemon/runtime_test.go
  - internal/config/config.go
  - internal/config/config_test.go
  - internal/cli/config.go
  - internal/cli/stop_shutdown_test.go
  - docs/design.md
  - docs/development.md
priority: medium
type: bug
ordinal: 41700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: hum never silently changes an existing operator-managed runtime directory mode and preserves an explicit zero stop-grace duration as immediate escalation. Evidence: `PrepareRuntime` chmods every existing runtime directory to 0700, including a pre-existing 0755 directory, while `Config.WithDefaults` treats `StopGrace == 0` as absent and rewrites explicit `HUM_STOP_GRACE=0s` to ten seconds. Scope: distinguish created directories from pre-existing directories, validate unsafe modes without mutating them, and represent unset stop grace separately from an explicit zero. Document both semantics. Non-goals: do not relax secure permissions for directories created by hum, change nonzero grace parsing, or alter process signal order.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise exec go -- go test -race ./internal/config ./internal/daemon ./internal/cli` exits 0.
- [ ] #2 A focused runtime test creates an existing 0755 directory, runs preparation, and exits 0 after proving the mode remains 0755; a newly created runtime directory is still 0700.
- [ ] #3 A focused config/CLI test sets `HUM_STOP_GRACE=0s`, stops a non-cooperative fixture, and exits 0 after proving escalation is immediate rather than delayed by the ten-second default.
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
