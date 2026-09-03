---
id: HUM-006
title: 'Deliver serve, run, list, logs, stop, and shutdown commands'
status: Done
assignee:
  - '@brett'
created_date: '2026-09-02 17:06'
updated_date: '2026-09-03 09:23'
labels:
  - cli
  - daemon
  - protocol
  - output
milestone: m-0
dependencies:
  - HUM-005
modified_files:
  - cmd/hum/
  - internal/cli/
  - internal/app/
  - internal/protocol/
priority: high
type: feature
ordinal: 600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: with a daemon running, `hum run` hands child ownership to the daemon and streams live output while attached, and the remaining commands expose detached start, listing, bounded and followed logs, stop, and shutdown, all with human defaults and stable JSON.

Scope: urfave/cli commands `serve` (foreground only; diagnostics on stderr; Ctrl+C runs the stop-all sequence then exits), `run <name> [--detach] [--json] -- <command> [args...]`, `list [--all] [--json]`, `logs <name> [--stream stdout|stderr|both] [--tail N] [--after-cursor N] [--limit-bytes N] [--match REGEX] [--follow] [--json]`, `stop <name>... [--json]`, and `shutdown [--stop-processes] [--json]`; the CLI-edge adapter that reads global flags and `HUM_*` environment variables into the HUM-002 typed input; `run` sends the client cwd and full environment. Attached run streams stdout/stderr with raw line content, returns the managed exit code, forwards the first Ctrl+C as SIGINT to the group and stays attached, turns a second Ctrl+C into the graceful stop sequence, and detaches without signaling the process on SIGTERM/SIGHUP or connection loss. Detached run prints name and PID, or JSON with name, pid, and cursor. Starting a name that is already running fails naming the running PID and suggesting `hum logs <name> --follow`. `list` shows the current project by default and every project with root paths under `--all`. Human `logs` output ends with a one-line stderr trailer naming the next cursor and any truncation so shell callers can continue with `--after-cursor` without JSON. Follow returns the initial bounded selection then cursor-based events; Ctrl+C cancels only the follower; `--json --follow` is NDJSON. `stop` accepts several names, applies the stop sequence to each, returns one stable result per name, and succeeds for a name that is not running (`<name> is not running`). Default shutdown refuses and lists `<project root>: <name>` for each active process; `--stop-processes` terminates every group before daemon exit. No command starts a daemon in this task. Without a daemon: `logs` fails with `Nothing is running. Start a process with hum run <name> -- <command>.`, `stop` succeeds with `Nothing is running.`, `shutdown` succeeds with `No hum daemon is running.`, and `run` fails with `No hum daemon is running. Start it with hum serve --daemon.` until HUM-011 adds automatic start. `hum serve --daemon` is otherwise named only in `serve` help. Keep exact argv forwarding, bounds, and validation errors that name the next command.

Non-goals: `serve --daemon`, automatic daemon start, startup locking, stale recovery, and `daemon.log` (all HUM-011); `status`, `wait`, `restart`, and `down`; PTY or arbitrary interactive input; configuration files; MCP; launchd, systemd, login startup, or OS-service installation.

