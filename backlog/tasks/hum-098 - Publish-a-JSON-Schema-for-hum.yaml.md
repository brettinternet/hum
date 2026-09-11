---
id: HUM-098
title: Publish a JSON Schema for hum.yaml
status: To Do
assignee: []
created_date: '2026-09-11 17:04'
labels:
  - config
  - docs
  - integration
milestone: m-5
dependencies: []
references:
  - docs/design.md
  - internal/project/manifest.go
  - 'https://json-schema.org/draft/2020-12/schema'
modified_files:
  - schema/hum.schema.json
  - internal/project/
  - internal/cli/
  - README.md
  - docs/design.md
priority: medium
type: feature
ordinal: 70800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: editors and external tools validate and autocomplete `hum.yaml` from a committed JSON Schema, and a generated manifest points at it, so manifest authoring no longer depends on reading docs/design.md or trial and error against the strict parser.

Scope: add `schema/hum.schema.json` (draft 2020-12) covering `version: 1`, `processes`, and every process key the parser accepts today (`argv`, `cwd`, `tty`, `after`, `restart`, `stop_grace`, and `ready` with `match`, `exec`, `interval`, `timeout` and the exactly-one-of `match`/`exec` rule), with descriptions taken from docs/design.md, the process-name pattern from internal/project/manifest.go, and `additionalProperties: false` to mirror the strict parser. Make `hum init` emit a leading `# yaml-language-server: $schema=https://raw.githubusercontent.com/brettinternet/hum/main/schema/hum.schema.json` comment in both the generated and template outputs. Keep schema and parser in lockstep with a test: the schema's top-level, process, and ready property sets must equal the parser's accepted key sets in internal/project/manifest.go, and every manifest example in README.md, docs/design.md, and the init template must parse with the Go parser. Document editor setup and the authoritative-parser rule in README and design.

Decision for the implementer: validating documents against the schema in tests needs a JSON Schema library. Prefer the dependency-free property-set parity test; add a test-only validator dependency only if a rule the schema encodes (such as exactly-one-of) cannot otherwise be proven.

Non-goals: SchemaStore submission (a follow-up once the URL is stable), generating the schema from Go types, a `hum schema` subcommand, runtime validation with the schema (the Go parser remains authoritative), or any manifest semantics change.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/project -run '^TestManifestSchema' -count=1 -v` exits 0 and proves the schema's top-level, process, and ready property sets equal the parser's accepted keys and that every documented manifest example parses.
- [ ] #2 AC2 — `python3 -c 'import json; json.load(open("schema/hum.schema.json"))'` exits 0 and `rg -c 'draft/2020-12/schema|"additionalProperties": false' schema/hum.schema.json` prints at least 3.
- [ ] #3 AC3 — In an empty directory, `hum init && head -1 hum.yaml` prints the `# yaml-language-server: $schema=` line and `hum up` still prints exactly `No processes are declared in hum.yaml.`
- [ ] #4 AC4 — `rg -n 'hum.schema.json' README.md docs/design.md` exits 0 and the matched documentation explains editor setup and that the Go parser remains authoritative.
- [ ] #5 AC5 — `task test` and `task check` exit 0 with no deleted, skipped, or weakened tests.
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
