---
id: HUM-119
title: Start and control the native Windows daemon from the CLI
status: To Do
assignee: []
created_date: '2026-09-23 20:49'
updated_date: '2026-09-23 21:18'
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
  - internal/cli/doctor.go
  - internal/cli/*windows*.go
  - internal/cli/*unix*.go
  - internal/cli/*_test.go
  - cmd/hum/main.go
  - cmd/hum/*windows*.go
  - cmd/hum/*unix*.go
  - cmd/hum/*_test.go
  - internal/testutil/harness.go
  - internal/testutil/*windows*.go
  - internal/testutil/*unix*.go
  - internal/testutil/cmd/hum-fixture/*.go
  - integration/*windows*.go
  - integration/*unix*.go
  - integration/*_test.go
  - internal/mcp/*_test.go
  - .taskfiles/windows.yaml
priority: high
type: feature
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: on Windows, hum serve/run/up/status/logs/wait/stop/down/shutdown work for non-TTY exact-argv children with the same wire and bounded-output semantics as on Unix. Today internal/cli/daemon_start.go uses Setsid and negative-PID signals to spawn and cancel a detached daemon; cmd/hum/main.go and internal/cli/commands.go (:832-905, :1504, :3655-3712) rely on SIGHUP/SIGTERM; internal/cli/tty.go uses SIGWINCH; internal/cli/doctor.go:230 checks runtime ancestors with unix.Access. cmd/hum-man and internal/mcp import internal/cli and the daemon, so they only build for Windows once this lands. The integration fixture internal/testutil/cmd/hum-fixture compiles for Windows, but its signal-driven behaviors (SIGTERM/SIGHUP handlers) need Windows equivalents or Windows-specific replacements for tests that depend on them.

Scope: platform-specific daemon startup, cancellation, and shutdown; foreground Ctrl+C behavior; stable errors for Unix-signal and TTY requests that Windows does not support; a portable doctor check; and portable Windows executable, environment, and manifest handling. Keep the CLI/MCP error contract explicit and deterministic, and do not silently translate SIGHUP/SIGTERM into forced termination. Keep Unix behavior unchanged. Add real Windows CLI tests that use the built fixture binary, not only mocked daemon clients. Put Unix-only fixtures in platform-specific test files or behind build tags, and replace their coverage on Windows rather than skipping it.

Non-goals: ConPTY, a public TCP endpoint, and an installer.

Start at internal/cli/daemon_start.go:51-99 and :246-273, cmd/hum/main.go:26, internal/cli/commands.go:832-905, internal/cli/tty.go:124-143, internal/cli/doctor.go:230, internal/testutil/harness.go, internal/testutil/cmd/hum-fixture/main.go, and integration/main_test.go.

Windows verification: after the owner approves, push HEAD to `windows/<task-id>` and run `task windows:watch` (HUM-122). Windows-only tests live in `*_windows_test.go` files so the Windows CI job runs them. Add every package this task ports to WINDOWS_PACKAGES in .taskfiles/windows.yaml.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — After the owner-approved `git push origin HEAD:windows/HUM-119`, `task windows:watch` exits 0 on macOS, with WINDOWS_PACKAGES including ./internal/cli ./internal/mcp ./cmd/hum ./cmd/hum-man. Real built-binary Windows tests cover autostart, non-TTY run, status, logs, wait, stop, and shutdown.
- [ ] #2 AC2 — On macOS, `rg -n "func TestWindows" internal/cli cmd/hum` lists tests in `*_windows_test.go` files proving that concurrent autostart yields exactly one daemon, that cancellation leaves no orphan daemon, and that unsupported TTY and Unix-signal requests return explicit stable errors. AC1 run executes these tests.
- [ ] #3 AC3 — On macOS, `go test ./internal/cli ./internal/mcp ./cmd/hum -count=1` exits 0; existing detach, Ctrl+C, HUP, doctor, and interactive tests keep their coverage.
- [ ] #4 AC4 — On macOS, `GOOS=windows GOARCH=amd64 go build ./... && GOOS=windows GOARCH=amd64 go vet ./internal/cli ./internal/mcp ./cmd/...` exits 0.
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
