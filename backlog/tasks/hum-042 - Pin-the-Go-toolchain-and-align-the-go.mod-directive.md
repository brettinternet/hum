---
id: HUM-042
title: Pin the Go toolchain and align the go.mod directive
status: Done
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-07 05:32'
labels:
  - tooling
milestone: m-4
dependencies: []
modified_files:
  - mise.toml
  - go.mod
  - Taskfile.dist.yaml
  - docs/development.md
  - internal/cli/surface_test.go
priority: low
type: chore
ordinal: 19700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: mise.toml pins Go 1.27.1 and Staticcheck 2026.2.1 instead of latest. The go.mod go directive remains the deliberately supported minimum language version, Go 1.22, and a local compatibility target verifies that claim. docs/development.md records how and when both pins and the minimum are upgraded.

Scope: pin only Go and Staticcheck, add a project task that runs the build/test subset under Go 1.22 needed to prove source and module compatibility, and document the upgrade policy. The ordinary task ci gate continues on Go 1.27.1.

Why now: floating toolchains make CI and local results change without a repository diff. Keeping go.mod at 1.22 without testing it would also make an unsupported compatibility promise.

Non-goals: pinning other tools, dependency automation, supporting Go versions older than 1.22, or running Staticcheck under multiple Go versions.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `mise exec go -- go version` prints `go version go1.27.1` for the current platform, and `mise exec staticcheck -- staticcheck -version` prints `staticcheck 2026.2.1 (0.8.1)`.
- [x] #2 `go mod edit -json | grep -q '"Go": "1.22"'` exits 0, and `task check:go-min` exits 0 while compiling and testing the supported source under Go 1.22.
- [x] #3 `go test ./internal/cli -run '^TestPinnedToolchainDocs$' -count=1 -v` exits 0 and prints PASS, proving mise.toml contains no latest value for Go or Staticcheck and docs/development.md states the pin/minimum upgrade policy.
- [x] #4 `task ci` exits 0 with Go 1.27.1 and Staticcheck 2026.2.1.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Pin exact installed Go and Staticcheck versions in mise.toml.
2. Add a Go-1.22 compatibility task without changing the ordinary CI toolchain.
3. Document coordinated upgrades and run version, compatibility, and final gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation: pinned Go and Staticcheck in mise.toml, added the Go 1.22 compatibility task, documented coordinated upgrades, and added regression coverage. Implementation commit e1e4806; merged to main as e8aee2f.

AC#1 evidence: `mise exec go -- go version` printed `go version go1.27.1 darwin/arm64`; `mise exec staticcheck -- staticcheck -version` printed `staticcheck 2026.2.1 (0.8.1)`.
AC#2 evidence: `go mod edit -json | grep -q "\"Go\": \"1.22\""` exited 0; `task check:go-min` exited 0 with all Go 1.22 packages passing.
AC#3 evidence: the focused `TestPinnedToolchainDocs` go test exited 0 and printed PASS.
AC#4 evidence: `task ci` exited 0 with all checks, tests, race tests, build, and smoke test passing. An earlier run hit a transient existing TestManifestWorkflow timeout; the isolated test and full rerun both passed.
Independent verifier: PASS for AC#1 through AC#4; no actionable defects.
Scope: implementation changed only Taskfile.dist.yaml, docs/development.md, internal/cli/surface_test.go, and mise.toml, all within the declared list. go.mod already used 1.22 and stayed unchanged. No tests were deleted, skipped, or weakened. Taskfile.dist.yaml is a protected gate file, and the task carries the required tooling label.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Pinned Go 1.27.1 and Staticcheck 2026.2.1, added executable Go 1.22 compatibility coverage, documented the upgrade policy, and added a regression test. All four acceptance commands and an independent verifier pass succeeded; merged to main in e8aee2f.
<!-- SECTION:FINAL_SUMMARY:END -->
