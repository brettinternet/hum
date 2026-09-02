---
id: DEVPROC-011
title: Start the daemon detached and automatically from run
status: To Do
assignee: []
created_date: '2026-09-02 20:13'
updated_date: '2026-09-02 20:27'
labels:
  - cli
  - daemon
  - security
milestone: m-0
dependencies:
  - DEVPROC-006
modified_files:
  - cmd/hum/
  - internal/cli/
  - internal/daemon/
priority: high
type: feature
ordinal: 650
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the ordinary `hum run` workflow needs no separate daemon step: it starts a detached daemon when none is available, tolerates racing clients, recovers stale runtime files, retires an idle daemon left behind by a binary upgrade, and keeps detached diagnostics in a bounded log.

Scope: `serve --daemon` starts a fully detached daemon in a new session/process group with no inherited standard streams, writes diagnostics only to a bounded or rotating `daemon.log` in the runtime directory, waits for the readiness handshake, and prints PID and socket path; detached startup is idempotent, serializes concurrent starts through the startup lock, verifies live ownership before recovering stale PID/socket files, and reports readiness failures to the caller; `run` (attached or detached) auto-starts the detached daemon when no daemon answers, including when several `run` clients race; on a protocol-version mismatch `run` shuts down the old daemon and starts a new one when it has no managed processes, and otherwise fails naming the daemon version and `hum shutdown --stop-processes`; every other command still fails with `Start it with hum serve --daemon.` and never starts a daemon.

Non-goals: launchd, systemd, login startup, OS-service installation, idle daemon exit, `status`, `wait`, `restart`, or MCP.

Modified-file contract: cmd/hum/, internal/cli/, internal/daemon/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run 'TestServeDaemon|TestAutomaticDaemonStartup'` exits 0 and covers detached readiness/PID/socket output, idempotency, `run`-only auto-start for attached and detached runs, concurrent start races selecting one daemon, stale runtime recovery, startup-failure reporting, and the exact unavailable-daemon suggestion for other commands.
- [ ] #2 `go test ./internal/cli -run TestDetachedDaemonLog` exits 0 and proves detached diagnostics are written only to `daemon.log` in the runtime directory and rotation or truncation keeps the configured on-disk size bounded.
- [ ] #3 `go test ./internal/cli -run TestVersionMismatch` exits 0 and proves `run` replaces a mismatched idle daemon and refuses with the daemon version and the shutdown command when managed processes exist.
- [ ] #4 With `HUM_RUNTIME_DIR` set to a temporary directory and no daemon running, the built binary runs `hum run demo --detach -- sleep 30` successfully, `hum serve --daemon` twice reports the same PID, the daemon survives closing the launching terminal, and `daemon.log` exists while the caller's stdout/stderr received nothing from the daemon.
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
