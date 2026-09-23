---
id: HUM-120
title: Gate and distribute the supported non-TTY Windows build
status: To Do
assignee: []
created_date: '2026-09-23 20:50'
labels:
  - tooling
  - integration
  - docs
milestone: m-5
dependencies:
  - HUM-119
modified_files:
  - .github/workflows/ci.yaml
  - .github/workflows/release.yaml
  - .github/workflows/stress.yaml
  - Taskfile.dist.yaml
  - .taskfiles/*.yaml
  - integration/*_test.go
  - cmd/hum/*_test.go
  - internal/testutil/*_test.go
  - internal/testutil/*windows*.go
  - README.md
  - docs/design.md
  - docs/development.md
  - scripts/install_test.sh
priority: high
type: feature
ordinal: 4000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Windows is an honestly documented, tested and downloadable native non-TTY target rather than a cross-compiling curiosity. Scope: add a Windows runner that builds and tests the native binary and a Windows-focused local task target; adapt Unix-only untagged tests/fixtures to platform-specific implementations without deleting, skipping, or weakening Unix checks; add a Windows amd64 release artifact with checksums and a local packaging smoke test using the existing release conventions. Document verified PowerShell/manual install steps, PATH/executable conventions, named-pipe/runtime security, stop and signal semantics, and the deliberate absence of --tty/attach/ConPTY; update the Windows non-goal only once native Windows acceptance passes. Existing install.sh remains Unix-only. Inspect .github/workflows/ci.yaml, release.yaml, .taskfiles/cli.yaml, Taskfile.dist.yaml, integration/main_test.go, internal/testutil/harness.go, README.md:119 and docs/design.md:26. Non-goals: Windows GUI installer, winget/Homebrew, ConPTY, or adding unsupported Unix feature claims.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — On Windows, `task windows:test` exits 0 after native build and the Windows non-TTY integration suite; CI invokes this same target on a Windows runner.
- [ ] #2 AC2 — On macOS or Linux, `task windows:package:smoke` exits 0 and checks the cross-built Windows amd64 archive contains hum.exe with expected naming plus matching checksum; release.yaml publishes that artifact and checksum.
- [ ] #3 AC3 — On macOS or Linux, `task ci` exits 0 without weakening existing Unix tests; on Windows, `go test ./... -count=1` exits 0 with platform-specific replacements for Unix-only coverage.
- [ ] #4 AC4 — On macOS or Linux, `rg -n "Windows|ConPTY|non-TTY|PowerShell" README.md docs/design.md docs/development.md` exits 0 and those docs state verified installation, supported commands and unsupported TTY/signal semantics accurately.
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
