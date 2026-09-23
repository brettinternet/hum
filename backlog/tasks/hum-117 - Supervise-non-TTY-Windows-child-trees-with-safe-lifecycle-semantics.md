---
id: HUM-117
title: Supervise non-TTY Windows child trees with safe lifecycle semantics
status: To Do
assignee: []
created_date: '2026-09-23 20:49'
updated_date: '2026-09-23 21:18'
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
- [ ] #1 AC1 — After the owner-approved `git push origin HEAD:windows/HUM-117`, `task windows:watch` exits 0 on macOS, with WINDOWS_PACKAGES including ./internal/process ./internal/signals ./internal/project ./internal/app. Windows tests in those packages start an exact-argv fixture, observe its real exit code, and prove that a stop terminates the owned descendant tree, not only the leader.
- [ ] #2 AC2 — On macOS, `rg -n "func TestWindows" internal/process internal/app` lists tests in `*_windows_test.go` files proving that: a reused PID or an identity/ownership mismatch cannot authorize stopping an unrelated process; unsupported Unix signal requests and TTY requests fail with the stable explicit error; and PATH/PATHEXT resolution does not use the ambient PATH. AC1 run executes these tests.
- [ ] #3 AC3 — On macOS, `go test ./internal/process ./internal/signals ./internal/project ./internal/app -count=1` exits 0; existing Unix process-group, signal, readiness, discovery, and PTY behavior is retained.
- [ ] #4 AC4 — On macOS, `GOOS=windows GOARCH=amd64 go vet ./internal/process ./internal/signals ./internal/project ./internal/app` exits 0, which also type-checks the Windows test files.
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