Modified-file contract: cmd/hum/, internal/cli/, internal/app/, internal/protocol/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/cli -run 'TestForegroundServe|TestDaemonUnavailable'` exits 0 and covers foreground diagnostics on stderr, Ctrl+C on serve stopping managed groups before exit, and no-daemon behavior that creates no runtime state: `logs` fails with the exact `Nothing is running. Start a process with hum run <name> -- <command>.`, `stop` succeeds with `Nothing is running.`, `shutdown` succeeds with `No hum daemon is running.`, and `run` fails with `No hum daemon is running. Start it with hum serve --daemon.`
- [x] #2 `go test ./internal/cli -run 'TestAttachedRun|TestDetachedRun'` exits 0 and covers exact argv, client cwd and environment forwarding, live stdout/stderr, raw line preservation, managed exit codes, first Ctrl+C as SIGINT with continued attachment, second Ctrl+C as graceful stop, SIGTERM or connection loss detaching without termination, duplicate-name rejection naming the PID and suggesting `hum logs <name> --follow`, and detached name/PID and JSON output.
- [x] #3 `go test ./internal/cli -run 'TestList|TestLogsFollow'` exits 0 and covers current-project versus `--all` listing, initial tail/cursor/stream/match/byte-limit selection, the human stderr next-cursor and truncation trailer, multiple followers, bounded delivery, eviction reporting, follower cancellation without process termination, and NDJSON events for `--json --follow`.
- [x] #4 `go test ./internal/cli -run 'TestShutdown|TestStop'` exits 0 and proves default shutdown refuses listing `<project root>: <name>` entries, `--stop-processes` waits for graceful process-tree termination before daemon exit, `stop` with several names returns one human/JSON result per name, and stopping a name that is not running succeeds.
- [x] #5 With `HUM_RUNTIME_DIR` set to a temporary directory and a foreground `serve` running, the built binary performs attached and detached `run`, `list`, bounded and followed `logs`, multi-name and already-stopped `stop`, and both shutdown modes; every command exits as documented and all JSON/NDJSON decodes with stable fields.
- [x] #6 `go test ./internal/cli -run TestNoStatusWaitOrRestartYet` exits 0 and confirms this slice exposes none of `status`, `wait`, `restart`, or `down`.
- [x] #7 `go list -deps ./internal/config | grep -c urfave` prints 0 after the flag adapter is added, proving the adapter lives in internal/cli.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add the six urfave/cli command definitions and a CLI-edge config/environment adapter while preserving the existing root constructor.
2. Compose existing daemon client operations for attached/detached run, project-scoped/all-project list, bounded/followed logs, multi-name stop, and guarded/forced shutdown.
3. Add acceptance-level CLI tests for serve/unavailable behavior, run, list/logs, stop/shutdown, deferred-command absence, and a built-binary scenario within the declared paths.
4. Run every focused acceptance command, the built-binary scenario, task ci, and an independent verifier; fix all findings.
5. Record AC evidence, complete the backlog item, commit, merge to main, and remove the worktree/branch.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation commit: 4f606ff.
AC#1 evidence: `go test ./internal/cli -run 'TestForegroundServe|TestDaemonUnavailable' -count=1` passed (`ok hum/internal/cli`).
AC#2 evidence: `go test ./internal/cli -run 'TestAttachedRun|TestDetachedRun' -count=1` passed (`ok hum/internal/cli`).
AC#3 evidence: `go test ./internal/cli -run 'TestList|TestLogsFollow' -count=1` passed (`ok hum/internal/cli`).
AC#4 evidence: `go test ./internal/cli -run 'TestShutdown|TestStop' -count=1` passed (`ok hum/internal/cli`).
AC#5 evidence: `go test ./cmd/hum -run TestBuiltBinaryIntegration -count=1` passed (`ok hum/cmd/hum`).
AC#6 evidence: `go test ./internal/cli -run TestNoStatusWaitOrRestartYet -count=1` passed (`ok hum/internal/cli`).
AC#7 evidence: `go list -deps ./internal/config | grep -c urfave` printed `0`; grep returns status 1 for zero matches by definition.
Final gate: `task ci` passed on commit 4f606ff. Independent verifier: PASS for AC#1-AC#7 and durable follower timing invariants. Final adversarial review: PASS, no actionable defects.
Modified-file deviation: internal/output/store.go and store_test.go retain/replay the bounded latest terminal event and close subscriptions; internal/daemon/server.go and daemon_test.go route follow through replay-aware app subscriptions and close them. These source-level changes are required to prevent late attached/log followers from hanging or losing exit status across completion/retention eviction; a CLI-only polling workaround was removed after review. No other undeclared paths changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Delivered foreground serve, attached/detached run, list, bounded/followed logs, multi-name stop, and shutdown with stable human/JSON output and durable follower exits. Verified all seven acceptance criteria, built-binary flow, task ci, and independent review on 4f606ff.
<!-- SECTION:FINAL_SUMMARY:END -->
