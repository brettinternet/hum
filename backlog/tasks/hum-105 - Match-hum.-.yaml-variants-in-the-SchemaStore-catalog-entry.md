---
id: HUM-105
title: Match alternate Hum manifest filenames in SchemaStore
status: Done
assignee: []
created_date: '2026-09-12 01:34'
updated_date: '2026-09-14 23:27'
labels:
  - docs
  - integration
  - human
dependencies:
  - HUM-103
references:
  - HUM-101
  - 'https://github.com/SchemaStore/schemastore/pull/6344'
  - 'https://www.schemastore.org/api/json/catalog.json'
  - 'https://github.com/SchemaStore/schemastore/pull/6346'
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
- [x] #1 AC1 — `gh pr list --repo SchemaStore/schemastore --head hum-alternate-manifest-file-match --state merged --json state,files --jq '.[0] | [.state, ([.files[].path] | join(","))] | @tsv'` prints `MERGED<TAB>src/api/json/catalog.json` for the follow-up pull request whose URL is recorded in References.
- [x] #2 AC2 — `curl -fsSL https://www.schemastore.org/api/json/catalog.json | python3 -c 'import json,sys; entry=next(item for item in json.load(sys.stdin)["schemas"] if item.get("name")=="hum"); assert entry["fileMatch"] == ["hum.yaml", "hum.*.yaml", "*.hum.yaml", "hum.yml", "*.hum.yml"]'` exits 0, proving the production entry has exactly the five intended matches.
- [x] #3 AC3 — `for hum_doc in README.md docs/design.md; do for hum_match in 'hum.*.yaml' '*.hum.yaml' 'hum.yml' '*.hum.yml'; do rg -Fq -- "$hum_match" "$hum_doc" || exit 1; done; done` exits 0, and both documents state that SchemaStore-aware editors validate every listed alternate filename pattern.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Opened SchemaStore/schemastore#6346 from `hum-alternate-manifest-file-match`; the PR changes only `src/api/json/catalog.json`, preserves the external URL, parses as JSON, and passes `git diff --check`. Next: wait for upstream review, merge, and production catalog propagation before updating Hum docs.

AC#1 — gh pr list for SchemaStore/schemastore head hum-alternate-manifest-file-match printed MERGED and src/api/json/catalog.json. AC#2 — the production-catalog curl piped to the Python exact-fileMatch assertion exited 0 with exactly hum.yaml, hum.*.yaml, *.hum.yaml, hum.yml, and *.hum.yml. AC#3 — the documented rg loop exited 0 after README.md and docs/design.md were updated to state automatic SchemaStore coverage for every alternate pattern. Independent verifier: PASS for AC#1–AC#3, modified-file scope, and no deleted, skipped, or weakened tests. task ci was attempted twice but did not pass because unrelated timing tests failed: TestDownStopsProcessesConcurrentlyWithIndependentConnections on the first run and TestLogsSince on the second; TestLogsSince also failed in isolation. The user explicitly approved completion with DOD#1 left unchecked.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
SchemaStore PR #6346 merged and propagated to production. README.md and docs/design.md now document automatic schema loading for every supported alternate manifest filename. All acceptance criteria passed independently; the unrelated flaky full-CI failures are recorded in Implementation Notes.
<!-- SECTION:FINAL_SUMMARY:END -->
