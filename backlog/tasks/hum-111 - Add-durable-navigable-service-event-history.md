---
id: HUM-111
title: 'Add durable, navigable service event history'
status: To Do
assignee:
  - '@brett'
created_date: '2026-09-14 22:52'
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
modified_files:
  - internal/app/
  - internal/daemon/
  - internal/protocol/
  - internal/cli/
  - internal/mcp/
  - internal/output/
  - cmd/hum/
  - integration/
  - README.md
  - docs/cli-json-v1.md
  - docs/coding-agents.md
  - docs/design.md
  - internal/skill/SKILL.md
  - plugins/hum/skills/hum/SKILL.md
priority: medium
type: feature
ordinal: 83800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: operators and coding agents can answer what happened to services in the current Hum scope, when it happened, and which Hum control operation caused it, even after the daemon restarts. The default experience is a useful recent timeline rather than an empty usage error.

## Product contract

Add `hum events [NAME...]` as a project-scoped, bounded event-history command. With no names it shows the recent timeline across the current project; positional names narrow it to one or more services. Global scope follows existing `--global` selection rules. A scope with no retained events prints a concise human empty state and succeeds.

The timeline has two explicit event kinds:

- Lifecycle: launch, readiness transition or startup failure, exit with code or signal, operator stop, explicit restart, automatic relaunch scheduling/attempt/failure/exhaustion, and removal. Events survive removal of the process record.
- Operation: Hum control operations that can alter or interact with a service (`start`, `up`, `down`, `restart`, `stop`, `remove`, `signal`, and `input`), including origin (`cli` or `mcp`), target, outcome, and a generated correlation ID tying the operation to resulting lifecycle events. Record bounded actor/session metadata only when a caller explicitly supplies it; never infer identity or retain environment values, input payloads, child output, or credentials. Read-only polling operations are excluded to avoid recursive/noisy history.

Events are totally ordered within a scope by an opaque cursor and wall-clock timestamp. Store them durably under the private Hum runtime boundary with fixed byte/entry retention, crash-safe writes, and deterministic oldest-first eviction. Daemon replacement preserves retained events and cursor ordering. A cursor older than retention produces explicit truncation metadata and resumes at the oldest retained event; malformed durable state fails safely without preventing service control. Project and global scopes never leak events into each other.

## Navigation and human output

The no-argument view returns a bounded recent window in chronological order. Users can combine positional names with `--since DURATION`, repeatable `--kind lifecycle|operation`, `--failed`, `--match REGEX`, `--tail N`, and `--after-cursor CURSOR`. Filters compose, have clear validation errors, and preserve cursor semantics so the next page neither loses nor duplicates matching events. Help and completion lead with the no-argument view and common narrowing examples.

Default human output is a compact TIME, NAME, EVENT, DETAIL timeline. Keep fixed columns short and constrain every rendered line to the detected terminal width; when width is unavailable use a conservative bounded fallback. Elide only DETAIL, preserve the distinguishing suffix where practical, and provide `--full` for unabridged multi-line details. Apply existing TTY/`TERM`/`NO_COLOR` policy: color only semantic lifecycle or outcome words, never names or user-controlled detail, and do not change content when ANSI is stripped.

`--json` emits schema-versioned NDJSON, one complete event per line, in cursor order, followed by bounded pagination/truncation metadata using the established CLI v1 compatibility rules. JSON is never terminal-width truncated and exposes structured fields rather than requiring consumers to parse DETAIL. Add a bounded MCP `events` read with equivalent selection, filtering, cursor, retention, and structured event semantics; MCP remains independently versioned and exposes no unbounded follow operation.

## Non-goals

Capturing arbitrary agent-harness tool calls; a remote telemetry or tracing system; unbounded history or follow streams; environment, command output, input payload, or secret capture; public daemon protocol access; user callbacks/hooks; cross-machine identity; configurable query languages; or replacing per-process `hum logs` output.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `mise exec go -- go test ./internal/app ./internal/daemon ./internal/protocol -run "Test.*Event" -count=1` exits 0 and proves lifecycle and control-operation events are correlated, totally ordered, bounded, durably recovered across daemon replacement, retained after process removal, isolated by scope, and safely handle truncation and malformed durable state without exposing prohibited payload data.
- [ ] #2 AC2 — `mise exec go -- go test ./internal/cli -run "Test.*Events.*(NoArgs|Filter|Width|Color|Help)" -count=1` exits 0 and proves no-argument, empty-state, positional-name, composable narrowing, pagination, completion/help, compact/full, terminal-width, and existing color-policy behavior, including lines constrained at 80 columns with long names and details.
- [ ] #3 AC3 — `mise exec go -- go test ./internal/cli ./integration -run "Test.*Events.*JSON" -count=1` exits 0 and proves `hum events --json` emits cursor-ordered schema-versioned NDJSON with structured lifecycle/operation fields, correlation IDs, and explicit page/truncation metadata while preserving exit-code and JSON error behavior and never width-truncating values.
- [ ] #4 AC4 — `mise exec go -- go test ./internal/mcp ./internal/cli -run "Test.*Events.*MCP" -count=1` exits 0 and proves the bounded MCP events tool matches CLI selection/filter/cursor semantics, identifies MCP-origin control operations without recording read-only polling, and offers no follow or arbitrary-history operation.
- [ ] #5 AC5 — `rg -n "hum events|Lifecycle events|Operation events|after-cursor|NO_COLOR|persistent process history" README.md docs/cli-json-v1.md docs/coding-agents.md docs/design.md internal/skill/SKILL.md plugins/hum/skills/hum/SKILL.md` exits 0 and the matched documentation covers discoverable no-argument usage, narrowing examples, width/color behavior, durable bounded retention and privacy boundaries, CLI JSON/MCP contracts, and removes persistent event history from the documented non-goals without claiming that child output logs are persistent.
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
