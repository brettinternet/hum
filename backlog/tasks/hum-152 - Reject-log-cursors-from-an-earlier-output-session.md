---
id: HUM-152
title: Reject log cursors from an earlier output session
status: To Do
assignee: []
created_date: '2026-10-16 17:12'
updated_date: '2026-10-16 17:13'
labels:
  - output
  - protocol
  - cli
  - mcp
  - events
  - contract
  - json
dependencies: []
references:
  - >-
    backlog/archive/drafts/draft-006 -
    Provide-bounded-service-snapshots-with-resumable-checkpoints-for-agents.md
modified_files:
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/output/types.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/protocol/event_history_test.go
  - internal/daemon/server.go
  - internal/daemon/event_history.go
  - internal/daemon/event_history_test.go
  - internal/daemon/server_test.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/wait_test.go
  - internal/cli/status_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/logs_test.go
  - docs/design.md
  - docs/cli-json-v1.md
  - docs/coding-agents.md
  - internal/skill/SKILL.md
  - plugins/hum/skills/hum/SKILL.md
priority: medium
type: bug
ordinal: 34000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: every response that hands out a log cursor also says which output session the cursor belongs to, and `logs`, `logs --follow`, and `wait` reject a cursor from a session that no longer exists with an explicit `cursor_stale` error instead of reading the wrong session. An agent resuming after `hum remove`, completed-record eviction, or daemon replacement learns its checkpoint is gone instead of silently missing output.

Why (split from DRAFT-006, now archived): log cursors count from 0 within one output session. A session ends on `hum remove`, completed-record eviction, or daemon replacement, and the next launch of that name starts again at cursor 0. A held cursor at or beyond the new session's next cursor gets `output_error` "output cursor N is in the future", which is misleading because the cursor is stale. A smaller one is accepted and silently skips the new session's first N+1 entries (internal/output/ring.go:107-160). Event history survives daemon replacement, so an event's `log_cursor` is the most exposed. The docs currently tell clients to compare entry and event times "when in doubt" (docs/cli-json-v1.md:142, docs/design.md:882, docs/coding-agents.md:173). Ordinary stop, start, restart, and relaunch keep the session (HUM-020) and are unaffected.

Decisions (owner-approved 2026-10-16; reviewer may overturn):
1. Identity: each output store gets an opaque random `session_id` when its record is created, at every `output.NewStore` site in internal/app/app.go (reclaimed unresolved records near :1845, first launch near :2016, pre-launch follow and wait sessions in `ensureSessionScoped` near :2942, and TTY preparation near :2997). It stays stable across incarnations of that record and is never reused. Share the existing random-ID helper (`daemon.NewOperationID`, internal/daemon/event_history.go:579) instead of adding another generator.
2. Exposure, additive within cli-json-v1 with no schema_version bump: `session_id` on process snapshots (named and aggregate status, list, and start/up/restart/run results), on bounded and streamed output results, and on wait results. Lifecycle `launch` and `exit` events carry `log_session_id` beside `log_cursor`, persisted in event history. Human output is unchanged except named `hum status NAME`, which prints a `session:` line.
3. Check: `logs` (bounded and follow) and `wait` accept the session with a cursor: CLI `--session-id ID`, valid only with `--after-cursor` (a usage error before daemon contact otherwise), and MCP `session_id`. When it differs from the name's current session, the daemon returns the new error code `cursor_stale`, naming the current session (if any) and suggesting a read without a cursor, and reads nothing. The comparison and the read use the same store reference, so a session replaced mid-request is never read with the old cursor.
4. MCP requires it: `logs` and `wait` require `session_id` whenever `after` is present (schema `dependentRequired` plus a handler check before daemon contact). The CLI flag stays optional so people can still page with `--after-cursor N` from human output; the docs tell scripts to always pass it.
5. Without a session, behavior is unchanged except the future-cursor message, which says the cursor is beyond the session's latest cursor and may belong to an earlier session.
6. Docs: replace the "compare time" advice with the session rule in docs/design.md ("Cursors and entries", "Event history"), docs/cli-json-v1.md, docs/coding-agents.md, and both bundled skills.

