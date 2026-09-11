---
id: HUM-067
title: Report declared stopped processes consistently in status
status: Done
assignee: []
created_date: '2026-09-10 01:50'
updated_date: '2026-09-10 09:27'
labels: []
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/status_test.go
  - integration/status_test.go
  - docs/design.md
priority: medium
type: enhancement
ordinal: 43700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum status NAME` reports a resolved but never-launched manifest definition as stopped without starting the daemon, matching `hum status` and `hum list`. JSON and human output use the same process projection and actionable guidance. Evidence: aggregate status renders a synthetic stopped definition when no daemon exists, while detail status returns daemon unavailable for the same name. Scope: reuse the existing manifest-to-stopped projection in detail status and cover daemon-absent human/JSON behavior. Non-goals: do not synthesize ad-hoc records, start the daemon, or mask manifest resolution errors.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `mise exec go -- go test ./internal/cli -run TestStatus -count=1` exits 0.
- [x] #2 `mise exec go -- go test ./internal/cli -run TestStatusDeclaredProcessWithoutDaemon -count=1` exits 0 after human and JSON assertions report `stopped` and verify no daemon socket or process was created.
- [x] #3 `mise exec go -- go test ./internal/cli ./integration -count=1` exits 0.
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
AC#1: `mise exec go -- go test ./internal/cli -run TestStatus -count=1` exited 0 (PASS).
AC#2: `mise exec go -- go test ./internal/cli -run TestStatusDeclaredProcessWithoutDaemon -count=1` exited 0 (PASS); human and JSON report `stopped`, and the test verifies no daemon socket, PID file, or runtime artifact is created.
AC#3: `mise exec go -- go test ./internal/cli ./integration -count=1` exited 0 for both packages (PASS).
Independent verifier: PASS for AC#1, AC#2, and AC#3; no tests deleted, skipped, or weakened.
Modified-file deviation: `internal/cli/mcp_test.go` updates an existing no-daemon status expectation from error to the newly required stopped projection. This is necessary for AC#3 and remains within task scope; no `integration/status_test.go` exists.

`task ci` exited 0 on the final source snapshot: gofmt, vet, staticcheck, full tests, race tests, build, and smoke test passed.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: confirmed. In `statusCommand` (internal/cli/commands.go:888) the daemon-present not-found branch already projects a declared definition through `manifestProcess` (:965), and `projectProcessList` (:866) does the same when the daemon is absent; only the detail daemon-absent branch (:940-944) returns `manifestUnavailableMessage`, an error in both human and JSON mode. Fix is to reuse `manifestProcess` there.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Detail status now projects resolved declarations as stopped when no daemon exists, matching aggregate status without daemon startup. Added human/JSON and no-artifact coverage, updated discovery expectations and design docs, and passed all acceptance commands, independent verification, and task ci.
<!-- SECTION:FINAL_SUMMARY:END -->
