import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import hum_herdr


def completed(command, code=0, stdout="", stderr=""):
    return subprocess.CompletedProcess(command, code, stdout, stderr)


class QueueRunner:
    def __init__(self, *results):
        self.results = list(results)
        self.calls = []

    def __call__(self, command, **kwargs):
        self.calls.append((list(command), kwargs))
        if not self.results:
            raise AssertionError(f"unexpected command: {command}")
        result = self.results.pop(0)
        result.args = command
        return result


class HumHerdrTest(unittest.TestCase):
    def test_capability_discovery_accepts_version_one(self):
        runner = QueueRunner(completed([], stdout='{"schema_version":1,"version":"1.2.3","build_time":"now"}\n'))
        value = hum_herdr.verify_capability("/opt/hum", runner)
        self.assertEqual(value["version"], "1.2.3")
        self.assertEqual(runner.calls[0][0], ["/opt/hum", "version", "--json"])

    def test_capability_discovery_reports_unsupported_version(self):
        runner = QueueRunner(completed([], stdout='{"schema_version":2}\n'))
        with self.assertRaisesRegex(hum_herdr.PluginError, "Unsupported Hum machine-output version"):
            hum_herdr.verify_capability(runner=runner)

    def test_capability_discovery_reports_unavailable_hum(self):
        def missing(command, **kwargs):
            raise FileNotFoundError(command[0])

        with self.assertRaisesRegex(hum_herdr.PluginError, "Install hum.*Herdr's PATH"):
            hum_herdr.verify_capability(runner=missing)

    def test_workspace_comes_from_herdr_context(self):
        with tempfile.TemporaryDirectory(prefix="hum plugin space ") as workspace:
            raw = json.dumps({"workspace_cwd": workspace, "focused_pane_cwd": "/ignored"})
            self.assertEqual(hum_herdr.workspace_from_context(raw), str(Path(workspace).resolve()))

    def test_workspace_requires_a_selected_project(self):
        with self.assertRaisesRegex(hum_herdr.PluginError, "No Herdr workspace project is selected"):
            hum_herdr.workspace_from_context("{}")

    def test_list_discovery_preserves_canonical_project_root(self):
        workspace = "/tmp/project alias"
        canonical = "/private/tmp/project & canonical"
        processes = [
            {"name": "web $(touch nope)", "state": "running", "project_root": canonical},
            {"name": "worker; echo nope", "state": "stopped", "project_root": canonical},
        ]
        runner = QueueRunner(
            completed([], stdout='{"schema_version":1,"version":"dev","build_time":"unknown"}\n'),
            completed([], stdout=json.dumps({"schema_version": 1, "processes": processes}) + "\n"),
        )
        project, found = hum_herdr.discover_processes(workspace, runner=runner)
        self.assertEqual(project, canonical)
        self.assertEqual(found, processes)
        self.assertEqual(
            runner.calls[1][0],
            ["hum", "--project", str(Path(workspace).resolve()), "list", "--json"],
        )

    def test_empty_project_is_actionable(self):
        runner = QueueRunner(
            completed([], stdout='{"schema_version":1,"version":"dev","build_time":"unknown"}\n'),
            completed([], stdout='{"schema_version":1,"processes":[]}\n'),
        )
        with self.assertRaisesRegex(hum_herdr.PluginError, "No Hum processes.*hum.yaml.*hum --project"):
            hum_herdr.discover_processes("/tmp/empty project", runner=runner)

    def test_running_and_stopped_actions_are_distinct(self):
        running = dict(hum_herdr.operations_for({"name": "web", "state": "running"}))
        stopped = dict(hum_herdr.operations_for({"name": "web", "state": "operator-stopped"}))
        self.assertIn("attach", running)
        self.assertNotIn("start-attach", running)
        self.assertIn("start-attach", stopped)
        self.assertEqual(stopped["start-attach"], "Start/attach")
        self.assertNotIn("attach", stopped)
        self.assertIn("restart", running)
        self.assertNotIn("restart", stopped)
        self.assertEqual(
            hum_herdr.operation_for_mode({"state": "running"}, "start-attach"),
            "attach",
        )
        self.assertEqual(
            hum_herdr.operation_for_mode({"state": "operator-stopped"}, "start-attach"),
            "start-attach",
        )

    def test_exact_argv_never_shell_interpolates_names_or_paths(self):
        project = "/tmp/project $(echo unsafe)"
        name = "api; touch /tmp/nope"
        expected = {
            "logs": ["hum", "--project", project, "logs", name, "--follow"],
            "attach": ["hum", "--project", project, "attach", name],
            "start": ["hum", "--project", project, "start", name],
            "stop": ["hum", "--project", project, "stop", name],
            "restart": ["hum", "--project", project, "restart", name],
            "remove": ["hum", "--project", project, "remove", name],
        }
        for operation, argv in expected.items():
            with self.subTest(operation=operation):
                self.assertEqual(hum_herdr.exact_command("hum", project, operation, name), argv)

    def test_process_pane_arguments_preserve_metacharacters(self):
        project = "/tmp/project with spaces; echo no"
        name = "web $(echo no)"
        command = hum_herdr.pane_open_command("/opt/herdr", "process", project, name, "logs", "w9")
        self.assertEqual(command[0], "/opt/herdr")
        self.assertIn(f"HUM_PROJECT={project}", command)
        self.assertIn(f"HUM_PROCESS={name}", command)
        self.assertIn("HUM_OPERATION=logs", command)
        self.assertEqual(command[-2:], ["--workspace", "w9"])

    def test_open_picker_passes_selected_workspace_and_mode(self):
        with tempfile.TemporaryDirectory(prefix="selected project ") as workspace:
            runner = QueueRunner(completed([]))
            environment = {
                "HERDR_PLUGIN_CONTEXT_JSON": json.dumps({"workspace_cwd": workspace}),
                "HERDR_PLUGIN_ACTION_ID": "restart",
                "HERDR_BIN_PATH": "/opt/herdr",
                "HERDR_WORKSPACE_ID": "w3",
                "HUM_BIN_PATH": "/opt/hum",
            }
            self.assertEqual(hum_herdr.open_picker(environment, runner), 0)
            command = runner.calls[0][0]
            self.assertIn(f"HUM_PICKER_MODE=restart", command)
            self.assertIn(f"HUM_BIN_PATH=/opt/hum", command)
            self.assertIn(str(Path(workspace).resolve()), command)

    def test_start_attach_starts_then_executes_non_starting_attach(self):
        environment = {
            "HUM_BIN_PATH": "/opt/hum",
            "HUM_PROJECT": "/tmp/project with spaces",
            "HUM_PROCESS": "console; safe",
            "HUM_OPERATION": "start-attach",
        }
        runner = QueueRunner(completed([]))
        with mock.patch("hum_herdr.os.execvp") as execvp:
            hum_herdr.run_process(environment, runner)
        self.assertEqual(
            runner.calls[0][0],
            ["/opt/hum", "--project", environment["HUM_PROJECT"], "start", environment["HUM_PROCESS"]],
        )
        attach = ["/opt/hum", "--project", environment["HUM_PROJECT"], "attach", environment["HUM_PROCESS"]]
        execvp.assert_called_once_with("/opt/hum", attach)


if __name__ == "__main__":
    unittest.main()
