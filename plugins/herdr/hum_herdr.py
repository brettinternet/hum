#!/usr/bin/env python3
"""Herdr adapter for Hum's public version 1 CLI contract."""

from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import sys
from typing import Callable, Mapping, Sequence

PLUGIN_ID = "brettinternet.hum"
RUNNING_STATES = {"running", "ready", "starting", "relaunching"}


class PluginError(Exception):
    """An actionable error safe to show in a plugin pane or command log."""


Runner = Callable[..., subprocess.CompletedProcess[str]]


def exact_command(hum: str, project: str, operation: str, name: str) -> list[str]:
    if operation == "logs":
        args = ["logs", name, "--follow"]
    elif operation in {"attach", "start", "stop", "restart", "remove"}:
        args = [operation, name]
    else:
        raise PluginError(f"Unsupported Hum operation: {operation}")
    return [hum, "--project", project, *args]


def _run(command: Sequence[str], runner: Runner) -> subprocess.CompletedProcess[str]:
    try:
        return runner(command, text=True, capture_output=True, check=False)
    except FileNotFoundError as error:
        raise PluginError(
            "Hum is unavailable. Install hum and ensure it is on Herdr's PATH, then retry."
        ) from error


def _json_object(result: subprocess.CompletedProcess[str], purpose: str) -> dict:
    try:
        value = json.loads(result.stdout)
    except (json.JSONDecodeError, TypeError) as error:
        detail = (result.stderr or result.stdout or "no output").strip()
        raise PluginError(f"Hum {purpose} returned invalid JSON: {detail}") from error
    if not isinstance(value, dict):
        raise PluginError(f"Hum {purpose} returned an unexpected JSON value.")
    if result.returncode != 0:
        message = value.get("error", {}).get("message") if isinstance(value.get("error"), dict) else None
        raise PluginError(message or f"Hum {purpose} failed with exit code {result.returncode}.")
    return value


def verify_capability(hum: str = "hum", runner: Runner = subprocess.run) -> dict:
    result = _run([hum, "version", "--json"], runner)
    value = _json_object(result, "version discovery")
    if value.get("schema_version") != 1:
        found = value.get("schema_version", "missing")
        raise PluginError(
            f"Unsupported Hum machine-output version ({found}); update Hum to a release that supports schema version 1."
        )
    return value


def workspace_from_context(raw: str | None) -> str:
    try:
        context = json.loads(raw or "{}")
    except json.JSONDecodeError as error:
        raise PluginError("Herdr supplied invalid workspace context; reopen the action.") from error
    if not isinstance(context, dict):
        raise PluginError("Herdr supplied invalid workspace context; reopen the action.")
    candidate = context.get("workspace_cwd") or context.get("focused_pane_cwd")
    if not isinstance(candidate, str) or not candidate:
        raise PluginError("No Herdr workspace project is selected. Focus a project workspace and retry.")
    return str(Path(candidate).expanduser().resolve())


def discover_processes(
    workspace: str, hum: str = "hum", runner: Runner = subprocess.run
) -> tuple[str, list[dict]]:
    verify_capability(hum, runner)
    requested_project = str(Path(workspace).expanduser().resolve())
    result = _run([hum, "--project", requested_project, "list", "--json"], runner)
    value = _json_object(result, "project discovery")
    if value.get("schema_version") != 1 or not isinstance(value.get("processes"), list):
        raise PluginError("Hum list output is not the supported schema version 1 process snapshot.")

    processes = value["processes"]
    roots = {
        process.get("project_root")
        for process in processes
        if isinstance(process, dict) and isinstance(process.get("project_root"), str)
    }
    if len(roots) > 1:
        raise PluginError("Hum returned processes from more than one project scope.")
    project = roots.pop() if roots else requested_project
    if not Path(project).is_absolute():
        raise PluginError("Hum returned a non-absolute project scope; update Hum and retry.")
    if not processes:
        raise PluginError(
            f"No Hum processes are declared or retained in {project}. Add hum.yaml entries or run hum --project {project} run NAME -- COMMAND."
        )
    if any(not isinstance(process, dict) or not isinstance(process.get("name"), str) for process in processes):
        raise PluginError("Hum list output contains an invalid process record.")
    return project, processes


def is_running(process: Mapping[str, object]) -> bool:
    return process.get("state") in RUNNING_STATES


def operations_for(process: Mapping[str, object]) -> list[tuple[str, str]]:
    if is_running(process):
        return [
            ("logs", "Follow logs"),
            ("attach", "Attach"),
            ("stop", "Stop"),
            ("restart", "Restart"),
            ("remove", "Remove"),
        ]
    return [
        ("logs", "View retained logs"),
        ("start-attach", "Start/attach"),
        ("start", "Start"),
        ("remove", "Remove"),
    ]


def eligible(process: Mapping[str, object], mode: str) -> bool:
    if mode in {"processes", "logs", "remove", "start-attach"}:
        return True
    if mode == "start":
        return not is_running(process)
    if mode in {"stop", "restart"}:
        return is_running(process)
    return False


def operation_for_mode(process: Mapping[str, object], mode: str) -> str:
    if mode == "start-attach" and is_running(process):
        return "attach"
    return mode


