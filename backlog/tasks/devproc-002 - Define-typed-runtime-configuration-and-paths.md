---
id: DEVPROC-002
title: Define typed runtime configuration and paths
status: To Do
assignee: []
created_date: '2026-09-02 17:05'
updated_date: '2026-09-02 20:05'
labels:
  - config
  - security
milestone: m-0
dependencies:
  - DEVPROC-001
modified_files:
  - internal/config/
priority: high
type: feature
ordinal: 200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: devproc resolves build metadata, socket location, output bounds, and stop timings through a typed configuration boundary independent of urfave/cli objects.

Scope: follow the useful policydev config pattern with BuildOpts, testable typed inputs, args/environment/default precedence, platform-aware path helpers, validation, and flags kept at the CLI edge. The socket root is `$XDG_RUNTIME_DIR/devproc` when available and otherwise a per-user directory below the OS temporary directory.

Non-goals: persistent configuration files or YAML loading, secrets, authentication, remote endpoints, service-manager integration, or daemon behavior.

Modified-file contract: internal/config/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/config` exits 0 and covers argument/environment/default precedence plus invalid bounds and durations.
- [ ] #2 `go test ./internal/config -run TestRuntimeDir` proves XDG selection and per-user temporary fallback on supported Unix platforms.
- [ ] #3 `go list -deps ./internal/config` succeeds and source inspection confirms no urfave/cli or YAML/config-file loader type crosses into the typed config.
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
