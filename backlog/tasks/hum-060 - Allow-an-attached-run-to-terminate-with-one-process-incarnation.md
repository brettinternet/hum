---
id: HUM-060
title: Make attached run control one process incarnation
status: To Do
assignee: []
created_date: '2026-09-06 19:10'
updated_date: '2026-09-09 16:37'
labels:
  - cli
  - lifecycle
milestone: m-4
dependencies: []
references:
  - HUM-020
  - HUM-055
modified_files:
  - internal/cli/commands.go
  - internal/cli/serve_run_test.go
  - internal/cli/help_contract_test.go
  - cmd/hum/integration_test.go
  - integration/run_reconnect_test.go
  - integration/durable_session_test.go
  - integration/tty_interactive_test.go
  - README.md
  - docs/design.md
priority: high
type: enhancement
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
**Problem.** `hum run NAME -- COMMAND` looks like an ordinary foreground command, but today Ctrl+C only detaches the observer and the process keeps running. The attached client also follows the durable named session across exits and later launches. This makes `--detach` describe output attachment rather than process lifecycle and duplicates the observer role now provided explicitly by `hum attach` and `hum logs --follow`. **Outcome.** An attached `hum run` controls exactly one process incarnation. It streams that incarnation’s output, returns when it exits, propagates its exit status, and Ctrl+C signals/stops it. `hum run --detach`, `hum up`, and `hum start` continue handing process ownership to the daemon; `hum attach` and `hum logs --follow` remain the durable observer interfaces. **Run selection.** Attached `run` launches a declared, discovered, or retained stopped definition and follows only the resulting incarnation. An already-running name is not silently adopted as a foreground-owned process: fail with copyable `hum attach NAME` guidance. An unresolved argv-free name fails with guidance to supply `-- COMMAND`. Raw ad-hoc argv rules and declared-name conflict rules remain unchanged. **Exit and signals.** Natural exit returns the child’s ordinary exit code. Signal termination uses conventional shell status `128 + signal` where representable. The first Ctrl+C forwards SIGINT to the current process group and remains attached until it exits; a second Ctrl+C invokes the existing bounded graceful stop sequence. A Ctrl+C-controlled exit is intentional and must not schedule `restart: on-failure`. Parent SIGTERM, SIGHUP, transport loss, and terminal loss keep the existing handoff behavior: detach the client without signaling the daemon-owned child. **Session boundaries.** Run closes after the observed incarnation’s terminal event even when a restart policy schedules a successor; the successor remains daemon-managed and is visible through status, attach, logs, and wait. The retained session and output remain available after run exits. Fast child exit, terminal event replay, and launch/follow registration must be race-free. **Unchanged behavior.** Detached run still returns after launch. Up/start processes survive client exit and are stopped with down/stop. Attach and logs --follow detach on Ctrl+C and continue following durable sessions across launches. TTY Ctrl+C reaches the child through the PTY; when that incarnation exits, attached run returns with the mapped signal status. No MCP arbitrary-command operation, general task runner, shell command strings, CI orchestration, or change to retention is introduced. **Coordination.** This lifecycle change is semantically independent of HUM-058 project-scope canonicalization and HUM-059 global scope. It may be implemented in parallel with HUM-058 in an isolated worktree. HUM-059 should consume the final run parsing and scope-selection shape when rebased; no dependency is required.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/cli -run "^TestAttachedRunOneIncarnation$" -count=1 -v` exits 0 and prints PASS, proving attached run launches a declared, discovered, or retained stopped definition, streams raw stdout/stderr, closes on that incarnation’s terminal event including a fast exit, returns ordinary child exit codes, maps signal exits conventionally, and leaves the retained session and output observable.
- [ ] #2 AC2 — `go test ./internal/cli -run "^TestAttachedRunInterruptLifecycle$" -count=1 -v` exits 0 and prints PASS, proving the first Ctrl+C forwards SIGINT to the current process group and waits for exit, the second invokes the bounded stop sequence, the resulting controlled exit does not schedule on-failure recovery, and SIGTERM, SIGHUP, context/transport loss, and output failure detach without signaling the child.
- [ ] #3 AC3 — `go test ./internal/cli -run "^TestRunSelectionSemantics$" -count=1 -v` exits 0 and prints PASS, proving attached run refuses an already-running name with copyable `hum attach NAME` guidance, an unresolved argv-free name requires `-- COMMAND`, stopped retained and resolved names launch one incarnation, and raw ad-hoc/declared conflict validation is unchanged.
- [ ] #4 AC4 — `go test ./internal/cli -run "^TestDetachedAndObserverLifecycleUnchanged$" -count=1 -v` exits 0 and prints PASS, proving run --detach still returns after launch, up/start processes remain running after their client exits, and Ctrl+C on attach or logs --follow only detaches the observer while durable following across stop/start remains available.
- [ ] #5 AC5 — `go test ./integration -run "^TestAttachedRunForegroundLifecycle$" -count=1 -v` exits 0 and prints PASS with the built binary, proving an attached ad-hoc process receives group SIGINT and exits with status 130, a normal exit status is propagated, retained logs remain readable, an on-failure successor is not followed by the original run, a detached run survives its launcher, and attach Ctrl+C does not stop the target.
- [ ] #6 AC6 — `go test ./internal/cli -run "HelpContract|AttachedRun" -count=1 -v` exits 0 and prints PASS, and README.md plus docs/design.md clearly distinguish foreground one-incarnation run, detached daemon-owned run, durable observation through attach/logs --follow, and daemon-owned up/start lifecycle with copyable examples.
- [ ] #7 AC7 — `task ci` exits 0.
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
