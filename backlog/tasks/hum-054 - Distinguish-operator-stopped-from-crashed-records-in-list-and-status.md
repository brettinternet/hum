---
id: HUM-054
title: Distinguish operator-stopped from crashed records in list and status
status: To Do
assignee: []
created_date: '2026-09-06 17:07'
updated_date: '2026-09-06 17:08'
labels:
  - cli
  - mcp
  - daemon
  - protocol
milestone: m-4
dependencies: []
modified_files:
  - internal/app/app.go
  - internal/protocol/protocol.go
  - internal/daemon/wire_protocol.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/mcp/tools.go
  - docs/design.md
priority: medium
type: enhancement
ordinal: 31700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a record whose last incarnation ended through `stop`, `down`, `restart`, `remove`, or daemon shutdown reports `state: stopped` in list, status, CLI JSON, and MCP snapshots, while an incarnation that exited on its own (any exit code or signal) reports `state: exited` with its exit details. Human list and status render the two states differently so an operator stop is never mistaken for a crash.

Why now (2026-09-06 audit): since `hum list` started including completed records (so exited ad-hoc sessions are visible), a process stopped by `hum down` and a process that crashed both show `exited`; the daemon records only `exited` for both. The supervisor already knows which exits were operator-owned, because the relaunch policy skips them, so the distinction only needs to be recorded on the record and exposed in the snapshot.

Non-goals: persisting history, changing exit codes, changing relaunch policy.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/app -run '^TestOperatorStopReportsStopped$' -count=1 -v` exits 0 and prints PASS for stop, down-equivalent stop, restart, and a plain crash reporting exited.
- [ ] #2 `go test ./internal/cli -run '^TestListShowsCrashedManifestProcess$' -count=1 -v` exits 0 and prints PASS, and `rg -n dropStoppedDeclaredRecords internal/cli` prints nothing.
- [ ] #3 `go test ./integration -run '^TestDownWorkflow$' -count=1` exits 0.
- [ ] #4 `task ci` exits 0.
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
