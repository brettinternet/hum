---
id: HUM-050
title: 'Add TTY-aware emphasis to list, status, and up output'
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - cli
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/render.go
  - internal/cli/render_test.go
  - internal/cli/commands.go
  - docs/design.md
priority: low
type: enhancement
ordinal: 27700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: when stdout is a terminal and NO_COLOR is unset, `list`, `status`, and `up` use minimal ANSI emphasis (state words: running green, exited or error red, starting yellow; header bold); when piped or with NO_COLOR, output is byte-identical to today. `--json` never colors.

Why now: nothing visually distinguishes an exited row in `hum list` today; every comparable tool highlights state.

Non-goals: colorizing child output, themes, configuration beyond NO_COLOR.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestColor' -count=1 -v` exits 0 and prints PASS: a TTY writer receives ANSI sequences, a pipe or NO_COLOR=1 receives none, and piped output equals the pre-change golden output.
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
