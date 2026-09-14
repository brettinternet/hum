---
id: HUM-111
title: 'Add durable, navigable service event history'
status: To Do
assignee:
  - '@brett'
created_date: '2026-09-14 22:52'
updated_date: '2026-09-14 23:35'
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

Add `hum events [NAME...]` as a project-scoped, bounded event-history command. Reading history never requires a manifest and never starts a missing daemon; retained history must remain readable after shutdown. With no names it shows the recent timeline across the current project; positional names narrow it to one or more services. Global scope follows existing `--global` selection rules. A scope with no retained events prints a concise human empty state and exits 0. Unknown names are not an error: a name with no events simply contributes nothing.

The timeline has two explicit event kinds:

- Lifecycle: launch, readiness transition or startup failure, exit with code or signal, operator stop, explicit restart, automatic relaunch scheduling/attempt/failure/exhaustion, and removal. Events survive removal of the process record and later reuse of the same name.
- Operation: Hum control operations that alter or interact with a service (`run`, `start`, `up`, `down`, `restart`, `stop`, `remove`, `signal`, and `input`), including origin (`cli` or `mcp`), target name(s), outcome, and a generated `operation_id`. Lifecycle events caused directly by an operation carry the same `operation_id`; automatic relaunch events carry none. Actor/session metadata is optional, not a new identity integration requirement; if exposed, accept it only explicitly with fixed documented byte limits; never infer identity or retain environment values, input payloads, child output, or credentials. Read-only operations (`list`, `status`, `logs`, `wait`, `doctor`, `version`, and MCP reads including `events`) are never recorded.

Ordering and cursors: events are totally ordered within a scope by a monotonically increasing unsigned 64-bit cursor starting at 1 assigned at append time, plus a wall-clock timestamp. Clients treat the cursor as opaque; it is never reused within a scope and daemon replacement continues the sequence. Persist the cursor high-water mark independently of the evictable event payload. If that mark is unreadable, report history unavailable rather than silently reusing cursors; service control still works. Runtime-directory cleanup ends this guarantee. Project and global scopes never leak events into each other.

Durability and retention: store events under the private runtime directory of the serving daemon (`HUM_RUNTIME_DIR`/`XDG_RUNTIME_DIR` rules), one history per scope. Retention is fixed at 2,000 events or 1 MiB of encoded events per scope, whichever is reached first, with deterministic oldest-first eviction; count UTF-8 encoded event bytes including framing/newlines, excluding fixed cursor metadata. Bound each event to 16 KiB by truncating optional detail before persistence; never retain argv, raw errors that contain payloads, or HTTP query values as detail. No configuration knob. Writes are crash-safe (append plus fsync or atomic replace; a torn tail is discarded while complete preceding events remain readable). Durability spans daemon restart and replacement; it does not promise survival of runtime-directory cleanup such as reboot. Malformed event payload is reported once per scope per daemon lifetime as a bounded diagnostic and treated as empty history, retaining the independent cursor high-water mark and without preventing service control. A failed write is diagnosed and never acknowledged as durable; do not roll back an otherwise successful process operation. Update the design statement of runtime-directory contents accordingly.

## Navigation and human output

Selection flags compose and validate before daemon contact with clear usage errors: `--since DURATION`, repeatable `--kind lifecycle|operation`, `--failed` (startup failures, non-zero or signal exits, relaunch failure/exhaustion, and operations whose outcome is not success), `--match REGEX` (applied to NAME and DETAIL), `--tail N` (default 50, range 1..2000), and `--after-cursor CURSOR`.

