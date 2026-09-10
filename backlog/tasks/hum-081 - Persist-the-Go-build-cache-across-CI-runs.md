---
id: HUM-081
title: Persist the Go build cache across CI runs
status: In Progress
assignee: []
created_date: '2026-09-10 20:35'
updated_date: '2026-09-10 22:59'
labels:
  - tooling
dependencies:
  - HUM-077
references:
  - .github/workflows/ci.yaml
  - mise.toml
modified_files:
  - .github/workflows/ci.yaml
  - docs/development.md
priority: low
type: enhancement
ordinal: 55800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
In CI run 34522958965 the race phase took 127s on Linux and 146s on macOS while its longest package (hum/internal/cli) took 103.6s and 121.7s, so roughly 23s per job is race-instrumented compilation of the standard library and dependencies. The normal test phase shows only about 2s of compile overhead because `go vet` and staticcheck warm the build cache earlier in the same job. jdx/mise-action caches tools only (Install mise: 4 to 8s); nothing preserves GOCACHE or GOMODCACHE between runs, and `go mod download` repeats every job.

Outcome: after HUM-077 splits the race jobs out, every CI job restores a warm Go build and module cache so instrumented stdlib and dependency compilation is not repeated on each push, shortening the race critical path.

Scope: an `actions/cache` step (pinned to a commit SHA with the major version in a comment, like the other actions) for `$(go env GOCACHE)` and `$(go env GOMODCACHE)`, keyed on runner OS, the Go version from mise.toml, and `hashFiles('go.sum')`, with a restore-keys prefix fallback. Set `GOFLAGS=-count=1` in the workflow `env` so restored test results are never reused across commits; local `task test` and `task race` behaviour is unchanged. Apply to all four jobs; measure on the race jobs.

Non-goals: caching in release.yaml (HUM-076 removes its test run), changing Taskfile targets, caching built test binaries, or caching in the stress workflow.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `rg -n 'actions/cache@|GOFLAGS|restore-keys|hashFiles' .github/workflows/ci.yaml` shows the SHA-pinned cache step, `-count=1`, a `go.sum` hash in the key, and a fallback key.
- [ ] #2 For the second main push after the change (cache populated by the first), `gh run view RUN_ID --log | rg -c 'Cache restored from key'` prints 4, and `gh run view RUN_ID --json jobs --jq '.jobs[] | select(.name|test("race")) | "\(.name) \((.completedAt|fromdate) - (.startedAt|fromdate))s"'` reports each race job at least 15s faster than the same job in the HUM-077 acceptance run.
- [ ] #3 `task ci` exits 0.
- [ ] #4 `gh run view RUN_ID --log | rg '\(cached\)'` finds no match (exit status 1), proving no test result was reused.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation commit d5dc295 (ci: cache Go build artifacts), fast-forward merged to main. AC#1 evidence: rg -n actions/cache@\|GOFLAGS\|restore-keys\|hashFiles .github/workflows/ci.yaml found GOFLAGS -count=1 plus four SHA-pinned actions/cache v6 steps, four go.sum-hashed keys, and four restore prefixes. Each job resolves GOCACHE, GOMODCACHE, and GOVERSION from the mise-installed Go toolchain before restoring caches. Verification: mise exec actionlint -- actionlint .github/workflows/ci.yaml exited 0; task check:staged exited 0; git diff --check exited 0. Independent verifier passed AC#1, declared-file scope, and no-test-weakening checks. Its initial shellcheck finding was fixed by grouping GITHUB_OUTPUT writes, after which actionlint exited 0. Pending: AC#2 and AC#4 require logs from the second main push, so they remain unchecked. AC#3 remains unchecked because task ci exits 201 on pre-existing TestREADMEQuickstartStructure: README.md has 926 words, maximum 900; the verifier reproduced the identical failure on the branch base. Next step: after a second main push, collect AC#2/#4 cache and timing evidence, rerun task ci once the baseline README gate is repaired, then request a final independent verifier pass.
<!-- SECTION:NOTES:END -->
