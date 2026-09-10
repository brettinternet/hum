---
id: HUM-076
title: Make releases reuse the exact commit CI result
status: To Do
assignee: []
created_date: '2026-09-10 20:08'
updated_date: '2026-09-10 21:13'
labels:
  - tooling
  - blocked
dependencies: []
references:
  - .github/workflows/release.yaml
  - .github/workflows/ci.yaml
  - docs/development.md
modified_files:
  - .github/workflows/release.yaml
  - docs/development.md
priority: high
type: enhancement
ordinal: 52700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
GitHub Actions run 34523456848 released v0.8.0 from d6cb6fb in 4m10s: its Run CI step consumed 3m25s while archive construction consumed 32s. CI run 34522958965 had already passed for the same commit 23 seconds before the release started.

Outcome: a tag release verifies the newest CI workflow run for the exact tag commit instead of re-running `task ci`, waits for that run while it is still queued or in progress, and fails closed before archive construction when no such run exists or it does not conclude successfully.

Design (plain shell inside release.yaml; no new scripts, Taskfile targets, or workflow-policy checker):
1. Resolve the tag to its commit after checkout (fetch-depth 0 is already set): `commit="$(git rev-parse "${GITHUB_REF_NAME}^{commit}")"`. Annotated tags must dereference to the commit, never the tag object.
2. Select the newest CI run for that commit produced by a push to main: `gh run list --workflow ci.yaml --commit "$commit" --branch main --event push --limit 1 --json databaseId --jq '.[0].databaseId'`. `gh run list` orders newest first, so an older success superseded by a newer non-success run is never selected. Exit 1 with a clear message when the output is empty.
3. `gh run watch "$run_id" --exit-status` blocks while the run is queued or in progress and exits non-zero for any non-success conclusion (failure, cancelled, timed out). Bound the job with `timeout-minutes: 20`.
4. Step order: verify CI result -> build release archives -> create GitHub release. Permissions: `actions: read` (run list and watch) and `contents: write` (release upload). Set `GH_TOKEN: ${{ github.token }}` on the verify step.
5. Document the release flow in docs/development.md: push to main, tag the pushed commit; the release waits for that commit's CI and refuses to publish if it failed. Recovery is to push a fix and tag again.

Interplay: HUM-077 adds concurrency cancellation and must not cancel main-branch runs, otherwise this check fails closed on a cancelled run after rapid pushes. The CI workflow filename must stay ci.yaml because the selection filters by workflow file.

Non-goals: trusting a branch name, pull-request result, other workflow, or a different commit; reducing test coverage; changing archive contents; adding a workflow-lint or policy-checker tool; changing local `task ci`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `rg -n 'task ci|go test' .github/workflows/release.yaml` prints nothing, and `rg -n '^      - name:' .github/workflows/release.yaml` lists the CI verification step before both "Build release archives" and "Create GitHub release".
- [x] #2 `rg -n '^\s+(actions|contents):' .github/workflows/release.yaml` prints exactly `actions: read` and `contents: write`, and `rg -c 'timeout-minutes' .github/workflows/release.yaml` prints 1.
- [x] #3 The selection command from the verify step, run locally, prints `34522958965` for the v0.8.0 commit: `gh run list --workflow ci.yaml --commit d6cb6fb7e2afc24a0774b49f81a14b54bed0bc6e --branch main --event push --limit 1 --json databaseId --jq '.[0].databaseId'`; the same command with commit `9d7b5bd529d475f77b6e3e7d87fcb16540fe4c6b` (Dependabot PR head, never pushed to main) prints an empty line, which the step must turn into exit 1.
- [ ] #4 For the first tag release after this change (RUN_ID from `gh run list --workflow release.yaml --limit 1 --json databaseId --jq '.[0].databaseId'`), `gh run view RUN_ID --json jobs --jq '.jobs[0].steps[].name'` contains no "Run CI" step and `gh run view RUN_ID --json jobs --jq '.jobs[0] | (.completedAt|fromdate) - (.startedAt|fromdate)'` prints a value no greater than 90.
- [x] #5 `task ci` exits 0 (local gate unchanged).
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation merged to main in commit b1e746a.
AC#1 evidence — `rg -n "task ci|go test" .github/workflows/release.yaml` printed nothing; `rg -n "^      - name:" .github/workflows/release.yaml` listed Verify CI result at line 23, Build release archives at line 39, and Create GitHub release at line 65.
AC#2 evidence — `rg -n "^\\s+(actions|contents):" .github/workflows/release.yaml` printed actions: read and contents: write only; `rg -c timeout-minutes .github/workflows/release.yaml` printed 1. Independent actionlint validation passed.
AC#3 evidence — `gh run list --workflow ci.yaml --commit d6cb6fb7e2afc24a0774b49f81a14b54bed0bc6e --branch main --event push --limit 1 --json databaseId` selected 34522958965; the same command for 9d7b5bd529d475f77b6e3e7d87fcb16540fe4c6b returned empty. The verify step converts empty output to exit 1 and watches the selected run with --exit-status.
AC#5 evidence — `task ci` exited 0 on merged main commit b1e746a.
Review evidence — independent verifier passed AC#1, AC#2, AC#3, and AC#5; found no concrete defects, confirmed modified-path compliance, and confirmed no tests were deleted, skipped, or weakened.
AC#4 pending — requires the first tag release after b1e746a. After that release, run the two recorded `gh run view RUN_ID` commands and record absence of Run CI plus job duration no greater than 90 seconds. The current latest release 34523456848 predates this change and is not valid evidence.

Blocked on AC#4: no tag release exists after implementation commit b1e746a. The latest release run remains 34523456848 for v0.8.0 at d6cb6fb, which predates the change. User chose not to authorize pushing main or creating a release. Resume after an authorized post-b1e746a tag release; then run the two AC#4 gh run view commands, obtain an independent verifier PASS, and complete the task.
<!-- SECTION:NOTES:END -->
