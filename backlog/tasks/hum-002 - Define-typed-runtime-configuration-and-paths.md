---
id: HUM-002
title: Define typed runtime configuration and paths
status: To Do
assignee: []
created_date: '2026-09-02 17:05'
updated_date: '2026-09-02 20:27'
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
- [ ] #1 `go test ./internal/config` exits 0 and covers flag/environment/default precedence for every setting plus rejection of byte limits below 64 KiB, completed-record limits outside 1-1000, and invalid or negative durations.
- [ ] #2 `go test ./internal/config -run TestRuntimeDir` exits 0 and proves the `HUM_RUNTIME_DIR` override, XDG selection, and per-user temporary fallback on supported Unix platforms.
- [ ] #3 `go list -deps ./internal/config` succeeds and contains no urfave/cli, altsrc, YAML, or config-file loader package.
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
