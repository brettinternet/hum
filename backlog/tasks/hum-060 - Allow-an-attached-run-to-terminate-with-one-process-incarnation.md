---
id: HUM-060
title: Make attached run own one process incarnation
status: To Do
assignee: []
created_date: '2026-09-06 19:10'
updated_date: '2026-09-09 16:46'
labels:
  - cli
  - lifecycle
milestone: m-4
dependencies: []
references:
  - HUM-020
  - HUM-055
  - HUM-048
modified_files:
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/daemon/client.go
  - internal/daemon/server.go
  - internal/daemon/wire_protocol.go
  - internal/daemon/daemon_test.go
  - internal/daemon/wire_protocol_test.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/serve_run_test.go
  - internal/cli/run_args_test.go
  - internal/cli/surface_test.go
  - internal/cli/help_contract_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - internal/skill/SKILL.md
  - internal/skill/skill_test.go
  - cmd/hum/integration_test.go
  - integration/run_reconnect_test.go
  - integration/durable_session_test.go
  - integration/tty_interactive_test.go
  - integration/attached_run_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: high
type: enhancement
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
**Problem.** `hum run NAME -- COMMAND` looks like an ordinary foreground command, but today it is a durable observer: Ctrl+C only detaches, the child keeps running, the exit status is lost, and the client follows the session across exits and later launches. `--detach` therefore describes output attachment rather than process lifecycle, and the observer role is already served explicitly by `hum attach` and `hum logs --follow`.

**Outcome.** Attached `hum run` behaves like running the command yourself while the daemon keeps supervising it: it launches exactly one incarnation, streams that incarnation's output, returns when it exits, exits with the same status, and Ctrl+C or SIGTERM stops it. `hum run --detach`, `hum up`, and `hum start` keep handing ownership to the daemon; `hum attach` and `hum logs --follow` remain the durable observer interfaces.

**Selection (before any launch).**
- `hum run NAME -- COMMAND` launches an ad-hoc incarnation. `hum run NAME` launches a declared, discovered, or retained stopped definition with its existing launch rules (retained ad-hoc records reuse recorded argv, cwd, and environment).
- A running name is never adopted: fail before launch with `NAME is already running; join it with hum attach NAME or stop it with hum stop NAME`. The same guidance replaces the detached `logs --follow` hint for name-in-use.
- An argv-free name that resolves to nothing fails with `run NAME requires a command after --`.
- A declared name given argv still fails; its guidance names both `hum run NAME` (foreground) and `hum start NAME` (background). Every selection failure exits 1 and happens before daemon mutation.
- Flag grammar is unchanged: `--detach`/`-d`, `--json` (detached only), `--tty`, and `--project`/`-C` before or after NAME. No new flags.

**Output contract.** Child stdout goes to hum stdout and child stderr to hum stderr, raw and unprefixed, without launch, exit, or waiting boundary lines. hum itself writes to stderr only: the Ctrl+C hint, a one-line detach notice, and one closing line when the incarnation was signal-terminated or when a restart policy scheduled a successor (`NAME exited with code 1; on-failure restart scheduled, follow it with hum attach NAME`). Retained output is not replayed: the follow starts at the launch cursor.

**Exit status.** Natural exit returns the child's exit code unchanged. Signal termination returns `128 + signal` (Ctrl+C typically 130, SIGTERM typically 143). Once the incarnation has launched, the exit status is always the child's, so hum's own 1/2/3 codes apply only to failures before launch. A detach exits 0.

**Signals.** Signals that mean "stop the work" stop the child; the signal that means "the terminal went away" detaches and leaves the daemon-owned child running.
- First SIGINT (Ctrl+C): hum sends SIGINT to the child process group as a control action, prints `interrupt sent to NAME; press Ctrl+C again to stop` on stderr, and stays attached until the incarnation exits.
- Second SIGINT: hum invokes the existing bounded stop sequence (SIGTERM, StopGrace, SIGKILL) and stays attached until the terminal event.
- SIGTERM (`timeout`, `kill %1`, CI cancellation, a task runner tearing down children): hum invokes the same bounded stop sequence, stays attached until the terminal event, and exits with the child's status. A sender of SIGTERM must never be left believing the work stopped while the child runs on.
- All three are operator intent. The daemon must record control intent for the forwarded SIGINT so a `restart: on-failure` definition does not schedule a successor after a Ctrl+C or SIGTERM exit. `Supervisor.Signal` and `hum signal` / MCP `signal` stay observational; the control variant is a separate request field or path used only by attached run.
- An in-flight stop sequence completes daemon-side even if the client disconnects or is killed mid-sequence. If SIGKILL reaches hum before it has sent the stop request, the child survives daemon-owned; document this rather than mask it.
- SIGHUP, terminal loss, transport loss, and output write failure detach the client without signaling the daemon-owned child, printing `detached from NAME; it keeps running (hum attach NAME)` when stderr is still writable, and exit 0. Closing the terminal or `kill -HUP` is therefore the deliberate way to walk away from a foreground run without stopping it.
- TTY runs (`--tty`, or a declared/retained TTY definition) forward Ctrl+C as bytes through the PTY while input is attached; after Ctrl-] releases input the SIGINT rules above apply. SIGTERM and SIGHUP behave as above regardless of input state. When the incarnation exits, run returns with the mapped status. Input-lease conflict is not possible because a running name is refused.

