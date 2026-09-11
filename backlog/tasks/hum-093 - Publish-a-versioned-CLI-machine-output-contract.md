---
id: HUM-093
title: Publish a versioned CLI machine-output contract
status: To Do
assignee: []
created_date: '2026-09-06 19:10'
updated_date: '2026-09-11 16:58'
labels:
  - cli
  - json
  - integration
  - contract
dependencies:
  - HUM-092
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

## Why this exists

“Hum emits JSON” is not yet a complete integration promise. A consumer can parse today's output and still break if a later release renames `state`, changes a field type, changes one JSON document into multiple records, reorders lifecycle results, or changes cursor meaning. This task makes machine output a deliberate public subprocess contract and locks it with tests.

HUM-092 is the first real consumer. Its picker parses `list --json`; lifecycle actions may parse `start`, `restart`, `stop`, and `remove` results; logs and attach remain terminal panes and do not need to parse human output. Completing HUM-092 first provides concrete feedback, while this task keeps one consistent version marker across every CLI `--json` surface rather than creating versioned and unversioned islands.

## JSON, NDJSON, and the envelope

A bounded command emits one complete JSON document and exits. Proposed `list --json` shape:

```json
{
  "schema_version": 1,
  "processes": [
    {"name": "api", "state": "running", "pid": 48102}
  ]
}
```

A streaming or incremental command emits NDJSON: one independently parseable JSON object per line. Proposed `up --json` shape:

```json
{"schema_version":1,"name":"database","outcome":"already_running"}
{"schema_version":1,"name":"api","outcome":"started"}
{"schema_version":1,"name":"web","outcome":"started"}
```

A proposed output event remains one line:

```json
{"schema_version":1,"op":"event","type":"output","name":"api","entries":[{"cursor":42,"stream":"stdout","text":"ready\n"}]}
```

Here “envelope” means the existing top-level response object or NDJSON record. Add `schema_version: 1` directly to it. Do not introduce a disruptive wrapper such as `{"schema_version":1,"data":{...}}`; preserve existing payload shapes apart from the additive version field. Nested process and entry objects do not each repeat the schema version.

Standalone and streamed errors are versioned too, for example:

```json
{"schema_version":1,"error":{"code":"daemon_unavailable","message":"..."}}
{"schema_version":1,"op":"event","type":"error","name":"api","error":{"code":"daemon_unavailable","message":"..."}}
```

## Scope

Cover every command that supports `--json`, including success, aggregate, lifecycle-result, bounded-log, stream, warning, and terminal-error records. Attached `hum run` is the explicit exception: its documented output remains raw child stdout/stderr. Human-readable output and exit codes remain unchanged. MCP retains its separately advertised schemas and receives no `schema_version` change from this work.

Create `docs/cli-json-v1.md` as the normative contract. For each command, document whether it emits one JSON document or NDJSON, required and optional fields, field types and units, timestamps, cursor semantics, ordering guarantees, warning/error placement, and interaction with process exit status. Cross-link the contract from README.md and docs/design.md while keeping the daemon protocol private.

## Consumer example

A client should be able to do the equivalent of:

```python
result = subprocess.run(
    ["hum", "--project", project_root, "list", "--json"],
    capture_output=True,
    text=True,
    check=True,
)
document = json.loads(result.stdout)
if document.get("schema_version") != 1:
    raise UnsupportedHumOutputVersion()
for process in document["processes"]:
    render(process["name"], process["state"])
```

For NDJSON, read stdout line-by-line, parse each line independently, verify `schema_version`, dispatch on known record types/outcomes, and handle unknown values explicitly.

## Version 1 compatibility rules

Within schema version 1, Hum may add optional object fields and new enum/outcome values. It may not remove or rename documented fields, change their types or meanings, change JSON versus NDJSON framing, or weaken documented ordering/cursor guarantees. Consumers must ignore unknown optional fields and safely handle unknown enum values. An incompatible change requires a new schema version and a documented migration path.

Implementation should use the smallest typed CLI-only mechanism for the version field and avoid generic JSON map rewriting. Contract tests should validate representative shapes and invariants rather than freeze dynamic values such as PIDs and timestamps.

## Non-goals

A runtime plugin system; a public Go API; public daemon socket access; remote transport; MCP schema or behavior changes; versioning human-readable output; stabilizing undocumented internal fields; adding lifecycle operations; or requiring consumers to link Go code.
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
