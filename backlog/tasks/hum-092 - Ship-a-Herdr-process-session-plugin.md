---
id: HUM-092
title: Ship a Herdr process-session plugin
status: To Do
assignee: []
created_date: '2026-09-06 19:10'
updated_date: '2026-09-11 16:58'
labels:
  - herdr
  - integration
  - plugin
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

## Mental model

This is a Herdr UI adapter around existing Hum CLI commands, not code loaded into Hum:

```text
Herdr workspace
      |
      v
Hum process picker
      |
      +-- hum --project ROOT list --json
      +-- hum --project ROOT start|restart|stop|remove NAME --json
      +-- pane: hum --project ROOT logs NAME --follow
      +-- pane: hum --project ROOT attach NAME
                            |
                            v
                      private Hum daemon
```

Herdr owns panes, popups, focus, and selection. Hum continues to own process supervision, retained output, readiness, canonical project scope, and exclusive TTY input.

## User workflow

Install from this repository:

```sh
herdr plugin install brettinternet/hum/plugins/herdr --yes
```

The proposed stable plugin ID is `brettinternet.hum` and the proposed primary action ID is `open`, making direct invocation:

```sh
herdr plugin action invoke open --plugin brettinternet.hum
```

The same action should be discoverable as “Hum processes” from Herdr's normal action or command-palette UI.

Invoking it from a project workspace opens a popup similar to:

```text
+-- Hum - ~/dev/my-app -----------------------------+
| NAME       STATE      READINESS   PID             |
| api        running    ready       48102           |
| database   running    ready       48091           |
| console    running    unverified  48115   TTY     |
| web        stopped    -           -               |
|                                                   |
| Enter: actions    R: refresh    Esc: close         |
+---------------------------------------------------+
```

Selecting a process opens a state-appropriate action menu. Expected actions:

| Process state | Actions |
| --- | --- |
| Running non-TTY | View logs, Restart, Stop, Remove session |
| Running TTY | Attach, View logs, Restart, Stop, Remove session |
| Declared but never launched | Start |
| Stopped/exited retained session | Start, View logs when retained output exists, Remove session |

“Attach” must only mean the non-starting `hum attach NAME` operation. Any operation that may launch stopped work is labelled “Start” or “Start/attach,” never “Attach.” Lifecycle actions refresh the picker after completion and show Hum's actionable error when they fail.

Choosing “View logs” opens a Herdr pane running the argv equivalent of:

```sh
hum --project /absolute/path/to/my-app logs api --follow
```

Choosing “Attach” for a running TTY session opens a pane running:

```sh
hum --project /absolute/path/to/my-app attach console
```

The resulting workspace may look like:

```text
+-- editor -----------------+-- Hum: api logs --------+
|                           | [api] Listening on 3000 |
|                           | [api] GET /health 200   |
+---------------------------+-------------------------+
```

## Implementation constraints

Add the plugin under `plugins/herdr/`, including `herdr-plugin.toml`, its runtime script(s), tests, and a local README if useful. Use Python 3 standard library subprocess calls with argv arrays, or equivalently safe direct argv execution; never interpolate project paths or process names into shell source. No third-party Python package is allowed. If an external picker such as `fzf` is used, make it an explicit documented prerequisite and keep non-interactive parsing/action logic independently testable.

Resolve the project from the invoking pane's cwd by passing that directory through Hum's documented `--project` selector. Parse `hum list --json` to populate the picker. Pass the canonical project root and selected process to plugin panes as separate arguments or environment values. The logs and attach panes execute Hum CLI commands; they do not duplicate Hum lifecycle logic or read the daemon socket.

Tests use fake `hum` and `herdr` executables or injected subprocess runners to prove exact argv, state-to-action mapping, refresh behavior, spaces/metacharacters, no-process output, malformed/unsupported JSON, unavailable executables, and nonzero Hum results without requiring a live user session.

## Integration boundary and non-goals

Herdr owns discovery UI, pane creation, labels, focus, and terminal lifecycle. Hum owns supervised processes, retained output, lifecycle state, and the exclusive TTY input lease.

Non-goals: a Hum-side Herdr command; direct use or stabilization of Hum's private daemon socket; tmux integration; remote transport; MCP changes; arbitrary terminal control; plugin-defined manifest fields; custom lifecycle logic; or a general Hum plugin API.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `python3 -m unittest discover -s plugins/herdr -p 'test_*.py' -v` exits 0 and proves project/list discovery, running-versus-stopped action selection, exact argument construction for logs/attach/lifecycle operations, safe handling of spaces and metacharacters in names and project paths, and actionable empty/unavailable states.
- [ ] #2 AC2 — `task test` exits 0 and includes the Herdr plugin tests in the normal project test gate on Linux and macOS without adding a non-standard Python package dependency.
- [ ] #3 AC3 — `rg -n 'herdr plugin install brettinternet/hum/plugins/herdr|hum list --json|hum attach' README.md docs/coding-agents.md` exits 0 and the matched documentation explains installation, prerequisites, process/pane behavior, and that the plugin composes with the public CLI rather than the private daemon protocol.
- [ ] #4 AC4 — `task check` exits 0 and validates all repository source plus the new plugin files without deleting, skipping, or weakening an existing check.
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
