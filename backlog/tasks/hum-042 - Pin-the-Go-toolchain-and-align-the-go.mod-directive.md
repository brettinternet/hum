---
id: HUM-042
title: Pin the Go toolchain and align the go.mod directive
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - tooling
milestone: m-4
dependencies: []
modified_files:
  - mise.toml
  - go.mod
  - docs/development.md
priority: low
type: chore
ordinal: 19700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: mise.toml pins `go` and `staticcheck` to explicit versions instead of `latest`, the go.mod `go` directive states the deliberately supported minimum language version (today 1.22 while builds run 1.27), and docs/development.md records the upgrade policy.

Why now: CI and local gates download whichever Go is newest on the day they run, so a new Go minor release (new vet analyzers, toolchain behavior) can break `task ci` with no code change. Reproducible builds are a stated project value.

Non-goals: pinning other tools, adding renovate or dependabot.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `grep -E '^go = "[0-9]+\.[0-9]+' mise.toml` matches and `mise exec go -- go version` prints that pinned version.
- [ ] #2 `task ci` exits 0 with the pinned toolchain.
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
