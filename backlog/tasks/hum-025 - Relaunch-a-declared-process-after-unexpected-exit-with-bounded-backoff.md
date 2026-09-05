---
id: HUM-025
title: Relaunch a declared process after unexpected exit with bounded backoff
status: To Do
assignee: []
created_date: '2026-09-05 15:14'
labels:
  - config
  - process
  - daemon
  - protocol
  - cli
  - docs
milestone: m-3
dependencies: []
modified_files:
  - internal/project/
  - internal/app/
  - internal/protocol/
  - internal/daemon/
  - internal/cli/
  - internal/mcp/
  - internal/skill/
  - internal/testutil/
  - integration/
  - plugins/hum/skills/hum/SKILL.md
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: feature
ordinal: 3600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a declared process can opt into automatic relaunch after an unexpected exit, so a dev server that crashes on a bad save comes back without a human or agent noticing first, while every exit, backoff, and relaunch stays visible in status, list, followers, and MCP. The default stays as today: an exited process remains stopped until someone runs `start`.

Why now: the README promises hum "keeps project processes alive between commands", but today an incarnation that dies stays dead. Agents editing code trip crash-on-reload servers constantly; without relaunch each trip costs a `status` round-trip and a `start`. Relaunch must never hide the failure, because the exit output is exactly what the agent needs to read.

Scope, manifest: `restart` is an optional per-process string field in `hum.yaml` with values `never` (default) and `on-failure`; any other value, or a non-string, is an error with file and entry context. Discovered zero-config definitions and ad hoc `run` sessions are always `never`. `init` templates include a commented `restart: on-failure` line.

Scope, policy: `on-failure` applies to every incarnation launched by `start`, `up`, `restart`, or a prior relaunch. When such an incarnation exits with a non-zero code or is killed by a signal that hum did not send, the daemon schedules a relaunch after a backoff of 1s, 2s, 4s, 8s, 16s, then 30s for later attempts, using the record's retained launch specification (argv, cwd, environment, readiness, tty) exactly as `start` on a retained record does. Exit code 0 never relaunches. `stop`, `down`, `restart`, `remove`, and `shutdown --stop-processes` never trigger a relaunch, and any of them during a pending backoff cancels it; `restart` then launches immediately as today. The consecutive-failure counter resets once an incarnation has run for 30 seconds. After 5 consecutive failures without such a run the daemon gives up and the record stays stopped with its last exit code. A readiness timeout is not a failure and does not relaunch. Relaunch continues the session's single cursor sequence and resets readiness at the new launch cursor, as any relaunch does.

Scope, visibility: process snapshots gain required `restart` (`never`|`on-failure`), integer `relaunches` (consecutive automatic relaunches so far, reset with the counter), and optional `next_launch_at` (RFC 3339, present only while a backoff is pending). Human `status` prints `restart: on-failure` and, while pending, `relaunching in 4s (attempt 2/5)`; when exhausted it prints `restart: on-failure (gave up after 5 failures)`. Human `list` adds `restart=on-failure` only for opted-in records. Attached `run` and `logs --follow` print an existing-style boundary line `relaunching in 4s (attempt 2/5)` at exit and the existing launch boundary at relaunch. `wait` continues to report `exited` on an incarnation exit; a caller wanting the successor uses `wait` again with no explicit cursor, which already waits for the next incarnation. `start` and `up` early-exit results are unchanged: they report exit code 3 for the observed incarnation while the daemon keeps relaunching. MCP `status` and `list` carry the same three fields.

Scope, protocol and docs: bump the private protocol version; launch specifications and retained definitions carry `restart`. docs/design.md removes "automatic crash restart/backoff" from Non-goals and "restart policy" from the manifest exclusions, and documents the policy above. README.md, docs/coding-agents.md, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` tell agents that a relaunching process still exposes the failing incarnation's output via bounded `logs`, and to read it before editing again.

Non-goals: relaunch for ad hoc or discovered definitions; a CLI or MCP flag to set the policy; `always` or `on-success` policies; configurable backoff or attempt limits; relaunch of explicitly stopped processes; relaunch across daemon replacement; health probes; changing stop, grace, or signal semantics; Windows.

Modified-file contract: internal/project/, internal/app/, internal/protocol/, internal/daemon/, internal/cli/, internal/mcp/, internal/skill/, internal/testutil/, integration/, plugins/hum/skills/hum/SKILL.md, README.md, docs/design.md, docs/coding-agents.md. No go.mod or go.sum change.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/project -run "^TestRestartPolicyManifest$" -count=1 -v` exits 0 and prints `--- PASS: TestRestartPolicyManifest`. It proves `restart` accepts `never` and `on-failure`, defaults to `never` when absent, rejects other strings and non-strings with file and entry context, is always `never` for discovered definitions, and appears as a commented line in the `init` template.
- [ ] #2 AC2 — `go test ./internal/app -run "^TestRelaunchOnFailure$" -count=1 -v` exits 0 and prints `--- PASS: TestRelaunchOnFailure`. It proves a non-zero exit or foreign signal schedules a relaunch with backoff 1s, 2s, 4s, 8s, 16s, 30s using the retained launch specification; exit 0 never relaunches; `never` never relaunches; stop, down, restart, remove, and shutdown cancel a pending backoff and do not trigger one; the counter resets after an incarnation runs 30s; the daemon gives up after 5 consecutive failures leaving the last exit code; readiness timeout does not relaunch; each relaunch continues the cursor sequence and resets readiness at the new launch cursor; and snapshots carry `restart`, `relaunches`, and `next_launch_at` with the documented presence rules. Timing uses an injected clock.
- [ ] #3 AC3 — `go test ./internal/protocol -run "^TestRestartPolicyProtocol$" -count=1 -v`, `go test ./internal/cli -run "^TestRestartPolicyCLI$" -count=1 -v`, and `go test ./internal/mcp -run "^TestRestartPolicyMCP$" -count=1 -v` all exit 0 and print the corresponding named PASS line. They prove the bumped protocol carries `restart` on launch specifications and retained definitions and the three snapshot fields; human `status` prints `restart: on-failure`, the pending `relaunching in Ns (attempt K/5)` line, and the gave-up form; human `list` adds `restart=on-failure` only for opted-in records; `--json` and MCP `status`/`list` include the fields; followers receive the relaunching boundary line; and `start`/`up` early-exit results are unchanged.
- [ ] #4 AC4 — `go test ./integration -run "^TestRelaunchAfterCrash$" -count=1 -v` exits 0 and prints `--- PASS: TestRelaunchAfterCrash`. With the built binary and a fixture that exits 1 on its first two launches and then stays up, it proves `hum up` returns early exit for the first incarnation, `hum status` shows a pending relaunch, the third incarnation reaches `ready`, `hum logs` still contains the first incarnation output, `relaunches` reads 2, `hum stop` during a pending backoff leaves the process stopped, and a `restart: never` process stays stopped after exiting 1.
- [ ] #5 AC5 — `go test ./internal/cli ./internal/skill -run "^TestRestartPolicyDocs$" -count=1 -v` exits 0 and prints both named PASS lines. README.md, docs/design.md, docs/coding-agents.md, CLI help, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` document the `restart` field, backoff schedule, attempt limit and reset rule, which commands cancel relaunch, the three snapshot fields, follower boundary lines, and the guidance to read the failing incarnation output before editing again; docs/design.md no longer lists crash restart or restart policy as a non-goal or manifest exclusion.
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
