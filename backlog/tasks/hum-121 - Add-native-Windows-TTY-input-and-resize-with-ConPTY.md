---
id: HUM-121
title: Add native Windows TTY input and resize with ConPTY
status: To Do
assignee: []
created_date: '2026-09-23 20:50'
labels:
  - process
  - cli
  - daemon
  - integration
milestone: m-5
dependencies:
  - HUM-120
modified_files:
  - internal/process/process.go
  - internal/process/*windows*.go
  - internal/process/*_test.go
  - internal/cli/tty.go
  - internal/cli/*windows*.go
  - internal/cli/*_test.go
  - internal/app/app.go
  - internal/app/*_test.go
  - internal/daemon/*_test.go
  - integration/*windows*.go
  - integration/*_test.go
  - README.md
  - docs/design.md
  - go.mod
  - go.sum
priority: medium
type: feature
ordinal: 5000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the existing run --tty/attach/input lease supports interactive native Windows console children, not just pipe-backed non-TTY processes. This is a separate follow-up after the explicitly non-TTY Windows release in HUM-120. Current Unix implementation uses creack/pty, Setsid/Setctty and Fcntl/Poll in internal/process/process.go, SIGWINCH in internal/cli/tty.go, and daemon-owned PTY input/resize sessions in internal/daemon/client.go and app. Scope: implement ConPTY backed by Windows APIs or a maintained dependency, preserve single-owner input lease, bounded merged output, cursor validation, detach behavior, resize and cleanup on process exit/cancellation; define Ctrl+C, Ctrl+D and Ctrl+] behavior for Windows console apps and document intentional differences from Unix. Ensure failed ConPTY initialization does not leak child/console handles; preserve Unix PTY behavior and non-TTY Windows behavior. Non-goals: console emulation for legacy apps that do not support ConPTY, desktop terminal UI or altering wire protocol without necessity. Start at internal/process/process.go:145-216 and :318-448, internal/cli/tty.go, internal/app/app.go input methods, integration/tty_interactive_test.go.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — On Windows, `go test ./internal/process ./internal/app ./internal/daemon ./internal/cli ./integration -run "TestWindowsTTY" -count=1` exits 0; tests use a real ConPTY child to prove input, output, resize and detach/reattach without a second input owner.
- [ ] #2 AC2 — On Windows, `go test ./internal/process ./internal/cli -run "TestWindowsTTY" -count=1` exits 0; tests verify cancellation/failed startup restores local console mode and closes child/console handles, and Ctrl+C/Ctrl+] semantics are documented and exercised.
- [ ] #3 AC3 — On macOS or Linux, `go test ./internal/process ./internal/cli ./integration -count=1` exits 0; the existing Unix PTY and non-TTY tests still pass.
- [ ] #4 AC4 — On Windows, `go test ./... -count=1` exits 0; on macOS or Linux, `GOOS=windows GOARCH=amd64 go build ./...` exits 0.
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
