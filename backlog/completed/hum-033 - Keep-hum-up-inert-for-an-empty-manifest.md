---
id: HUM-033
title: Keep up inert for an empty manifest
status: Done
assignee: []
created_date: '2026-09-06 04:57'
updated_date: '2026-09-06 07:03'
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
- [x] #1 AC1 — `go test ./internal/cli -run "^TestUpEmptyManifest$" -count=1 -v` exits 0 and prints `--- PASS: TestUpEmptyManifest`. It proves the exact human stdout, empty stderr, and absence of daemon socket, PID, startup lock, ready marker, and log artifacts.
- [x] #2 AC2 — `go test ./internal/cli -run "^TestUpEmptyManifestJSON$" -count=1 -v` exits 0 and prints `--- PASS: TestUpEmptyManifestJSON`. It proves stdout and stderr are both zero bytes and no daemon or runtime artifacts are created.
- [x] #3 AC3 — `go test ./internal/cli -run "^TestUpNonEmptyManifestStartsDaemon$" -count=1 -v` exits 0 and prints `--- PASS: TestUpNonEmptyManifestStartsDaemon`. It proves one non-empty declaration still starts the daemon and process.
- [x] #4 AC4 — `go test ./internal/cli -run "^TestUpEmptyManifestValidatesInput$" -count=1 -v` exits 0 and prints `--- PASS: TestUpEmptyManifestValidatesInput`. It proves arguments, cancellation, and invalid timeout values still fail before the inert return.
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
- [x] T1 — Short-circuit an empty resolved up after input validation and before daemon setup.
- [x] T2 — Prove no runtime artifacts, preserve non-empty behavior, and document the output contract.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation started on main; selected by task backlog:next.

AC#1 — PASS: `go test ./internal/cli -run "^TestUpEmptyManifest$" -count=1 -v` exited 0 and printed `--- PASS: TestUpEmptyManifest`; exact human output and all runtime artifacts were verified.
AC#2 — PASS: `go test ./internal/cli -run "^TestUpEmptyManifestJSON$" -count=1 -v` exited 0 and printed `--- PASS: TestUpEmptyManifestJSON`; both streams were empty and all runtime artifacts were absent.
AC#3 — PASS: `go test ./internal/cli -run "^TestUpNonEmptyManifestStartsDaemon$" -count=1 -v` exited 0 and printed `--- PASS: TestUpNonEmptyManifestStartsDaemon`; the daemon and declared process started.
AC#4 — PASS: `go test ./internal/cli -run "^TestUpEmptyManifestValidatesInput$" -count=1 -v` exited 0 and printed `--- PASS: TestUpEmptyManifestValidatesInput`; arguments, cancellation, malformed, non-positive, and sub-millisecond timeouts failed before the inert return.
Verifier — PASS for AC1–AC4; confirmed declared-file scope, no deleted or weakened tests, and no protected gate changes.
Gate — PASS: `task ci` completed gofmt, vet, staticcheck, all tests, race tests, build, and smoke test.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented an early empty-manifest return for `hum up` after argument, context, dependency-option, and timeout validation but before daemon configuration or contact. Human mode prints the documented message, JSON mode emits zero bytes, non-empty manifests retain automatic daemon startup, and focused regression coverage proves output, artifact absence, validation, and non-empty behavior.
<!-- SECTION:FINAL_SUMMARY:END -->
