---
id: HUM-114
title: 'Document Hum''s positioning, non-goals, and comparison with pitchfork'
status: Done
assignee: []
created_date: '2026-09-14 23:16'
updated_date: '2026-09-15 09:32'
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
- [x] #1 AC1 — `rg -n -A 35 "^## Hum and other process managers" README.md` exits 0 and displays a compact table covering config, scoping, logs, wait, TTY input, MCP, and UI, with distinct evidence for Hum, pitchfork, Overmind, Hivemind, and mprocs. `backlog task HUM-114 --plain` displays source URLs and versions/dates in Implementation Notes for every externally asserted capability; manual source review confirms the cells rather than treating a keyword match as factual verification. The opening explains the bounded exact-argv tools/agents interface and retains the existing media.
- [x] #2 AC2 — `for doc in README.md docs/design.md; do rg -n -i -A 25 "^#+ .*non-goals" "$doc" || exit 1; done` exits 0 and displays each scope/non-goals section. Review both against the complete description list: UI, ports/proxy, cron, boot start, shell hooks, file watching, liveness, CPU/RSS enforcement, log query languages, shell templating, and Windows must be excluded consistently.
- [x] #3 AC3 — `rg -n -A 22 "^#+ Ports across worktrees" README.md` exits 0 and displays a valid environment.files: [.env.local] or --file example with literal per-worktree PORT values and the exact statement that Hum does not allocate ports; the example must not imply PORT interpolation or cross-worktree service networking.
- [x] #4 AC4 — `rg -n "decision-001" docs/design.md && task cli:check` exits 0. Review the displayed reference and surrounding scope section for the contracts/integrations rationale and correct links; `git diff -- README.md docs/design.md` shows only the requested sections and no deleted media or unrelated installation changes.
- [x] #5 AC5 — `for doc in README.md docs/design.md; do rg -n -i "CPU|RSS|resource limits" "$doc" || exit 1; done` exits 0 for each file. Review the matched paragraphs: Hum neither samples child CPU/RSS nor enforces child quotas; operators can wrap exact argv with platform-native tools, while Hum still bounds its own output and machine-facing operations.
- [x] #6 AC6 — `test -s README.md && git diff --check -- README.md` exits 0. A complete line-by-line review of README.md confirms every section is concise, redundant prose is removed, and the document shows rather than tells wherever possible by using concrete commands, examples, tables, diagrams, or linked evidence instead of abstract explanation; required technical caveats, the quickstart, diagram, demo GIFs, and install instructions remain.
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
Comparison evidence (first-party sources, retrieved 2026-09-15):
- Pitchfork v2.25.0: release https://github.com/jdx/pitchfork/releases/tag/v2.25.0; layered TOML config, filesystem hierarchy/namespace, readiness modes https://pitchfork.jdx.dev/reference/configuration.html; SQLite logs/search/retention https://pitchfork.jdx.dev/guides/logs.html; exit wait https://pitchfork.jdx.dev/cli/wait.html; MCP lifecycle/log tools https://pitchfork.jdx.dev/cli/mcp.html and https://pitchfork.jdx.dev/guides/mcp.html; TUI https://pitchfork.jdx.dev/cli/tui.html; optional web UI https://pitchfork.jdx.dev/guides/web-ui.html. Interactive TTY input was not documented in the reviewed sources.
- Overmind v2.5.1, commit a9907243c989baac013c705bcec415f8b82cb8c8: release https://github.com/DarthSim/overmind/releases/tag/v2.5.1 and README https://github.com/DarthSim/overmind/blob/master/README.md support the Procfile/env model, working-directory/per-project socket scope, tmux output/echo, attachable tmux input, and tmux UI. Readiness/wait and MCP were not documented.
- Hivemind v1.1.0, commit 580abe5b3faf585c450604227e40e960cdbb21bd: release https://github.com/DarthSim/hivemind/releases/tag/v1.1.0; README https://github.com/DarthSim/hivemind/blob/master/README.md, options/scope and exit waiting https://github.com/DarthSim/hivemind/blob/master/main.go and https://github.com/DarthSim/hivemind/blob/master/hivemind.go, PTY input/output https://github.com/DarthSim/hivemind/blob/master/output.go. Readiness, MCP, historical logs, and separate TUI/web UI were not documented.
- mprocs/dekit v0.9.6, commit f296af454d5406489b688c5431503d9e1f030d58: release https://github.com/pvolok/dekit/releases/tag/v0.9.6 and legacy first-party README https://github.com/pvolok/dekit/blob/master/README-mprocs.md support local/global YAML or Procfile config, current-directory scope, TUI output and optional log files, shutdown waiting, interactive terminal/send-key, and full terminal UI. Readiness, MCP, and historical query API were not documented.
- Hum claims were checked against README.md, docs/design.md, docs/cli-json-v1.md, docs/coding-agents.md, and the shipped CLI/MCP implementation at a4d0c70.

Delivery evidence:
- AC#1 PASS — rg -n -A 35 on the process-manager heading in README.md exited 0; backlog task HUM-114 --plain shows first-party source URLs and versions/commits/retrieval date; independent source, opening, and media review passed.
- AC#2 PASS — rg -n -i -A 25 on non-goals headings in README.md and docs/design.md exited 0; manual completeness review passed.
- AC#3 PASS — rg -n -A 22 on the Ports across worktrees heading in README.md exited 0; literal environment/files/PORT and networking/interpolation review passed.
- AC#4 PASS — rg -n decision-001 docs/design.md and task cli:check exited 0; link and scoped-diff review passed.
- AC#5 PASS — rg -n -i CPU-or-RSS-or-resource-limits on README.md and docs/design.md exited 0; manual resource-boundary review passed.
- AC#6 PASS — test -s README.md and git diff --check -- README.md exited 0; complete README review confirmed concise concrete documentation and retained quickstart, diagram, demo GIFs, and install instructions.
- Final implementation commit: babf423. task check:staged, task cli:check, and task ci passed on the final commit. Independent verifier returned PASS for AC1-AC6 after reading provider state from the primary checkout. Diff is limited to README.md and docs/design.md; no tests or protected gate files changed.

Correction: the final Hum documentation commit is babf423; this supersedes the earlier a4d0c70 draft reference above.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: agent
created: 2026-09-14 23:20
---
Comparison follow-up: treat child CPU/RSS sampling and enforcement as a product non-goal, not a new implementation item. Cross-platform enforcement would require polling policy or platform-specific cgroups/rlimits, adds implicit kill/restart behavior, and overlaps Pitchfork rather than strengthening Hum’s exact-argv process API. Existing bounded logs, payloads, waits, and recovery remain Hum’s resource-management responsibility.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Completed in babf423 and fast-forwarded to main. README.md now states Hum positioning, compares first-party-documented manager capabilities, records product non-goals, and shows literal per-worktree port configuration. docs/design.md makes scope/non-goals authoritative and records the contracts-and-integrations thesis. Final task ci and independent AC1-AC6 verification passed.
<!-- SECTION:FINAL_SUMMARY:END -->
