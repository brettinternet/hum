---
id: HUM-011
title: Start the daemon detached and automatically from run
status: Done
assignee:
  - '@agent'
created_date: '2026-09-02 20:13'
updated_date: '2026-09-03 10:14'
labels:
  - cli
  - daemon
  - security
milestone: m-0
dependencies:
  - HUM-006
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

Scope: `serve --daemon` starts a fully detached daemon in a new session/process group with no inherited standard streams, writes diagnostics only to a bounded or rotating `daemon.log` in the runtime directory, waits for the readiness handshake, and prints PID and socket path; detached startup is idempotent, serializes concurrent starts through the startup lock, verifies live ownership before recovering stale PID/socket files, and reports readiness failures to the caller; `run` (attached or detached) auto-starts the detached daemon when no daemon answers, including when several `run` clients race, replacing the temporary HUM-006 `run` no-daemon error; on a protocol-version mismatch `run` shuts down the old daemon and starts a new one when it has no managed processes, and otherwise fails naming the daemon version and `hum shutdown --stop-processes`; every other command keeps its HUM-006 no-daemon behavior and never starts a daemon.

Non-goals: launchd, systemd, login startup, OS-service installation, idle daemon exit, `status`, `wait`, `restart`, or MCP.

Modified-file contract: cmd/hum/, internal/cli/, internal/daemon/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/cli -run 'TestServeDaemon|TestAutomaticDaemonStartup'` exits 0 and covers detached readiness/PID/socket output, idempotency, `run`-only auto-start for attached and detached runs, concurrent start races selecting one daemon, stale runtime recovery, startup-failure reporting, and the unchanged HUM-006 no-daemon messages for every other command.
- [x] #2 `go test ./internal/cli -run TestDetachedDaemonLog` exits 0 and proves detached diagnostics are written only to `daemon.log` in the runtime directory and rotation or truncation keeps the configured on-disk size bounded.
- [x] #3 `go test ./internal/cli -run TestVersionMismatch` exits 0 and proves `run` replaces a mismatched idle daemon and refuses with the daemon version and the shutdown command when managed processes exist.
- [x] #4 With `HUM_RUNTIME_DIR` set to a temporary directory and no daemon running, the built binary runs `hum run demo --detach -- sleep 30` successfully, `hum serve --daemon` twice reports the same PID, the daemon survives closing the launching terminal, and `daemon.log` exists while the caller stdout/stderr received nothing from the daemon.
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
Claimed for implementation in isolated worktree on 2026-09-03.

AC#1: `mise exec go -- go test ./internal/cli -run 'TestServeDaemon|TestAutomaticDaemonStartup' -count=1` exited 0; covers detached readiness/status, idempotency, attached/detached run autostart, racing clients, stale recovery, startup failure, and unchanged list/logs/stop/shutdown no-daemon outputs.
AC#2: `mise exec go -- go test ./internal/cli -run TestDetachedDaemonLog -count=1` exited 0; detached diagnostics stayed in bounded daemon.log with empty daemon stdout and configured size enforcement.
AC#3: `mise exec go -- go test ./internal/cli -run TestVersionMismatch -count=1` exited 0; idle mismatch replacement and active-process refusal with daemon version/shutdown guidance passed.
AC#4: `mise exec go -- go test ./cmd/hum -run 'TestBuiltBinaryIntegration/daemon_autostart' -count=1` exited 0; fresh-runtime built binary autostarted run, repeated serve --daemon returned one live PID/socket, and daemon.log existed without daemon stream leakage.
Race evidence: `mise exec go -- go test -race ./internal/cli -run 'TestServeDaemon|TestAutomaticDaemonStartup' -count=1` exited 0.
Gate evidence: `task ci` exited 0 on final commit 56d18ac (gofmt, go vet, staticcheck, go test ./...). Independent verifier returned PASS for AC#1-#4, declared-path scope, and no weakened/deleted tests; final adversarial review returned PASS. Changed files are only under cmd/hum/, internal/cli/, and internal/daemon/; no protected gate files changed.
Final commit: 56d18ac (`feat(cli): start daemon automatically`).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented detached, idempotent daemon startup for `serve --daemon` and automatic startup from `run`, including concurrent-start convergence, stale runtime recovery, bounded detached diagnostics, idle version replacement, active-version refusal, and built-binary coverage. Final commit 56d18ac; focused acceptance commands, race detector, independent verifier, adversarial review, and `task ci` all passed.
<!-- SECTION:FINAL_SUMMARY:END -->
