---
id: HUM-049
title: Emit structured errors on stdout in JSON mode
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - cli
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/config.go
  - cmd/hum/main.go
  - internal/cli/json_errors_test.go
  - docs/design.md
priority: medium
type: enhancement
ordinal: 26700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: when `--json` is given, every command failure writes one JSON object to stdout of the form `{"error": {"code": "not_found", "message": "..."}}` (code from the wire error or a CLI-defined code such as `usage`, `daemon_unavailable`, `manifest_invalid`) and nothing to stderr; exit codes are unchanged. Human mode is unchanged.

Why now: `hum status nosuch --json` prints nothing on stdout and a plain-text error on stderr, so agents must parse two formats and cannot distinguish error classes without string matching.

Non-goals: changing success payloads, changing exit codes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestJSONErrors' -count=1 -v` exits 0 and prints PASS for not-found, daemon-unavailable, invalid-manifest, and usage errors, each a single JSON object on stdout with empty stderr and the existing exit code.
- [ ] #2 `task ci` exits 0.
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
