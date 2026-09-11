---
id: HUM-092
title: Ship a Herdr process-session plugin
status: To Do
assignee: []
created_date: '2026-09-06 19:10'
updated_date: '2026-09-11 17:49'
labels:
  - herdr
  - integration
  - plugin
milestone: m-5
dependencies:
  - HUM-055
  - HUM-100
references:
  - HUM-055
  - docs/design.md
  - HUM-100
modified_files:
  - plugins/herdr/
  - .taskfiles/cli.yaml
  - README.md
  - docs/coding-agents.md
  - docs/design.md
priority: medium
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Hum ships an installable Herdr plugin in this repository so users can discover and operate Hum-managed project sessions from Herdr without Hum depending on Herdr or exposing its private daemon protocol.

Scope: add a Herdr plugin under `plugins/herdr` with a project-process picker backed by the version 1 `hum list --json` contract; verify support first with `hum version --json`; open read-only process panes with `hum logs NAME --follow`; open interactive panes with the non-starting `hum attach NAME`; and expose explicit start, stop, restart, and remove actions through existing Hum CLI commands. The plugin preserves the selected canonical project scope, passes process names and paths without shell interpolation, distinguishes stopped from running sessions, and reports actionable unsupported-version, unavailable-Hum, and empty-project states. Document installation, prerequisites, and the ownership boundary.

Integration boundary: Herdr owns discovery UI, pane creation, labels, focus, and terminal lifecycle. Hum owns supervised processes, retained output, lifecycle state, and the exclusive TTY input lease. Any action that can launch a stopped process is labelled Start or Start/attach, never Attach.

Plugin conventions (verified 2026-09-11 against the installed Herdr 0.8.x): the plugin directory holds `herdr-plugin.toml` with `id = "brettinternet.hum"`, `min_herdr_version`, `platforms = ["linux", "macos"]`, `[[actions]]` entries with `contexts`, and `[[panes]]` entries with explicit placement whose command is exact argv. Scripts are Python 3.10+ standard library only. Install is `herdr plugin install brettinternet/hum/plugins/herdr --yes`; local development uses `herdr plugin link plugins/herdr`. Every Hum invocation passes the canonical absolute `--project` selector because pane cwd is not guaranteed; version 1 list records carry `project_root`.

Non-goals: compatibility with Hum releases that do not report machine-output version 1; a Hum-side Herdr command; direct use or stabilization of Hum's private daemon socket; tmux integration; remote transport; MCP changes; arbitrary terminal control; plugin-defined manifest fields; or a general Hum plugin API.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `python3 -m unittest discover -s plugins/herdr -p 'test_*.py' -v` exits 0 and proves capability discovery, project/list discovery, running-versus-stopped action selection, exact argument construction for logs/attach/lifecycle operations with the absolute project selector, safe handling of spaces and metacharacters, and actionable unsupported/empty/unavailable states.
- [ ] #2 AC2 — `task test` exits 0 and includes the Herdr plugin tests in the normal project test gate on Linux and macOS without adding a non-standard Python package dependency.
- [ ] #3 AC3 — `rg -n 'herdr plugin install brettinternet/hum/plugins/herdr --yes' README.md docs/coding-agents.md plugins/herdr/README.md` exits 0, and `rg -n 'Start/attach|hum version --json|hum list --json|hum attach|private daemon' plugins/herdr/README.md` exits 0; the plugin README explains prerequisites, capability requirements, pane behavior, action labelling, and the public-CLI boundary.
- [ ] #4 AC4 — From the repository root, `herdr plugin link plugins/herdr`, `herdr plugin list --plugin brettinternet.hum --json`, and `herdr plugin action list --plugin brettinternet.hum` all exit 0 and report the enabled plugin, actions, and panes; after recording evidence, restore the prior Herdr plugin state.
- [ ] #5 AC5 — `task check` exits 0 and validates all repository source plus the new plugin files without deleting, skipping, or weakening an existing check.
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
