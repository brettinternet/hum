---
id: HUM-073
title: Make CLI JSON error codes type-driven
status: Done
assignee: []
created_date: '2026-09-10 01:52'
updated_date: '2026-09-10 17:10'
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
- [x] #1 `mise exec go -- go test ./internal/cli -run TestJSONErrors -count=1` exits 0.
- [x] #2 `mise exec go -- go test ./internal/cli -run TestJSONErrorClassificationIgnoresMessageText -count=1` exits 0 after phrase-collision cases remain `internal` and typed usage/unavailable errors retain their documented codes.
- [x] #3 `rg -n "likelyCLIUsageError|likelyDaemonUnavailableMessage|strings.Contains\(message" internal/cli/config.go` exits 1 with no matches.
- [x] #4 `mise exec go -- go test ./internal/cli ./integration -count=1` exits 0.
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
Implementation Notes (2026-09-10):
- AC#1: `mise exec go -- go test ./internal/cli -run TestJSONErrors -count=1` exited 0.
- AC#2: `mise exec go -- go test ./internal/cli -run TestJSONErrorClassificationIgnoresMessageText -count=1` exited 0; message collisions remained `internal`, while typed usage and unavailable errors retained `usage` and `daemon_unavailable`.
- AC#3: `rg -n "likelyCLIUsageError|likelyDaemonUnavailableMessage|strings.Contains\(message" internal/cli/config.go` exited 1 with no matches.
- AC#4: `mise exec go -- go test ./internal/cli ./integration -count=1` exited 0.
- Final gate: independent verifier ran `task ci`; it exited 0 on the final implementation.
- Review: independent review found untyped config validation, over-typed filesystem failures, init validation-order drift, and incomplete collision strings. All findings were fixed and focused tests passed.
- Modified-file contract: implementation touches only `internal/cli/config.go`, `internal/cli/root.go`, `internal/cli/commands.go`, and `internal/cli/json_errors_test.go`; the provider-owned HUM-073 task file changed only for required claim, evidence, and completion metadata. No tests were deleted, skipped, or weakened; no protected gate file changed.

Independent verification (2026-09-10): PASS for AC#1 through AC#4. The verifier reran every exact acceptance command and `task ci`, and confirmed no weakened tests or protected gate changes.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: confirmed. `likelyCLIUsageError` (internal/cli/config.go:173) matches 15 phrase markers and `likelyDaemonUnavailableMessage` (:157) matches two; both feed `jsonErrorFor`. AC#3 widened to also require removal of the unavailable-message heuristic, which the description already covers.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Made CLI JSON error classification type-driven by marking usage and daemon-unavailable failures at command boundaries, removing message heuristics, preserving human error text and ordering, and adding phrase-collision and command-validation regressions.
<!-- SECTION:FINAL_SUMMARY:END -->
