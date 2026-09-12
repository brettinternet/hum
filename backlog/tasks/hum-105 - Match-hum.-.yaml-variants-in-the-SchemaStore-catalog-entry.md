---
id: HUM-105
title: Match alternate Hum manifest filenames in SchemaStore
status: To Do
assignee: []
created_date: '2026-09-12 01:34'
updated_date: '2026-09-12 01:43'
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
Outcome: SchemaStore-aware editors validate and autocomplete the alternate manifest filenames supported by Hum without an inline schema directive.

Scope:
- After HUM-103 lands, create the `hum-alternate-manifest-file-match` branch in the existing SchemaStore fork and open a follow-up pull request against SchemaStore/schemastore. Change only the merged `hum` catalog entry introduced by SchemaStore/schemastore#6344 so `fileMatch` is `["hum.yaml", "hum.*.yaml", "*.hum.yaml", "hum.yml", "*.hum.yml"]`; preserve its external `url`.
- Record the follow-up pull request URL in References. Keep this task In Progress through upstream review, merge, and production-catalog propagation.
- Only after the production catalog serves every match, update README.md and docs/design.md to state that SchemaStore-aware editors cover the documented alternate filenames.

External completion boundary: merge of the follow-up pull request and production propagation are required outcomes, not human-verification notes.

Non-goals: schema content changes; filename patterns beyond the five listed matches; editor-specific configuration; changing the HUM-101 pull request.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `gh pr list --repo SchemaStore/schemastore --head hum-alternate-manifest-file-match --state merged --json state,files --jq '.[0] | [.state, ([.files[].path] | join(","))] | @tsv'` prints `MERGED<TAB>src/api/json/catalog.json` for the follow-up pull request whose URL is recorded in References.
- [ ] #2 AC2 — `curl -fsSL https://www.schemastore.org/api/json/catalog.json | python3 -c 'import json,sys; entry=next(item for item in json.load(sys.stdin)["schemas"] if item.get("name")=="hum"); assert entry["fileMatch"] == ["hum.yaml", "hum.*.yaml", "*.hum.yaml", "hum.yml", "*.hum.yml"]'` exits 0, proving the production entry has exactly the five intended matches.
- [ ] #3 AC3 — `for hum_doc in README.md docs/design.md; do for hum_match in 'hum.*.yaml' '*.hum.yaml' 'hum.yml' '*.hum.yml'; do rg -Fq -- "$hum_match" "$hum_doc" || exit 1; done; done` exits 0, and both documents state that SchemaStore-aware editors validate every listed alternate filename pattern.
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
