---
id: HUM-115
title: Publish Hum in the mise registry
status: To Do
assignee: []
created_date: '2026-09-14 23:16'
updated_date: '2026-09-14 23:17'
labels:
  - tooling
  - docs
milestone: m-5
dependencies: []
references:
  - 'https://github.com/jdx/mise/blob/main/registry.toml'
  - HUM-095
  - HUM-102
modified_files:
  - README.md
  - docs/development.md
priority: low
type: task
ordinal: 87800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: mise use -g hum installs the latest verified GitHub release on macOS and Linux, matching the install experience pitchfork offers its users and removing the github:brettinternet/hum backend spelling from the README.

## Scope

- Open a PR against jdx/mise adding a registry.toml entry for hum using the github (or aqua, if a checksum-verified aqua registry entry is preferable) backend, pointing at brettinternet/hum releases, with test = ['hum version', 'hum']-style verification per registry conventions. Follow the registry contribution guide in the mise repository for the exact format and required fields.
- Verify locally before opening the PR: mise exec with a local registry override or mise use github:brettinternet/hum@latest installs the archive, the binary runs, and hum version --json prints a schema_version 1 object.
- After the registry PR merges, replace the README mise block with mise use -g hum and keep the github: backend as a fallback line for users on older mise versions.
- Record in Implementation Notes the PR URL and the mise version in which the entry ships.

## Non-goals

An aqua-registry submission if the github backend suffices; Windows; Homebrew or curl installer changes; release pipeline changes.

Modified-file contract: README.md, docs/development.md (release checklist note). The registry change itself lives in jdx/mise.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — gh pr view <registry-pr-url> --json state,mergedAt --jq '.state' prints MERGED for the jdx/mise registry PR recorded in Implementation Notes.
- [ ] #2 AC2 — mise use -g hum@latest && hum version --json | jq -e '.schema_version == 1' exits 0 on the macOS workstation using the released mise version named in Implementation Notes.
- [ ] #3 AC3 — rg -n 'mise use -g hum' README.md exits 0 and task cli:check exits 0.
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