MCP budget (measured with hum 0.17.1 on 2026-10-16): tools/list is 38,569 of 40,000 bytes and 16,893 of 17,000 without output schemas (internal/mcp/tools_budget_test.go). Pay for the new schema text by shortening existing descriptions; do not raise either budget. HUM-150 draws on the same budget, so whichever lands second must re-measure.

Rejected alternatives: monotonic cursors across sessions with persisted reservations (no new input, but disk writes on the output hot path and new crash-recovery state); opaque string cursors that encode the session (changes the integer cursor type across cli-json-v1, MCP, and human output); a composed snapshot or checkpoint tool (DRAFT-006: status, events with `log_cursor`, logs, and `wait --after-cursor` already cover checkpoint, edit, and verify once cursors are session-safe).

Non-goals: keeping output across remove or daemon replacement; changing cursor numbering, retention, eviction, or event-history cursors; session checks for `input` (already launch-cursor checked) or `attach`; follow reconnection; a snapshot or checkpoint API.

Modified-file contract: the frontmatter list. Record a justification before touching anything else.

Testing: implement first; follow docs/development.md#tests. Regressions to catch: a stale session's cursor reading the replacement session (silent loss); the ID changing across restart or relaunch (false stale errors); `log_session_id` lost when event history reloads; MCP accepting `after` without `session_id`.

Next action: add the ID to `record` beside `store` (internal/app/app.go:1179; set at every `output.NewStore` site), expose it on `Process`, check it where `OutputScoped` and `WaitScoped` resolve the store (:2901, :3609), and map the error in the daemon `OpOutput`, follow, and wait dispatch (internal/daemon/server.go:986 and `errorCode`).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 mise exec go -- go test ./internal/app -run 'CursorSession' -count=1 -v exits 0 with named tests executed: a record keeps one session ID across stop/start, restart, and automatic relaunch; remove followed by start, and completed-record eviction followed by start, produce a new ID; output and wait reads with the current ID succeed; a stale ID returns the stale error without reading, including when the record is replaced between lookup and read.
- [ ] #2 mise exec go -- go test ./internal/protocol ./internal/daemon -run 'CursorSession' -count=1 -v exits 0 with named tests executed: process snapshots, output results, and wait responses round-trip session_id; launch and exit history events round-trip log_session_id and keep it after the history is reopened from disk; the stale error maps to wire code cursor_stale; a future cursor without a session gets the new message.
- [ ] #3 mise exec go -- go test ./internal/cli ./internal/mcp -run 'CursorSession|TestToolsListBudget' -count=1 -v exits 0 with named tests executed and budgets unchanged: --session-id without --after-cursor, or with several names, is a usage error before daemon contact; logs, logs --follow, and wait forward it; JSON renders session_id and the cursor_stale error code; named human status prints a session line; MCP logs and wait reject after without session_id before daemon contact and return session_id in structuredContent.
- [ ] #4 mise exec go -- go test ./integration -run '^TestLogCursorSession$' -count=1 -v exits 0 with the built binary: read logs --json from a detached process to get next and session_id; hum remove, then run the same name again; logs and wait with --after-cursor next --session-id OLD exit nonzero with cursor_stale; the current session_id succeeds; after hum shutdown --stop-processes and a fresh launch the earlier ID is stale too; events --json launch log_session_id equals the status session_id.
- [ ] #5 task cli:test exits 0 with no test skipped, deleted, or weakened.
- [ ] #6 rg -n -i 'compare (returned )?log' docs README.md internal/skill/SKILL.md plugins/hum/skills/hum/SKILL.md exits 1 (today it matches docs/design.md:883, docs/cli-json-v1.md:143, and docs/coding-agents.md:174); rg -n 'session[-_]id' docs/design.md docs/cli-json-v1.md docs/coding-agents.md internal/skill/SKILL.md plugins/hum/skills/hum/SKILL.md prints at least one line from each file; mise exec go -- go test ./internal/cli ./internal/mcp ./internal/skill -run 'TestHelpContract|TestDocs|TestSkill' -count=1 -v exits 0.
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
