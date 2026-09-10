---
id: HUM-067
title: Report declared stopped processes consistently in status
status: To Do
assignee: []
created_date: '2026-09-10 01:50'
updated_date: '2026-09-10 06:01'
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
- [ ] #1 `mise exec go -- go test ./internal/cli -run TestStatus -count=1` exits 0.
- [ ] #2 `mise exec go -- go test ./internal/cli -run TestStatusDeclaredProcessWithoutDaemon -count=1` exits 0 after human and JSON assertions report `stopped` and verify no daemon socket or process was created.
- [ ] #3 `mise exec go -- go test ./internal/cli ./integration -count=1` exits 0.
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
Refinement 2026-09-10: confirmed. In `statusCommand` (internal/cli/commands.go:888) the daemon-present not-found branch already projects a declared definition through `manifestProcess` (:965), and `projectProcessList` (:866) does the same when the daemon is absent; only the detail daemon-absent branch (:940-944) returns `manifestUnavailableMessage`, an error in both human and JSON mode. Fix is to reuse `manifestProcess` there.
---
<!-- COMMENTS:END -->
