---
id: HUM-121
title: Add native Windows TTY input and resize with ConPTY
status: To Do
assignee: []
created_date: '2026-09-23 20:50'
updated_date: '2026-09-23 21:18'
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
  - internal/app/*windows*.go
  - internal/app/*_test.go
  - internal/daemon/*windows*.go
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
Outcome: the existing run --tty/attach/input lease supports interactive native Windows console children, not only pipe-backed non-TTY processes. This is a separate follow-up after the non-TTY Windows release in HUM-120. The current Unix implementation uses creack/pty, Setsid/Setctty, and Fcntl/Poll in internal/process/process.go, SIGWINCH in internal/cli/tty.go, and daemon-owned PTY input and resize sessions in internal/daemon/client.go and internal/app.

Scope: implement ConPTY on Windows APIs or a maintained dependency. Keep the single-owner input lease, bounded merged output, cursor validation, detach behavior, resize, and cleanup on process exit or cancellation. Define Ctrl+C, Ctrl+D, and Ctrl+] behavior for Windows console apps and document intentional differences from Unix. A failed ConPTY initialization must not leak child or console handles. Keep Unix PTY behavior and Windows non-TTY behavior unchanged. GitHub Windows runners have no interactive console, so tests that exercise local console-mode restoration must host hum itself under a ConPTY instead of depending on the runner console.

Non-goals: console emulation for legacy apps without ConPTY support, a desktop terminal UI, and wire-protocol changes that are not strictly necessary.

Start at internal/process/process.go:145-216 and :318-448, internal/cli/tty.go, the input methods in internal/app/app.go, and integration/tty_interactive_test.go.

Windows verification: after the owner approves, push HEAD to `windows/<task-id>` and run `task windows:watch` (HUM-122). Windows-only tests live in `*_windows_test.go` files so the Windows CI job runs them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — After the owner-approved `git push origin HEAD:windows/HUM-121`, `task windows:watch` exits 0 on macOS, with WINDOWS_PACKAGES still ./... . Windows tests use a real ConPTY child to prove input, output, resize, and detach/reattach, with no second input owner.
- [ ] #2 AC2 — On macOS, `rg -n "func TestWindowsTTY" internal/process internal/cli integration` lists tests in `*_windows_test.go` files proving that cancellation and failed startup restore the local console mode and close child and console handles, and that exercise Ctrl+C and Ctrl+]. `rg -n "ConPTY" README.md` exits 0 and documents those semantics. AC1 run executes these tests.
- [ ] #3 AC3 — On macOS, `go test ./internal/process ./internal/cli ./integration -count=1` exits 0; existing Unix PTY and non-TTY tests still pass.
- [ ] #4 AC4 — On macOS, `GOOS=windows GOARCH=amd64 go vet ./...` exits 0.
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
