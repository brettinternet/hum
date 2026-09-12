---
id: HUM-105
title: Match hum.*.yaml variants in the SchemaStore catalog entry
status: To Do
assignee: []
created_date: '2026-09-12 01:34'
updated_date: '2026-09-12 01:34'
labels:
  - docs
  - integration
  - human
  - waiting
dependencies:
  - HUM-103
references:
  - HUM-101
  - 'https://github.com/SchemaStore/schemastore/pull/6344'
  - 'https://www.schemastore.org/api/json/catalog.json'
modified_files:
  - README.md
  - docs/design.md
priority: low
type: task
ordinal: 77800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: SchemaStore-aware editors validate and autocomplete alternate manifests named `hum.<variant>.yaml` (HUM-103 convention) without an inline schema directive.

Scope: open and land a pull request against SchemaStore/schemastore changing the merged `hum` catalog entry from HUM-101 (SchemaStore/schemastore#6344) so `fileMatch` is `["hum.yaml", "hum.*.yaml"]`. Keep the external `url`. Once the production catalog serves the change, note in README.md and docs/design.md that variant manifests are covered.

External completion boundary: the task remains In Progress until the upstream pull request is merged and the production catalog serves the updated entry.

Non-goals: schema content changes; matching `*.hum.yaml` or other patterns; editor-specific configuration.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `gh pr view PR_NUMBER --repo SchemaStore/schemastore --json state --jq .state` prints `MERGED`.
- [ ] #2 AC2 — `curl -fsSL https://www.schemastore.org/api/json/catalog.json | python3 -c 'import json,sys; c=json.load(sys.stdin); e=next(x for x in c["schemas"] if x.get("name")=="hum"); assert "hum.yaml" in e["fileMatch"] and "hum.*.yaml" in e["fileMatch"]'` exits 0.
- [ ] #3 AC3 — `rg -n 'hum\.\*\.yaml|hum\.<variant>\.yaml' README.md docs/design.md` exits 0 and the matched text states variant manifests are validated by SchemaStore-aware editors.
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
