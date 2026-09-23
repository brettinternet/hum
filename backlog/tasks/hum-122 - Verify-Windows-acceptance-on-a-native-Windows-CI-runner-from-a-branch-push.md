---
id: HUM-122
title: Verify Windows acceptance on a native Windows CI runner from a branch push
status: To Do
assignee: []
created_date: '2026-09-23 21:17'
labels:
  - tooling
  - integration
milestone: m-5
dependencies: []
modified_files:
  - .taskfiles/windows.yaml
  - Taskfile.dist.yaml
  - .github/workflows/ci.yaml
priority: high
type: feature
ordinal: 500
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
- [ ] #1 AC1 — On macOS, `task --list-all | rg -n "windows:(test|watch)"` exits 0, and `task windows:test` exits non-zero with output stating that a Windows host is required.
- [ ] #2 AC2 — On macOS, `rg -n "windows-latest" .github/workflows/ci.yaml && rg -n "task windows:test" .github/workflows/ci.yaml && rg -n "windows/\*\*" .github/workflows/ci.yaml` exits 0.
- [ ] #3 AC3 — On macOS, with HEAD committed but not pushed to any `windows/**` ref, `task windows:watch` exits non-zero within its bounded wait and prints the HEAD SHA and a no-run message.
- [ ] #4 AC4 — On macOS, after the owner approves and runs `git push origin HEAD:windows/<this-task-id>`, `task windows:watch` exits 0, and `gh run view <id> --json jobs --jq ".jobs[] | select(.name | test(\"Windows\")) | .conclusion"` prints `success`.
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