Paging: without `--after-cursor` the page is the newest N matching events rendered oldest-first. With `--after-cursor` the page is the oldest N matching events strictly after the cursor, rendered oldest-first, so repeated calls with the returned `next_cursor` neither lose nor duplicate matching events. When the requested cursor predates an evicted or discarded event range, the response marks `truncated: true` and resumes from the oldest retained event. Use one immutable snapshot per read. `next_cursor` is the last returned cursor when another matching page exists, otherwise the snapshot high-water mark (0 for a never-used history); an after-cursor beyond that mark is a usage error. An empty filtered page still advances to the snapshot high-water mark. `has_more` means another matching event exists after the last returned cursor in that snapshot. For the default newest-window view it is false: older omitted matches are not forward pages. If the byte limit reduces that newest window, retain its newest suffix; forward after-cursor pages instead retain their oldest prefix. Keep filters fixed across pages; a relative --since is evaluated at each request and is a moving time window, not a snapshot token. Help and completion lead with the no-argument view and common narrowing examples.

Default human output is a compact TIME, NAME, EVENT, DETAIL timeline. Keep fixed columns short and constrain every rendered line to the detected terminal width; when width is unavailable use 80 columns. Elide DETAIL first, then other columns only when they cannot fit (including long names or very narrow terminals); preserve distinguishing suffixes where practical, and provide `--full` for unabridged multi-line details. Apply existing TTY/`TERM`/`NO_COLOR` policy: color only semantic lifecycle or outcome words, never names or user-controlled detail, and do not change content when ANSI is stripped.

`--json` emits schema-versioned NDJSON, one complete event per line in cursor order, followed by one metadata record carrying `next_cursor`, `truncated`, and `has_more`, under the CLI v1 compatibility rules in docs/cli-json-v1.md. JSON is never terminal-width truncated and exposes structured fields (`cursor`, `time`, `kind`, `name`, `event`, `operation_id`, origin/outcome/exit fields as applicable) rather than requiring consumers to parse DETAIL. Discriminate event and metadata records with `type: event|metadata`. Add a bounded MCP `events` read with equivalent selection, filtering, cursor, retention, and structured event semantics; MCP remains independently versioned and exposes no unbounded follow operation.

## Non-goals

Capturing arbitrary agent-harness tool calls; a remote telemetry or tracing system; unbounded history or follow streams; environment, command output, input payload, or secret capture; public daemon protocol access; user callbacks/hooks; cross-machine identity; configurable retention or query languages; survival of runtime-directory cleanup; or replacing per-process `hum logs` output.

## Implementation boundaries

Use one operation_id per admitted CLI command or MCP control request, shared across its per-target operation records and directly caused lifecycle events. For bulk operations record per-target outcomes under that ID, including partial failure; do not duplicate an operation for internal start/stop subrequests. Pre-validation failures before a scope is resolved are not persisted, and a daemon crash may leave a lifecycle event without a terminal operation outcome: this is useful history, not an audit or exactly-once guarantee. Automatic relaunches remain unattributed.

Reuse private runtime path/permission and scope-keying rules in internal/daemon/runtime.go. Query response pages must also respect the existing protocol byte limit, returning fewer whole events with has_more rather than an oversized response. Validate names and regex input under existing request bounds; MCP and CLI use the same limits.

