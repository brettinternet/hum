---
id: HUM-070
title: Pin the toolchain and add repository security gates
status: Done
assignee: []
created_date: '2026-09-10 01:51'
updated_date: '2026-09-10 10:30'
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
- [x] #1 `mise install --yes && mise current` exits 0 and its output contains no `latest` selector for a committed project tool.
- [x] #2 `task security` exits 0 after full-repository gitleaks and `govulncheck ./...` report no leaked secrets or reachable vulnerabilities.
- [x] #3 `rg -n "uses: [^ ]+@[0-9a-f]{40} +# v" .github/workflows/*.yaml` exits 0 and `rg -n "uses: .*@(v[0-9]+|main|master|latest)" .github/workflows` exits 1, proving workflow actions use immutable SHA refs with version comments.
- [x] #4 `task ci` exits 0 and its log includes the repository security gate.
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
AC#1 PASS — `mise install --yes && mise current` exited 0; `mise ls --local` confirmed every committed project tool uses an exact version and no local selector is `latest`.
AC#2 PASS — `task security` exited 0; gitleaks scanned 272 commits with no leaks and govulncheck reported 0 reachable vulnerabilities.
AC#3 PASS — `rg -n "uses: [^ ]+@[0-9a-f]{40} +# v" .github/workflows/*.yaml` exited 0 with six immutable action matches; `rg -n "uses: .*@(v[0-9]+|main|master|latest)" .github/workflows` exited 1 with no mutable refs.
AC#4 PASS — `task ci` exited 0 and began by logging both repository security scans before checks, tests, race, and smoke.
Minimum-Go evidence — `task check:go-min` exited 0 with x/sys v0.30.0 and x/term v0.29.0, the newest releases before their modules raise the Go directive above 1.22.
Independent verifier — PASS for AC#1, AC#2, AC#3, and AC#4; confirmed action tags, Go 1.22 dependency compatibility, unchanged tests, and tooling-label authorization.
Modified-file deviation — the authoritative HUM-070 task file changed only to record the provider claim, acceptance evidence, completion, and release required by repository workflow.
Review — traced full-history checkout into both CI consumers, exact tool resolution, security gate ordering and exit policy, dependency floor compatibility, and update paths; no item-scoped defects remain.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: confirmed all evidence; added the Go 1.22 floor constraint on dependency updates, made govulncheck pinning explicit (the shim currently has no version), and added a dependency on HUM-069 because both edit Taskfile.dist.yaml and ci.yaml. `mise install --yes` in AC#1 is a valid flag.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Pin project tools and workflow actions, add weekly dependency updates and an explicit tool upgrade path, run full-history secret and reachable-vulnerability scans in CI, and safely update Go dependencies while preserving Go 1.22 support.
<!-- SECTION:FINAL_SUMMARY:END -->
