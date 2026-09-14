---
id: HUM-115
title: Publish Hum in the mise registry
status: To Do
assignee: []
created_date: '2026-09-14 23:16'
updated_date: '2026-09-14 23:35'
labels:
  - tooling
  - docs
milestone: m-5
dependencies: []
references:
  - HUM-095
  - HUM-102
  - 'https://github.com/jdx/mise/tree/main/registry'
  - 'https://github.com/jdx/mise/blob/main/docs/contributing.md'
modified_files:
  - README.md
  - docs/development.md
priority: low
type: task
ordinal: 87800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: mise use -g hum installs the latest checksum-verified GitHub release on macOS and Linux, matching the install experience pitchfork offers its users with the short registry name documented as primary and the explicit github:brettinternet/hum backend retained as a compatibility fallback.

## Scope

- Prepare registry/hum.toml in jdx/mise using github:brettinternet/hum and current registry conventions. Upstream now uses one file per tool, not registry.toml. Neighbor registry/herdr.toml uses backends, description, version_order, and a test object with cmd/expected; use hum version as the non-daemon smoke test, not bare hum. Re-read docs/contributing.md and the current registry examples before implementation. Submit only after explicit authorization to push/open the upstream PR; this backlog refinement itself grants neither.
- Verify the actual entry before submission using the upstream development binary: target/debug/mise test-tool hum. A direct github backend install alone does not test registry resolution. Also run mise exec github:brettinternet/hum@latest -- hum version --json and assert schema_version 1. Use the upstream isolated test harness for install checks; do not mutate the normal global mise configuration or replace the workstation hum. Record release tag, OS/architecture, and backend checksum behavior. The selected backend must verify the release checksum; installation success alone is insufficient. If github cannot meet that contract, stop for the backend decision below rather than silently reducing verification.
- After the registry PR merges AND an available mise release contains the entry, replace the README mise block with mise use -g hum and keep the github: backend as a fallback line for users on older mise versions.
- Record in Implementation Notes the PR URL and the mise version in which the entry ships.

## Non-goals

An aqua-registry submission if the github backend suffices; Windows; Homebrew or curl installer changes; release pipeline changes.

Modified-file contract: README.md, docs/development.md (release checklist note). The registry change itself lives in jdx/mise.

## Delivery and external dependency

Next action: confirm registry/hum.toml is still absent, inspect the upstream contribution/test instructions, then prepare and locally validate the one-file entry. No other unfinished Hum task is a prerequisite (HUM-095/HUM-102 are already Done). Keep this task open through upstream delivery: after submission, record the exact PR URL/head and local evidence, then label blocked while waiting for maintainers to merge and release it. Unblock only when a released mise resolves hum and the recorded PR is merged; record the shipping mise version and resume the README/checklist update. Ask for publication authorization at the submission boundary, not by opening a PR speculatively. If checksum verification needs a different backend, record the evidence and obtain a decision before expanding into an aqua submission.

The upstream modified-file contract is registry/hum.toml only (required generated-file deviations must be explained); the local contract remains README.md and docs/development.md. No release pipeline changes or host-wide installs.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — In the upstream checkout, `target/debug/mise test-tool hum` exits 0 and runs the configured hum version check against registry/hum.toml. After submission, `gh pr view --repo jdx/mise --json state,mergedAt,url,files --jq '{state,mergedAt,url,files:[.files[].path]}'` run from that PR branch reports MERGED, a non-null mergedAt, the recorded PR URL, and registry/hum.toml in the changed files. Record the exact invocation with its PR number for use outside that branch; until merge this criterion stays unchecked, not waived.
- [ ] #2 AC2 — Using the released mise version recorded in Implementation Notes, `mise --version && mise exec hum@latest -- hum version --json | jq -e '(.schema_version == 1) and (.version | type == "string")'` exits 0 on macOS and Linux without changing global mise configuration. Record OS/architecture, Hum release tag, shipping mise version, and separate evidence that the selected backend verified the release checksum; an unverified backend does not satisfy this criterion. A pre-merge direct github backend install does not satisfy this registry-name check.
- [ ] #3 AC3 — `rg -n "mise use -g hum" README.md && rg -n "github:brettinternet/hum" README.md && rg -n -i "mise.*registry|registry.*mise" docs/development.md && task cli:check` exits 0. Review the install block for the short primary name, minimum shipping mise version and older-version fallback, and the release checklist note; do not advertise the short name before AC1/AC2 pass.
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
