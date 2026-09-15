---
id: HUM-114
title: 'Document Hum''s positioning, non-goals, and comparison with pitchfork'
status: To Do
assignee: []
created_date: '2026-09-14 23:16'
updated_date: '2026-09-15 04:32'
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
- Edit the README as a whole, not only the sections named here. Make every section concise, remove redundant prose, and show rather than tell wherever possible by preferring concrete commands, examples, tables, diagrams, or linked evidence over abstract explanation. Preserve necessary caveats and technical precision.
- Add a short section, placed after Quickstart, titled 'Hum and other process managers' with a compact table comparing Hum with pitchfork, Overmind/Hivemind, and mprocs on: config model, scoping, logs, wait semantics, TTY input, MCP surface, and UI. Treat these as comparison dimensions, not preassigned claims: verify each product separately against current first-party documentation and record source URL plus release/commit or retrieval date in Implementation Notes. Do not conflate Overmind with Hivemind or claim lack of a feature from missing documentation; mark unknown/not documented where evidence is absent. Hum scope uses a canonical project root with its documented non-git fallback; readiness is not limited to output matching. Two or three sentences after the table state plainly: recommend pitchfork or another manager only for capabilities verified in its cited version; for a process substrate that tools and agents build on, use Hum. Link pitchfork.
- Add a 'Non-goals' list: TUI/web UI (Herdr provides panes), port allocation and reverse proxy, cron scheduling, boot start, shell-hook autostart, file-watch restarts, liveness/health monitoring, child CPU/RSS sampling and enforcement (platform-native argv wrappers remain possible), log parsing or query languages, runtime shell interpretation or templating, Windows.
- Add a 'Ports across worktrees' note under Manifest environments showing per-worktree environment files (environment.files: [.env.local]) or --file manifests as the supported way to vary PORT, and stating Hum does not allocate ports.

## design.md

- Move the non-goals list into a 'Scope and non-goals' subsection so it is authoritative for contributors, and reference decision-001.
- Add one paragraph on the differentiation thesis: contracts and integrations (CLI JSON v1, closed MCP schemas, doctor, Herdr/Claude Code/Codex plugins) are the moat, not any single feature.

## Non-goals of this task

Changing behavior; promotional marketing copy or README restructuring unrelated to the concision and show-rather-than-tell requirement; benchmarks; a docs site.

Modified-file contract: README.md, docs/design.md.

Next action: after HUM-113 lands, verify the comparison sources and current Hum behavior before making the documentation changes. HUM-111/HUM-112 are not dependencies: describe only shipped features, and do not announce pending event history or native probes as available. Preserve the current quickstart, diagram, demo GIFs, and install instructions. Review existing ready.exec as well as match readiness. Keep per-worktree PORT examples literal, consistent with manifest environment precedence and the absence of value interpolation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `rg -n -A 35 "^## Hum and other process managers" README.md` exits 0 and displays a compact table covering config, scoping, logs, wait, TTY input, MCP, and UI, with distinct evidence for Hum, pitchfork, Overmind, Hivemind, and mprocs. `backlog task HUM-114 --plain` displays source URLs and versions/dates in Implementation Notes for every externally asserted capability; manual source review confirms the cells rather than treating a keyword match as factual verification. The opening explains the bounded exact-argv tools/agents interface and retains the existing media.
- [ ] #2 AC2 — `for doc in README.md docs/design.md; do rg -n -i -A 25 "^#+ .*non-goals" "$doc" || exit 1; done` exits 0 and displays each scope/non-goals section. Review both against the complete description list: UI, ports/proxy, cron, boot start, shell hooks, file watching, liveness, CPU/RSS enforcement, log query languages, shell templating, and Windows must be excluded consistently.
- [ ] #3 AC3 — `rg -n -A 22 "^#+ Ports across worktrees" README.md` exits 0 and displays a valid environment.files: [.env.local] or --file example with literal per-worktree PORT values and the exact statement that Hum does not allocate ports; the example must not imply PORT interpolation or cross-worktree service networking.
- [ ] #4 AC4 — `rg -n "decision-001" docs/design.md && task cli:check` exits 0. Review the displayed reference and surrounding scope section for the contracts/integrations rationale and correct links; `git diff -- README.md docs/design.md` shows only the requested sections and no deleted media or unrelated installation changes.
- [ ] #5 AC5 — `for doc in README.md docs/design.md; do rg -n -i "CPU|RSS|resource limits" "$doc" || exit 1; done` exits 0 for each file. Review the matched paragraphs: Hum neither samples child CPU/RSS nor enforces child quotas; operators can wrap exact argv with platform-native tools, while Hum still bounds its own output and machine-facing operations.
- [ ] #6 AC6 — `test -s README.md && git diff --check -- README.md` exits 0. A complete line-by-line review of README.md confirms every section is concise, redundant prose is removed, and the document shows rather than tells wherever possible by using concrete commands, examples, tables, diagrams, or linked evidence instead of abstract explanation; required technical caveats, the quickstart, diagram, demo GIFs, and install instructions remain.
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
