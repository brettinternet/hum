---
id: HUM-101
title: Register the hum.yaml schema in SchemaStore
status: To Do
assignee: []
created_date: '2026-09-11 17:38'
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
modified_files:
  - README.md
  - docs/design.md
priority: low
type: task
ordinal: 73800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: editors that use the SchemaStore catalog (VS Code YAML, JetBrains, Neovim yaml-language-server) validate and autocomplete `hum.yaml` automatically, without the `# yaml-language-server: $schema=` comment that HUM-098 writes.

Scope: after HUM-098 ships and the schema URL is stable, open and land a pull request against SchemaStore/schemastore adding a catalog entry named `hum` with `fileMatch: ["hum.yaml"]` and `url` pointing at `https://raw.githubusercontent.com/brettinternet/hum/main/schema/hum.schema.json`, plus the positive and negative test documents SchemaStore requires for a catalog entry. Prefer the external URL over a hosted copy so schema updates do not need a second upstream PR; if SchemaStore reviewers require a hosted copy, record that decision in Implementation Notes and add a follow-up for keeping the copy in sync. Once merged, document in README and docs/design.md that the inline `$schema` comment is optional for SchemaStore-aware editors and remains supported.

Human-required input: the SchemaStore PR is an external contribution that may wait on maintainer review; the task stays In Progress until merged and must not be marked Done on an open PR.

Non-goals: changing the schema content, versioned schema URLs per release, editor-specific configuration, or removing the `hum init` comment.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `gh pr view <PR_NUMBER> --repo SchemaStore/schemastore --json state --jq .state` prints MERGED.
- [ ] #2 AC2 — `curl -fsSL https://www.schemastore.org/api/json/catalog.json | python3 -c 'import json,sys; c=json.load(sys.stdin); e=[s for s in c["schemas"] if "hum.yaml" in s.get("fileMatch",[])]; assert len(e)==1 and e[0]["url"].endswith("/schema/hum.schema.json")'` exits 0.
- [ ] #3 AC3 — `rg -n 'SchemaStore' README.md docs/design.md` exits 0 and the matched documentation states the inline `$schema` comment is optional for SchemaStore-aware editors and still supported.
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
