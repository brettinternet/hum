---
id: HUM-045
title: Default bounded logs to the newest window and unify cursor naming
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 16:17'
labels:
  - cli
  - mcp
  - docs
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/mcp/tools.go
  - internal/protocol/protocol.go
  - docs/design.md
  - README.md
priority: medium
type: enhancement
ordinal: 22700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum logs NAME` with no selector returns the newest bounded window (equivalent to `--tail` with the default entry limit), matching `docker logs --tail`, `pm2 logs`, and `journalctl -e` expectations, while `--after-cursor` keeps forward paging from the oldest retained entry. When a tail read is truncated by the byte or entry limit, the newest entries are kept and the older ones dropped (today the ring keeps the oldest entries of the tail window and sets More, the paging-friendly but surprising direction pinned by TestTail* in internal/output/ring_test.go). The two cursor conventions (`logs` `next` = last cursor returned; `status` `next_cursor` = latest+1) are documented side by side in docs/design.md or reconciled to one name.

Why now (observed 2026-09-06): on a 300k-line process `hum logs NAME` returned cursors 0-99, the oldest lines, so the crash at the end of a log is exactly what the default hides; agents following the README guidance to read retained output before editing see stale startup noise instead. `hum logs NAME --tail 200` of long lines still returns the oldest lines that fit the byte budget.

Non-goals: changing follow semantics, changing MCP `logs` field names.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestLogsDefaultNewestWindow$' -count=1 -v` exits 0 and prints PASS: with 300 retained lines and no selector the returned entries end at the newest line.
- [ ] #2 `rg -n 'next_cursor' docs/design.md` matches an explanation of both cursor names or shows a single unified name used by logs and status.
- [ ] #3 `task ci` exits 0.
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
