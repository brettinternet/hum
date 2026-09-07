---
id: HUM-049
title: Emit structured errors on stdout in JSON mode
status: Done
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-07 04:11'
labels:
  - cli
milestone: m-4
dependencies: []
modified_files:
  - cmd/hum/main.go
  - cmd/hum/main_test.go
  - internal/cli/root.go
  - internal/cli/root_test.go
  - internal/cli/render.go
  - internal/cli/config.go
  - internal/cli/json_errors_test.go
  - internal/cli/list_logs_test.go
  - internal/cli/manifest_test.go
  - docs/design.md
priority: medium
type: enhancement
ordinal: 26700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: JSON-capable CLI failures use a stable {"error":{"code":"...","message":"..."}} shape on stdout, never a Hum diagnostic on stderr, and preserve exit codes. Before any output, failure emits exactly one newline-terminated object. After an NDJSON command has emitted success events, a later failure emits exactly one final typed error event on stdout. Wire errors retain their code; CLI failures use usage, daemon_unavailable, manifest_invalid, and internal. Human mode is byte-for-byte unchanged.

Scope: implement one root/main error-rendering seam plus a small streaming-error path. JSON mode is selected when the target command supports JSON and a valid standalone --json or documented -j alias appears before the payload separator, even if later parsing fails. start/up and logs --follow remain NDJSON and append a final error event after partial output. Attached run --json remains its documented raw child-output mode and is excluded from the structured-error promise. Child stderr is payload, not a Hum diagnostic.

Why now: agents currently parse success JSON from stdout and unrelated error strings from stderr. Stable same-channel terminal errors allow one parser without buffering unbounded streams.

Non-goals: buffering streaming output, changing success payloads, changing exit codes, changing human diagnostics, changing attached run raw output, or adding JSON to unsupported commands.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/cli -run '^TestJSONErrorsBeforeOutput$' -count=1 -v` exits 0 and prints PASS for wire not_found plus CLI usage, daemon_unavailable, manifest_invalid, and internal failures, each producing one newline-terminated object on stdout, no Hum stderr, and the prior exit code.
- [x] #2 `go test ./internal/cli -run '^TestJSONStreamingErrors$' -count=1 -v` exits 0 and prints PASS for start/up partial results and a logs --follow late daemon/read failure, each preserving earlier NDJSON events then appending exactly one final typed error event without buffering or stderr diagnostics.
- [x] #3 `go test ./internal/cli -run '^TestJSONErrorModeDetection$' -count=1 -v` exits 0 and prints PASS across every structured-JSON command, --json and supported -j positions, parse failures, and payload separators; attached run raw mode and payload text resembling --json are excluded.
- [x] #4 `go test ./internal/cli -run '^TestHumanErrorsUnchanged$' -count=1 -v` exits 0 and prints PASS against pre-change human output/stderr/exit goldens, including attached child stderr.
- [x] #5 `go test ./internal/cli -run '^TestJSONErrorDocs$' -count=1 -v` exits 0 and prints PASS for the code table, stdout/stderr contract, NDJSON terminal event, and attached-run exception in docs/design.md.
- [x] #6 `task ci` exits 0.
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
1. Classify errors and detect requested JSON mode at the root error boundary, including parse failures.
2. Emit one stable envelope on stdout and suppress only Hum diagnostics on stderr.
3. Table-test every JSON-capable command, aliases, separators, human parity, and final gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed by @brett for implementation in an isolated worktree.

Implemented in 21a7dc7 and merged to main by 85b8b90.

AC#1: `go test ./internal/cli -run '^TestJSONErrorsBeforeOutput$' -count=1 -v` PASS; covered usage/parse, daemon unavailable (including input and aggregate logs), manifest invalid, retained wire codes, one newline-terminated stdout object, empty stderr, and exit-code preservation.
AC#2: `go test ./internal/cli -run '^TestJSONStreamingErrors$' -count=1 -v` PASS; start/up and logs-follow retained prior NDJSON and appended one typed terminal error.
AC#3: `go test ./internal/cli -run '^TestJSONErrorModeDetection$' -count=1 -v` PASS across every JSON-capable command, documented aliases, parse failure, separator, attached-run, and payload-lookalike cases.
AC#4: `go test ./internal/cli -run '^TestHumanErrorsUnchanged$' -count=1 -v` PASS; `go test ./internal/cli -run '^TestInputCommand$' -count=1 -v` PASS; attached child channels and human daemon-unavailable guidance preserved.
AC#5: `go test ./internal/cli -run '^TestJSONErrorDocs$' -count=1 -v` PASS.
AC#6: post-merge `task ci` PASS on main.

Independent verifier: PASS for AC#1-AC#6 after fixes. No tests were deleted, skipped, or weakened; no protected gate files changed.

Modified-file deviation: `internal/cli/commands.go` routes aggregate-log daemon failures and shutdown refusal through the shared renderer; `internal/cli/input.go` removes the legacy JSON error shape while preserving human guidance; `internal/cli/input_test.go` and `cmd/hum/integration_test.go` update the directly affected contracts. These changes are necessary to make the structured envelope universal for existing JSON-capable failure paths.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
JSON-capable CLI failures now emit stable structured errors on stdout, streaming commands append typed terminal errors, wire and exit codes are preserved, and human/attached-run output remains unchanged. Implemented by 21a7dc7 and merged to main by 85b8b90; post-merge task ci and independent verification passed.
<!-- SECTION:FINAL_SUMMARY:END -->
