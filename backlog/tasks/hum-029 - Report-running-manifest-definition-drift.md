---
id: HUM-029
title: Report manifest/runtime drift during up
status: To Do
assignee: []
created_date: '2026-09-06 04:57'
updated_date: '2026-09-06 05:06'
labels:
  - cli
  - mcp
  - config
milestone: m-3
dependencies:
  - HUM-026
  - HUM-030
modified_files:
  - internal/app/
  - internal/protocol/
  - internal/daemon/
  - internal/cli/manifest.go
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/manifest_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/manifest_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
  - internal/skill/SKILL.md
  - internal/skill/after_docs_test.go
  - plugins/hum/skills/hum/SKILL.md
priority: high
type: bug
ordinal: 6700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: repeated CLI and MCP reconciliation distinguishes current resolved definitions from both changed retained launch specifications and manifest-sourced sessions whose declaration was removed. Operators get an explicit safe action instead of a misleading `already_running`.

Why now: `up` is becoming the project reconciliation command, but matching only the source label lets an outdated argv, cwd, readiness matcher, TTY mode, or restart policy appear current and lets stale readiness satisfy an `after` gate. Branch switches can also leave removed declarations running or eligible to relaunch without any notice.

Scope, changed definitions: for a declared running, pending-recovery, or exhausted record, compare effective argv, canonical cwd, readiness-match presence and value, TTY mode, and normalized restart policy. The response-safe process snapshot retains the readiness matcher for terminal recovery records without exposing environment. A mismatch returns `definition_drift`, sorted `changed_fields`, and `hum restart NAME` guidance; CLI exits 1. CLI and MCP `start` use the same drift classification for the requested resolved name. A drifted result never satisfies an `after` gate.

Scope, removed declarations: CLI and MCP `up` also report each running, pending-recovery, or exhausted manifest-sourced project record absent from current declarations as `removed_definition`, including its runtime state and `hum stop NAME` or `hum remove NAME` guidance. These warnings are lexical, do not change aggregate exit status, and never include ad-hoc or conventionally discovered records.

Scope, classification order: an absent manifest declaration is `removed_definition`; a present but mismatched record is `definition_drift`; a matching recovery record uses HUM-030 `recovery_pending` or `recovery_exhausted`; and a matching running record remains `already_running`. Human, CLI NDJSON, and MCP results carry equivalent outcomes and fields.

Docs: CLI help, README.md, docs/design.md, docs/coding-agents.md, and both bundled agent skills document that only restart applies a changed definition and that removed records require an explicit stop or remove.

Non-goals: automatically restarting, stopping, removing, or otherwise mutating a session; comparing the client environment or readiness timeout, because environment is response-private and timeout is an invocation-local wait limit; reporting ordinary stopped records, ad-hoc sessions, or conventionally discovered sessions as removed declarations; changing explicit restart semantics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/cli -run "^TestUpReportsManifestRuntimeDrift$" -count=1 -v` exits 0 and prints `--- PASS: TestUpReportsManifestRuntimeDrift`. It proves matching definitions remain `already_running`; each supported field change returns exit 1 with `definition_drift`, sorted `changed_fields`, and restart guidance; and CLI start uses the same classification.
- [ ] #2 AC2 — `go test ./internal/mcp -run "^TestUpReportsManifestRuntimeDrift$" -count=1 -v` exits 0 and prints `--- PASS: TestUpReportsManifestRuntimeDrift`. It proves equivalent MCP up/start outcomes and fields for running, pending-recovery, and exhausted records, including readiness-match drift retained after exit.
- [ ] #3 AC3 — `go test ./internal/cli ./internal/mcp -run "^TestUpRejectsDriftedReadinessGate$" -count=1 -v` exits 0 and prints a named PASS line for both packages. It proves readiness from an old incarnation cannot satisfy the current declaration or launch its dependent.
- [ ] #4 AC4 — `go test ./internal/cli ./internal/mcp -run "^TestUpReportsRemovedManifestSessions$" -count=1 -v` exits 0 and prints a named PASS line for both packages. It proves running, pending, and exhausted removed manifest records are lexical and actionable, while current declarations, ordinary stopped records, ad-hoc records, and discovery records are excluded and aggregate exit status is unchanged.
- [ ] #5 AC5 — `go test ./integration -run "^TestUpReportsManifestRuntimeDrift$" -count=1 -v` exits 0 and prints `--- PASS: TestUpReportsManifestRuntimeDrift`. Against the built binary and real daemon, it edits and removes declarations, proves stable human/NDJSON results and stale-gate rejection, and proves no existing process is mutated.
- [ ] #6 AC6 — `go test ./internal/cli ./internal/skill -run "^TestUpDriftDocs$" -count=1 -v` exits 0 and prints a named PASS line for both packages. It proves every declared documentation surface states the outcomes, exit behavior, comparison boundary, and explicit operator actions.
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
- [ ] T1 — Retain a response-safe readiness matcher and compare every observable effective definition field.
- [ ] T2 — Classify changed and removed records consistently in CLI and MCP without mutating runtime state.
- [ ] T3 — Prevent drifted readiness from satisfying dependency gates and prove behavior against the real daemon.
- [ ] T4 — Document restart, stop, and remove guidance for humans and coding agents.
<!-- SECTION:PLAN:END -->