def _choose(title: str, choices: Sequence[tuple[str, object]]) -> object:
    print(title)
    print()
    for index, (label, _) in enumerate(choices, 1):
        print(f"  {index}. {label}")
    print("  q. Cancel")
    while True:
        answer = input("\nChoose: ").strip().lower()
        if answer in {"q", "quit", ""}:
            raise KeyboardInterrupt
        if answer.isdigit() and 1 <= int(answer) <= len(choices):
            return choices[int(answer) - 1][1]
        print(f"Enter 1-{len(choices)} or q.")


def pane_open_command(
    herdr: str,
    entrypoint: str,
    project: str,
    name: str,
    operation: str,
    workspace_id: str | None = None,
) -> list[str]:
    command = [
        herdr,
        "plugin",
        "pane",
        "open",
        "--plugin",
        PLUGIN_ID,
        "--entrypoint",
        entrypoint,
        "--placement",
        "tab" if operation in {"attach", "start-attach"} else "split",
        "--focus",
        "--cwd",
        project,
        "--env",
        f"HUM_PROJECT={project}",
        "--env",
        f"HUM_PROCESS={name}",
        "--env",
        f"HUM_OPERATION={operation}",
    ]
    if workspace_id:
        command.extend(["--workspace", workspace_id])
    return command


def open_picker(environ: Mapping[str, str], runner: Runner = subprocess.run) -> int:
    project = workspace_from_context(environ.get("HERDR_PLUGIN_CONTEXT_JSON"))
    herdr = environ.get("HERDR_BIN_PATH", "herdr")
    mode = environ.get("HERDR_PLUGIN_ACTION_ID", "processes")
    command = [
        herdr,
        "plugin",
        "pane",
        "open",
        "--plugin",
        PLUGIN_ID,
        "--entrypoint",
        "picker",
        "--placement",
        "popup",
        "--focus",
        "--cwd",
        project,
        "--env",
        f"HUM_PICKER_MODE={mode}",
    ]
    hum = environ.get("HUM_BIN_PATH")
    if hum:
        command.extend(["--env", f"HUM_BIN_PATH={hum}"])
    workspace_id = environ.get("HERDR_WORKSPACE_ID")
    if workspace_id:
        command.extend(["--workspace", workspace_id])
    result = runner(command, text=True, capture_output=True, check=False)
    if result.returncode != 0:
        raise PluginError((result.stderr or result.stdout).strip() or "Herdr could not open the Hum picker.")
    return 0


def run_picker(environ: Mapping[str, str], runner: Runner = subprocess.run) -> int:
    workspace = str(Path.cwd().resolve())
    hum = environ.get("HUM_BIN_PATH", "hum")
    mode = environ.get("HUM_PICKER_MODE", "processes")
    project, processes = discover_processes(workspace, hum, runner)
    choices = [
        (f"{process['name']}  [{process.get('state', 'unknown')}]", process)
        for process in processes
        if eligible(process, mode)
    ]
    if not choices:
        label = {"start": "stopped", "stop": "running", "restart": "running"}.get(mode, "eligible")
        raise PluginError(f"No {label} Hum processes are available in {project}.")
    process = _choose(f"Hum processes — {project}", choices)
    operation = operation_for_mode(process, mode)
    if mode == "processes":
        operation = _choose(
            f"{process['name']} — {process.get('state', 'unknown')}",
            [(label, key) for key, label in operations_for(process)],
        )

    name = process["name"]
    if operation in {"logs", "attach", "start-attach"}:
        command = pane_open_command(
            environ.get("HERDR_BIN_PATH", "herdr"),
            "process",
            project,
            name,
            operation,
            environ.get("HERDR_WORKSPACE_ID"),
        )
        result = runner(command, text=True, capture_output=True, check=False)
    else:
        result = runner(exact_command(hum, project, operation, name), text=True, capture_output=True, check=False)
    if result.stdout:
        print(result.stdout, end="")
    if result.stderr:
        print(result.stderr, end="", file=sys.stderr)
    if result.returncode != 0:
        raise PluginError(f"Hum {operation} failed for {name} (exit {result.returncode}).")
    return 0


def run_process(environ: Mapping[str, str], runner: Runner = subprocess.run) -> int:
    hum = environ.get("HUM_BIN_PATH", "hum")
    project = environ.get("HUM_PROJECT", "")
    name = environ.get("HUM_PROCESS", "")
    operation = environ.get("HUM_OPERATION", "")
    if not project or not Path(project).is_absolute() or not name:
        raise PluginError("The Hum pane is missing its absolute project scope or process name; reopen the action.")
    if operation == "start-attach":
        started = runner(exact_command(hum, project, "start", name), check=False)
        if started.returncode != 0:
            return started.returncode
        operation = "attach"
    command = exact_command(hum, project, operation, name)
    os.execvp(command[0], command)
    return 1


def main() -> int:
    if len(sys.argv) != 2:
        raise PluginError("Usage: hum_herdr.py open-picker|picker|process")
    command = sys.argv[1]
    if command == "open-picker":
        return open_picker(os.environ)
    if command == "picker":
        return run_picker(os.environ)
    if command == "process":
        return run_process(os.environ)
    raise PluginError(f"Unknown plugin command: {command}")


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        raise SystemExit(0)
    except PluginError as error:
        print(f"Hum plugin: {error}", file=sys.stderr)
        raise SystemExit(1)
