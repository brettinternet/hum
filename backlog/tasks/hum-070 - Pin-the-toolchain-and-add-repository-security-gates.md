---
id: HUM-070
title: Pin the toolchain and add repository security gates
status: To Do
assignee: []
created_date: '2026-09-10 01:51'
updated_date: '2026-09-10 06:01'
labels:
  - tooling
dependencies:
  - HUM-069
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
Outcome: Developer tools and GitHub Actions resolve reproducibly, and pull-request CI runs secret and reachable-vulnerability scans. Evidence: `mise.toml` pins gitleaks, lefthook, Task, and Backlog.md to `latest`; ci.yaml and release.yaml use `actions/checkout@v6` and `jdx/mise-action@v4`; gitleaks runs only in the lefthook staged hook; govulncheck is not a project tool and no gate runs it. Scan results 2026-09-10: full-history gitleaks found no leaks; govulncheck found 0 called vulnerabilities, 1 uncalled in required modules; `go list -m -u all` shows golang.org/x/sys v0.25.0 -> v0.48.0, golang.org/x/term v0.24.0 -> v0.46.0, testify v1.11.1 -> v1.12.1. Constraint: go.mod declares `go 1.22` and `task check:go-min` builds with Go 1.22, while x/sys v0.48.0 requires go 1.26.0; update only to the newest versions that still build under the minimum supported Go, or split raising the floor into its own decision. Scope: pin exact tool versions and action commit SHAs with update comments, add govulncheck as a pinned mise tool, add a maintained update mechanism, add full-repository gitleaks and `govulncheck ./...` as a `task security` target wired into `task ci`, and update safe Go dependencies. Non-goals: do not add network services, upload source to third-party scanners, raise the minimum Go version, or fail builds solely on unavailable code paths without a documented policy.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise install --yes && mise current` exits 0 and its output contains no `latest` selector for a committed project tool.
- [ ] #2 `task security` exits 0 after full-repository gitleaks and `govulncheck ./...` report no leaked secrets or reachable vulnerabilities.
- [ ] #3 `rg -n "uses: [^ ]+@[0-9a-f]{40} +# v" .github/workflows/*.yaml` exits 0 and `rg -n "uses: .*@(v[0-9]+|main|master|latest)" .github/workflows` exits 1, proving workflow actions use immutable SHA refs with version comments.
- [ ] #4 `task ci` exits 0 and its log includes the repository security gate.
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
created: 2026-09-10 06:01
---
Refinement 2026-09-10: confirmed all evidence; added the Go 1.22 floor constraint on dependency updates, made govulncheck pinning explicit (the shim currently has no version), and added a dependency on HUM-069 because both edit Taskfile.dist.yaml and ci.yaml. `mise install --yes` in AC#1 is a valid flag.
---
<!-- COMMENTS:END -->
