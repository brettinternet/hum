---
id: HUM-017
title: Add consistent short aliases for common CLI flags
status: To Do
assignee: []
created_date: '2026-09-03 02:43'
labels:
  - cli
  - docs
milestone: m-1
dependencies:
  - HUM-014
modified_files:
  - internal/cli/
  - README.md
  - docs/design.md
priority: medium
type: enhancement
ordinal: 1125
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: frequently typed Hum options have predictable one-letter aliases while every long form remains canonical in help, documentation, scripts, and error messages.

Scope: define command-local aliases with identical parsing, defaults, validation, output, and exit behavior: `-h/--help` and `-v/--version`; `-j/--json` wherever JSON exists; `serve -d/--daemon`; `run -d/--detach`; `list -a/--all`; `logs -s/--stream`, `-n/--tail`, `-c/--after-cursor`, `-b/--limit-bytes`, `-g/--grep`, and `-f/--follow`; `wait -c/--after-cursor`, `-m/--match`, and `-t/--timeout`; and `start`/`up` `-w/--wait` and `-t/--timeout`. Help shows the short and long spelling together and examples prefer readable long forms except where demonstrating aliases.

Keep uncommon configuration options long-only. Keep consequential `shutdown --stop-processes` long-only so it cannot be triggered by an opaque single letter. Reject duplicate or conflicting aliases when assembling the command tree; do not add alternate aliases, combined-short parsing rules, or aliases to MCP fields.

Modified-file contract: internal/cli/, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run TestFlagAliases -count=1` exits 0 and proves the complete command tree exposes exactly the documented command-local aliases, has no collisions, keeps `--stop-processes` and uncommon configuration flags long-only, and renders short/long pairs together in help.
- [ ] #2 `go test ./internal/cli -run TestFlagAliasParity -count=1` exits 0 and table-tests every alias against its long form with the same parsed value, validation result, output mode, daemon request, and exit code.
- [ ] #3 `go test ./internal/cli -run TestLifecycleHelp -count=1` exits 0 and `README.md` plus `docs/design.md` document the stable alias table while retaining long options in primary examples.
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
