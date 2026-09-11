---
id: HUM-093
title: Publish a versioned CLI machine-output contract
status: To Do
assignee: []
created_date: '2026-09-06 19:10'
updated_date: '2026-09-11 17:48'
labels:
  - cli
  - json
  - integration
  - contract
milestone: m-5
dependencies: []
references:
  - HUM-092
  - docs/design.md
modified_files:
  - internal/cli/
  - cmd/hum/
  - integration/
  - docs/cli-json-v1.md
  - docs/design.md
  - README.md
priority: medium
type: enhancement
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: external local clients can depend on a documented, discoverable version 1 contract for Hum's CLI JSON and NDJSON output instead of parsing human output or consuming the private daemon protocol.

Scope: cover every command that supports `--json`, excluding attached `hum run`, whose documented output remains raw child output. Add `schema_version: 1` to every covered top-level JSON object and NDJSON record, including success, lifecycle-result, log, and terminal-error records. Document framing, field requiredness and optionality, ordering guarantees, exit-code interaction, and a compatibility policy: version 1 may add optional object fields and new enum values but may not remove or rename fields, change field types or meanings, or change record framing. Preserve current payload shapes apart from the additive version field.

Delivery boundary: contract tests exercise the CLI encoders and representative built-binary output. The MCP tool schemas may share field names, but MCP and CLI remain independently versioned public surfaces; this task must not export MCP internals or make CLI compatibility depend on MCP schema tables. HUM-100 and HUM-092 consume this contract after it lands.

Non-goals: a runtime plugin system; a public Go API; public daemon socket access; remote transport; MCP schema or behavior changes; versioning human-readable output; stabilizing undocumented internal fields; or adding lifecycle operations.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/cli -run '^TestCLIMachineOutputV1Contract$' -count=1 -v` exits 0 and prints PASS while proving `schema_version: 1` and the documented required fields for representative aggregate, single-process, lifecycle, bounded-log, NDJSON-stream, and terminal-error records.
- [ ] #2 AC2 — `go test ./cmd/hum ./integration -run 'Test.*MachineOutputV1' -count=1 -v` exits 0 and proves the built CLI emits version 1 records without changing human output, attached-run raw output, or exit-code behavior.
- [ ] #3 AC3 — `rg -n 'schema_version|Compatibility|NDJSON|private daemon' docs/cli-json-v1.md docs/design.md README.md` exits 0 and the matched documentation defines the covered commands, framing, field semantics, compatibility rules, and private-protocol boundary.
- [ ] #4 AC4 — `task test` and `task check` both exit 0 with no MCP contract changes and no deleted, skipped, or weakened tests.
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
