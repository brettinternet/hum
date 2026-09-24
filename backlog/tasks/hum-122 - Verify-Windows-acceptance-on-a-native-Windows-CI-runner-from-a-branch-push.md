---
id: HUM-122
title: Verify Windows acceptance on a native Windows CI runner from a branch push
status: Done
assignee: []
created_date: '2026-09-23 21:17'
updated_date: '2026-09-24 12:39'
labels:
  - tooling
  - integration
  - reviewed
milestone: m-5
dependencies:
  - HUM-130
  - HUM-131
  - HUM-132
modified_files:
  - .taskfiles/windows.yaml
  - Taskfile.dist.yaml
  - .github/workflows/ci.yaml
priority: high
type: feature
ordinal: 8000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: every Windows task in this milestone can prove its native-Windows acceptance from a macOS checkout, and regressions in already-ported packages fail CI. Today .github/workflows/ci.yaml runs only ubuntu-latest and macos-latest jobs, there is no local Windows VM tooling, and HUM-117 through HUM-121 all need native Windows test runs; without this lane their Windows criteria cannot be executed until HUM-120 adds a runner.

Scope: add .taskfiles/windows.yaml, included as `windows` in Taskfile.dist.yaml, with:
- `windows:test`: on a Windows host, runs `go test -count=1` over a maintained WINDOWS_PACKAGES list; an optional RUN var is passed as `-run`. On a non-Windows host it exits non-zero with a message saying a Windows host is required.
- `windows:watch`: run locally. Finds the ci.yaml run for `git rev-parse HEAD` with `gh run list --workflow ci.yaml --commit <sha>`, waits a bounded time for the run to appear, then runs `gh run watch <id> --exit-status`. It exits non-zero and names the SHA when no run appears.
Add a `test-windows` job to ci.yaml on windows-latest that runs `task windows:test`. Reuse the existing checkout, mise, and cache steps. Add `windows/**` to the ci.yaml push branches so an agent can check an unmerged task branch: `git push origin HEAD:windows/<task-id>` (each push needs owner approval), then `task windows:watch`.

Known pitfall: mise.toml pins `github:brettinternet/hum`, which has no Windows release asset, plus Unix-oriented developer tools. The Windows job must install only the tools it needs, e.g. go and task via mise-action install_args or an equivalent, so mise install does not fail on Windows.

Initial WINDOWS_PACKAGES is the set whose tests cross-compile for windows/amd64 today: ./internal/config ./internal/orchestrate ./internal/output ./internal/protocol ./internal/skill ./internal/testutil/cmd/hum-fixture. If one fails natively, remove it and record the failure and its owning task in Implementation Notes (config belongs to HUM-118). Do not fix product code here. Each later Windows task adds the packages it ports; HUM-120 finishes with ./... .

Non-goals: porting product code, release artifacts, docs, Windows VM provisioning, and pull-request automation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — On macOS, `task --list-all | rg -n "windows:(test|watch)"` exits 0, and `task windows:test` exits non-zero with output stating that a Windows host is required.
- [x] #2 AC2 — On macOS, `rg -n "windows-latest" .github/workflows/ci.yaml && rg -n "task windows:test" .github/workflows/ci.yaml && rg -n "windows/\*\*" .github/workflows/ci.yaml` exits 0.
- [x] #3 AC3 — On macOS, with HEAD committed but not pushed to any `windows/**` ref, `task windows:watch` exits non-zero within its bounded wait and prints the HEAD SHA and a no-run message.
- [x] #4 AC4 — On macOS, after the owner approves and runs `git push origin HEAD:windows/<this-task-id>`, `task windows:watch` exits 0, and `gh run view <id> --json jobs --jq ".jobs[] | select(.name | test(\"Windows\")) | .conclusion"` prints `success`.
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
Implementation commits: 84f5ef1 (Windows tasks/CI), 2902910 (remove failing skill package); fast-forward merged to main. Owner approved both pushes to windows/HUM-122. Review: one independent verifier pass found AC1–AC4 PASS with no code defects; its only DoD finding was missing provider evidence, supplied below.
AC1 — `task --list-all | rg -n "windows:(test|watch)"` exited 0 and listed both tasks; `task windows:test` exited 201 on macOS with "A Windows host is required".
AC2 — `rg -n "windows-latest" .github/workflows/ci.yaml && rg -n "task windows:test" .github/workflows/ci.yaml && rg -n "windows/\\*\\*" .github/workflows/ci.yaml` exited 0 (lines 59, 91, 6 respectively).
AC3 — On committed, unpushed 84f5ef1, `task windows:watch` exited 1 after six bounded attempts (~25s), printing "No Windows-branch ci.yaml run appeared for 84f5ef1e6d8524c66bc95fe3febc359aae3e843a".
AC4 — Owner-approved `git push origin HEAD:windows/HUM-122`; `task windows:watch` on 2902910 exited 0 for CI run 35954749129 (latest rerun). `gh run view 35954749129 --json jobs --jq ".jobs[] | select(.name | test(\"Windows\")) | .conclusion"` printed success. All other jobs also passed after rerunning transient macOS TestTTYCLI race failure.
DoD — `task ci` passed on final implementation commit 2902910 (also independently rerun by verifier). `task check:staged` passed before each commit. `git diff --check 5dc6a9e..2902910` clean; only .taskfiles/windows.yaml, Taskfile.dist.yaml, .github/workflows/ci.yaml changed, no tests or protected gate files touched.
Native discovery: run 35953101713 proved internal/skill TestSkillContentHasRequiredFrontmatter fails on Windows checkout CRLF (skill_test.go:24). Removed ./internal/skill from initial WINDOWS_PACKAGES as permitted; HUM-120 owns its test port when expanding to ./..., without weakening or skipping existing tests. No remaining blocker; next step HUM-117 can add its packages to WINDOWS_PACKAGES.

AC2 command correction (shell spelling): `rg -n "windows-latest" .github/workflows/ci.yaml && rg -n "task windows:test" .github/workflows/ci.yaml && rg -n "windows/\*\*" .github/workflows/ci.yaml` exited 0.

Review: no findings. windows:watch filters by HEAD SHA and windows/ branch with a bounded wait; WINDOWS_PACKAGES includes HUM-117/118 packages. Remaining Windows work is already tracked in HUM-119/120/121. No follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added Windows test/watch tasks and CI lane, merged implementation commits 84f5ef1 and 2902910 to main. Local task ci and native CI run 35954749129 pass; independent verifier confirmed AC1–AC4. Windows skill frontmatter CRLF test remains for HUM-120 when it expands the package list.
<!-- SECTION:FINAL_SUMMARY:END -->
