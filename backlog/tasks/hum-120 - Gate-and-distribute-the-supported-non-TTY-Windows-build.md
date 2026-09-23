---
id: HUM-120
title: Gate and distribute the supported non-TTY Windows build
status: To Do
assignee: []
created_date: '2026-09-23 20:50'
updated_date: '2026-09-23 22:24'
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
  - integration/*windows*.go
  - integration/*unix*.go
  - cmd/hum/*_test.go
  - internal/testutil/*.go
  - internal/testutil/cmd/hum-fixture/*.go
  - README.md
  - docs/design.md
  - docs/development.md
  - scripts/install_test.sh
priority: high
type: feature
ordinal: 12000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Windows becomes a documented, tested, downloadable native non-TTY target instead of something that merely cross-compiles. HUM-122 already provides the Windows CI job and `task windows:test`. This task grows WINDOWS_PACKAGES to ./... by porting the remaining untagged Unix-only tests and fixtures, chiefly the integration suite and internal/testutil, into platform-specific implementations, without deleting, skipping, or weakening any Unix check.

Release: today .github/workflows/release.yaml builds only linux/darwin `hum-<version>-<os>-<arch>.tar.gz` archives, each holding `hum` and `hum.1`, and writes checksums with `sha256sum ./*.tar.gz`. Add a Windows amd64 artifact named `hum-<version>-windows-x64.zip` that contains `hum.exe`, because zip is the format PowerShell Expand-Archive handles natively. Produce it through a task target that release.yaml invokes, so the local smoke test exercises exactly what ships. Include it in checksums.txt and in the release upload. install.sh stays Unix-only.

Docs: document verified PowerShell/manual install steps, PATH and executable conventions, named-pipe/runtime security, stop and signal semantics, and the deliberate absence of --tty/attach/ConPTY. Describe the Windows verification lane in docs/development.md. Today README.md (the last row of the "Hum and pitchfork" table) and docs/design.md (the paragraph after the non-goals list) say native Windows support is planned. Replace both with the verified support statement only after native Windows acceptance passes.

Inspect .github/workflows/release.yaml:64-101, .taskfiles/windows.yaml, .taskfiles/cli.yaml, Taskfile.dist.yaml, integration/main_test.go, internal/testutil/harness.go, the "Hum and pitchfork" section of README.md, and the non-goals section of docs/design.md.

Non-goals: a Windows GUI installer, winget or Homebrew, ConPTY, and claims about unsupported Unix features.

Windows verification: after the owner approves, push HEAD to `windows/<task-id>` and run `task windows:watch` (HUM-122). Windows-only tests live in `*_windows_test.go` files so the Windows CI job runs them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — On macOS, `rg -n "WINDOWS_PACKAGES.*\./\.\.\." .taskfiles/windows.yaml` exits 0, and after the owner-approved `git push origin HEAD:windows/HUM-120`, `task windows:watch` exits 0, so `go test ./...` including ./integration passes natively on Windows with platform-specific replacements for Unix-only coverage.
- [ ] #2 AC2 — On macOS, `task windows:package:smoke` exits 0. It cross-builds `dist/hum-<version>-windows-x64.zip`, confirms the zip contains `hum.exe`, and checks that checksums.txt has a matching sha256 line. `rg -n "windows:package|windows-x64" .github/workflows/release.yaml` exits 0, showing that release.yaml builds, checksums, and uploads the same artifact through that target.
- [ ] #3 AC3 — On macOS, `task ci` exits 0 with no existing Unix test deleted, skipped, or weakened.
- [ ] #4 AC4 — On macOS, `rg -n "Windows|ConPTY|non-TTY|PowerShell" README.md docs/design.md docs/development.md` exits 0 and `rg -n -i "windows support is planned" README.md docs/design.md` exits 1; the docs accurately state verified installation, supported commands, and unsupported TTY/signal semantics.
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
