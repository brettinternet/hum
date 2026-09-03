---
id: HUM-019
title: Scaffold hum.yaml with init
status: To Do
assignee: []
created_date: '2026-09-03 03:15'
labels:
  - cli
  - config
  - docs
milestone: m-1
dependencies:
  - HUM-016
modified_files:
  - internal/project/
  - internal/cli/
  - integration/
  - README.md
  - docs/design.md
priority: medium
type: feature
ordinal: 1175
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a developer who needs an explicit manifest gets a correct first draft from `hum init` instead of writing `hum.yaml` from memory, and the no-candidate and ambiguity errors point at that command.

Scope: `hum init [--json]` resolves the nearest project root and refuses with exit 1 when `hum.yaml` already exists there, naming the path and leaving it untouched. Otherwise it runs the same discovery as `up` without launching anything or starting a daemon. With exactly one candidate it writes a version-1 document containing that definition under its discovered name with the exact argv, a comment naming the source, and a commented `ready` example with `match` and `timeout`. With ambiguity or no candidate it writes a commented template with one example entry whose comments list every detected candidate by source and argv, and says why no entry was generated. Every generated file must pass the HUM-014 strict parser. Human output prints the written path and the next command (`hum up`); JSON reports path, outcome (`generated`, `template`, `exists`), and candidates. Update the HUM-016 no-candidate and ambiguity errors to name `hum init`. Document `init` in README.md and docs/design.md.

Non-goals: interactive prompts, editing or merging an existing manifest, inferring readiness, cwd, or multiple processes, alternate filenames, an MCP tool, or writing outside the project root.

Modified-file contract: internal/project/, internal/cli/, integration/, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/project -run TestInit -count=1` exits 0 and proves a single discovered candidate yields a manifest with the exact argv, a source comment, and a commented ready example; ambiguity and no candidate yield a commented template listing every candidate; every generated document passes the strict parser; an existing hum.yaml is refused unchanged.
- [ ] #2 `go test ./internal/cli -run TestInit -count=1` exits 0 and proves human and JSON output name the written path, outcome, and next command, exit codes are 0 for generated/template and 1 for exists or resolution failure, no daemon is started, and the discovery no-candidate and ambiguity errors name `hum init`.
- [ ] #3 `go test ./integration -run TestInitThenUp -count=1` exits 0 and proves that in a package.json fixture `hum init` writes hum.yaml and `hum up --json` then starts the definition with source `manifest` and the same argv discovery produced.
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
