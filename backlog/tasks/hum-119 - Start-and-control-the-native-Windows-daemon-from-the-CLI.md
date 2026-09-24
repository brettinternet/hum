---
id: HUM-119
title: Start and control the native Windows daemon from the CLI
status: In Progress
assignee: []
created_date: '2026-09-23 20:49'
updated_date: '2026-09-24 15:18'
labels:
  - cli
  - daemon
  - integration
milestone: m-5
dependencies:
  - HUM-117
  - HUM-118
  - HUM-128
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
ordinal: 11000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: on Windows, hum serve/run/up/status/logs/wait/stop/down/shutdown work for non-TTY exact-argv children with the same wire and bounded-output semantics as on Unix. Today internal/cli/daemon_start.go uses Setsid and negative-PID signals to spawn and cancel a detached daemon; cmd/hum/main.go and internal/cli/commands.go (:832-905, :1504, :3655-3712) rely on SIGHUP/SIGTERM; internal/cli/tty.go uses SIGWINCH; internal/cli/doctor.go:199 checks the runtime directory owner with syscall.Stat_t and :234 checks runtime ancestors with unix.Access. cmd/hum-man and internal/mcp import internal/cli and the daemon, so they only build for Windows once this lands. The integration fixture internal/testutil/cmd/hum-fixture compiles for Windows, but its signal-driven behaviors (SIGTERM/SIGHUP handlers) need Windows equivalents or Windows-specific replacements for tests that depend on them.

Scope: platform-specific daemon startup, cancellation, and shutdown; foreground Ctrl+C behavior; stable errors for Unix-signal and TTY requests that Windows does not support; a portable doctor check; and portable Windows executable, environment, and manifest handling. Keep the CLI/MCP error contract explicit and deterministic, and do not silently translate SIGHUP/SIGTERM into forced termination. Keep Unix behavior unchanged. Windows cancellation must also bound daemon requests the way HUM-128 does for SIGTERM and SIGHUP. Add real Windows CLI tests that use the built fixture binary, not only mocked daemon clients. Put Unix-only fixtures in platform-specific test files or behind build tags, and replace their coverage on Windows rather than skipping it.

Non-goals: ConPTY, a public TCP endpoint, and an installer.

Start at internal/cli/daemon_start.go:51-99 and :246-273, cmd/hum/main.go:26, internal/cli/commands.go:832-905, internal/cli/tty.go:124-143, internal/cli/doctor.go:199 and :234, internal/testutil/harness.go, internal/testutil/cmd/hum-fixture/main.go, and integration/main_test.go.

Windows verification: after the owner approves, push HEAD to `windows/<task-id>` and run `task windows:watch` (HUM-122). Windows-only tests live in `*_windows_test.go` files so the Windows CI job runs them. Add every package this task ports to WINDOWS_PACKAGES in .taskfiles/windows.yaml.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — After the owner-approved `git push origin HEAD:windows/HUM-119`, `task windows:watch` exits 0 on macOS, with WINDOWS_PACKAGES including ./internal/cli ./internal/mcp ./cmd/hum ./cmd/hum-man. Real built-binary Windows tests cover autostart, non-TTY run, status, logs, wait, stop, and shutdown.
- [x] #2 AC2 — On macOS, `rg -n "func TestWindows" internal/cli cmd/hum` lists tests in `*_windows_test.go` files proving that concurrent autostart yields exactly one daemon, that cancellation leaves no orphan daemon, and that unsupported TTY and Unix-signal requests return explicit stable errors. AC1 run executes these tests.
- [x] #3 AC3 — On macOS, `go test ./internal/cli ./internal/mcp ./cmd/hum -count=1` exits 0; existing detach, Ctrl+C, HUP, doctor, and interactive tests keep their coverage.
- [x] #4 AC4 — On macOS, `GOOS=windows GOARCH=amd64 go build ./... && GOOS=windows GOARCH=amd64 go vet ./internal/cli ./internal/mcp ./cmd/...` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Isolate Unix startup, signals, TTY and doctor checks behind OS-specific implementations while preserving Unix behavior.
2. Port Windows CLI and fixture workflows with real built-binary tests for autostart, lifecycle, cancellation, unsupported requests.
3. Cross-build/vet and run focused Unix checks, then native Windows CI with owner-approved push; independent verification and task ci.
4. Commit implementation, integrate main, finalize provider evidence and clean up owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1: git push https://github.com/brettinternet/hum.git HEAD:windows/HUM-119 at 47eca25; task windows:watch exited 0 (run 36018910860). Go CI (Windows) task windows:test ran ./internal/cli ./internal/mcp ./cmd/hum ./cmd/hum-man; TestWindowsBuiltBinaryConcurrentAutostartAndLifecycle covers built-binary autostart, run, status, logs, wait, stop, shutdown.
AC#2: rg -n "func TestWindows" internal/cli cmd/hum lists native tests for concurrent autostart, daemon cancellation/reaping, unsupported TTY and Unix-signal errors, doctor readiness/manifest, and foreground interrupt cleanup; run 36018910860 passed.
AC#3: go test ./internal/cli ./internal/mcp ./cmd/hum -count=1 exited 0 on macOS; GOFLAGS=-p=1 task ci passed at 47eca25 with Unix detach, Ctrl+C, HUP, doctor and interactive coverage intact.
AC#4: GOOS=windows GOARCH=amd64 go build ./... and GOOS=windows GOARCH=amd64 go vet ./internal/cli ./internal/mcp ./cmd/... exited 0 on macOS.
Modified-file deviations: internal/daemon/transport_windows.go handles absent named pipes for built-binary autostart; internal/daemon/event_history.go and internal/daemon/event_history_windows_test.go handle Windows-incompatible directory fsync. Required support for scoped Windows CLI runtime.
Review: independent verifier PASS AC1-4 at 7a0fb7b; its doctor/interrupt findings corrected in b056b9f and dc470b0. Targeted verifier PASS DoD #5 at dc470b0; additional native doctor readiness/private-manifest/environment/colors tests passed in run 36018910860. Console-level Ctrl+C delivery remains untested on headless Windows CI; synthetic interrupt through production follow-loop stops and reaps real child. Main integration and worktree cleanup remain.
<!-- SECTION:NOTES:END -->
