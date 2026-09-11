---
id: HUM-097
title: Publish Hum in nixpkgs
status: To Do
assignee: []
created_date: '2026-09-11 16:40'
updated_date: '2026-09-11 17:50'
labels:
  - tooling
  - docs
milestone: m-5
dependencies: []
references:
  - 'https://github.com/NixOS/nixpkgs/blob/master/CONTRIBUTING.md'
  - 'https://nixos.org/manual/nixpkgs/stable/'
  - .github/workflows/release.yaml
modified_files:
  - README.md
priority: low
type: task
ordinal: 69800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Nix and NixOS users install Hum as the top-level `nixpkgs#hum` package, built reproducibly from a tagged source release.

Scope: contribute and maintain a nixpkgs package using `buildGoModule`, cover Hum's supported Linux and macOS architectures, set release version metadata so `hum --version` is truthful, satisfy nixpkgs package tests and review, and document installation after the upstream change is available while retaining Mise instructions.

Packaging constraints: the module path is `hum`, the binary entrypoint is `./cmd/hum`, release builds set `CGO_ENABLED=0`, and linker flags set `main.buildVersion` to the package version. Use the nixpkgs version check hook or `testers.testVersion` for the installed-binary assertion.

Modified-file boundary: this repository may change only `README.md`; the external nixpkgs contribution owns the package expression, maintainer metadata, and upstream tests.

Non-goals: a first-party flake; a binary cache; a NixOS service module; repackaging GitHub release binaries; changing Hum runtime behavior; or replacing Mise installation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — In the nixpkgs contribution checkout, `nix build .#hum` exits 0 on Linux x86_64 and macOS aarch64.
- [ ] #2 AC2 — After `nix build .#hum`, `./result/bin/hum --version` exits 0 and reports the packaged release version rather than `dev`.
- [ ] #3 AC3 — Before submission, `nixpkgs-review wip` exits 0 and reports Hum successfully built on the supported host platform.
- [ ] #4 AC4 — After the nixpkgs change is available, `rg -n "nix profile install nixpkgs#hum" README.md` finds one current Nix installation example while the existing Mise method remains documented.
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