Next action: define table-driven event, recovery, attribution, and paging cases before threading the shared contract through the supervisor and adapters. Every focused test command below must print RUN/PASS for its named new tests; a no-tests-to-run package or a documentation keyword match alone is not evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `mise exec go -- go test ./internal/app ./internal/daemon ./internal/protocol -run "^TestEventHistory" -count=1 -v` exits 0 and prints RUN/PASS for tests proving: lifecycle events for every listed transition; control-operation events with origin, outcome, and `operation_id` shared by directly caused lifecycle events; monotonic cursors continued across daemon replacement; retention at 2,000 events or 1 MiB with oldest-first eviction; events retained after record removal and name reuse; project/global scope isolation; a cursor preceding an evicted/discarded range yields `truncated` and resumes at the oldest retained event, while cursor 0 on a never-truncated history does not; a torn tail preserves the complete prefix, malformed event payload becomes empty with one diagnostic per scope per daemon lifetime, an intact high-water mark prevents cursor reuse, corrupt cursor metadata makes history unavailable, and write failures are diagnosed while control operations still succeed; read-only operations recorded nothing; stored bytes contain no environment values or input payload. Cases also cover 16 KiB event bounds, encoded-byte accounting, bulk partial outcomes, read-without-daemon behavior, and oversized/secret-bearing errors without payload persistence.
- [ ] #2 AC2 — `mise exec go -- go test ./internal/cli -run "^TestEvents(NoArgs|EmptyState|Names|Filters|Paging|Help|Completion|Width|Color)" -count=1 -v` exits 0 and prints RUN/PASS for tests proving the no-argument recent window, exit-0 empty state, positional names, composed `--since/--kind/--failed/--match/--tail/--after-cursor` semantics with usage errors before daemon contact, lossless `next_cursor` paging, help/completion leading with the no-argument view, compact versus `--full` output, every line at or under the detected width and the 80-column fallback with long names and details, and existing TTY/`TERM`/`NO_COLOR` color policy coloring only semantic words. Include empty filtered pages, a future cursor, max-tail rejection, byte-limited pages, and long-name/narrow-terminal fallback cases; compact output must not contain user-controlled terminal escapes.
- [ ] #3 AC3 — `mise exec go -- go test ./internal/cli ./cmd/hum ./integration -run "^TestEventsJSON|^TestCLIMachineOutputV1Contract$|^TestBuiltCLIMachineOutputV1$" -count=1 -v` exits 0 and prints RUN/PASS proving `hum events --json` emits cursor-ordered `schema_version: 1` NDJSON with structured lifecycle/operation fields and `operation_id`, one trailing metadata record with `next_cursor`, `truncated`, and `has_more`, no width truncation, and unchanged exit-code and JSON error behavior, and the integration test observes events across a real daemon restart.
- [ ] #4 AC4 — `mise exec go -- go test ./internal/mcp -run "^TestEvents" -count=1 -v` exits 0 and prints RUN/PASS proving the bounded MCP `events` tool matches CLI selection, filter, cursor, and truncation semantics, that MCP-origin control operations are recorded with origin `mcp` while MCP reads including `events` record nothing, and that the tool list exposes no follow or unbounded history operation. Include tail above 2000, protocol-byte-bound pages, and malformed filter requests rejected before side effects.
- [ ] #5 AC5 — `for doc in README.md docs/coding-agents.md docs/design.md internal/skill/SKILL.md plugins/hum/skills/hum/SKILL.md; do rg -n "hum events" "$doc" || exit 1; done; rg -n "Event history|next_cursor|truncated|has_more" docs/cli-json-v1.md && rg -n "supersedes|HUM-111" docs/design.md && ! rg -n "persistent process history" docs/design.md` exits 0. Review the displayed sections against the product contract: no-argument usage, narrowing, width/color fallback, privacy, fixed retention, cursor-corruption limits, runtime cleanup, CLI record discriminators, and bounded MCP semantics must be accurate; child output logs are not described as persistent.
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
2. Daemon: add a per-scope durable event history under the runtime directory with fixed 2,000-event/1 MiB retention, crash-safe append, torn-tail recovery preserving complete events, malformed payload recovery to empty history plus one diagnostic per scope/daemon lifetime while retaining cursor high-water metadata, and cursor continuation across replacement; append lifecycle events from the supervisor and operation events from control handlers, threading `operation_id` and origin through the request path.
3. App/CLI: add `hum events [NAME...]` with flag validation before daemon contact, oldest-first paging, compact width-bounded TIME/NAME/EVENT/DETAIL rendering with `--full`, existing color policy, NDJSON output with trailing metadata, help/completion/man updates, and the v1 contract table entry.
4. MCP: add the bounded `events` tool, tag MCP-origin control operations, and assert reads record nothing.
5. Docs: README, coding-agents, skill files, cli-json-v1 event family, design (runtime-directory contents, non-goal removal, HUM-094 supersession note).
6. Run AC1-AC6, obtain an independent verifier PASS, record evidence, and commit.
<!-- SECTION:PLAN:END -->
