---
id: HUM-021
title: Report follower counts in status and list
status: Done
assignee: []
created_date: '2026-09-04 18:55'
updated_date: '2026-09-04 20:45'
labels:
  - cli
  - daemon
  - protocol
  - mcp
  - docs
dependencies:
  - HUM-020
priority: medium
type: feature
ordinal: 2300
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: every runtime record reports how many live followers are attached to its supervision session, so an agent or human can see that someone is watching a name before running `remove`, and can confirm a pre-launch follower is actually waiting.

Scope: the daemon counts live followers per session, including pre-launch and stopped-session followers, and exposes the count on the existing status/list snapshot. `hum status <name>` and `hum list --all` show it in human output and as a `followers` integer in JSON. MCP `status` and `list` return the same field. The count reflects only followers the daemon currently holds open: attached `run` and `logs --follow` clients. It is a read-only observation; nothing changes behavior based on it, and `remove` does not prompt, warn, or refuse. Records with no daemon or no session report zero.

Non-goals: identifying who the followers are, follower PIDs or client metadata, changing `remove`, `stop`, or `down` semantics, gating any operation on the count, a plain `list` column when nothing is followed, or persisting counts.

Modified-file contract: internal/app/, internal/protocol/, internal/daemon/, internal/cli/, internal/mcp/, integration/, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/app ./internal/protocol ./internal/daemon -run 'Test.*Follower(Count|s)' -count=1` exits 0 and proves the count rises on attach, falls on detach and on remove, includes pre-launch and stopped-session followers, survives incarnation exit and relaunch, and is zero for records with no session.
- [x] #2 `go test ./internal/cli ./internal/mcp -run 'Test.*(Status|List).*Followers' -count=1` exits 0 and proves status and list --all human output show the count, JSON carries a `followers` integer, MCP status and list return the same field, and plain list output is unchanged when no record is followed.
- [x] #3 `go test ./integration -run 'TestStatusReportsFollowers' -count=1` exits 0 and proves a two-shell flow: a follower opened before launch is counted, the count drops after Ctrl+C, and after remove the record and count are gone.
- [x] #4 `go test ./internal/cli -run 'TestLifecycleHelp' -count=1` exits 0 and README.md, docs/design.md, and CLI help document the followers field as a read-only observation that does not gate remove.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add an app-level follower subscription wrapper that counts only durable follow clients, carries the count in immutable snapshots, and proves attach/detach, pre-launch, stopped, relaunch, removal, and zero-session behavior.
2. Propagate followers through protocol and daemon wire conversions with round-trip and daemon coverage.
3. Render followers in status and list JSON/human output, preserve plain list output when all counts are zero, and expose the field through MCP schemas/results.
4. Add the two-shell integration test and document the read-only, non-gating observation in CLI help, README, and design docs.
5. Run each acceptance command, task ci, independent verification, then merge and clean the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed for implementation in an isolated worktree.

Implemented in commits 3b5a87d, 3f4aa5e, and 0967548 and fast-forwarded to main.
AC#1 PASS — `go test ./internal/app ./internal/protocol ./internal/daemon -run 'Test.*Follower(Count|s)' -count=1` exited 0.
AC#2 PASS — `go test ./internal/cli ./internal/mcp -run 'Test.*(Status|List).*Followers' -count=1` exited 0.
AC#3 PASS — `go test ./integration -run 'TestStatusReportsFollowers' -count=1` exited 0.
AC#4 PASS — `go test ./internal/cli -run 'TestLifecycleHelp' -count=1` exited 0; README.md and docs/design.md were inspected and updated.
Final gate PASS — `task ci` exited 0 on rebased final commit 0967548.
Independent verifier PASS — all four acceptance criteria passed; no acceptance-blocking defect found.
Scope review PASS — all 18 changed files are within the modified-file contract; no protected gate files changed and no tests were deleted, skipped, or weakened.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Reported live attached run and logs --follow counts through runtime snapshots, daemon protocol, CLI human/JSON output, and MCP. Added lifecycle and integration coverage plus read-only/non-gating documentation. All acceptance commands, task ci, and independent verification passed.
<!-- SECTION:FINAL_SUMMARY:END -->
