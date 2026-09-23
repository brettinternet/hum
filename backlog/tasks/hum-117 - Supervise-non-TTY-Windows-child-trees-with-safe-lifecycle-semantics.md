---
id: HUM-117
title: Supervise non-TTY Windows child trees with safe lifecycle semantics
status: To Do
assignee: []
created_date: '2026-09-23 20:49'
labels:
  - process
  - architecture
milestone: m-5
dependencies: []
modified_files:
  - internal/process/process.go
  - internal/process/*windows*.go
  - internal/process/*unix*.go
  - internal/process/*_test.go
  - internal/app/app.go
  - internal/app/*windows*.go
  - internal/app/*unix*.go
  - internal/app/*_test.go
  - internal/signals/signals.go
  - internal/signals/*windows*.go
  - internal/signals/*unix*.go
  - internal/signals/*_test.go
  - go.mod
  - go.sum
priority: high
type: feature
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: native Windows can launch exact-argv, non-TTY processes, retain output, identify their incarnation, and stop the whole owned descendant tree. Today internal/process/process.go uses Unix sessions/groups, signals, wait status, pty and x/sys/unix; identity_*.go and group_*.go exist only for Darwin/Linux. internal/app/app.go launches readiness probes with Unix process attributes and calls SIGTERM/SIGKILL; internal/signals/signals.go exposes a Unix-specific signal contract. Scope: isolate platform mechanics behind build-tagged implementations, preserve existing Unix behavior, use Windows Job Objects or an equivalently provable owned-tree mechanism and a PID+creation-time identity to avoid PID-reuse errors; fail closed when group ownership cannot be proved. Specify Windows stop semantics honestly: no claim that arbitrary native programs receive SIGTERM or can honor stop_grace; unsupported Unix signal requests return a stable explicit error (including CLI/MCP-facing paths downstream), not a success-shaped no-op. Provide a Windows non-TTY result with correct exit code and no invented Unix signal. Explicitly reject TTY until its separate follow-up. Adapt executable resolution to Windows PATH/PATHEXT, case-insensitive environment keys and executable extension rules without ambient-path leakage. Non-goals: ConPTY, transport/daemon startup, packaging, or changing Unix signal behavior. A future agent should first inspect internal/process/process.go, internal/app/app.go:1093 and :3771, internal/signals/signals.go, and docs/design.md process lifecycle; choose an identity/ownership API whose handles and error states can be persisted or proved by the daemon task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — On Windows, `go test ./internal/process ./internal/signals ./internal/app` exits 0; Windows-specific tests start an exact-argv fixture, observe its real exit code, and verify a stop terminates the owned descendant tree rather than just the leader.
- [ ] #2 AC2 — On Windows, `go test ./internal/process ./internal/app -run "TestWindows" -count=1` exits 0; tests show reused PID/identity or ownership mismatch cannot authorize stopping an unrelated process, and unsupported Unix signal or TTY operations fail explicitly.
- [ ] #3 AC3 — On macOS or Linux, `go test ./internal/process ./internal/signals ./internal/app -count=1` exits 0; existing Unix process-group, signal, readiness, and PTY behavior is retained.
- [ ] #4 AC4 — On macOS or Linux, `GOOS=windows GOARCH=amd64 go build ./internal/process ./internal/signals ./internal/app` exits 0 without relying on a Windows host.
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
