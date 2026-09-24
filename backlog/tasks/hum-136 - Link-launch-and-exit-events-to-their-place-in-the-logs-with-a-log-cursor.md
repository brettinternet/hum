---
id: HUM-136
title: Link launch and exit events to their place in the logs with a log cursor
status: To Do
assignee: []
created_date: '2026-09-24 22:20'
labels:
  - events
  - output
  - json
  - mcp
  - contract
dependencies: []
modified_files:
  - internal/protocol/protocol.go
  - internal/app/app.go
  - internal/app/event_history_test.go
  - internal/daemon/server.go
  - internal/daemon/event_history_test.go
  - internal/cli/events.go
  - internal/cli/events_test.go
  - internal/mcp/tools.go
  - internal/mcp/events_test.go
  - integration/events_test.go
  - docs/cli-json-v1.md
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: enhancement
ordinal: 20000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `launch` and `exit` records from `hum events` (CLI JSON and MCP `events`) carry an optional `log_cursor`. Passing it to `hum logs NAME --after-cursor N` (MCP `logs` `after`) jumps from an event to the matching place in the retained output of that process, with no guessing by time.

Why: today `hum events` shows `15:48:06 api exit` but nothing links it to the log lines around it. Time filters (`--since`) are too coarse and can cut a burst of lines in half. Hum already uses cursors for exact log paging, so the event should hand back a cursor. This came out of a docs discussion comparing Hum with the time-based log search in pitchfork (2026-09-24).

Target usage (JSON trimmed):

```console
$ hum events api --json
{"schema_version":1,"type":"event","kind":"lifecycle","name":"api","event":"launch","log_cursor":780,...}
{"schema_version":1,"type":"event","kind":"lifecycle","name":"api","event":"exit","exit_code":1,"log_cursor":812,...}
$ hum logs api --after-cursor 780     # output of this incarnation, ending at cursor 812
$ hum logs api --after-cursor 812     # output after the exit (a later incarnation)
```

Meaning of `log_cursor` (same meaning as the logs `next` field: pass it as `--after-cursor` to read what came after):
- `launch`: the launch cursor of the process, which is the entry just before the output of this incarnation (usually the `NAME launched` system marker). Omit it when no entry exists at or before that point, because the output of the incarnation starts at the beginning of the session and the reader should not pass `--after-cursor`. Cursor 0 is a real entry, so never emit 0 to mean "none".
- `exit`: the newest output cursor when the exit is recorded, which covers all output of that incarnation. Omit it when the session has no output yet.
- All other lifecycle and operation events: omit it.

Where the code is (line numbers as of commit 7fb1721):
- `internal/protocol/protocol.go:493` `HistoryEvent`: add `LogCursor *Cursor` with the `json:"log_cursor,omitempty"` tag. It must be a pointer because 0 is a valid cursor. Records are appended to disk as JSON lines, so old records without the field keep decoding.
- `internal/app/app.go:1393` `LifecycleEvent`: add the same optional field. `emitLifecycle` (`app.go:1750`) builds the event.
- Launch is emitted at `app.go:2178` while `s.mu` is held. The launch cursor is the local `launchCursor` (set at `app.go:2099-2114`: `store.NextCursor()-1`, or the cursor of the marker when `markStarted` appends one). Record whether an entry at or before `launchCursor` exists: `NextCursor()` was non-zero before launch, or the marker append succeeded. Pass the cursor only in that case.
- Exit is emitted at `app.go:2601`, after `s.mu.Unlock()` at about `app.go:2593`. Read `rec.store.NextCursor()` there, before emitting. If it is non-zero, the cursor is `next-1`. All output of the incarnation is already in the store at that point: `internal/process/process.go:636-640` drains stdout and stderr before the result is delivered.
- `internal/daemon/server.go:748` `recordLifecycle` copies `LifecycleEvent` into `protocol.HistoryEvent`. Copy the new field too.
- CLI JSON: `internal/cli/events.go:117` `writeEventsJSON` builds a map by hand. Add `log_cursor` when it is set.
- CLI human: in `writeEventsHuman` (`events.go:146`), only in `--full` mode, append `log_cursor=N` to the detail line when it is set. Default (non-full) output stays unchanged.
- MCP: `internal/mcp/events.go` returns `protocol.HistoryEvent` values directly, but the output schema is closed. Add `"log_cursor": {"type":"integer","minimum":0}` to `eventRecord` in `internal/mcp/tools.go:475`, otherwise schema validation fails (see `internal/mcp/output_schema_test.go`).

