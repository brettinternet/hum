---
id: DEVPROC-006
title: 'Deliver serve, run, list, logs, stop, and shutdown commands'
status: To Do
assignee: []
created_date: '2026-09-02 17:06'
updated_date: '2026-09-02 20:13'
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
Outcome: with a daemon running, `devproc run` hands child ownership to the daemon and streams live output while attached, and the remaining commands expose detached start, listing, bounded and followed logs, stop, and shutdown, all with human defaults and stable JSON.

Scope: urfave/cli commands `serve` (foreground only; diagnostics on stderr; Ctrl+C runs the stop-all sequence then exits), `run <name> [--detach] [--json] -- <command> [args...]`, `list [--all] [--json]`, `logs <name> [--stream stdout|stderr|both] [--tail N] [--after-cursor N] [--limit-bytes N] [--grep REGEX] [--follow] [--json]`, `stop <name>`, and `shutdown [--stop-processes]`; the CLI-edge adapter that reads global flags and `DEVPROC_*` environment variables into the DEVPROC-002 typed input; `run` sends the client's cwd and full environment. Attached run streams stdout/stderr with raw line content, returns the managed exit code, forwards the first Ctrl+C as SIGINT to the group and stays attached, turns a second Ctrl+C into the graceful stop sequence, and detaches without signaling the process on SIGTERM/SIGHUP or connection loss. Detached run prints name and PID, or JSON with name, pid, and cursor. `list` shows the current project by default and every project with root paths under `--all`. Follow returns the initial bounded selection then cursor-based events; Ctrl+C cancels only the follower; `--json --follow` is NDJSON. Default shutdown refuses and lists `<project root>: <name>` for each active process; `--stop-processes` terminates every group before daemon exit. In this task every command, including `run`, fails concisely with `Start it with devproc serve --daemon.` when no daemon is available; DEVPROC-011 adds automatic start for `run`. Keep exact argv forwarding, bounds, and validation errors that name the next command.

Non-goals: `serve --daemon`, automatic daemon start, startup locking, stale recovery, and `daemon.log` (all DEVPROC-011); `status`, `wait`, and `restart`; PTY or arbitrary interactive input; configuration files; MCP; launchd, systemd, login startup, or OS-service installation.

Modified-file contract: cmd/devproc/, internal/cli/, internal/app/, internal/protocol/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run 'TestForegroundServe|TestDaemonUnavailable'` exits 0 and covers foreground diagnostics on stderr, Ctrl+C on serve stopping managed groups before exit, and the exact `Start it with devproc serve --daemon.` failure for every command when no socket answers.
- [ ] #2 `go test ./internal/cli -run 'TestAttachedRun|TestDetachedRun'` exits 0 and covers exact argv, client cwd and environment forwarding, live stdout/stderr, raw line preservation, managed exit codes, first Ctrl+C as SIGINT with continued attachment, second Ctrl+C as graceful stop, SIGTERM or connection loss detaching without termination, duplicate-name rejection naming the PID, and detached name/PID and JSON output.
- [ ] #3 `go test ./internal/cli -run 'TestList|TestLogsFollow'` exits 0 and covers current-project versus `--all` listing, initial tail/cursor/stream/grep/byte-limit selection, multiple followers, bounded delivery, eviction reporting, follower cancellation without process termination, and NDJSON events for `--json --follow`.
- [ ] #4 `go test ./internal/cli -run TestShutdown` exits 0 and proves default shutdown refuses listing `<project root>: <name>` entries while `--stop-processes` waits for graceful process-tree termination before daemon exit.
- [ ] #5 With `DEVPROC_RUNTIME_DIR` set to a temporary directory and a foreground `serve` running, the built binary performs attached and detached `run`, `list`, bounded and followed `logs`, `stop`, and both shutdown modes; every command exits as documented and all JSON/NDJSON decodes with stable fields.
- [ ] #6 `go test ./internal/cli -run TestNoStatusWaitOrRestartYet` exits 0 and confirms this slice exposes none of the three later commands.
- [ ] #7 `go list -deps ./internal/config | grep -c urfave` prints 0 after the flag adapter is added, proving the adapter lives in internal/cli.
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
