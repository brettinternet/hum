---
id: HUM-092
title: Ship a Herdr process-session plugin
status: To Do
assignee: []
created_date: '2026-09-06 19:10'
updated_date: '2026-09-11 17:03'
labels:
  - herdr
  - integration
  - plugin
milestone: m-5
dependencies:
  - HUM-055
references:
  - HUM-055
  - docs/design.md
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

Scope: add a Herdr plugin under plugins/herdr with a project-process picker backed by `hum list --json`; open read-only process panes with `hum logs NAME --follow`; open interactive panes with the non-starting `hum attach NAME`; and expose explicit start, stop, restart, and remove actions through existing Hum CLI commands. The plugin must preserve the selected canonical project scope, safely pass process names and paths without shell interpolation, distinguish stopped from running sessions, and show actionable errors when Hum is unavailable or no processes exist. Document installation, prerequisites, and the ownership boundary.

Integration boundary: Herdr owns discovery UI, pane creation, labels, focus, and terminal lifecycle. Hum owns supervised processes, retained output, lifecycle state, and the exclusive TTY input lease. Any action that can launch a stopped process must be labelled Start or Start/attach, never Attach.

Plugin conventions (verified 2026-09-11 against the installed Herdr 0.8.x and brettinternet/herdr-plugins): the plugin directory holds `herdr-plugin.toml` with `id = "brettinternet.hum"`, `min_herdr_version`, `platforms = ["linux", "macos"]`, `[[actions]]` entries with `contexts`, and `[[panes]]` entries with `placement` (`popup` for the picker, `split` for log and attach panes) whose `command` is exact argv. Scripts are Python 3.10+ standard library only, invoked as `python3 script.py`, and pane commands reference `$HERDR_PLUGIN_ROOT`. Install is `herdr plugin install brettinternet/hum/plugins/herdr --yes`; local development uses `herdr plugin link "$PWD/plugins/herdr"`. Every Hum invocation passes the canonical absolute `--project` selector explicitly because pane cwd is not guaranteed; `hum list --json` records carry `project_root` for this purpose.

Non-goals: a Hum-side Herdr command; direct use or stabilization of Hum's private daemon socket; tmux integration; remote transport; MCP changes; arbitrary terminal control; plugin-defined manifest fields; or a general Hum plugin API.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `python3 -m unittest discover -s plugins/herdr -p 'test_*.py' -v` exits 0 and proves project/list discovery, running-versus-stopped action selection, exact argument construction for logs/attach/lifecycle operations (including the absolute `--project` selector), safe handling of spaces and metacharacters in names and project paths, and actionable empty/unavailable states.
- [ ] #2 AC2 — `task test` exits 0 and includes the Herdr plugin tests in the normal project test gate on Linux and macOS without adding a non-standard Python package dependency.
- [ ] #3 AC3 — `rg -n 'herdr plugin install brettinternet/hum/plugins/herdr --yes' README.md docs/coding-agents.md plugins/herdr/README.md` exits 0, and `rg -n 'Start/attach|hum list --json|hum attach|private daemon' plugins/herdr/README.md` exits 0; the plugin README explains prerequisites, process/pane behavior, the Start-versus-Attach labelling rule, and that the plugin composes with the public CLI rather than the private daemon protocol.
- [ ] #4 AC4 — `rg -c 'id = "brettinternet.hum"|min_herdr_version|platforms = \["linux", "macos"\]' plugins/herdr/herdr-plugin.toml` prints 3, and `herdr plugin link "$PWD/plugins/herdr" && herdr plugin list --plugin brettinternet.hum --json` reports the plugin enabled with its actions and panes; `herdr plugin unlink` afterwards restores the previous state.
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
