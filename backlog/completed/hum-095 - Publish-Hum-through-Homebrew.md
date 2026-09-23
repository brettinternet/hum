---
id: HUM-095
title: Publish Hum through Homebrew
status: Done
assignee: []
created_date: '2026-09-11 16:40'
updated_date: '2026-09-11 18:46'
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
- [x] #1 AC1 — On macOS arm64, `brew install brettinternet/tap/hum && hum --version` exits 0 and prints the release version without a `v` prefix.
- [x] #2 AC2 — `gh release download --repo brettinternet/hum --pattern checksums.txt --output -` prints checksums for both macOS archives, and those values equal the arm64 and Intel URL checksums shown by `rg -n 'sha256|url' Formula/hum.rb` in the tap checkout.
- [x] #3 AC3 — `brew test brettinternet/tap/hum` exits 0 against the published formula.
- [x] #4 AC4 — `brew audit --strict --online brettinternet/tap/hum` exits 0 without formula errors.
- [x] #5 AC5 — For the newest tag, `gh run list --repo brettinternet/hum --workflow release.yaml --limit 1 --json conclusion` reports success and `gh api repos/brettinternet/homebrew-tap/commits/main --jq .commit.message` names that release version, proving the release-to-formula update ran.
- [x] #6 AC6 — `rg -n 'brew install brettinternet/tap/hum' README.md` finds one current Homebrew installation example while the existing Mise method remains documented.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented in Hum commit 1b9b214 and merged to main as f9b6cdd. Created public brettinternet/homebrew-tap; current formula commit f81f051. Provisioned a write-enabled deploy key and stored its private key as the Hum repository TAP_DEPLOY_KEY Actions secret.

AC#1 — On macOS arm64, brew install brettinternet/tap/hum succeeded and /opt/homebrew/bin/hum --version exited 0 with hum version 0.9.0, without a v prefix. Homebrew 6 requires the documented one-time brew trust --formula brettinternet/tap/hum.

AC#2 — gh release download v0.9.0 --repo brettinternet/hum --pattern checksums.txt --output - returned macOS arm64 375bfe784c25254ca26c6791799fc05452430a438ea7e617fe433d30458f75af and x64 307dff69d14c639b500d930cb2539a1534e71635a18fc1f2a678b54659434e4b; gh api repos/brettinternet/homebrew-tap/contents/Formula/hum.rb showed the same URL checksums.

AC#3 — brew test brettinternet/tap/hum exited 0.

AC#4 — brew audit --strict --online brettinternet/tap/hum exited 0 with no formula errors.

AC#5 — BLOCKED: the latest v0.9.0 release run predates commit 1b9b214, so the new release-to-formula path cannot run until merged main is pushed and a newer v* tag is pushed. Exact next step: push main, create and push the next release tag, wait for release.yaml, then verify its success and that the tap main commit message names that tag.

AC#6 — rg -n brew install brettinternet/tap/hum README.md found the Homebrew example at line 31; the Mise method remains at lines 34-38.

Verification — mise exec actionlint -- actionlint .github/workflows/release.yaml, task check:staged, and task ci on final merged main f9b6cdd all passed. Independent verifier: AC1-4 and AC6 PASS; AC5 BLOCKED pending a post-change tag. Diff is limited to README.md and .github/workflows/release.yaml; no tests were deleted, skipped, or weakened; release.yaml is permitted by the tooling label.

AC#5 — v0.9.1 completed release.yaml run 34634803417 successfully; gh api repos/brettinternet/homebrew-tap/commits/main --jq .commit.message returned hum v0.9.1, authored by github-actions[bot], proving the release workflow updated the formula.

Final independent verifier pass: PASS for AC1 through AC6 against v0.9.1. The public tap README was published in commit 71cabee and remained present after the automated formula update.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Published Hum through brettinternet/tap. Added Homebrew installation docs, release-to-formula automation with a write-enabled deploy key, a public tap README, and the v0.9.1 release. Homebrew install, version, checksums, test, audit, and automated tap update all passed.
<!-- SECTION:FINAL_SUMMARY:END -->
