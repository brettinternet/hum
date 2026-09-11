---
id: HUM-065
title: Preserve explicit runtime permissions and zero stop grace
status: Done
assignee: []
created_date: '2026-09-10 01:50'
updated_date: '2026-09-10 09:09'
labels: []
dependencies: []
modified_files:
  - internal/daemon/runtime.go
  - internal/daemon/runtime_test.go
  - internal/daemon/server.go
  - internal/daemon/client.go
  - internal/daemon/client_test.go
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
- [x] #1 `mise exec go -- go test -race ./internal/daemon && mise exec go -- go test ./internal/cli` exits 0.
- [x] #2 `mise exec go -- go test ./internal/daemon -run TestPrepareRuntimePreservesExistingMode -count=1` exits 0 after proving an existing 0755 directory is not silently chmodded and a newly created directory is 0700.
- [x] #3 `mise exec go -- go test ./internal/daemon ./internal/cli -run TestExplicitZeroStopGrace -count=1` exits 0 after proving a daemon started with `HUM_STOP_GRACE=0s` escalates to KILL immediately on stop and during startup reclamation rather than after ten seconds.
- [x] #4 `rg -n "stopGrace = 10 \* time.Second|grace = 10 \* time.Second" internal/daemon` exits 1 with no matches.
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
Claimed on main for implementation.

Modified-file contract correction: internal/daemon/client.go and internal/daemon/client_test.go are included because StartupBudget also rewrote zero grace to ten seconds; leaving it unchanged would violate AC#4 and over-budget explicit-zero startup.

AC#1 PASS — `mise exec go -- go test -race ./internal/daemon && mise exec go -- go test ./internal/cli` exited 0 (daemon 22.257s; CLI 57.015s).

AC#2 PASS — `mise exec go -- go test ./internal/daemon -run TestPrepareRuntimePreservesExistingMode -count=1` exited 0; covers preserved 0755, created 0700, and rejected unchanged 0775.

AC#3 PASS — `mise exec go -- go test ./internal/daemon ./internal/cli -run TestExplicitZeroStopGrace -count=1` exited 0; covers immediate KILL escalation during startup reclamation and CLI stop.

AC#4 PASS — `rg -n "stopGrace = 10 \* time.Second|grace = 10 \* time.Second" internal/daemon` exited 1 with no matches.

DoD evidence: `task ci` passed; independent verifier fbb07f91-c10b-4a52-b734-b8caf2c663b0 returned PASS for AC#1-AC#4; `git diff --check` passed; no tests were deleted, skipped, or weakened; no protected gate files changed.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: chmod claim confirmed. Stop-grace claim was mis-located: there is no `Config.WithDefaults`; internal/config/config.go preserves an explicit 0s. The rewrite happens in internal/daemon/server.go:93-96 and runtime.go:618-620. Modified-file contract moved from internal/config to internal/daemon/server.go accordingly.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Preserved safe pre-existing runtime directory modes without chmod, retained 0700 for hum-created directories, rejected group/world-writable directories, and made zero stop grace authoritative through daemon startup budgeting, reclamation, and ordinary stop. Added focused regression coverage and documented both contracts.
<!-- SECTION:FINAL_SUMMARY:END -->
