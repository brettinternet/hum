---
id: HUM-101
title: Register the hum.yaml schema in SchemaStore
status: Done
assignee:
  - '@pi'
created_date: '2026-09-11 17:38'
updated_date: '2026-09-12 01:47'
labels:
  - docs
  - integration
  - human
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
- [x] #1 AC1 — `gh pr view PR_NUMBER --repo SchemaStore/schemastore --json state --jq .state` prints `MERGED`.
- [x] #2 AC2 — `curl -fsSL https://www.schemastore.org/api/json/catalog.json | python3 -c 'import json,sys; c=json.load(sys.stdin); e=[s for s in c["schemas"] if "hum.yaml" in s.get("fileMatch",[])]; assert len(e)==1 and e[0]["url"]=="https://raw.githubusercontent.com/brettinternet/hum/main/hum.schema.json"'` exits 0.
- [x] #3 AC3 — `rg -n 'SchemaStore' README.md docs/design.md` exits 0 and the matched documentation states the inline schema directive is optional for SchemaStore-aware editors and remains supported.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
SchemaStore PR `SchemaStore/schemastore#6344` merged and the production catalog now serves the `hum` entry. Repository documentation landed in commit `f164560`.

AC#1 evidence — `gh pr view 6344 --repo SchemaStore/schemastore --json state --jq .state` printed `MERGED`.
AC#2 evidence — the acceptance command against `https://www.schemastore.org/api/json/catalog.json` exited 0, confirming exactly one `hum.yaml` match with URL `https://raw.githubusercontent.com/brettinternet/hum/main/hum.schema.json`.
AC#3 evidence — `rg -n 'SchemaStore' README.md docs/design.md` exited 0; both passages state that the inline directive is optional for SchemaStore-aware editors and remains supported.

Delivery evidence — `task ci` passed on commit `f164560`. Independent verifier run `a1341888-83e9-4342-aafa-31a687b45fbf` returned PASS for AC1, AC2, and AC3 and confirmed the implementation commit touches only README.md and docs/design.md, with no tests or protected gate files changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
SchemaStore/schemastore#6344 merged, the production catalog serves the Hum schema for `hum.yaml`, and README.md plus docs/design.md document automatic SchemaStore support while retaining the inline directive.
<!-- SECTION:FINAL_SUMMARY:END -->
