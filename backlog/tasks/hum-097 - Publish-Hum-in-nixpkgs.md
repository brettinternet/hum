---
id: HUM-097
title: Publish Hum in nixpkgs
status: In Progress
assignee: []
created_date: '2026-09-11 16:40'
updated_date: '2026-09-11 20:24'
labels:
  - tooling
  - docs
  - waiting
milestone: m-5
dependencies: []
references:
  - 'https://github.com/NixOS/nixpkgs/blob/master/CONTRIBUTING.md'
  - 'https://nixos.org/manual/nixpkgs/stable/'
  - .github/workflows/release.yaml
  - 'https://github.com/NixOS/nixpkgs/pull/562380'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Blocked before implementation: nixpkgs does not currently contain pkgs/by-name/hu/hum/package.nix; this host has neither nix nor nixpkgs-review installed; and no brettinternet/nixpkgs fork exists. Completing AC1-AC3 requires creating an external nixpkgs contribution and exercising it on macOS aarch64 and Linux x86_64. Repository policy requires explicit authorization before forking/pushing/opening that external PR. Next action: authorize a NixOS/nixpkgs fork, branch push, and pull request; then use CI for both supported hosts and nixpkgs-review, merge upstream, and only afterward add the README install command.

External contribution opened: NixOS/nixpkgs#562380. AC1 evidence: GitHub Actions run 34637814632 executed `nix build -L .#hum` successfully on x86_64-linux (ubuntu-24.04) and aarch64-darwin (macos-14). AC2 evidence: the same run executed `./result/bin/hum --version` successfully on both hosts and printed `hum version 0.9.1 (built 1970-01-01T00:00:00Z)`. AC3 evidence: the same run executed `nix run github:Mic92/nixpkgs-review -- wip` successfully on both hosts and reported `1 package built: hum`. Nixpkgs PR checks are green after adding required structured attributes. Waiting for upstream review and merge before documenting `nixpkgs#hum` in README.md.
<!-- SECTION:NOTES:END -->
