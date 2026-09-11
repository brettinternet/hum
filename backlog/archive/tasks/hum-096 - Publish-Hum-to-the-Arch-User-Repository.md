---
id: HUM-096
title: Publish Hum to the Arch User Repository
status: To Do
assignee: []
created_date: '2026-09-11 16:40'
updated_date: '2026-09-11 21:14'
labels:
  - tooling
  - docs
  - deferred
milestone: m-5
dependencies: []
references:
  - 'https://wiki.archlinux.org/title/AUR_submission_guidelines'
  - 'https://wiki.archlinux.org/title/PKGBUILD'
modified_files:
  - README.md
  - .github/workflows/release.yaml
priority: medium
type: task
ordinal: 68800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Arch users install the released Hum binary as `hum-bin` from the Arch User Repository instead of translating GitHub release assets into a package themselves.

Scope: publish and maintain `PKGBUILD` and `.SRCINFO` in the external AUR `hum-bin` repository for the existing Linux x64 and arm64 release archives, establish the tagged-release update path, and document installation with an AUR helper while retaining Mise instructions. Package sources use immutable release URLs and matching published checksums.

Modified-file boundary: this repository may change only `README.md` and `.github/workflows/release.yaml`; the AUR repository owns `PKGBUILD` and `.SRCINFO`.

External blocker (verified 2026-09-11): new AUR account registration is disabled during the maintainers' response to malicious activity. Keep this task unclaimed until registration reopens or an existing trusted maintainer agrees to publish `hum-bin`; record that objective unblock condition before implementation.

Non-goals: an Arch official-repository submission; a source-built `hum` package; support for architectures absent from GitHub releases; changing Hum runtime behavior; or replacing Mise installation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 In a clean Arch Linux x86_64 environment, `yay -S --noconfirm hum-bin && hum --version` exits 0 and reports the packaged release tag.
- [ ] #2 In a clean Arch Linux aarch64 environment, `makepkg --verifysource` in the hum-bin AUR checkout exits 0 using the matching immutable GitHub release asset and checksum.
- [ ] #3 `namcap PKGBUILD` and `namcap hum-bin-*.pkg.tar.zst` exit 0 without package errors.
- [ ] #4 `rg -n "yay -S hum-bin" README.md` finds one current AUR installation example while the existing Mise method remains documented.
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

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-11 16:50
---
Deferred because AUR account registration is currently disabled while maintainers respond to malicious activity. Resume when registration reopens or an existing trusted maintainer can publish hum-bin.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Cancelled on 2026-09-11. Stopped pursuing AUR packaging because publishing and maintaining another versioned distribution package is not justified for now.
<!-- SECTION:FINAL_SUMMARY:END -->
