---
id: HUM-073
title: Make CLI JSON error codes type-driven
status: To Do
assignee: []
created_date: '2026-09-10 01:52'
updated_date: '2026-09-10 06:01'
labels: []
dependencies: []
modified_files:
  - internal/cli/config.go
  - internal/cli/root.go
  - internal/cli/commands.go
  - internal/cli/json_errors_test.go
  - integration/json_test.go
  - docs/design.md
priority: medium
type: bug
ordinal: 49700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Public JSON error codes are determined by typed error categories, never mutable English message fragments. Evidence: `likelyCLIUsageError` and unavailable classification scan phrases such as ` requires `, ` must `, and `duplicate`; rewording can change the API code and an internal error containing one marker can be mislabeled as user input. A typed `cliUsageError` already exists but is not applied consistently. Scope: wrap every command-edge validation and unavailable condition in existing or minimal typed errors, delete message heuristics, and add collision regressions. Non-goals: do not change human error wording, add a new error framework, or alter daemon/MCP wire error codes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise exec go -- go test ./internal/cli -run TestJSONErrors -count=1` exits 0.
- [ ] #2 `mise exec go -- go test ./internal/cli -run TestJSONErrorClassificationIgnoresMessageText -count=1` exits 0 after phrase-collision cases remain `internal` and typed usage/unavailable errors retain their documented codes.
- [ ] #3 `rg -n "likelyCLIUsageError|likelyDaemonUnavailableMessage|strings.Contains\(message" internal/cli/config.go` exits 1 with no matches.
- [ ] #4 `mise exec go -- go test ./internal/cli ./integration -count=1` exits 0.
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
Refinement 2026-09-10: confirmed. `likelyCLIUsageError` (internal/cli/config.go:173) matches 15 phrase markers and `likelyDaemonUnavailableMessage` (:157) matches two; both feed `jsonErrorFor`. AC#3 widened to also require removal of the unavailable-message heuristic, which the description already covers.
---
<!-- COMMENTS:END -->
