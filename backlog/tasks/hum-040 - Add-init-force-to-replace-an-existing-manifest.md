---
id: HUM-040
title: Add init --force to replace an existing manifest
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - cli
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/init.go
  - internal/cli/init_test.go
  - internal/project/init.go
  - internal/project/init_test.go
  - docs/design.md
priority: low
type: enhancement
ordinal: 17700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum init --force` replaces an existing hum.yaml atomically (temp file plus rename) and reports `outcome: replaced` in human and JSON output; without the flag the existing refusal is unchanged.

Why now: regenerating a manifest after discovery changes requires manually deleting hum.yaml first.

Non-goals: merging with existing content, backups.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestInitForce' -count=1 -v` exits 0 and prints PASS for replacing an existing file and for the unchanged refusal without the flag.
- [ ] #2 `task ci` exits 0.
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
