---
id: HUM-095
title: Publish Hum through Homebrew
status: To Do
assignee: []
created_date: '2026-09-11 16:40'
labels:
  - tooling
  - docs
dependencies: []
references:
  - 'https://docs.brew.sh/How-to-Create-and-Maintain-a-Tap'
  - 'https://docs.brew.sh/Formula-Cookbook'
modified_files:
  - README.md
  - .github/workflows/release.yaml
priority: medium
type: task
ordinal: 67800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Hum currently asks macOS users to install through Mise even though tagged releases already contain macOS x64 and arm64 archives. Scope: publish and maintain an official brettinternet/tap/hum formula backed by those immutable release assets, connect tag releases to the formula update path, and document installation. Non-goals: submission to homebrew-core, changing Hum runtime behavior, or replacing the existing Mise installation path. External delivery artifact: the Formula/hum.rb file in the brettinternet/homebrew-tap repository.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 On macOS arm64 and x64, `brew install brettinternet/tap/hum && hum --version` exits 0 and reports the installed release tag.
- [ ] #2 `brew test brettinternet/tap/hum` exits 0 against the published formula.
- [ ] #3 `brew audit --strict --online brettinternet/tap/hum` exits 0 without formula errors.
- [ ] #4 `rg -n "brew install brettinternet/tap/hum" README.md` finds one current Homebrew installation example while the existing Mise method remains documented.
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
