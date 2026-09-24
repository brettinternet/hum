---
id: HUM-117
title: Supervise non-TTY Windows child trees with safe lifecycle semantics
status: Done
assignee: []
created_date: '2026-09-23 20:49'
updated_date: '2026-09-24 05:42'
labels:
  - process
  - architecture
milestone: m-5
dependencies:
  - HUM-122
modified_files:
  - internal/process/process.go
  - internal/process/*windows*.go
  - internal/process/*unix*.go
  - internal/process/group_*.go
  - internal/process/identity_*.go
  - internal/process/*_test.go
  - internal/app/app.go
  - internal/app/*windows*.go
  - internal/app/*unix*.go
  - internal/app/*_test.go
  - internal/signals/signals.go
  - internal/signals/*windows*.go
  - internal/signals/*unix*.go
  - internal/signals/*_test.go
  - internal/project/resolver.go
  - internal/project/*windows*.go
  - internal/project/*unix*.go
  - internal/project/*_test.go
  - .taskfiles/windows.yaml
  - go.mod
  - go.sum
priority: high
type: feature
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: native Windows can launch exact-argv, non-TTY processes, retain their output, identify each incarnation, and stop the whole owned descendant tree. Today internal/process/process.go uses Unix sessions and groups, signals, wait status, pty, and x/sys/unix, and identity_*.go and group_*.go exist only for Darwin and Linux. internal/app/app.go starts readiness probes with Unix process attributes and sends SIGTERM/SIGKILL. internal/signals/signals.go exposes a Unix-specific signal contract. internal/app also imports internal/project, and internal/project/resolver.go:434 reads discovery declarations with unix.Open(O_NONBLOCK) so that a FIFO cannot block it, which means internal/app cannot build for Windows until that read is ported. On Windows, a portable open followed by the existing regular-file check is enough.

Scope: move platform mechanics behind build-tagged implementations and keep existing Unix behavior. Use Windows Job Objects, or another owned-tree mechanism whose ownership can be proven, plus a PID+creation-time identity so PID reuse cannot cause a wrong stop. Fail closed when group ownership cannot be proven. Describe Windows stop semantics honestly: do not claim that arbitrary native programs receive SIGTERM or can honor stop_grace. Unsupported Unix signal requests must return a stable, explicit error, including on CLI- and MCP-facing paths downstream, never a success-shaped no-op. A Windows non-TTY result reports the correct exit code and no invented Unix signal. Reject TTY explicitly until its separate follow-up (HUM-121). Resolve executables with Windows PATH/PATHEXT, case-insensitive environment keys, and executable-extension rules, without leaking the ambient PATH.

Non-goals: ConPTY, transport or daemon startup, packaging, and any change to Unix signal behavior.

Start with internal/process/process.go, internal/app/app.go:1093 and :3771, internal/signals/signals.go, internal/project/resolver.go:432-450, and the process lifecycle section of docs/design.md. Choose an identity and ownership API whose handles and error states the daemon task (HUM-118) can persist or prove.

Windows verification: after the owner approves, push HEAD to `windows/<task-id>` and run `task windows:watch` (HUM-122). Windows-only tests live in `*_windows_test.go` files so the Windows CI job runs them. Add every package this task ports to WINDOWS_PACKAGES in .taskfiles/windows.yaml.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — After the owner-approved `git push origin HEAD:windows/HUM-117`, `task windows:watch` exits 0 on macOS, with WINDOWS_PACKAGES including ./internal/process ./internal/signals ./internal/project ./internal/app. Windows tests in those packages start an exact-argv fixture, observe its real exit code, and prove that a stop terminates the owned descendant tree, not only the leader.
- [x] #2 AC2 — On macOS, `rg -n "func TestWindows" internal/process internal/app` lists tests in `*_windows_test.go` files proving that: a reused PID or an identity/ownership mismatch cannot authorize stopping an unrelated process; unsupported Unix signal requests and TTY requests fail with the stable explicit error; and PATH/PATHEXT resolution does not use the ambient PATH. AC1 run executes these tests.
- [x] #3 AC3 — On macOS, `go test ./internal/process ./internal/signals ./internal/project ./internal/app -count=1` exits 0; existing Unix process-group, signal, readiness, discovery, and PTY behavior is retained.
- [x] #4 AC4 — On macOS, `GOOS=windows GOARCH=amd64 go vet ./internal/process ./internal/signals ./internal/project ./internal/app` exits 0, which also type-checks the Windows test files.
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
Implementation Notes (2026-09-24):
Commits 766bec4, ed9faa4, 8d5891f merged by fast-forward into main; Worktrunk checkout/branch removed. Native Windows branch windows/HUM-117 at 8d5891f was owner-approved and pushed solely for CI; no main push or PR.
AC#1: git push origin HEAD:windows/HUM-117 and task windows:watch exited 0 for CI run 35960366549 at 8d5891f; native Windows task windows:test passed all four new WINDOWS_PACKAGES. Tests start exact argv, capture exit 23, and terminate a surviving descendant via owned Job Object.
AC#2: rg -n "func TestWindows" internal/process internal/app exited 0, listing process_windows_test.go and app_windows_test.go; run 35960366549 executed mismatch/reused PID protection, unsupported TTY/signals, and isolated PATH/PATHEXT tests.
AC#3: go test ./internal/process ./internal/signals ./internal/project ./internal/app -count=1 exited 0 on macOS (also independently rerun by verifier); Unix lifecycle, PTY, readiness and discovery tests remain green.
AC#4: GOOS=windows GOARCH=amd64 go vet ./internal/process ./internal/signals ./internal/project ./internal/app exited 0, including Windows test typecheck.
DoD: task ci exited 0 on final implementation commit 8d5891f (one preceding unrelated CLI attach-flood timeout, focused CLI tests then a full clean rerun); independent verifier returned PASS for AC1-AC4, no tests deleted/skipped/weakened and no unauthorized protected gate changes. One general review identified orphan cleanup, foreign-job termination, executable resolution, and Windows discovery test issues; each was corrected and revalidated. docs/design.md is the sole modified-file-contract deviation, required to describe immediate Windows owned-tree stop, ignored stop_grace, unsupported signals/TTY, and signal-free exit accurately. No blocker. Next: record completion and commit this provider evidence on main.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Native Windows non-TTY exact-argv processes now run in private Job Objects with PID/creation-time identity checks, tree-wide stop and output capture. Unsupported Unix signals and TTY fail explicitly; Unix behavior remains intact. Native Windows CI, cross-vet, focused tests, and task ci passed. Merged into main at 8d5891f; Worktrunk checkout and branch removed.
<!-- SECTION:FINAL_SUMMARY:END -->
