---
id: HUM-043
title: Restructure README into a quickstart and reference
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - docs
milestone: m-4
dependencies: []
modified_files:
  - README.md
  - docs/design.md
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
Outcome: README.md leads with install, a sixty-second quickstart (`hum run`, `hum up`, `hum logs --follow`, `hum down`), a short feature tour (manifest with `after` and `ready`, crash relaunch, TTY sessions, MCP for agents) of one or two paragraphs each, and links to docs/design.md for the full contract. Contract paragraphs that restate docs/design.md are removed; anything README states that design.md does not is moved into design.md first.

Why now: README.md is about 1800 words and most sections repeat the specification prose of docs/design.md (drift outcomes, stripping rules, lease semantics). New users cannot find the quickstart. The docs tests pin phrases across README, design, coding-agents, and both skills, so trimming needs the pins moved to design.md.

Non-goals: behavior changes, rewriting docs/design.md prose, changing CLI help (separate task).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `wc -w README.md` prints a number at or below 900, and `grep -c '^## ' README.md` prints at least 4 including Install, Quickstart, and Coding agents headings.
- [ ] #2 `go test ./internal/cli ./internal/skill -run 'Docs' -count=1` exits 0 with every removed README phrase asserted against docs/design.md instead of dropped.
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
