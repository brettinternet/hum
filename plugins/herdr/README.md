# Hum for Herdr

Pick a Hum process in the current Herdr workspace, then follow its logs, attach to it, or control it.

```text
Hum: Processes…
  api   running  ──▶ Follow logs · Attach · Stop · Restart · Remove
  web   stopped  ──▶ View retained logs · Start/attach · Start · Remove
```

## Install

Requires Herdr 0.8.0+, Python 3.10+, and a `hum` that supports [CLI JSON v1](../../docs/cli-json-v1.md)
on the `PATH` Herdr inherits.

```sh
herdr plugin install brettinternet/hum/plugins/herdr --yes
```

For local development from this repository:

```sh
herdr plugin link plugins/herdr
```

## Actions

Run **Hum: Processes…**, choose a process, then an action. The dedicated Follow logs, Start/attach,
Start, Stop, Restart, and Remove actions open the same picker, filtered.

| Action | Opens | Runs |
| --- | --- | --- |
| Follow logs | read-only split | `hum --project PROJECT logs NAME --follow` |
| Attach (running) | interactive tab | `hum --project PROJECT attach NAME` |
| Start/attach (stopped) | interactive tab | `hum start`, then `hum attach` |
| Start, Stop, Restart, Remove | output in the picker | the matching Hum command |

The plugin checks `hum version --json`, finds processes with `hum list --json` from the workspace
path, and passes Hum's canonical `project_root` as `--project` on every later call. Commands run as
exact argument arrays, so paths and names (even with spaces or shell characters) are never
interpreted by a shell.

A missing `hum`, an unsupported JSON version, a missing workspace, or a project with no processes
each show a clear message in the picker or the Herdr plugin log.

## Who owns what

Herdr owns the picker, panes, labels, focus, and terminal lifecycle. Hum owns the processes, their
output, lifecycle state, and the single TTY input lease. The plugin uses only Hum's public CLI,
never its private daemon protocol.