Traps:
- Do NOT call any `output.Store` method from inside `emitLifecycle`. The `ready` event for `match` readiness is emitted from a store observer (`newReadinessTracker` at `app.go:827` registers it with `store.ObserveAppend`). Observers run while the store mutex is held (see the comment in `internal/output/store.go` `Append`), and every `Store` method takes that same non-reentrant mutex, so the process would deadlock. Compute cursors at the launch and exit call sites and pass them in.
- The explicit `restart` path (`app.go:2420-2495`) emits no `launch` lifecycle event today; the server records `explicit_restart` without a cursor. Leave that as is (see non-goals).

Documentation:
- `docs/cli-json-v1.md` "Event history records": document `log_cursor`, when it appears, and the `launch`/`exit` usage above.
- `docs/design.md` "Event history": the same, in one or two bullets, plus the caveat below.
- `docs/coding-agents.md` "Workflows": add a short "from a failed exit to its logs" example using `events` then `logs` with `after`.
- Caveat to document: log cursors are only meaningful against the same retained session. Output is kept in memory and is lost on `hum remove` and on daemon replacement, while event history survives both, and a new session restarts its cursors. Tell readers to compare the returned entry `time` with the event `time` when in doubt.

Non-goals:
- `--until`, absolute timestamps, or any new time-search flag on `logs` or `events`.
- A "through cursor" or "before cursor" flag on `logs`.
- Adding a `launch` event or cursor to the explicit `restart` path or to operation events (`explicit_restart`, `operator_stop`, `removal`, and so on).
- Session identifiers to detect stale cursors across `remove` or daemon replacement.
- Changes to the existing default human `hum events` output, to any other JSON field, or to retention.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/app -run "^TestLifecycleLogCursor$" -count=1 -v` exits 0. The new test, next to TestEventHistoryLifecycleKinds in internal/app/event_history_test.go and using the fake children there, asserts: the first launch of a fresh session has no LogCursor; the exit LogCursor equals the cursor of the last output entry; after a second Start of the same session, the launch LogCursor equals the cursor of the "NAME launched" marker, and a store Read after that cursor returns only second-incarnation entries; an exit with no output at all has no LogCursor.
- [ ] #2 AC2 — `go test ./internal/daemon -run "^(TestEventHistoryLifecycleOperationAttributionAndReadIsolation|TestEventHistoryLogCursorRoundTrip)$" -count=1 -v` exits 0. recordLifecycle copies LogCursor into the queued HistoryEvent. The new round-trip test appends events with LogCursor 0, with 812, and without it, reopens the history, and reads back 0, 812, and nil.
- [ ] #3 AC3 — `go test ./internal/cli -run "^TestEvents" -count=1 -v` and `go test ./internal/mcp -run "^(TestEvents|TestOutputSchemasAcceptStructuredContent)$" -count=1 -v` both exit 0. New assertions: CLI JSON includes log_cursor when set (including 0) and leaves it out otherwise; `hum events --full` prints log_cursor=N on the detail line; default human output is byte-for-byte unchanged; an MCP events result containing log_cursor passes the output-schema check.
- [ ] #4 AC4 — `go test ./integration -run "^TestEventsLogCursor$" -count=1 -v` exits 0. Against the built binary: `hum run NAME --detach -- /bin/sh -c "echo one; echo two; exit 3"`, wait for the exit, then `hum start NAME` and wait for the second exit. In `hum events NAME --json`, the first launch has no log_cursor; each exit log_cursor equals the cursor of the last entry that `hum logs NAME --json` returns for that incarnation; and `hum logs NAME --after-cursor <second launch log_cursor> --json` returns only the second incarnation lines "one" and "two".
- [ ] #5 AC5 — `go test ./internal/cli -run "^(TestDocs|TestREADME)" -count=1` exits 0, and `rg -n log_cursor docs/cli-json-v1.md docs/design.md docs/coding-agents.md` prints at least one line from each file. The design.md text includes the caveat that cursors do not carry across `hum remove` or daemon replacement.
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
