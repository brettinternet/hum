---
id: HUM-020
title: Keep named supervision sessions attached across process incarnations
status: To Do
assignee: []
created_date: '2026-09-04 17:00'
updated_date: '2026-09-04 17:01'
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
Outcome: hum treats a project-scoped process name as a durable supervision session and each child launch as one incarnation, so stopping a task does not tear down terminals following that name. A later start, run, restart, or up launch resumes output in those terminals without hooks, task orchestration, or manual reattachment.

Scope: attached `hum run <name>` and `hum logs <name> --follow` follow the named session rather than only its current output store. They emit a visible lifecycle boundary when an incarnation exits, remain idle while the name is stopped, and resume at the next launch until the client interrupts, the session is explicitly removed, or the daemon/transport ends. This is the default behavior; no keep-open flag is added. Client interruption cleanly detaches that observer and does not implicitly stop the child or remove the session. Non-following logs and `wait` remain bounded, incarnation-scoped observations.

`hum logs <name> --follow` may be invoked before the name has ever launched. It validates the project and name, ensures a daemon exists, reports that it is waiting for the first launch, and then streams that launch without polling. A live pre-launch or stopped-session follower reserves the name-scoped session from completed-record eviction. Unobserved completed sessions remain subject to the existing bounded retention policy.

Broaden `hum start <name>` to process-manager semantics: a running record is already running; a retained stopped record is relaunched; and an absent record must resolve from hum.yaml or conventional discovery. Retained ad hoc records reuse their exact recorded argv, cwd, and environment. Resolved records use the current definition and requesting-client environment. `restart` remains stop-then-start for an already known record.

Add an explicit `hum remove <name>...` lifecycle operation, with CLI and MCP parity, that stops any running incarnation, closes all followers for that named session, clears its retained launch specification and output, and removes only runtime state—never hum.yaml. `stop` and `down` stop children but preserve live sessions and followers. Do not overload force or kill terminology: immediate SIGKILL policy is separate from session removal.

`up` remains a one-shot operation that ensures current resolved definitions are running and waits for readiness; it does not attach or adopt unrelated ad hoc sessions. Launches caused by `up` become visible to existing name followers. Readiness resets per incarnation as today. Status/list continue to distinguish running and stopped runtime records.

Non-goals: task-runner hooks, arbitrary prerequisite commands, dependency ordering, PTYs or stdin forwarding, persistent state across daemon restart, changing readiness or wait semantics, making plain bounded `logs` start a daemon, keeping unobserved completed records forever, or using `stop --force` to control observers.

Modified-file contract: internal/app/, internal/output/, internal/protocol/, internal/daemon/, internal/cli/, internal/mcp/, internal/skill/, integration/, README.md, docs/design.md, docs/coding-agents.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/app ./internal/output -run 'Test.*(Session|Follow|StartStopped|Remove)' -count=1` exits 0 and proves a name follower survives ordinary stop and natural exit, observes ordered exit/wait/launch boundaries and successor output, cannot miss a rapid relaunch, protects its session from eviction, detaches independently on cancellation, and is closed by session removal or daemon shutdown.
- [ ] #2 `go test ./internal/protocol ./internal/daemon -run 'Test.*(Follow|Start|Remove)' -count=1` exits 0 and proves the versioned protocol supports a race-free name-scoped follow before, during, and between incarnations; ordinary start/restart/up launches wake followers; stop/down do not close them; remove does; and transport cancellation releases every subscription.
- [ ] #3 `go test ./internal/cli ./internal/mcp -run 'Test.*(Start|Follow|Remove|LifecycleHelp)' -count=1` exits 0 and proves start is idempotent for running names, relaunches retained stopped resolved and ad hoc records with the specified launch-source/environment rules, absent names still require resolution, remove has CLI/MCP parity and never edits hum.yaml, follow starts a daemon only when requested, and no keep-open, force, or hook interface is introduced.
- [ ] #4 `go test ./integration -run 'Test(DurableFollowAcrossStopStart|FollowBeforeFirstLaunch|RemoveSupervisionSession|UpWithDurableFollowers)' -count=1` exits 0 and proves two-shell workflows end to end: attached run and logs followers remain open across stop/start and down/up, a follower opened before any process receives a later run, Ctrl+C detaches without stopping the child, remove closes followers and discards retained ad hoc launch state, and up remains one-shot/readiness-oriented.
- [ ] #5 `go test ./internal/cli ./internal/skill -run 'Test(LifecycleHelp|ResolvedProjectInstructions)' -count=1` exits 0 and README.md, docs/design.md, docs/coding-agents.md, CLI help, and the bundled skill consistently document durable default following, future-name following, start of retained stopped records, stop versus remove, Ctrl+C detachment, bounded eviction, daemon-loss behavior, and unchanged up/wait semantics.
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
