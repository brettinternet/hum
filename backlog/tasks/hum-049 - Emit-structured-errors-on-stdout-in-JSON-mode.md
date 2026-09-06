---
id: HUM-049
title: Emit structured errors on stdout in JSON mode
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 17:41'
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
- [ ] #1 `go test ./internal/cli -run '^TestJSONErrorsBeforeOutput$' -count=1 -v` exits 0 and prints PASS for wire not_found plus CLI usage, daemon_unavailable, manifest_invalid, and internal failures, each producing one newline-terminated object on stdout, no Hum stderr, and the prior exit code.
- [ ] #2 `go test ./internal/cli -run '^TestJSONStreamingErrors$' -count=1 -v` exits 0 and prints PASS for start/up partial results and a logs --follow late daemon/read failure, each preserving earlier NDJSON events then appending exactly one final typed error event without buffering or stderr diagnostics.
- [ ] #3 `go test ./internal/cli -run '^TestJSONErrorModeDetection$' -count=1 -v` exits 0 and prints PASS across every structured-JSON command, --json and supported -j positions, parse failures, and payload separators; attached run raw mode and payload text resembling --json are excluded.
- [ ] #4 `go test ./internal/cli -run '^TestHumanErrorsUnchanged$' -count=1 -v` exits 0 and prints PASS against pre-change human output/stderr/exit goldens, including attached child stderr.
- [ ] #5 `go test ./internal/cli -run '^TestJSONErrorDocs$' -count=1 -v` exits 0 and prints PASS for the code table, stdout/stderr contract, NDJSON terminal event, and attached-run exception in docs/design.md.
- [ ] #6 `task ci` exits 0.
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
1. Classify errors and detect requested JSON mode at the root error boundary, including parse failures.
2. Emit one stable envelope on stdout and suppress only Hum diagnostics on stderr.
3. Table-test every JSON-capable command, aliases, separators, human parity, and final gates.
<!-- SECTION:PLAN:END -->
