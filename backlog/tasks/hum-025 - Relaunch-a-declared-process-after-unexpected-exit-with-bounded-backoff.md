---
id: HUM-025
title: Relaunch a declared process after unexpected exit with bounded backoff
status: To Do
assignee: []
created_date: '2026-09-05 15:14'
updated_date: '2026-09-05 15:30'
labels:
  - config
  - process
  - daemon
  - protocol
  - cli
  - docs
milestone: m-3
dependencies:
  - HUM-022
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
Outcome: a declared process can opt into automatic relaunch after an unexpected exit, so a dev server that crashes on a bad save comes back without a human or agent noticing first, while every failure, backoff, relaunch, and final exhaustion remains visible in retained logs, status, list, durable followers, JSON, and MCP. The default stays `never`: an exited process remains stopped until an operator starts it.

Why now: the README promises hum keeps project processes alive between commands, but an incarnation that dies currently stays dead. Agents editing code routinely trip crash-on-reload servers; an opt-in bounded policy removes that recovery round-trip without hiding the failure output they need to diagnose.

Scope, manifest: `restart` is an optional per-process string in `hum.yaml`, accepting only `never` (default) and `on-failure`; other strings and non-strings fail with file and entry context. Discovered definitions and ad hoc sessions are always `never`. `init` templates include an inert commented `restart: on-failure` example.

Scope, failure and retry policy: `on-failure` applies to every incarnation launched from that declared session. An exit is unexpected when it has a non-zero code or signal and no explicit control intent owns the termination at the exit's linearization point. Exit zero resets crash-loop state and stays stopped. An initial unexpected exit schedules automatic relaunch attempt 1 after 1s. At most five automatic attempts occur in one crash loop, delayed 1s, 2s, 4s, 8s, and 16s respectively. If an automatically launched child exits unexpectedly before surviving 30 seconds, the completed attempt is consumed and the next is scheduled. If an automatic spawn fails, that attempt is likewise consumed, the prior child exit remains the retained exit status, a bounded system entry `relaunch failed: ...` records the error, and the next attempt is scheduled or the loop is exhausted. Failure of attempt 5 appends one `gave up after 5 relaunch attempts` system boundary and leaves the record stopped. Thus one loop can contain the initial failed child plus at most five failed relaunched children.

Scope, race linearization: child exit, timer claim, and operator intent are serialized per record under the supervisor lock and guarded by a monotonically changing relaunch generation/token. `stop`, `down`, `restart`, `remove`, and shutdown register control intent and invalidate pending timers before sending a signal. An exit linearized after that intent is expected regardless of its code/signal and never schedules; an exit linearized first may schedule, but a later control operation cancels it. A timer callback may launch only after it wins the same lock and verifies the record identity, generation, pending state, and open supervisor. If explicit `start`, `up`, or `restart` wins first, it invalidates the timer, resets crash-loop state, and launches immediately. If the timer claims first, existing per-record launch serialization applies: `start`/`up` observe the automatic launch as in progress or running, while `restart` uses its existing stop-then-start semantics; no race creates two live children. Stale callbacks are no-ops.

Scope, reset and operator control: an incarnation that stays alive for 30 seconds resets the crash-loop counter to zero; its next unexpected exit begins a fresh loop at attempt 1. Explicit `start`, `up`, or `restart` while backoff is still pending cancels it and launches as above. Explicit `stop`, `down`, `remove`, or any successful daemon shutdown cancels pending work and resets it without relaunch. Hum-originated TERM/KILL never triggers policy. Readiness state and client-side readiness timeout do not affect restart policy; only child exit or automatic spawn failure does. Pending timers cannot launch after cancellation, record removal, or daemon close.

Scope, launch specification: an automatic attempt uses the failed incarnation's last effective argv, cwd, environment, readiness, tty setting, and output store. It does not re-read `hum.yaml` or capture a newer client environment; an explicit `start`, `up`, or `restart` is required to adopt definition/environment changes. Every successful automatic launch continues the durable session cursor sequence, resets readiness at a new launch cursor, and increments the existing `restart_count` as a same-session relaunch.

Scope, retention and observers: a record with pending backoff is not eligible for completed-record eviction. Bounded logs preserve every failed incarnation's child output and system boundaries. `wait` still reports `exited` for the observed incarnation immediately; a caller can wait again for a successor. Attached `run` and `logs --follow` preserve HUM-020 durable-session behavior: they receive the ordinary exit/wait boundary followed by `relaunching in Ns (attempt K/5)`, remain attached while backoff is pending or exhausted, and resume on either an automatic or later explicit launch. Stop/down cancellation emits no duplicate exit; start/up/restart emits the ordinary successor launch boundary. Exhaustion and final spawn failure emit the gave-up boundary exactly once but do not close followers. Only `remove` or daemon/transport shutdown closes them. `start` and `up` keep their existing early-exit result for the observed incarnation even while the daemon schedules a successor.

