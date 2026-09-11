# Hum for Herdr

Browse and operate the Hum processes in the selected Herdr workspace.

## Prerequisites and installation

Install Herdr 0.8.0 or newer, Python 3.10 or newer, and a current `hum` binary on the PATH inherited by Herdr. The Hum release must support the version 1 machine-output contract: the plugin checks `hum version --json` before trusting `hum list --json`.

Install from GitHub:

```sh
herdr plugin install brettinternet/hum/plugins/herdr --yes
```

For local development from this repository:

```sh
herdr plugin link plugins/herdr
```

## Actions and panes

Run **Hum: Processes…** in a workspace to choose a process, then choose an operation. Dedicated Follow logs, Start/attach, Start, Stop, Restart, and Remove actions filter the same picker.

- **Follow logs** opens a read-only split running `hum --project PROJECT logs NAME --follow`.
- **Attach** opens an interactive tab running the non-starting `hum --project PROJECT attach NAME`.
- A stopped process says **Start/attach**, never Attach. It runs `hum start` successfully before `hum attach`.
- **Start**, **Stop**, **Restart**, and **Remove** invoke the corresponding Hum CLI commands and show their output in the picker.

The plugin takes the workspace path from Herdr's invocation context, discovers processes with `hum list --json`, and then preserves the canonical absolute `project_root` reported by Hum. Every later Hum call carries that path with `--project`. Commands are launched as exact argument arrays: project paths and process names are never evaluated by a shell, including names containing spaces or metacharacters.

An unavailable Hum binary, unsupported machine-output version, missing workspace, and a project with no declared or retained processes each produce actionable messages in the picker or Herdr plugin log.

## Ownership boundary

Herdr owns discovery UI, pane creation, pane labels, focus, and terminal lifecycle. Hum owns supervised processes, retained output, lifecycle state, and the exclusive TTY input lease. The plugin uses only Hum's public CLI; it does not connect to or stabilize Hum's private daemon protocol.
