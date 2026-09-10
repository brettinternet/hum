---
id: HUM-065
title: Preserve explicit runtime permissions and zero stop grace
status: To Do
assignee: []
created_date: '2026-09-10 01:50'
updated_date: '2026-09-10 06:01'
labels: []
dependencies: []
modified_files:
  - internal/daemon/runtime.go
  - internal/daemon/runtime_test.go
  - internal/daemon/server.go
  - internal/daemon/daemon_test.go
  - internal/cli/stop_shutdown_test.go
  - docs/design.md
  - docs/development.md
priority: medium
type: bug
ordinal: 41700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: hum never silently changes an existing operator-managed runtime directory mode, and an explicit zero stop-grace duration reaches the supervisor as immediate escalation. Evidence: `ensurePrivateDir` (internal/daemon/runtime.go:382) runs `os.Chmod(dir, 0o700)` on every runtime directory, including a pre-existing 0755 one. `config.New` correctly preserves `HUM_STOP_GRACE=0s` as zero and `daemonChildEnvironment` forwards it, but the daemon then substitutes ten seconds in two places: `New` in internal/daemon/server.go:93 (`if stopGrace == 0 { stopGrace = 10s }`) and `reconcileStartup` in internal/daemon/runtime.go:618, even though `app.Options` documents zero as kill-at-grace-check. Scope: distinguish created directories from pre-existing directories and validate unsafe modes without mutating them; treat the CLI-resolved stop grace as authoritative in the daemon and remove the zero-to-default substitutions (test callers that rely on the implicit default set it explicitly). Document both semantics. Non-goals: do not relax secure permissions for directories created by hum, change nonzero grace parsing, add a separate unset representation to config, or alter process signal order.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise exec go -- go test -race ./internal/daemon && mise exec go -- go test ./internal/cli` exits 0.
- [ ] #2 `mise exec go -- go test ./internal/daemon -run TestPrepareRuntimePreservesExistingMode -count=1` exits 0 after proving an existing 0755 directory is not silently chmodded and a newly created directory is 0700.
- [ ] #3 `mise exec go -- go test ./internal/daemon ./internal/cli -run TestExplicitZeroStopGrace -count=1` exits 0 after proving a daemon started with `HUM_STOP_GRACE=0s` escalates to KILL immediately on stop and during startup reclamation rather than after ten seconds.
- [ ] #4 `rg -n "stopGrace = 10 \* time.Second|grace = 10 \* time.Second" internal/daemon` exits 1 with no matches.
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
Refinement 2026-09-10: chmod claim confirmed. Stop-grace claim was mis-located: there is no `Config.WithDefaults`; internal/config/config.go preserves an explicit 0s. The rewrite happens in internal/daemon/server.go:93-96 and runtime.go:618-620. Modified-file contract moved from internal/config to internal/daemon/server.go accordingly.
---
<!-- COMMENTS:END -->
