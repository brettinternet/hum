---
id: HUM-066
title: Make CLI scope selection unambiguous
status: Done
assignee: []
created_date: '2026-09-10 01:50'
updated_date: '2026-09-10 08:06'
labels: []
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/root.go
  - internal/cli/run_args_test.go
  - internal/cli/signal_test.go
  - integration/run_reconnect_test.go
  - README.md
  - docs/coding-agents.md
  - internal/skill/SKILL.md
priority: high
type: bug
ordinal: 42700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Child arguments can never retarget a process into another namespace, and scope selectors are parsed consistently around positional command arguments. Evidence: `hum run demo /bin/echo -g` passes `-g` to the child but also creates the retained record in global scope because `rawScopeFlag` scans beyond the child-command boundary. `hum signal TERM name --global` is documented by the shared selector model but currently fails after positional arguments. Scope: require the documented `--` before ad-hoc run commands or otherwise establish one authoritative child boundary, stop scope scanning at that boundary, and make signal selector placement match the supported CLI grammar. Update user and agent guidance. Non-goals: do not add implicit project/global fallback, change namespace identity, or reinterpret arguments after an explicit `--`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `mise exec go -- go test ./internal/cli ./integration -count=1` exits 0.
- [x] #2 `mise exec go -- go test ./internal/cli -run TestRunChildArgsCannotSelectScope -count=1` exits 0 after proving `hum run demo -- /bin/echo -g` stays project-scoped with `-g` only in child argv and the separator-less form is rejected before daemon contact.
- [x] #3 `mise exec go -- go test ./internal/cli -run TestSignalGlobalSelectorPlacement -count=1` exits 0 after proving supported pre- and post-positional `--global` forms target only the global record.
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
AC#1 PASS — `mise exec go -- go test ./internal/cli ./integration -count=1` exited 0.
AC#2 PASS — `mise exec go -- go test ./internal/cli -run TestRunChildArgsCannotSelectScope -count=1` exited 0; explicit child `-g` remained project-scoped and separator-less argv was rejected before daemon startup.
AC#3 PASS — `mise exec go -- go test ./internal/cli -run TestSignalGlobalSelectorPlacement -count=1` exited 0; pre- and post-positional `--global` targeted only the global record.
CI PASS — `task ci` exited 0 (vet, staticcheck, all tests, race tests, build, smoke).
Independent verifier — PASS for AC#1, AC#2, and AC#3; no deleted, skipped, or weakened tests and no protected gate changes.
Modified-file deviation — the authoritative `backlog/tasks/hum-066 - Make-CLI-scope-selection-unambiguous.md` changed only to record claim, evidence, completion, and release required by repository workflow.
Review — traced raw run argument reconstruction, direct-command fallback, signal selector parsing, daemon-contact ordering, and same-name cross-scope targeting; no item-scoped defects remain.
Delivery — included in the final commit containing this completion record.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: confirmed. `parseRunArgs` (internal/cli/commands.go:401) accepts the separator-less form when args[1] has no leading dash, and `rawScopeFlag` (internal/cli/root.go:495) scans the whole invocation and only stops at a literal `--`, so `hum run demo /bin/echo -g` both forwards `-g` and selects global scope. `parseSignalArgs` (:1893) handles --json, --project/-C and runtime flags after positionals but not --global/-g: `--global` fails as an unknown option and `-g` is taken as a third positional.
---

created: 2026-09-10 07:44
---
Claimed for implementation on main.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Require an explicit run child boundary, preserve child scope isolation, and accept global signal selectors around positional arguments. Updated CLI guidance and regression coverage.
<!-- SECTION:FINAL_SUMMARY:END -->
