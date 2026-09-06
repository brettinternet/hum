---
id: HUM-033
title: Keep up inert for an empty manifest
status: To Do
assignee: []
created_date: '2026-09-06 04:57'
updated_date: '2026-09-06 05:08'
labels:
  - cli
  - daemon
milestone: m-3
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/manifest_test.go
  - docs/design.md
priority: low
type: bug
ordinal: 10700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: CLI `hum up` against an authoritative `hum.yaml` with `processes: {}` succeeds without creating, connecting to, replacing, or shutting down a daemon.

Why now: an empty manifest is a valid explicit declaration that the project currently has no managed processes. Starting an idle supervisor is a surprising side effect and differs from MCP `up`, which already returns an empty result before daemon contact.

Scope: after normal argument, context, and timeout validation, return before daemon configuration or contact when manifest resolution yields zero definitions. Human stdout is exactly `No processes are declared in hum.yaml.` and stderr is empty. `--json` writes an empty NDJSON stream (zero bytes), matching other empty CLI result sets. No runtime socket, PID, startup lock, ready marker, or log path is created. MCP keeps returning `[]` without daemon contact.

Docs: docs/design.md records the empty-manifest human and NDJSON contract and the no-daemon guarantee.

Non-goals: changing behavior for an absent manifest, conventional discovery, any non-empty manifest, invalid arguments or timeout values, or MCP’s existing empty result.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/cli -run "^TestUpEmptyManifest$" -count=1 -v` exits 0 and prints `--- PASS: TestUpEmptyManifest`. It proves the exact human stdout, empty stderr, and absence of daemon socket, PID, startup lock, ready marker, and log artifacts.
- [ ] #2 AC2 — `go test ./internal/cli -run "^TestUpEmptyManifestJSON$" -count=1 -v` exits 0 and prints `--- PASS: TestUpEmptyManifestJSON`. It proves stdout and stderr are both zero bytes and no daemon or runtime artifacts are created.
- [ ] #3 AC3 — `go test ./internal/cli -run "^TestUpNonEmptyManifestStartsDaemon$" -count=1 -v` exits 0 and prints `--- PASS: TestUpNonEmptyManifestStartsDaemon`. It proves one non-empty declaration still starts the daemon and process.
- [ ] #4 AC4 — `go test ./internal/cli -run "^TestUpEmptyManifestValidatesInput$" -count=1 -v` exits 0 and prints `--- PASS: TestUpEmptyManifestValidatesInput`. It proves arguments, cancellation, and invalid timeout values still fail before the inert return.
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
- [ ] T1 — Short-circuit an empty resolved up after input validation and before daemon setup.
- [ ] T2 — Prove no runtime artifacts, preserve non-empty behavior, and document the output contract.
<!-- SECTION:PLAN:END -->
