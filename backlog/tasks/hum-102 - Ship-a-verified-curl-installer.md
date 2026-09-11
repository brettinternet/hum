---
id: HUM-102
title: Ship a verified curl installer
status: To Do
assignee: []
created_date: '2026-09-11 18:31'
updated_date: '2026-09-11 18:31'
labels:
  - tooling
  - docs
  - distribution
  - security
milestone: m-5
dependencies: []
references:
  - README.md
  - .github/workflows/release.yaml
  - >-
    https://docs.github.com/en/repositories/releasing-projects-on-github/linking-to-releases
modified_files:
  - install.sh
  - scripts/install_test.sh
  - Taskfile.dist.yaml
  - README.md
priority: medium
type: task
ordinal: 74800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users on supported Linux and macOS hosts can install a released Hum binary without first installing a package manager, while every downloaded binary is matched to its published release checksum.

Context: Hum currently documents Mise, HUM-095 adds Homebrew, and the release workflow already publishes versioned Linux and macOS archives plus `checksums.txt` for x64 and arm64. A small POSIX-shell installer fills the generic Linux and automation gap without adding another package repository.

Scope: add a repository-root `install.sh` intended for the documented `curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/brettinternet/hum/main/install.sh | sh` flow. The script runs under POSIX `sh`; maps Linux/Darwin and x86_64/amd64/aarch64/arm64 to the existing release asset names; defaults to the newest GitHub release; accepts `HUM_VERSION` with or without the leading `v` for a pinned release; and installs to `${HUM_INSTALL_DIR:-$HOME/.local/bin}/hum`. It downloads the selected archive and that release's `checksums.txt` over HTTPS, verifies the exact archive entry with `sha256sum` or `shasum -a 256`, extracts only `hum`, and atomically replaces the target only after successful verification. It must fail clearly before replacing an existing target when the platform, architecture, required tools, release, archive, or checksum is unavailable or invalid. Successful output names the installed version and path and warns when the install directory is absent from `PATH`. Add hermetic installer tests with stubbed release responses plus a pinned-release live smoke mode, run the hermetic suite from the existing `task test` gate on Linux and macOS, and document curl as an alternative while retaining Mise and package-manager instructions.

Security boundary: the installer may create the selected install directory but never invokes `sudo`, edits shell startup files, executes downloaded content before checksum verification, or writes outside that directory and a securely created temporary directory. Temporary files are removed with a trap on success, failure, and signals.

Non-goals: a `hum self-update` command or any CLI/runtime behavior; detecting or modifying Homebrew, Mise, Nix, or AUR installations; Windows or unsupported architectures; package-manager publication; artifact signing or attestations; changing release archive names; or adding an installer framework.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `sh scripts/install_test.sh` exits 0 and its hermetic platform cases prove Linux and Darwin select the existing x64 or arm64 archive for each accepted `uname -s`/`uname -m` spelling, while unsupported OS or architecture exits nonzero with no `hum` target created.
- [ ] #2 AC2 — `sh scripts/install_test.sh` exits 0 and its version/configuration cases prove an unset `HUM_VERSION` selects the newest release, `HUM_VERSION=0.9.0` and `HUM_VERSION=v0.9.0` both select tag `v0.9.0` and asset version `0.9.0`, `HUM_INSTALL_DIR` overrides the default, a missing install directory is created without `sudo` or shell-file edits, and success warns when that directory is absent from `PATH`.
- [ ] #3 AC3 — `sh scripts/install_test.sh` exits 0 and its fault cases prove a missing checksum entry, checksum mismatch, download failure, malformed archive, or missing required tool exits nonzero, removes temporary files, and leaves a pre-existing target byte-for-byte unchanged; its success case proves only the verified `hum` member is atomically installed executable.
- [ ] #4 AC4 — On any supported Linux or macOS host, `sh scripts/install_test.sh --live v0.9.0` exits 0 after installing the pinned published release into an isolated temporary directory and asserting `hum --version` reports `0.9.0` without a leading `v`.
- [ ] #5 AC5 — `rg -n "curl --proto.*raw\.githubusercontent\.com/brettinternet/hum/main/install\.sh.*\| sh" README.md` finds one current curl installation example, `rg -n "HUM_VERSION|HUM_INSTALL_DIR" README.md` documents pinning and destination overrides, and `rg -n "github:brettinternet/hum" README.md` confirms Mise remains documented; any additional installation methods present when implementation starts also remain documented.
- [ ] #6 AC6 — `task test && task check` exits 0 on Linux and macOS, with `task test` running `scripts/install_test.sh` in addition to the existing Go tests and no existing test deleted, skipped, or weakened.
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