Scope, visibility: process snapshots gain required `restart` (`never`|`on-failure`), integer `relaunches` (successful or failed automatic launch attempts completed in the current loop, 0..5, excluding manual restarts), and optional `next_launch_at` (RFC 3339 at whole-second precision, present only during pending backoff). While pending, the next attempt is `relaunches+1`. Exhaustion is represented by `restart=on-failure`, a terminal unexpected exit, `relaunches=5`, and no `next_launch_at`; every other terminal/reset path clears `relaunches`, making that state unambiguous. Human `status` prints `restart: on-failure`, `relaunching in Ns (attempt K/5)` while pending (remaining duration rounded up to a whole second, minimum zero), and `restart: on-failure (gave up after 5 relaunch attempts)` when exhausted. Human `list` adds `restart=on-failure` only for opted-in records. CLI JSON and MCP `status`/`list` carry the same snapshot fields.

Scope, protocol and docs: bump the private protocol version once. Restart policy is carried on start and definition-updating restart requests; the three visibility fields are carried on process/list/get responses and synthetic stopped manifest snapshots. README.md, docs/design.md, docs/coding-agents.md, CLI help, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` document the policy and tell agents to read the failing incarnation's retained output before editing again. docs/design.md removes restart policy and automatic crash restart/backoff from the corresponding exclusions.

Non-goals: relaunch for ad hoc or discovered definitions; CLI/MCP flags to set policy; `always` or `on-success`; configurable delays, stability window, or attempt limit; relaunch across daemon replacement; readiness/health failure as a restart trigger; port, HTTP, or command health checks; changing HUM-020 follower lifetime; changing stop/grace/signal behavior; Windows.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/project -run "^TestRestartPolicyManifest$" -count=1 -v` exits 0 and prints `--- PASS: TestRestartPolicyManifest`. It proves `restart` accepts `never` and `on-failure`, defaults to `never`, rejects other strings and non-strings with file/entry context, remains `never` for discovered definitions, and appears as an inert commented line in generated `init` templates without making those manifests invalid.
- [ ] #2 AC2 — `go test ./internal/app -run "^TestRelaunchOnFailure$" -count=1 -v` exits 0 and prints `--- PASS: TestRelaunchOnFailure`. With injected clock/launcher and race barriers it proves unexpected non-zero and foreign-signal exits schedule at most five attempts after 1s/2s/4s/8s/16s; child exit before 30s and spawn failure consume attempts; attempt-5 failure exhausts with one boundary; exit zero and `never` stay stopped; surviving 30s resets; explicit controls cancel/reset according to which exit/intent/timer linearizes first; generation checks make stale timers no-ops; a timer racing start/up/restart creates at most one child under existing launch semantics; readiness/timeouts do not trigger policy; automatic launches reuse the last effective spec, continue cursors, reset readiness, and increment `restart_count`; snapshots obey the visibility contract; and pending records resist completed-record eviction.
- [ ] #3 AC3 — `go test ./internal/protocol -run "^TestRestartPolicyProtocol$" -count=1 -v`, `go test ./internal/cli -run "^TestRestartPolicyCLI$" -count=1 -v`, and `go test ./internal/mcp -run "^TestRestartPolicyMCP$" -count=1 -v` all exit 0 and print the corresponding named PASS line. They prove one protocol-version bump; policy propagation on start/restart requests and process snapshots; synthetic stopped, discovered, and ad hoc values; exact status/list pending and exhausted forms with ceiling countdown; stable JSON/MCP fields; existing start/up early-exit behavior; and durable followers receive exactly one ordinary exit/wait boundary plus the applicable relaunching, launch, or gave-up boundaries, remain open across pending/exhausted/operator-stop states, resume after a later launch, and close only on remove or daemon/transport shutdown.
- [ ] #4 AC4 — `go test ./integration -run "^TestRelaunchAfterCrash$" -count=1 -v` exits 0 and prints `--- PASS: TestRelaunchAfterCrash`. With the built binary and a declared fixture that exits 1 on its first two launches and then stays up, it proves `hum up` returns exit 3 for the first incarnation, status observes pending relaunch, the third incarnation reaches ready, bounded logs retain both failures and boundaries, `relaunches` is 2 before stability reset, and followers remain attached. Race cases prove stop-before-exit prevents scheduling, exit-before-stop schedules then cancels, start-before-timer launches explicitly, timer-before-start yields one idempotent launch, exhaustion leaves a follower waiting and a later explicit start resumes it, and `restart: never` stays stopped after exit 1.
- [ ] #5 AC5 — `go test ./internal/cli ./internal/skill -run "^TestRestartPolicyDocs$" -count=1 -v` exits 0 and prints both named PASS lines. README.md, docs/design.md, docs/coding-agents.md, CLI help, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` document manifest validation, five-attempt schedule and 30-second reset, spawn failures, race linearization and operator override, last-effective-spec behavior, snapshot fields, retention/durable-follower semantics, and guidance to inspect failing output; docs/design.md no longer lists restart policy or crash restart/backoff as exclusions.
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
- [ ] T1 — Add strict manifest policy propagation and stable protocol/snapshot fields.
- [ ] T2 — Add a generation-guarded, cancellation-safe crash-loop scheduler with retained-spec relaunch, eviction protection, and deterministic clock/launcher/race tests.
- [ ] T3 — Integrate operator linearization, durable follower boundaries, CLI/MCP rendering, and existing restart-count semantics.
- [ ] T4 — Update operator/agent documentation and prove recovery, cancellation, race, exhaustion, and later resume with the built binary.
<!-- SECTION:PLAN:END -->
