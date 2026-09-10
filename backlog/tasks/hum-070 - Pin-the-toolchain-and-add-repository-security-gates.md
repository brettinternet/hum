---
id: HUM-070
title: Pin the toolchain and add repository security gates
status: To Do
assignee: []
created_date: '2026-09-10 01:51'
labels: []
dependencies: []
modified_files:
  - mise.toml
  - Taskfile.dist.yaml
  - .taskfiles/setup.yaml
  - .github/workflows/ci.yaml
  - .github/workflows/release.yaml
  - .github/dependabot.yml
  - go.mod
  - go.sum
  - docs/development.md
priority: medium
type: chore
ordinal: 46700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Developer tools and GitHub Actions resolve reproducibly, and pull-request CI runs secret and reachable-vulnerability scans. Evidence: `mise.toml` pins gitleaks, lefthook, Task, and Backlog.md to `latest`; workflows use mutable major tags; gitleaks only scans the staged snapshot; and no CI gate runs `govulncheck`. A current audit found no called vulnerabilities, but `golang.org/x/sys` is stale and includes a Windows advisory in an uncalled path. Scope: pin exact tool versions and action commit SHAs with update comments, add a maintained update mechanism, add full-repository gitleaks and `govulncheck ./...` tasks to CI, and update safe Go dependencies. Non-goals: do not add network services, upload source to third-party scanners, or fail builds solely on unavailable code paths without a documented policy.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise install --yes && mise current` exits 0 and reports no `latest` selectors for committed project tools.
- [ ] #2 `task security` exits 0 after running full-repository gitleaks and `govulncheck ./...`, with no leaked secrets or reachable vulnerabilities.
- [ ] #3 A locally executable workflow lint/grep test exits 0 after proving every `uses:` reference in CI and release workflows is pinned to a full commit SHA with a version comment.
- [ ] #4 `task ci` exits 0 and the pull-request workflow includes the security gate.
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
