---
id: HUM-095
title: Publish Hum through Homebrew
status: To Do
assignee: []
created_date: '2026-09-11 16:40'
updated_date: '2026-09-11 17:50'
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
Outcome: macOS users install Hum from the official `brettinternet/tap/hum` Homebrew formula, and each tagged Hum release updates that formula from immutable release assets.

Scope: create the `brettinternet/homebrew-tap` repository and its `Formula/hum.rb`, package the existing macOS x64 and arm64 archives, connect successful tag releases to the formula update path, and document Homebrew installation while retaining Mise instructions. The formula verifies the installed version and uses the checksums published with the matching GitHub release.

Current evidence (verified 2026-09-11): `gh repo view brettinternet/homebrew-tap` reports no such repository. Releases v0.5.1 through v0.9.0 contain both macOS archives and `checksums.txt`; `hum --version` omits the tag's `v` prefix. The hand-written release workflow therefore needs one post-release formula-render-and-push step rather than a GoReleaser integration.

Modified-file boundary: this repository may change only `README.md` and `.github/workflows/release.yaml`; the external tap repository owns `Formula/hum.rb` and any tap-local tests or metadata.

Human-required input: GitHub Actions needs a fine-grained token or deploy key with contents:write on the tap. Until the owner provides that credential, publish formula updates manually and do not add automation that cannot authenticate.

Non-goals: submission to homebrew-core; changing Hum runtime behavior; repackaging unsupported architectures; replacing Mise installation; or introducing GoReleaser.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — On macOS arm64, `brew install brettinternet/tap/hum && hum --version` exits 0 and prints the release version without a `v` prefix.
- [ ] #2 AC2 — `gh release download --repo brettinternet/hum --pattern checksums.txt --output -` prints checksums for both macOS archives, and those values equal the arm64 and Intel URL checksums shown by `rg -n 'sha256|url' Formula/hum.rb` in the tap checkout.
- [ ] #3 AC3 — `brew test brettinternet/tap/hum` exits 0 against the published formula.
- [ ] #4 AC4 — `brew audit --strict --online brettinternet/tap/hum` exits 0 without formula errors.
- [ ] #5 AC5 — For the newest tag, `gh run list --repo brettinternet/hum --workflow release.yaml --limit 1 --json conclusion` reports success and `gh api repos/brettinternet/homebrew-tap/commits/main --jq .commit.message` names that release version, proving the release-to-formula update ran.
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