**Session boundary.** Run closes after the observed incarnation's terminal event even when a restart policy schedules a successor; the successor stays daemon-managed and visible through status, attach, logs, and wait. The retained session and its output remain readable after run exits (`hum logs NAME`). Register the follower before launch and scope it with the launch cursor from the start response so a fast exit, a replayed earlier terminal event, and a concurrent launcher are all handled without races or polling.

**Unchanged.** Detached run returns after launch with today's human and JSON output. Up/start processes survive client exit and are stopped with down/stop. Attach, logs --follow, and up detach on Ctrl+C, SIGTERM, and SIGHUP and never signal managed processes. No MCP arbitrary-command operation, task runner, shell strings, CI orchestration, or retention change.

**Existing tests.** `TestAttachedRun`, `TestRunAttachesToRunning`, and `TestAttachSurface` encode the old start-or-attach contract; rewrite their run-specific assertions to this contract in place and note the change in Implementation Notes. Do not weaken unrelated assertions.

**Coordination.** Independent of HUM-058 project-scope canonicalization and HUM-059 global scope; implement in an isolated worktree in parallel with HUM-058. HUM-059 consumes the final run parsing and selection shape when rebased; no dependency is required.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run "^TestAttachedRunOneIncarnation$" -count=1 -v` exits 0 and prints PASS, proving attached run launches an ad-hoc, declared, discovered, and retained stopped definition; writes child stdout and stderr raw to hum stdout and stderr without boundary lines or retained replay; closes on that incarnation's terminal event including an exit faster than follower setup; returns the child's exit code; maps a signal exit to 128+signal; and leaves the retained session and output readable through `hum logs NAME`.
- [ ] #2 `go test ./internal/cli -run "^TestAttachedRunInterruptLifecycle$" -count=1 -v` exits 0 and prints PASS, proving the first Ctrl+C sends a control SIGINT to the process group, prints the hint, and waits for exit with status 130; the second Ctrl+C runs the bounded stop sequence; SIGTERM to hum runs the bounded stop sequence, stays attached, and exits with the child's status (143 for an unhandled SIGTERM); a Ctrl+C or SIGTERM exit of a `restart: on-failure` definition schedules no successor; an in-flight stop completes when the client disconnects mid-sequence; and SIGHUP, context cancellation, transport loss, and output failure detach with the attach notice and exit 0 without signaling the child.
- [ ] #3 `go test ./internal/cli -run "^TestRunSelectionSemantics$" -count=1 -v` exits 0 and prints PASS, proving a running name fails before launch with the `hum attach NAME` / `hum stop NAME` guidance for argv-free and argv forms; an unresolved argv-free name fails with `requires a command after --`; a declared name with argv fails naming `hum run NAME` and `hum start NAME`; every selection failure exits 1 without mutating the daemon; and flag placement before or after NAME is unchanged.
- [ ] #4 `go test ./internal/app ./internal/protocol ./internal/daemon -run "ControlSignal" -count=1 -v` exits 0 and prints PASS, proving the control-intent SIGINT path suppresses on-failure relaunch after the resulting exit, `Supervisor.Signal`, `hum signal`, and MCP `signal` remain observational, a stop sequence started by a client that then disconnects still runs to its terminal event, and the wire protocol round-trips the control variant while legacy signal requests stay observational.
- [ ] #5 `go test ./internal/cli -run "^TestDetachedAndObserverLifecycleUnchanged$" -count=1 -v` exits 0 and prints PASS, proving run --detach returns after launch with unchanged human and JSON output, up/start processes remain running after their client exits, and Ctrl+C, SIGTERM, or SIGHUP on attach or logs --follow only detaches the observer while durable following across stop/start remains available.
- [ ] #6 `go test ./integration -run "^TestAttachedRunForegroundLifecycle$" -count=1 -v` exits 0 and prints PASS with the built binary, proving an attached ad-hoc process receives group SIGINT on Ctrl+C and hum exits 130, SIGTERM to hum stops the child and hum exits 143, SIGHUP to hum leaves the child running and hum exits 0, a natural exit code is propagated, stdout and stderr are separated, retained logs remain readable, an on-failure successor is not followed by the original run, a detached run survives its launcher, a TTY run returns the mapped status when the child exits, and attach Ctrl+C does not stop the target.
- [ ] #7 `go test ./internal/cli -run "HelpContract|AttachedRun|AttachSurface|LifecycleHelp" -count=1 -v` exits 0 and prints PASS, and `hum run --help`, README.md, docs/design.md, docs/coding-agents.md, and internal/skill/SKILL.md distinguish foreground one-incarnation run (Ctrl+C and SIGTERM stop, SIGHUP detaches, exit status propagates), detached daemon-owned run, durable observation through attach and logs --follow, and daemon-owned up/start lifecycle, with copyable examples `hum run api -- bun run api`, `hum run api --detach -- bun run api`, and `hum attach api`.
- [ ] #8 `task ci` exits 0.
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
