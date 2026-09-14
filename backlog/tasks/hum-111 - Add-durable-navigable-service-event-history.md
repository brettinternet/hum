---
id: HUM-111
title: 'Add durable, navigable service event history'
status: To Do
assignee:
  - '@brett'
created_date: '2026-09-14 22:52'
updated_date: '2026-09-14 23:04'
labels:
  - cli
  - daemon
  - events
  - json
  - mcp
  - contract
  - output
milestone: m-5
dependencies: []
references:
  - HUM-094
  - HUM-093
  - docs/design.md
  - docs/cli-json-v1.md
  - docs/coding-agents.md
modified_files:
  - internal/protocol/
  - internal/daemon/
  - internal/app/
  - internal/cli/
  - internal/mcp/
  - internal/skill/
  - cmd/hum/
  - integration/
  - README.md
  - docs/cli-json-v1.md
  - docs/coding-agents.md
  - docs/design.md
  - plugins/hum/skills/hum/SKILL.md
priority: medium
type: feature
ordinal: 83800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: operators and coding agents can answer what happened to services in the current Hum scope, when it happened, and which Hum control operation caused it, even after the daemon restarts. The default experience is a useful recent timeline rather than an empty usage error.

This supersedes the `Decision: defer` recorded by HUM-094 for a different reason than that spike evaluated: the requirement is a durable, queryable history for operators and agents, not a live UI feed. docs/design.md must record that distinction under the existing decision section and keep a live stream, callbacks, and public socket access rejected.

## Product contract

Add `hum events [NAME...]` as a project-scoped, bounded event-history command. With no names it shows the recent timeline across the current project; positional names narrow it to one or more services. Global scope follows existing `--global` selection rules. A scope with no retained events prints a concise human empty state and exits 0. Unknown names are not an error: a name with no events simply contributes nothing.

The timeline has two explicit event kinds:

- Lifecycle: launch, readiness transition or startup failure, exit with code or signal, operator stop, explicit restart, automatic relaunch scheduling/attempt/failure/exhaustion, and removal. Events survive removal of the process record and later reuse of the same name.
- Operation: Hum control operations that alter or interact with a service (`run`, `start`, `up`, `down`, `restart`, `stop`, `remove`, `signal`, and `input`), including origin (`cli` or `mcp`), target name(s), outcome, and a generated `operation_id`. Lifecycle events caused directly by an operation carry the same `operation_id`; automatic relaunch events carry none. Record bounded actor/session metadata only when a caller explicitly supplies it; never infer identity or retain environment values, input payloads, signal-free child output, or credentials. Read-only operations (`list`, `status`, `logs`, `wait`, `doctor`, `version`, and MCP reads including `events`) are never recorded.

Ordering and cursors: events are totally ordered within a scope by a monotonically increasing unsigned 64-bit cursor assigned at append time, plus a wall-clock timestamp. Clients treat the cursor as opaque; it is never reused within a scope and daemon replacement continues the sequence. Project and global scopes never leak events into each other.

Durability and retention: store events under the private runtime directory of the serving daemon (`HUM_RUNTIME_DIR`/`XDG_RUNTIME_DIR` rules), one history per scope. Retention is fixed at 2,000 events or 1 MiB of encoded events per scope, whichever is reached first, with deterministic oldest-first eviction; no configuration knob. Writes are crash-safe (append plus fsync or atomic replace; a torn tail is discarded, not fatal). Durability spans daemon restart and replacement; it does not promise survival of runtime-directory cleanup such as reboot. Malformed durable state is reported once as a bounded daemon diagnostic and treated as empty history without preventing service control. Update the design statement of runtime-directory contents accordingly.

## Navigation and human output

Selection flags compose and validate before daemon contact with clear usage errors: `--since DURATION`, repeatable `--kind lifecycle|operation`, `--failed` (startup failures, non-zero or signal exits, relaunch failure/exhaustion, and operations whose outcome is not success), `--match REGEX` (applied to NAME and DETAIL), `--tail N` (default 50, must be positive), and `--after-cursor CURSOR`.

Paging: without `--after-cursor` the page is the newest N matching events rendered oldest-first. With `--after-cursor` the page is the oldest N matching events strictly after the cursor, rendered oldest-first, so repeated calls with the returned `next_cursor` neither lose nor duplicate matching events. When the requested cursor is older than the oldest retained event, the response marks `truncated: true` and resumes from the oldest retained event. Help and completion lead with the no-argument view and common narrowing examples.

Default human output is a compact TIME, NAME, EVENT, DETAIL timeline. Keep fixed columns short and constrain every rendered line to the detected terminal width; when width is unavailable use 80 columns. Elide only DETAIL, preserve the distinguishing suffix where practical, and provide `--full` for unabridged multi-line details. Apply existing TTY/`TERM`/`NO_COLOR` policy: color only semantic lifecycle or outcome words, never names or user-controlled detail, and do not change content when ANSI is stripped.

