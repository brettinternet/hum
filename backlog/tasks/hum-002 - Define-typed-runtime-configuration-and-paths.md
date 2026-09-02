---
id: HUM-002
title: Define typed runtime configuration and paths
status: Done
assignee: []
created_date: '2026-09-02 17:05'
updated_date: '2026-09-02 23:18'
labels:
  - config
  - security
milestone: m-0
dependencies:
  - HUM-001
modified_files:
  - internal/config/
priority: high
type: feature
ordinal: 200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: hum resolves build metadata, the runtime directory, output and completed-record bounds, read defaults, and stop timing through a typed configuration boundary that application code constructs without urfave/cli objects.

Scope: a `BuildOpts` struct for ldflags values (version, build time); a typed `Input` struct holding raw flag and environment values; a constructor `New(BuildOpts, Input) (Config, error)` that applies flag > environment > default precedence, validates bounds and durations, and returns typed fields; platform-aware runtime-directory resolution: `HUM_RUNTIME_DIR` when set, else `$XDG_RUNTIME_DIR/hum`, else a per-user directory below the OS temporary directory. Environment variables: `HUM_RUNTIME_DIR`, `HUM_STOP_GRACE` (default 10s), `HUM_OUTPUT_BYTES` (retained output per process, default 4 MiB, minimum 64 KiB), and `HUM_COMPLETED_RECORDS` (maximum completed process records retained across all projects, default 20, range 1-1000). Read defaults (100 entries, 16 KiB) and the maximum line length live here as typed constants. The thin adapter that copies urfave/cli flag values into `Input` belongs to internal/cli (HUM-006), so this package never imports the CLI framework.

Non-goals: persistent configuration files or YAML/JSON loading, secrets, authentication, remote endpoints, service-manager integration, or daemon behavior.

Modified-file contract: internal/config/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/config` exits 0 and covers flag/environment/default precedence for every setting plus rejection of byte limits below 64 KiB, completed-record limits outside 1-1000, and invalid or negative durations.
- [x] #2 `go test ./internal/config -run TestRuntimeDir` exits 0 and proves the `HUM_RUNTIME_DIR` override, XDG selection, and per-user temporary fallback on supported Unix platforms.
- [x] #3 `go list -deps ./internal/config` succeeds and contains no urfave/cli, altsrc, YAML, or config-file loader package.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add internal/config/config.go with BuildOpts, raw Input fields for flag and environment values, typed Config fields, documented defaults and bounds, and New(BuildOpts, Input) using flag > environment > default precedence.
2. Resolve runtime directories as explicit flag, HUM_RUNTIME_DIR, XDG_RUNTIME_DIR/hum, then os.TempDir()/hum-<uid>; parse decimal byte/count inputs and Go durations with field-specific errors, rejecting output below 64 KiB, completed counts outside 1-1000, and negative durations.
3. Export typed read/output constants for 100 entries, 16 KiB reads, and a 64 KiB maximum line, while keeping internal/config standard-library-only.
4. Add internal/config/config_test.go covering defaults, build metadata, every precedence path, all invalid bounds/parsing, zero/negative durations, and all runtime-directory branches.
5. Run focused acceptance commands, task ci, adversarial review, and independent verifier; fix findings before commit and integration.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented in commit 7f90149 (feat(config): add typed runtime configuration).

Decision: the design and task require a typed maximum-line constant but do not specify its numeric value; the recorded implementation plan sets MaxLineBytes to 64 KiB for the downstream bounded-output package.

AC#1: `go test ./internal/config` exited 0 after the review fix; coverage includes defaults, environment values, flag-over-environment precedence for every configurable setting, byte/count parsing and bounds, invalid durations, negative durations including `-0.1ns` and `-0s`, and valid zero duration.
AC#2: `go test ./internal/config -run TestRuntimeDir` exited 0; TestRuntimeDir covers explicit flag override, HUM_RUNTIME_DIR over XDG, XDG_RUNTIME_DIR/hum, and os.TempDir()/hum-<uid> fallback.
AC#3: `go list -deps ./internal/config` exited 0; inspected output contains no urfave/cli, altsrc, YAML, or config-file loader package.

Final-commit gate: `task ci` exited 0 on commit 7f90149, running gofmt verification, go vet, staticcheck, and all Go tests. `task check:staged` passed formatting and secret checks before commit. LSP diagnostics reported no issues in config.go or config_test.go.

Review: adversarial review found a time.ParseDuration sub-nanosecond negative-duration quantization hole; the implementation now rejects raw leading-minus values and regression tests cover it. Post-fix reviewer returned PASS. Independent verifier returned PASS for AC#1, AC#2, and AC#3.

Scope: commit 7f90149 adds only internal/config/config.go and internal/config/config_test.go. No existing test was deleted, skipped, or weakened, and no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added a standard-library-only typed runtime configuration boundary with build metadata, flag/environment/default precedence, validated stop/output/completed limits, runtime path resolution, and bounded read constants. Verified by focused config tests, dependency inspection, task ci on commit 7f90149, post-fix adversarial review, and an independent PASS on all acceptance criteria.
<!-- SECTION:FINAL_SUMMARY:END -->
