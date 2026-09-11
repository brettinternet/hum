---
id: HUM-095
title: Publish Hum through Homebrew
status: To Do
assignee: []
created_date: '2026-09-11 16:40'
updated_date: '2026-09-11 17:03'
labels:
  - tooling
  - docs
  - credential
milestone: m-5
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

Evidence (2026-09-11): `gh repo view brettinternet/homebrew-tap` reports no such repository, so the tap must be created first. Releases v0.5.1 through v0.9.0 publish `hum-<version>-macos-x64.tar.gz`, `hum-<version>-macos-arm64.tar.gz`, and `checksums.txt`; `hum --version` prints `<version> (built <time>)` without a `v` prefix. The release workflow is hand-rolled (no GoReleaser), so the update path is one step after `gh release create` that renders Formula/hum.rb from checksums.txt and pushes it to the tap.

Human-required input: pushing to the tap from GitHub Actions needs a repository secret (fine-grained PAT or deploy key with contents:write on the tap). Until the owner provides it, the formula bump is a manual commit in the tap and the workflow step stays unimplemented; do not fake the automation with a token that lacks write access.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — On macOS arm64, `brew install brettinternet/tap/hum && hum --version` exits 0 and prints the release version without a `v` prefix, for example `0.9.0 (built 2026-...)`.
- [ ] #2 AC2 — `gh release download vX.Y.Z --repo brettinternet/hum --pattern checksums.txt --output -` lists both macOS archives, and their sha256 values equal the formula's `sha256` for the arm64 and Intel `url` branches (`rg -n 'sha256|url' Formula/hum.rb` in the tap checkout).
- [ ] #3 AC3 — `brew test brettinternet/tap/hum` exits 0 against the published formula.
- [ ] #4 AC4 — `brew audit --strict --online brettinternet/tap/hum` exits 0 without formula errors.
- [ ] #5 AC5 — For the newest tag, `gh run list --repo brettinternet/hum --workflow release.yaml --limit 1 --json conclusion` reports success and `gh api repos/brettinternet/homebrew-tap/commits/main --jq .commit.message` references that version, proving the release-to-formula update path ran.
- [ ] #6 AC6 — `rg -n 'brew install brettinternet/tap/hum' README.md` finds one current Homebrew installation example while the existing Mise method remains documented.
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
