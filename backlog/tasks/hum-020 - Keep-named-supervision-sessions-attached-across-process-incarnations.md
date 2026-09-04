---
id: HUM-020
title: Keep named supervision sessions attached across process incarnations
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-04 17:00'
updated_date: '2026-09-04 20:20'
labels:
  - cli
  - daemon
  - lifecycle
  - mcp
  - docs
dependencies:
  - HUM-015
modified_files:
  - internal/app/
  - internal/output/
  - internal/protocol/
  - internal/daemon/
  - internal/cli/
  - internal/mcp/
  - internal/skill/
  - integration/
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: high
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: hum treats a project-scoped process name as a durable supervision session and each child launch as one incarnation. Stopping a task never tears down terminals or tools following that name, and observers may attach to a name before it has ever launched. The default flow for restarting with intermediate work needs no flags and no coordination: `hum stop web`, run migrations or installs, `hum start web`, and every follower resumes in place. The same flow works over MCP with `stop` then `start`.

Scope, following: attached `hum run <name>` and `hum logs <name> --follow` follow the named session, not one incarnation. They print one visible boundary line per lifecycle event (incarnation exited with code or signal, waiting for next launch, launched), remain idle while the name is stopped, and resume at the next `start`, `run`, `restart`, or `up` launch until the client interrupts, the session is removed, or the daemon/transport ends. This is the default; no keep-open flag exists. Ctrl+C detaches only that observer and neither stops the child nor removes the session. Followers must not miss a rapid stop/relaunch. On daemon loss a follower exits nonzero with a clear message and does not reconnect or poll.

Scope, pre-launch attach: `hum logs <name> --follow` and attached `hum run <name>` are race-free against concurrent launchers. A follow may be opened for a syntactically valid name that has never launched; it ensures a daemon exists, states that it is waiting for the first launch (and that the name does not resolve, when so, since an ad hoc `run` may create it), and then streams that launch without polling. Attached argv-free `hum run <name>` on an already-running session attaches to it (retained output, then live) instead of failing; `run <name> -- <command>` keeps today's conflict rules while the name is running and replaces the retained ad hoc launch spec when the session is stopped. A live pre-launch or stopped-session follower reserves its session from completed-record eviction; unobserved completed sessions keep the existing bounded retention.

Scope, bounded wait: `hum wait <name>` without `--after-cursor` on a stopped or never-launched session waits, within its existing timeout, for the next launch and then evaluates from that launch cursor. Match, exit, and timeout outcomes and exit codes are unchanged, the default timeout stays 30s, and a wait still observes one incarnation. This is the only wait change and exists so agents can wait on a name another agent or human is about to start without polling.

Scope, start: broaden `hum start <name>` to process-manager semantics. A running record is already running (success, no relaunch); a retained stopped record is relaunched; an absent record must resolve from hum.yaml or conventional discovery. Retained ad hoc records reuse their exact recorded argv, cwd, and environment; resolved records use the current definition and requesting-client environment. `restart` remains stop-then-start for a known record. MCP `start` shares these semantics.

Scope, remove: add `hum remove <name>...` with CLI and MCP parity. It stops any running incarnation, closes every follower for that session, clears retained launch spec and output, and removes only runtime state, never hum.yaml. It is the only operation that closes followers. `stop` and `down` stop children and preserve sessions and followers. Force and kill terminology is not overloaded: immediate SIGKILL policy is separate from session removal.

Scope, up/down: `up` remains one-shot; it ensures current resolved definitions are running, waits for readiness, and does not attach or adopt ad hoc sessions. `down` keeps stopping every running project record regardless of how it was launched, and an in-flight `start`/`up` readiness wait observes that exit as early exit rather than hanging. Launches from `up` are visible to existing followers; after `down` then `up`, an ad hoc session stays stopped and its followers keep waiting, which the docs state. Readiness resets per incarnation; status/list keep distinguishing running and stopped records.

No indefinite hangs for agents: MCP exposes no follow or other unbounded operation; `wait`, `start`, and `up` keep their bounded timeouts and `remove` completes within the stop grace period. Only `logs --follow` and attached `run` are unbounded, and both are terminal-oriented. Docs and the bundled skill tell agents to use `wait` and bounded `logs`, never `--follow`, and describe stop, intermediate commands, start as the default way to restart with work; the skill no longer limits `stop` to developer requests.

Non-goals: task-runner hooks, arbitrary prerequisite commands, dependency ordering, PTYs or stdin forwarding, persistent state across daemon restart, follower reconnect, changing readiness or wait outcomes beyond the pre-launch entry above, making plain bounded `logs` start a daemon, keeping unobserved completed records forever, follower counts in status, or using `stop --force` to control observers.

