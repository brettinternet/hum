---
id: HUM-043
title: Finish the README quickstart structure
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 17:32'
labels:
  - docs
milestone: m-4
dependencies: []
modified_files:
  - README.md
  - docs/design.md
  - internal/cli/surface_test.go
  - internal/cli/after_docs_test.go
  - internal/cli/restart_policy_docs_test.go
  - internal/cli/terminal_control_docs_test.go
  - internal/skill/after_docs_test.go
  - internal/skill/restart_policy_docs_test.go
  - internal/skill/terminal_control_docs_test.go
priority: medium
type: docs
ordinal: 20700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: README.md stays below 900 words and leads new users through exact top-level sections in this order: Install, Quickstart, then short feature-oriented sections including Coding agents. Quickstart contains copy-pasteable hum run, hum up, hum logs --follow, and hum down examples that can be completed in about a minute. Full behavioral contracts live in docs/design.md and remain covered there.

Scope: finish the simplification already landed in commit 2df5cf2 by renaming/reordering headings, adding only the minimal missing quickstart/install copy, and keeping documentation phrase pins pointed at canonical design, coding-agent, or skill documents.

Why now: the README is already about 620 words, but the remaining task contract is not met: installation appears late and the exact Install and Quickstart headings do not exist.

Non-goals: behavior changes, expanding README back into a specification, rewriting docs/design.md, or changing CLI help.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestREADMEQuickstartStructure$' -count=1 -v` exits 0 and prints PASS, proving README.md is at most 900 words; top-level Install precedes Quickstart, which precedes Coding agents; and the quickstart contains hum run, hum up, hum logs --follow, and hum down.
- [ ] #2 `go test ./internal/cli ./internal/skill -run 'Docs' -count=1` exits 0 with every removed README contract phrase still asserted against docs/design.md, docs/coding-agents.md, or the bundled skills.
- [ ] #3 `task ci` exits 0.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reorder and rename the concise README around Install and Quickstart without expanding contract prose.
2. Verify required commands and feature links, moving any unique contract statement to design.md before removal.
3. Run docs tests, independent verification, and the final gate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-09-06: README.md was simplified to about 620 words in commit 2df5cf2 (docs: simplify README) while the audit was running, and the audit re-pointed the contract phrase pins in the CLI docs tests (input, drift, restart policy, recovery, aggregate logs, terminal control, TTY) from README.md to docs/design.md, docs/coding-agents.md, and the skills; README keeps a pinned link to docs/design.md. Remaining: confirm the README headings match AC1 (Install, Quickstart, Coding agents) and record the verifier pass before closing.
<!-- SECTION:NOTES:END -->
