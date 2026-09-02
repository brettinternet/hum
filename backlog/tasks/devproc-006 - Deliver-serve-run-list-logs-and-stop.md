---
id: DEVPROC-006
title: Deliver daemon lifecycle attached runs and log following
status: To Do
assignee: []
created_date: '2026-09-02 17:06'
updated_date: '2026-09-02 20:05'
labels:
  - cli
  - daemon
  - protocol
  - output
milestone: m-0
dependencies:
  - DEVPROC-005
modified_files:
  - cmd/devproc/
  - internal/cli/
  - internal/app/
  - internal/protocol/
priority: high
type: feature
ordinal: 600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the ordinary `devproc run` workflow starts the daemon when needed, leaves child ownership with the daemon, streams live output while attached, and exposes explicit detached, observation, stop, and shutdown operations.

Scope: urfave/cli commands `serve [--daemon]`, `run <name> [--detach] -- <command> [args...]`, `list [--json]`, `logs <name> [--stream stdout|stderr|both] [--tail N] [--after-cursor N] [--limit-bytes N] [--grep REGEX] [--follow] [--json]`, `stop <name>`, and `shutdown [--stop-processes]`. Foreground serve writes diagnostics to stderr. Detached serve creates a new session/process group with no inherited standard streams, logs to bounded/rotating `daemon.log`, waits for readiness, prints PID and socket, is idempotent, serializes concurrent starts, recovers stale PID/socket files, and reports startup failures. `run` alone auto-starts the detached daemon; all other unavailable-daemon commands fail concisely with `Start it with devproc serve --daemon.` Attached run streams stdout/stderr with raw line content where possible, forwards Ctrl+C as SIGINT to the managed group, returns its exit code, and leaves it running on connection loss. Detached run prints name and PID. Follow returns the initial bounded selection then cursor-based events; Ctrl+C cancels only the follower; `--json --follow` is NDJSON. Default shutdown refuses and names active processes; `--stop-processes` terminates every group before daemon exit. Keep exact argv forwarding, human defaults, stable JSON, bounds, and clear validation errors.

Non-goals: `status`, `wait`, PTY or arbitrary interactive input, configuration files, MCP, launchd, systemd, login startup, or OS-service installation.

Modified-file contract: cmd/devproc/, internal/cli/, internal/app/, internal/protocol/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run 'TestServeModes|TestAutomaticDaemonStartup'` exits 0 and covers foreground diagnostics, detached flags/readiness output, idempotency, `run`-only auto-start, concurrent start races, stale runtime recovery, and the exact unavailable-daemon suggestion for other commands.
- [ ] #2 `go test ./internal/cli -run 'TestAttachedRun|TestDetachedRun'` exits 0 and covers exact argv, live stdout/stderr, raw line preservation, managed exit codes, SIGINT forwarding, connection loss without termination, and detached name/PID output.
- [ ] #3 `go test ./internal/cli -run TestLogsFollow` exits 0 and covers initial tail/cursor/stream/grep/byte-limit selection, multiple followers, bounded delivery, eviction reporting, follower cancellation without process termination, and NDJSON events for `--json --follow`.
- [ ] #4 `go test ./internal/cli -run TestShutdown` exits 0 and proves default shutdown refuses with active process names while `--stop-processes` waits for graceful process-tree termination before daemon exit.
- [ ] #5 With a temporary runtime directory, the built binary runs foreground `serve`, idempotent `serve --daemon`, attached and detached `run`, bounded and followed `logs`, `stop`, and both shutdown modes; every command exits as documented and all JSON/NDJSON decodes with stable fields.
- [ ] #6 `go test ./internal/cli -run TestNoStatusOrWaitYet` confirms this vertical slice exposes neither incomplete command before its integration gate passes.
- [ ] #7 `go test ./internal/cli -run TestDetachedDaemonLog` exits 0 and proves detached diagnostics are written only to `daemon.log` in the runtime directory and rotation or truncation keeps the configured on-disk size bounded.
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
