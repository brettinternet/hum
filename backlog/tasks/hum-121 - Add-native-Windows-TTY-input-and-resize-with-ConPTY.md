---
id: HUM-121
title: Add native Windows TTY input and resize with ConPTY
status: Done
assignee: []
created_date: '2026-09-23 20:50'
updated_date: '2026-09-24 22:10'
labels:
  - process
  - cli
  - daemon
  - integration
  - reviewed
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
ordinal: 13000
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
- [x] #1 AC1 — After the owner-approved `git push origin HEAD:windows/HUM-121`, `task windows:watch` exits 0 on macOS, with WINDOWS_PACKAGES still ./... . Windows tests use a real ConPTY child to prove input, output, resize, and detach/reattach, with no second input owner.
- [x] #2 AC2 — On macOS, `rg -n "func TestWindowsTTY" internal/process internal/cli integration` lists tests in `*_windows_test.go` files proving that cancellation and failed startup restore the local console mode and close child and console handles, and that exercise Ctrl+C and Ctrl+]. `rg -n "ConPTY" README.md` exits 0 and documents those semantics. AC1 run executes these tests.
- [x] #3 AC3 — On macOS, `go test ./internal/process ./internal/cli ./integration -count=1` exits 0; existing Unix PTY and non-TTY tests still pass.
- [x] #4 AC4 — On macOS, `GOOS=windows GOARCH=amd64 go vet ./...` exits 0.
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
Implementation: commits 346ebfd, 66fceef, 104fc68, 5d6a277, 974804d, fast-forward merged locally to main. Review found Windows stdin could not report console size; fixed via output handle/CONOUT$. Native Windows CI failures on runs 36040725588, 36041429602, and 36042924702 were fixed; run 36043264281 passed.
AC#1: owner-approved git push origin HEAD:windows/HUM-121 and task windows:watch exited 0 for 974804d (run 36043264281; WINDOWS_PACKAGES ./..., all five jobs passed).
AC#2: rg -n "func TestWindowsTTY" internal/process internal/cli integration listed Windows-only tests; rg -n "ConPTY" README.md exited 0. Native run 36043264281 executed console-mode cancellation/failed-startup, handle-count failure cleanup, Ctrl+C, and Ctrl+] tests; all passed.
AC#3: go test ./internal/process ./internal/cli ./integration -count=1 exited 0 on macOS at 974804d.
AC#4: GOOS=windows GOARCH=amd64 go vet ./... exited 0 on macOS at 974804d.
Delivery: task check:staged and task ci exited 0 on implementation commit 974804d; first task ci hit known HUM-134 burst flake, repeat passed. Independent verifier first found missing AC2 coverage, then returned PASS for AC1-AC4 on 974804d. Diff stays within declared paths; no tests deleted, skipped or weakened; no protected gate file changed. Residual: cancellation mode test calls input.close after synthetic cancellation instead of full command cancellation; CLI defers the same cleanup. No remaining blocker; worktree cleanup pending.

Review (55776af): an out-of-range Windows resize (e.g. 32768 columns) was retained before ConPTY rejected it, so the next launch under the same lease failed; app now validates sizes with platform process.ValidateTTYSize before retaining them (one helper replaces five zero checks; TestWindowsTTYRejectsOversizedConsoleBeforeRetaining). Windows resize polling cached a size before it was applied, losing resizes that happened after the attach request or failed with a stale cursor; it now sends the current size on the first tick and retries stale-cursor rejections. Also fixed a Linux CI race in TestTTYInteractiveSession (aa56958: poll logs after start --no-wait). Declined: replacing the synthetic cancellation console-mode test with a hosted full-command cancellation (already recorded residual; production path defers the same cleanup). Native Windows CI run 36065198341 passed. No follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented and locally merged Windows ConPTY TTY support; native Windows and macOS/Linux CI passed on 974804d. Independent verifier passed all four criteria. Next step: no implementation work; maintainers can publish main when ready.
<!-- SECTION:FINAL_SUMMARY:END -->
