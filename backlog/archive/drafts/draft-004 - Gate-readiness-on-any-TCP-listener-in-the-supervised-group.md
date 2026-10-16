---
id: DRAFT-004
title: Gate readiness on any TCP listener in the supervised group
status: Draft
assignee: []
created_date: '2026-10-10 02:13'
updated_date: '2026-10-16 17:12'
labels:
  - readiness
  - process
  - contract
dependencies:
  - HUM-148
priority: low
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a declaration whose port is not known in advance (port 0, framework fallback to the next free port, launcher-chosen ports) can gate `after`, `up`, and launch commands on "the owned group is listening on TCP", without `ready.tcp: HOST:PORT` or a log regex.

Idea: a new readiness method such as `ready: {listen: true}` (exact name TBD) that polls the HUM-148 group TCP-listener inspection at the readiness interval during startup only, and reports the observed endpoints in the readiness snapshot. It is a startup gate in domain per decision-001 item 2, not liveness monitoring.

Open questions before refinement: method name and whether it may filter by port/address; behavior when inspection is unavailable/denied (likely fail readiness with a diagnostic, never silently ready); polling cost of lsof on macOS; manifest schema, MCP schema, and drift classification impact.

Non-goals: allocation, $PORT injection, URL inference, post-ready monitoring.

Depends on HUM-148 shipping the platform inspection and its unavailable/partial semantics.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Archived 2026-10-16 (owner decision). The decision-001 revision of 2026-10-10 keeps readiness changes out of scope for port observation. "Any listener" is a weak readiness signal: `node --inspect` (9229), Vite's HMR socket, and dev servers that listen before their first build all open a listener early. When the port is unknown in advance, dependents cannot address it anyway, because Hum injects no ports. `ready.match` and `ready.exec` already cover these cases, and polling lsof every readiness interval is costly on macOS.
<!-- SECTION:NOTES:END -->
