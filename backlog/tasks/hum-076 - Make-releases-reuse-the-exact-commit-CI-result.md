---
id: HUM-076
title: Make releases reuse the exact commit CI result
status: To Do
assignee: []
created_date: '2026-09-10 20:08'
updated_date: '2026-09-10 20:20'
labels:
  - tooling
dependencies: []
references:
  - .github/workflows/release.yaml
  - .github/workflows/ci.yaml
modified_files:
  - .github/workflows/release.yaml
  - Taskfile.dist.yaml
  - .github/scripts/
priority: high
type: enhancement
ordinal: 52700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
GitHub Actions run 34523456848 released v0.8.0 from d6cb6fb in 4m10s: its Run CI step consumed 3m25s while archive construction consumed 32s. CI run 34522958965 had already passed for the same commit 23 seconds before the release started.

Outcome: a tag release reuses that exact commit result and aborts before artifact construction unless the newest trusted CI run for the resolved tag commit is completed successfully.

Scope: resolve the tag to a commit SHA; query the repository’s CI workflow runs; accept only the newest exact-SHA run produced by a push to main with status completed and conclusion success; fail closed for missing or non-success results; order archive construction and publication after verification; grant only the GitHub permissions those steps require; and add the workflow-policy checker to the local check gate.

Non-goals: trusting a branch name, pull-request result, other workflow, older commit, or older successful run superseded by a newer non-success run; reducing test coverage; changing archive contents; or removing/reordering the existing local quality phases.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task check:workflows` exits 0, and its fixtures prove that only the newest completed/success CI run from a main push whose `head_sha` equals the resolved tag commit is accepted; missing, queued, in-progress, failed, cancelled, mismatched-SHA, wrong-workflow, wrong-event, wrong-branch, and older-success/newer-failure cases exit non-zero.
- [ ] #2 `task check:workflows` exits 0 only when `.github/workflows/release.yaml` performs exact-SHA verification before archive construction or publication, contains no `task ci`, `go test ./...`, or `go test -race ./...` execution, and declares no permissions beyond `actions: read` and `contents: write`.
- [ ] #3 `task ci` exits 0; its log shows `check:workflows` plus the existing security, formatting, vet, staticcheck, normal-test, race-test, build, and smoke phases all executed successfully.
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
