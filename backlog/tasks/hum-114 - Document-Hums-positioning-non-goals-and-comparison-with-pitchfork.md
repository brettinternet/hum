---
id: HUM-114
title: 'Document Hum''s positioning, non-goals, and comparison with pitchfork'
status: To Do
assignee: []
created_date: '2026-09-14 23:16'
updated_date: '2026-09-14 23:20'
labels:
  - docs
  - product-boundary
milestone: m-5
dependencies:
  - HUM-113
references:
  - decision-001
  - 'https://pitchfork.jdx.dev/'
modified_files:
  - README.md
  - docs/design.md
priority: medium
type: docs
ordinal: 86800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a reader of README.md and docs/design.md can decide in under a minute whether Hum or a dev-services manager such as pitchfork fits their need, and contributors have a written list of things Hum deliberately does not do.

## README

- Tighten the opening so the first paragraph says what Hum is for: a bounded, exact-argv process interface that tools and coding agents can start, observe, wait on, and type into. Keep the existing diagram and demo GIFs.
- Add a short section, placed after Quickstart, titled 'Hum and other process managers' with a compact table comparing Hum with pitchfork, Overmind/Hivemind, and mprocs on: config model (exact argv vs shell string), scoping (canonical git root vs directory name), logs (bounded cursors vs SQLite/query), wait semantics (match output vs exit), TTY input, MCP surface, UI. Two or three sentences after the table state plainly: for a TUI, web dashboard, reverse proxy, cron, or autostart-on-cd, use pitchfork; for a process substrate that tools and agents build on, use Hum. Link pitchfork.
- Add a 'Non-goals' list: TUI/web UI (Herdr provides panes), port allocation and reverse proxy, cron scheduling, boot start, shell-hook autostart, file-watch restarts, liveness/health monitoring, log parsing or query languages, runtime shell interpretation or templating, Windows.
- Add a 'Ports across worktrees' note under Manifest environments showing per-worktree environment files (environment.files: [.env.local]) or --file manifests as the supported way to vary PORT, and stating Hum does not allocate ports.

## design.md

- Move the non-goals list into a 'Scope and non-goals' subsection so it is authoritative for contributors, and reference decision-001.
- Add one paragraph on the differentiation thesis: contracts and integrations (CLI JSON v1, closed MCP schemas, doctor, Herdr/Claude Code/Codex plugins) are the moat, not any single feature.

## Non-goals of this task

Changing behavior; marketing copy beyond the sections above; benchmarks; a docs site.

Modified-file contract: README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — rg -n -i 'pitchfork' README.md exits 0 and the match is inside a section headed 'Hum and other process managers' that contains a Markdown table with rows for config model, scoping, logs, wait, TTY input, MCP, and UI.
- [ ] #2 AC2 — rg -n -c 'Non-goals' README.md docs/design.md prints a count of at least 1 for each file, and rg -n -i 'reverse proxy|cron|boot start|shell-hook|file-watch|liveness|Windows' README.md exits 0 for every term inside the non-goals list.
- [ ] #3 AC3 — rg -n -i 'does not allocate ports' README.md exits 0 within a subsection that shows an environment.files or --file example for per-worktree PORT values.
- [ ] #4 AC4 — rg -n 'decision-001' docs/design.md exits 0, and task cli:check exits 0 (README changes do not break help-contract or doc-scan tests).
- [ ] #5 AC5 — `rg -n -i "CPU|memory|resource limits" README.md docs/design.md` exits 0 for both files, and the matched text states that Hum does not sample or enforce child CPU/RSS quotas; operators may wrap argv with platform-native tools, while Hum continues to bound its own retained output and machine-facing operations.
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
author: agent
created: 2026-09-14 23:20
---
Comparison follow-up: treat child CPU/RSS sampling and enforcement as a product non-goal, not a new implementation item. Cross-platform enforcement would require polling policy or platform-specific cgroups/rlimits, adds implicit kill/restart behavior, and overlaps Pitchfork rather than strengthening Hum’s exact-argv process API. Existing bounded logs, payloads, waits, and recovery remain Hum’s resource-management responsibility.
---
<!-- COMMENTS:END -->