`--json` emits schema-versioned NDJSON, one complete event per line in cursor order, followed by one metadata record carrying `next_cursor`, `truncated`, and `has_more`, under the CLI v1 compatibility rules in docs/cli-json-v1.md. JSON is never terminal-width truncated and exposes structured fields (`cursor`, `time`, `kind`, `name`, `event`, `operation_id`, origin/outcome/exit fields as applicable) rather than requiring consumers to parse DETAIL. Add a bounded MCP `events` read with equivalent selection, filtering, cursor, retention, and structured event semantics; MCP remains independently versioned and exposes no unbounded follow operation.

## Non-goals

Capturing arbitrary agent-harness tool calls; a remote telemetry or tracing system; unbounded history or follow streams; environment, command output, input payload, or secret capture; public daemon protocol access; user callbacks/hooks; cross-machine identity; configurable retention or query languages; survival of runtime-directory cleanup; or replacing per-process `hum logs` output.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `mise exec go -- go test ./internal/app ./internal/daemon ./internal/protocol -run "^TestEventHistory" -count=1 -v` exits 0 and prints RUN/PASS for tests proving: lifecycle events for every listed transition; control-operation events with origin, outcome, and `operation_id` shared by directly caused lifecycle events; monotonic cursors continued across daemon replacement; retention at 2,000 events or 1 MiB with oldest-first eviction; events retained after record removal and name reuse; project/global scope isolation; too-old cursor yields `truncated` and resumes at the oldest retained event; a torn tail and malformed file each degrade to empty history with one diagnostic while control operations still succeed; read-only operations recorded nothing; stored bytes contain no environment values or input payload.
- [ ] #2 AC2 — `mise exec go -- go test ./internal/cli -run "^TestEvents(NoArgs|EmptyState|Names|Filters|Paging|Help|Completion|Width|Color)" -count=1 -v` exits 0 and prints RUN/PASS for tests proving the no-argument recent window, exit-0 empty state, positional names, composed `--since/--kind/--failed/--match/--tail/--after-cursor` semantics with usage errors before daemon contact, lossless `next_cursor` paging, help/completion leading with the no-argument view, compact versus `--full` output, every line at or under the detected width and the 80-column fallback with long names and details, and existing TTY/`TERM`/`NO_COLOR` color policy coloring only semantic words.
- [ ] #3 AC3 — `mise exec go -- go test ./internal/cli ./cmd/hum ./integration -run "^TestEventsJSON|^TestCLIMachineOutputV1Contract$|^TestBuiltCLIMachineOutputV1$" -count=1 -v` exits 0 and prints RUN/PASS proving `hum events --json` emits cursor-ordered `schema_version: 1` NDJSON with structured lifecycle/operation fields and `operation_id`, one trailing metadata record with `next_cursor`, `truncated`, and `has_more`, no width truncation, and unchanged exit-code and JSON error behavior, and the integration test observes events across a real daemon restart.
- [ ] #4 AC4 — `mise exec go -- go test ./internal/mcp -run "^TestEvents" -count=1 -v` exits 0 and prints RUN/PASS proving the bounded MCP `events` tool matches CLI selection, filter, cursor, and truncation semantics, that MCP-origin control operations are recorded with origin `mcp` while MCP reads including `events` record nothing, and that the tool list exposes no follow or unbounded history operation.
- [ ] #5 AC5 — `rg -n "hum events" README.md docs/coding-agents.md docs/design.md internal/skill/SKILL.md plugins/hum/skills/hum/SKILL.md && rg -n "Event history|next_cursor|truncated|has_more" docs/cli-json-v1.md && rg -n "supersedes|HUM-111" docs/design.md && ! rg -n "persistent process history" docs/design.md` exits 0, and the matched documentation covers no-argument usage, narrowing examples, width/color behavior, fixed retention and privacy boundaries, the runtime-directory contents, the CLI JSON v1 event record family, the MCP contract, and the documented reason the HUM-094 deferral no longer applies, without claiming that child output logs are persistent.
- [ ] #6 AC6 — `task cli:check && task test` exits 0.
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
1. Protocol: add event kinds, the event record, and an `events` read request/response (scope, names, since, kinds, failed, match, tail, after-cursor; entries plus next_cursor, truncated, has_more) with the existing wire-protocol round-trip tests.
2. Daemon: add a per-scope durable event history under the runtime directory with fixed 2,000-event/1 MiB retention, crash-safe append, torn-tail/malformed recovery to empty history plus one diagnostic, and cursor continuation across replacement; append lifecycle events from the supervisor and operation events from control handlers, threading `operation_id` and origin through the request path.
3. App/CLI: add `hum events [NAME...]` with flag validation before daemon contact, oldest-first paging, compact width-bounded TIME/NAME/EVENT/DETAIL rendering with `--full`, existing color policy, NDJSON output with trailing metadata, help/completion/man updates, and the v1 contract table entry.
4. MCP: add the bounded `events` tool, tag MCP-origin control operations, and assert reads record nothing.
5. Docs: README, coding-agents, skill files, cli-json-v1 event family, design (runtime-directory contents, non-goal removal, HUM-094 supersession note).
6. Run AC1-AC6, obtain an independent verifier PASS, record evidence, and commit.
<!-- SECTION:PLAN:END -->
