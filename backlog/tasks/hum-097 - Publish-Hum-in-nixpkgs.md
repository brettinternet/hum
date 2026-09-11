---
id: HUM-097
title: Publish Hum in nixpkgs
status: To Do
assignee: []
created_date: '2026-09-11 16:40'
labels:
  - tooling
  - docs
dependencies: []
references:
  - 'https://github.com/NixOS/nixpkgs/blob/master/CONTRIBUTING.md'
  - 'https://nixos.org/manual/nixpkgs/stable/'
modified_files:
  - README.md
priority: low
type: task
ordinal: 69800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Nix and NixOS users currently have no package attribute for Hum. Scope: contribute and maintain a top-level nixpkgs hum package sourced from immutable tagged releases (or the corresponding tagged source), cover Hum’s supported Linux and macOS architectures, and document installation after the package is available. Non-goals: a first-party flake, a binary cache, a NixOS service module, or changing Hum runtime behavior. External delivery artifact: the package expression, maintainer metadata, and tests required by the upstream nixpkgs contribution.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Against the nixpkgs contribution branch, `nix build .#hum` exits 0 on Linux x86_64 and macOS aarch64.
- [ ] #2 After `nix build .#hum`, `./result/bin/hum --version` exits 0 and reports the packaged release version rather than dev.
- [ ] #3 `nixpkgs-review pr PR_NUMBER` exits 0 and reports Hum as successfully built on the supported host platform.
- [ ] #4 After the nixpkgs change is available, `rg -n "nix profile install nixpkgs#hum" README.md` finds one current Nix installation example while the existing Mise method remains documented.
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
