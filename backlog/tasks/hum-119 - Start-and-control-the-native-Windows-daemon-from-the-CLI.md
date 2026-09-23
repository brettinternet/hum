---
id: HUM-119
title: Start and control the native Windows daemon from the CLI
status: To Do
assignee: []
created_date: '2026-09-23 20:49'
updated_date: '2026-09-23 20:49'
labels:
  - cli
  - daemon
  - integration
milestone: m-5
dependencies:
  - HUM-117
  - HUM-118
modified_files:
  - internal/cli/daemon_start.go
  - internal/cli/tty.go
  - internal/cli/commands.go
  - internal/cli/root.go
  - internal/cli/*windows*.go
  - internal/cli/*unix*.go
  - internal/cli/*_test.go
  - cmd/hum/main.go
  - cmd/hum/*windows*.go
  - cmd/hum/*unix*.go
  - cmd/hum/*_test.go
  - internal/testutil/harness.go
  - internal/testutil/*windows*.go
  - integration/*windows*.go
  - integration/*unix*.go
  - integration/*_test.go
  - internal/mcp/*_test.go
priority: high
type: feature
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: on Windows hum serve/run/up/status/logs/wait/stop/down/shutdown work for non-TTY exact-argv children with the same wire and bounded output semantics as Unix. Today internal/cli/daemon_start.go uses Setsid and negative-PID signals to spawn/cancel a detached daemon; cmd/hum/main.go relies on SIGHUP/SIGTERM and internal/cli/tty.go uses SIGWINCH. Scope: platform-specific daemon startup, cancellation and shutdown, foreground Ctrl+C behavior, stable errors for unsupported Windows Unix-signal and TTY requests, and portable Windows executable/env/manifest use. Keep the CLI/MCP error contract explicit and deterministic; do not silently translate SIGHUP/SIGTERM to forced termination. Preserve Unix behavior. Add real Windows CLI tests using the built fixture binary rather than only mocked daemon clients; use platform-specific test files or tags for Unix-only fixtures, replacing coverage on Windows rather than skipping it. Non-goals: ConPTY, public TCP or installer. Start at internal/cli/daemon_start.go:51-99 and :246-273, cmd/hum/main.go:26, internal/cli/tty.go:124-143, internal/testutil/harness.go and integration/main_test.go.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — On Windows, `go test ./internal/cli ./cmd/hum -count=1` exits 0; real built-binary tests cover autostart, non-TTY run, status, logs, wait, stop and shutdown.
- [ ] #2 AC2 — On Windows, `go test ./internal/cli ./cmd/hum -run "TestWindows" -count=1` exits 0; tests prove concurrent autostart returns one daemon, cancellation does not leave an orphan daemon, and unsupported TTY/Unix-signal requests return explicit stable errors.
- [ ] #3 AC3 — On macOS or Linux, `go test ./internal/cli ./cmd/hum -count=1` exits 0; existing detach, Ctrl+C, HUP and interactive tests retain their coverage.
- [ ] #4 AC4 — On macOS or Linux, `GOOS=windows GOARCH=amd64 go build ./...` exits 0.
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