Modified-file contract: internal/app/, internal/output/, internal/protocol/, internal/daemon/, internal/cli/, internal/mcp/, internal/skill/, integration/, README.md, docs/design.md, docs/coding-agents.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/app ./internal/output -run 'Test.*(Session|Follow|StartStopped|Remove|WaitPreLaunch)' -count=1` exits 0 and proves a name follower survives ordinary stop and natural exit, observes ordered exit/wait/launch boundaries and successor output, cannot miss a rapid relaunch, protects its session from eviction, detaches independently on cancellation, and is closed only by session removal or daemon shutdown; and that wait without --after-cursor on a stopped or never-launched session blocks until the next launch within its deadline, evaluates from that launch cursor, and returns the existing timeout outcome when no launch arrives.
- [ ] #2 `go test ./internal/protocol ./internal/daemon -run 'Test.*(Follow|Start|Remove)' -count=1` exits 0 and proves the versioned protocol supports a race-free name-scoped follow before, during, and between incarnations, including a syntactically valid name that does not resolve; ordinary start/restart/up launches wake followers; stop/down do not close them; remove does; and transport cancellation releases every subscription.
- [ ] #3 `go test ./internal/cli ./internal/mcp -run 'Test.*(Start|Run|Follow|Remove|Wait|LifecycleHelp)' -count=1` exits 0 and proves start is idempotent for running names, relaunches retained stopped resolved and ad hoc records with the specified launch-source/environment rules, absent names still require resolution, attached argv-free run on a running name attaches instead of failing, remove has CLI/MCP parity and never edits hum.yaml, MCP start/stop/remove share session semantics, MCP exposes no follow or unbounded tool, wait keeps its 30s default and exits 2 when no launch arrives, follow starts a daemon only when requested, and no keep-open, force, or hook interface is introduced.
- [ ] #4 `go test ./integration -run 'Test(DurableFollowAcrossStopStart|FollowBeforeFirstLaunch|RunAttachesToRunning|WaitBeforeStart|RemoveSupervisionSession|UpWithDurableFollowers|FollowerExitsOnDaemonShutdown)' -count=1` exits 0 and proves two-shell workflows end to end: attached run and logs followers remain open across stop, intermediate commands, start and across down/up; a follower opened before any process receives a later run; a second attached run joins a running session; a wait started before another shell's start returns its match; Ctrl+C detaches without stopping the child; remove closes followers and discards retained ad hoc launch state; up remains one-shot and leaves ad hoc followers waiting; and daemon shutdown ends followers with a nonzero exit and message instead of hanging.
- [ ] #5 `go test ./internal/cli ./internal/skill -run 'Test(LifecycleHelp|ResolvedProjectInstructions)' -count=1` exits 0 and README.md, docs/design.md, docs/coding-agents.md, CLI help, and the bundled skill consistently document durable default following, pre-launch following and waiting, run attaching to running sessions, start of retained stopped records, stop versus remove, Ctrl+C detachment, bounded eviction, daemon-loss exit, unchanged up/down semantics, the stop, intermediate commands, start flow as the default restart-with-work pattern for humans and agents, and that agents use bounded wait/logs rather than --follow; the skill no longer restricts stop to developer requests.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Make app records durable sessions with one retained store across incarnations; add race-free pre-launch Subscribe/Wait, lifecycle boundary entries, retained/idempotent Start, removal, and subscriber-aware eviction.
2. Extend output subscriptions with explicit close semantics so remove closes cleanly and shutdown reports failure.
3. Add remove protocol/daemon/client support and make follow streams span exits and launches without polling.
4. Update CLI and MCP start/run/logs/wait/remove semantics while preserving bounded agent operations and one-shot up/down.
5. Add focused unit and integration coverage, update lifecycle documentation and bundled skill, run every acceptance command, review, verify, commit slices, and push main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented durable app sessions, pre-launch follow/wait, subscriber-aware retention, stopped-session relaunch, protocol v6 remove, durable daemon streams, CLI/MCP remove, and Ctrl+C detach semantics. Commits: 7350a3c, d0d2546. Focused app, protocol/daemon, and CLI/MCP acceptance command subsets pass.

AC#1 PASS — go test ./internal/app ./internal/output -run 'Test.*(Session|Follow|StartStopped|Remove|WaitPreLaunch)' -count=1 exited 0.
AC#2 PASS — go test ./internal/protocol ./internal/daemon -run 'Test.*(Follow|Start|Remove)' -count=1 exited 0.
AC#3 PASS — go test ./internal/cli ./internal/mcp -run 'Test.*(Start|Run|Follow|Remove|Wait|LifecycleHelp)' -count=1 exited 0.
AC#4 PASS — go test ./integration -run 'Test(DurableFollowAcrossStopStart|FollowBeforeFirstLaunch|RunAttachesToRunning|WaitBeforeStart|RemoveSupervisionSession|UpWithDurableFollowers|FollowerExitsOnDaemonShutdown)' -count=1 exited 0.
AC#5 PASS — go test ./internal/cli ./internal/skill -run 'Test(LifecycleHelp|ResolvedProjectInstructions)' -count=1 exited 0; README.md, docs/design.md, and docs/coding-agents.md were reviewed and updated.
Full gate PASS — task ci exited 0, including gofmt, vet, staticcheck, all tests, race tests, build, and smoke.
Modified-file contract deviations: cmd/hum/integration_test.go was required to update the existing built-binary smoke test for intentionally unbounded attached/follow semantics. internal/process/ was required to publish the launch boundary atomically after spawn succeeds but before child output capture begins. The backlog task file is provider-owned execution metadata updated through the backlog CLI.
<!-- SECTION:NOTES:END -->
