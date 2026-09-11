---
id: HUM-101
title: Register the hum.yaml schema in SchemaStore
status: In Progress
assignee:
  - '@pi'
created_date: '2026-09-11 17:38'
updated_date: '2026-09-11 19:04'
labels:
  - docs
  - integration
  - human
  - waiting
milestone: m-5
dependencies:
  - HUM-098
references:
  - HUM-098
  - 'https://github.com/SchemaStore/schemastore/blob/master/CONTRIBUTING.md'
  - 'https://www.schemastore.org/api/json/catalog.json'
  - hum.schema.json
modified_files:
  - README.md
  - docs/design.md
priority: low
type: task
ordinal: 73800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: editors that use the SchemaStore catalog validate and autocomplete `hum.yaml` automatically without requiring the inline schema directive emitted by `hum init`.

Scope: after HUM-098 lands the synchronization safeguards for the existing root schema, open and land a pull request against SchemaStore/schemastore adding a catalog entry named `hum` with `fileMatch: ["hum.yaml"]` and `url` set to `https://raw.githubusercontent.com/brettinternet/hum/main/hum.schema.json`, plus the positive and negative test documents SchemaStore requires. Prefer the external URL over a hosted copy so schema updates do not require a second upstream pull request; if reviewers require a hosted copy, record that decision in Implementation Notes and request approval for a separate synchronization follow-up. Once merged, document in README and docs/design.md that the inline directive is optional for SchemaStore-aware editors and remains supported.

External completion boundary: the task remains In Progress until the SchemaStore pull request is merged and the production catalog serves the entry.

Non-goals: changing schema content; moving the root schema; versioned schema URLs per release; editor-specific configuration; or removing the `hum init` directive.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `gh pr view PR_NUMBER --repo SchemaStore/schemastore --json state --jq .state` prints `MERGED`.
- [ ] #2 AC2 — `curl -fsSL https://www.schemastore.org/api/json/catalog.json | python3 -c 'import json,sys; c=json.load(sys.stdin); e=[s for s in c["schemas"] if "hum.yaml" in s.get("fileMatch",[])]; assert len(e)==1 and e[0]["url"]=="https://raw.githubusercontent.com/brettinternet/hum/main/hum.schema.json"'` exits 0.
- [ ] #3 AC3 — `rg -n 'SchemaStore' README.md docs/design.md` exits 0 and the matched documentation states the inline schema directive is optional for SchemaStore-aware editors and remains supported.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed with worklease. Implementation will prepare the SchemaStore contribution and complete repository documentation after the external catalog entry is merged.

SchemaStore PR `SchemaStore/schemastore#6344` is open; external contribution checks `bun cli.js check` and `bun cli.js coverage` passed. Repository docs are prepared on branch `agent/HUM-101-schemastore`. Because SchemaStore rejects positive/negative test directories unless a matching schema is hosted in its repository, and its documented external-schema workflow requires only the catalog entry, the submitted external-schema PR intentionally has no test documents. Next, wait for upstream merge and production catalog propagation, then run HUM-101 acceptance checks, independent verification, merge the docs to main, and clean up.

This task is waiting for upstream merge and production catalog propagation: https://github.com/SchemaStore/schemastore/pull/6344
<!-- SECTION:NOTES:END -->
