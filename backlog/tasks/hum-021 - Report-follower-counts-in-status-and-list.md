---
id: HUM-021
title: Report follower counts in status and list
status: To Do
assignee: []
created_date: '2026-09-04 18:55'
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
- [ ] #1 `go test ./internal/app ./internal/protocol ./internal/daemon -run 'Test.*Follower(Count|s)' -count=1` exits 0 and proves the count rises on attach, falls on detach and on remove, includes pre-launch and stopped-session followers, survives incarnation exit and relaunch, and is zero for records with no session.
- [ ] #2 `go test ./internal/cli ./internal/mcp -run 'Test.*(Status|List).*Followers' -count=1` exits 0 and proves status and list --all human output show the count, JSON carries a `followers` integer, MCP status and list return the same field, and plain list output is unchanged when no record is followed.
- [ ] #3 `go test ./integration -run 'TestStatusReportsFollowers' -count=1` exits 0 and proves a two-shell flow: a follower opened before launch is counted, the count drops after Ctrl+C, and after remove the record and count are gone.
- [ ] #4 `go test ./internal/cli -run 'TestLifecycleHelp' -count=1` exits 0 and README.md, docs/design.md, and CLI help document the followers field as a read-only observation that does not gate remove.
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
