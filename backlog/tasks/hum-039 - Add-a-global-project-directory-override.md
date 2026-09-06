---
id: HUM-039
title: Add a global --project/-C directory override
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
updated_date: '2026-09-06 17:17'
labels:
  - cli
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/
  - README.md
  - docs/design.md
priority: high
type: feature
ordinal: 16700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a persistent global `--project DIR` option with `-C` shorthand lets every project-scoped CLI command operate from DIR without changing the caller working directory. DIR resolves relative to the invocation directory, is cleaned to an absolute existing directory, and then follows the existing nearest-Git-root-or-directory-fallback rule. Both `hum --project DIR up` and `hum up --project DIR` work; `run` also accepts the option after NAME and before `--`.

Semantics: the selected DIR is the cwd for ad-hoc `run`; manifest processes retain their declared cwd relative to the resolved project root; `init` writes at the root resolved from DIR. `list --all` still uses the selected project when merging unlaunched declarations. With no override, behavior is unchanged. Empty, missing, nonexistent, and non-directory values fail before daemon contact with an actionable error. Daemon-global `serve` and `shutdown`, request-scoped `mcp`, and static `skill` reject an explicitly supplied project option as inapplicable rather than silently ignoring it.

Developer experience: human guidance and stable next-command fields emitted while an override is active retain a shell-safe canonical `--project` selector, including paths with spaces, so suggested follow-up commands work from the original unrelated directory. Help explains the project-root-versus-process-cwd distinction. docs/design.md records the behavior and alias; README.md includes one operate-from-anywhere example.

Why now: project-scoped commands repeatedly read os.Getwd(); operating on another checkout or from a scripts directory requires a subshell or `cd`. This is common for coding agents and worktree users. `--project` names the scope clearly alongside `--runtime-dir` and manifest `cwd`; `-C` follows Git and Make conventions. `-d` is not available because it already means `serve --daemon` and `run --detach`.

Non-goals: multi-project mutation, project registries, changing MCP `project_root`, changing manifest cwd semantics, or changing the process cwd of manifest definitions.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestProjectDirFlag$' -count=1 -v` exits 0 and prints PASS, proving absolute and relative `--project`/`-C` values select the intended root from an unrelated invocation directory for init, run, start, up, down, list (including `--all` declaration merging), status, single and aggregate logs, wait, input, restart, stop, and remove; ad-hoc run records exact DIR as child cwd while manifest cwd remains definition-derived.
- [ ] #2 `go test ./internal/cli -run '^TestProjectDirFlagParsing$' -count=1 -v` exits 0 and prints PASS, proving the long and short forms before and after the subcommand plus run pre-NAME and post-NAME placement; root and project-command help show `--project, -C`; empty, missing, nonexistent, and file values fail before daemon contact with actionable errors; serve, shutdown, mcp, and skill reject the option as inapplicable; existing `-d` daemon/detach behavior is unchanged.
- [ ] #3 `go test ./internal/cli -run '^TestProjectDirGuidance$' -count=1 -v` exits 0 and prints PASS, proving human guidance and stable next-command fields retain a shell-safe canonical `--project` selector for a directory containing spaces, while output without an override remains byte-for-byte unchanged.
- [ ] #4 `go test ./internal/cli -run '^(TestProjectDirDocs|TestFlagAliases)$' -count=1 -v` exits 0 and prints PASS, proving README.md and docs/design.md document the operate-from-anywhere example, project-root/cwd semantics, `-C` alias, and unchanged `-d` aliases.
- [ ] #5 `task ci` exits 0.
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
1. Add the persistent root flag and one validated selected-directory helper; route every project-scoped command through it while preserving the no-flag path.
2. Extend the special `run` argument parser and override-aware guidance without changing command payload parsing after `--`.
3. Add focused semantic, parsing, validation, guidance, help, and documentation coverage; run the focused tests and final gate.
<!-- SECTION:PLAN:END -->
