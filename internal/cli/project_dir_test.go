package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	urfavecli "github.com/urfave/cli/v3"
	"hum/internal/daemon"
)

func TestProjectDirFlag(t *testing.T) {
	testProjectDirFlag(t)
}

func testProjectDirFlag(t *testing.T) {
	t.Helper()
	invocation := t.TempDir()
	projectRoot := filepath.Join(invocation, "checkout")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	projectRoot = projectDirCanonical(t, projectRoot)
	oldwd := projectDirChdir(t, invocation)
	defer projectDirRestore(t, oldwd)

	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	stdout, stderr, err := stopShutdownRun(t, "run", "--project", "checkout", "adhoc", "--detach", "--", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("ad-hoc run with relative project: %v", err)
	}
	_ = stdout
	_ = stderr

	client, err := daemon.Dial(context.Background(), daemon.NewRuntimePaths(runtimeDir).Socket)
	if err != nil {
		t.Fatalf("dial project daemon: %v", err)
	}
	defer client.Close()
	adHoc, err := client.Get(context.Background(), daemon.GetRequest{Name: "adhoc", Cwd: projectRoot})
	if err != nil {
		all, listErr := client.List(context.Background(), daemon.ListRequest{Cwd: projectRoot, All: true, IncludeCompleted: true})
		t.Fatalf("get ad-hoc process: %v (run stdout=%q stderr=%q all=%#v listErr=%v)", err, stdout, stderr, all, listErr)
	}
	if adHoc.Cwd != projectRoot {
		t.Fatalf("ad-hoc cwd = %q, want selected directory %q", adHoc.Cwd, projectRoot)
	}
	if err := client.Stop(context.Background(), daemon.StopRequest{Name: "adhoc", Cwd: projectRoot}); err != nil {
		t.Fatalf("stop ad-hoc process: %v", err)
	}

	manifestRoot := filepath.Join(invocation, "manifest-checkout")
	if err := os.MkdirAll(filepath.Join(manifestRoot, ".git", "worktree"), 0o700); err != nil {
		t.Fatalf("create manifest root: %v", err)
	}
	manifestRoot = projectDirCanonical(t, manifestRoot)
	manifestCwd := filepath.Join(manifestRoot, "service")
	if err := os.MkdirAll(manifestCwd, 0o700); err != nil {
		t.Fatalf("create manifest cwd: %v", err)
	}
	manifest := "version: 1\nprocesses:\n  service:\n    argv: [/bin/sh, -c, 'echo ready; sleep 30']\n    cwd: service\n  tty:\n    argv: [/bin/sh, -c, 'read -r line; echo got:$line; sleep 30']\n    tty: true\n"
	if err := os.WriteFile(filepath.Join(manifestRoot, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	stdout, stderr, err = stopShutdownRun(t, "start", "-C", "manifest-checkout", "--no-wait", "--json", "service")
	if err != nil {
		t.Fatalf("manifest start with project alias: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	service, err := client.Get(context.Background(), daemon.GetRequest{Name: "service", Cwd: manifestRoot})
	if err != nil {
		t.Fatalf("get manifest process: %v", err)
	}
	if service.Cwd != manifestCwd {
		t.Fatalf("manifest cwd = %q, want declared cwd %q", service.Cwd, manifestCwd)
	}
	stdout, stderr, err = stopShutdownRun(t, "status", "--project", manifestRoot, "service", "--json")
	if err != nil {
		t.Fatalf("project status: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	var status statusJSON
	if err := json.Unmarshal([]byte(stdout), &status); err != nil || status.ProjectRoot != manifestRoot {
		t.Fatalf("project status = %#v err=%v, want root %q", stdout, err, manifestRoot)
	}
	stdout, stderr, err = stopShutdownRun(t, "logs", "--project", manifestRoot, "service", "--json")
	if err != nil {
		t.Fatalf("project single logs: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	stdout, stderr, err = stopShutdownRun(t, "logs", "--project", manifestRoot, "--json")
	if err != nil {
		t.Fatalf("project aggregate logs: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	stdout, stderr, err = stopShutdownRun(t, "wait", "--project", manifestRoot, "service", "--match", "ready", "--timeout", "2s", "--json")
	if err != nil {
		t.Fatalf("project wait: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	stdout, stderr, err = stopShutdownRun(t, "start", "--project", manifestRoot, "--no-wait", "--json", "tty")
	if err != nil {
		t.Fatalf("project tty start: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	stdout, stderr, err = stopShutdownRun(t, "input", "--project", manifestRoot, "tty", "--text", "hello\n")
	if err != nil {
		t.Fatalf("project input: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	stdout, stderr, err = stopShutdownRun(t, "stop", "-C", manifestRoot, "tty", "--json")
	if err != nil {
		t.Fatalf("project stop: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	stdout, stderr, err = stopShutdownRun(t, "remove", "--project", manifestRoot, "tty", "--json")
	if err != nil {
		t.Fatalf("project remove: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	stdout, stderr, err = stopShutdownRun(t, "restart", "--project", manifestRoot, "service", "--json")
	if err != nil {
		t.Fatalf("project restart: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	changedManifest := "version: 1\nprocesses:\n  service:\n    argv: [/bin/sh, -c, 'sleep 31']\n    cwd: service\n"
	if err := os.WriteFile(filepath.Join(manifestRoot, "hum.yaml"), []byte(changedManifest), 0o600); err != nil {
		t.Fatalf("rewrite manifest: %v", err)
	}
	stdout, stderr, err = stopShutdownRun(t, "up", "--project", manifestRoot, "--json", "--no-wait")
	if err == nil || manifestCLIExitCode(err) != 1 {
		t.Fatalf("project up drift: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	drift := manifestCLILaunchResults(t, stdout)
	wantGuidance := projectCommand("--project "+shellEscape(manifestRoot), "restart service")
	if len(drift) != 1 || drift[0].Guidance != wantGuidance {
		t.Fatalf("project drift guidance = %#v, want %q", drift, wantGuidance)
	}
	stdout, stderr, err = stopShutdownRun(t, "down", "--project", manifestRoot, "--json")
	if err != nil {
		t.Fatalf("project down: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	stdout, stderr, err = stopShutdownRun(t, "remove", "-C", manifestRoot, "service", "--json")
	if err != nil {
		t.Fatalf("project remove service: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}

	stdout, stderr, err = stopShutdownRun(t, "list", "--all", "--project", "manifest-checkout", "--json")
	if err != nil {
		t.Fatalf("list --all with project: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	var listed listJSON
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("decode project list: %v (stdout=%q)", err, stdout)
	}
	found := false
	for _, process := range listed.Processes {
		if process.Name == "service" && process.Root == manifestRoot {
			found = true
			if process.State != "stopped" && process.State != "exited" {
				t.Fatalf("declaration state = %q, want stopped or exited", process.State)
			}
		}
	}
	if !found {
		t.Fatalf("list --all did not merge declaration from %q: %#v", manifestRoot, listed.Processes)
	}

	initRoot := filepath.Join(invocation, "init-checkout")
	if err := os.MkdirAll(filepath.Join(initRoot, ".git"), 0o700); err != nil {
		t.Fatalf("create init root: %v", err)
	}
	stdout, stderr, err = stopShutdownRun(t, "init", "--project", "init-checkout", "--json")
	if err != nil {
		t.Fatalf("init with project: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(initRoot, "hum.yaml")); err != nil {
		t.Fatalf("init did not write selected root: %v", err)
	}
	if stderr != "" {
		t.Fatalf("init stderr = %q, want empty", stderr)
	}
}

func TestProjectDirFlagParsing(t *testing.T) {
	invocation := t.TempDir()
	projectRoot := filepath.Join(invocation, "target")
	if err := os.Mkdir(projectRoot, 0o700); err != nil {
		t.Fatalf("create target: %v", err)
	}
	oldwd := projectDirChdir(t, invocation)
	defer projectDirRestore(t, oldwd)

	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "before command long", args: []string{"--project", "target", "list"}},
		{name: "after command long", args: []string{"list", "--project", "target"}},
		{name: "before command short", args: []string{"-C", "target", "list"}},
		{name: "after command short", args: []string{"list", "-C", "target"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, set, _, _, err := captureProjectDirCommand(t, test.args...)
			if err != nil {
				t.Fatalf("capture %v: %v", test.args, err)
			}
			if !set || value != "target" {
				t.Fatalf("project = %q set=%t, want target and set", value, set)
			}
		})
	}

	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "run before name long", args: []string{"run", "--project", "target", "name", "--", "echo"}},
		{name: "run before name short", args: []string{"run", "-C", "target", "name", "--", "echo"}},
		{name: "run after name long", args: []string{"run", "name", "--project", "target", "--", "echo"}},
		{name: "run after name short", args: []string{"run", "name", "-C", "target", "--", "echo"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, set, name, argv, err := captureProjectDirCommand(t, test.args...)
			if err != nil {
				t.Fatalf("capture %v: %v", test.args, err)
			}
			if !set || value != "target" || name != "name" || len(argv) != 1 || argv[0] != "echo" {
				t.Fatalf("project=%q set=%t name=%q argv=%q, want project target, name, echo", value, set, name, argv)
			}
		})
	}

	filePath := filepath.Join(invocation, "file")
	if err := os.WriteFile(filePath, []byte("file"), 0o600); err != nil {
		t.Fatalf("write file project value: %v", err)
	}
	for _, value := range []string{"", filePath} {
		_, _, err := stopShutdownRun(t, "list", "--project="+value)
		if err == nil {
			t.Fatalf("project value %q unexpectedly succeeded", value)
		}
		if !strings.Contains(err.Error(), "--project") || !strings.Contains(err.Error(), "directory") {
			t.Fatalf("project value %q error = %v, want actionable directory error", value, err)
		}
	}
	_, _, err := stopShutdownRun(t, "list", "--project")
	if err == nil || !strings.Contains(err.Error(), "flag needs an argument") {
		t.Fatalf("missing project value error = %v", err)
	}

	for _, command := range []string{"serve", "shutdown", "mcp", "skill"} {
		_, _, err := stopShutdownRun(t, command, "--project", projectRoot)
		if err == nil || !strings.Contains(err.Error(), "does not accept --project/-C") {
			t.Fatalf("%s project option error = %v", command, err)
		}
	}

	var rootHelp, commandHelp bytes.Buffer
	if err := NewRootCommand("test", "test", &rootHelp, &bytes.Buffer{}).Run(context.Background(), []string{"hum", "--help"}); err != nil {
		t.Fatalf("root help: %v", err)
	}
	if !strings.Contains(rootHelp.String(), "--project") || !strings.Contains(rootHelp.String(), "-C") {
		t.Fatalf("root help omits project selector: %q", rootHelp.String())
	}
	if err := NewRootCommand("test", "test", &commandHelp, &bytes.Buffer{}).Run(context.Background(), []string{"hum", "list", "--help"}); err != nil {
		t.Fatalf("list help: %v", err)
	}
	if !strings.Contains(commandHelp.String(), "--project") || !strings.Contains(commandHelp.String(), "-C") {
		t.Fatalf("list help omits project selector: %q", commandHelp.String())
	}

	value, set, _, _, err := captureProjectDirCommand(t, "serve", "-d")
	if err != nil || set || value != "" {
		t.Fatalf("serve -d capture = value %q set=%t err=%v", value, set, err)
	}
}

func TestProjectDirGuidance(t *testing.T) {
	invocation := t.TempDir()
	projectRoot := filepath.Join(invocation, "checkout with spaces")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	projectRoot = projectDirCanonical(t, projectRoot)
	oldwd := projectDirChdir(t, invocation)
	defer projectDirRestore(t, oldwd)

	stdout, stderr, err := stopShutdownRun(t, "init", "--project", projectRoot, "--json")
	if err != nil {
		t.Fatalf("init guidance: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	var result initJSON
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode init guidance: %v", err)
	}
	selector := "--project " + shellEscape(projectRoot)
	wantNext := projectCommand(selector, "up")
	if result.NextCommand != wantNext {
		t.Fatalf("next command = %q, want %q", result.NextCommand, wantNext)
	}
	if strings.Contains(result.NextCommand, "-C") || !strings.Contains(result.NextCommand, "--project") {
		t.Fatalf("next command is not canonical --project guidance: %q", result.NextCommand)
	}
	_, _, err = stopShutdownRun(t, "init", "--project", projectRoot)
	if err == nil || !strings.Contains(err.Error(), projectCommand(selector, "init")) {
		t.Fatalf("human init guidance = %v, want %q", err, projectCommand(selector, "init"))
	}

	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	manifest := "version: 1\nprocesses:\n  service:\n    argv: [/bin/sh, -c, 'sleep 30']\n"
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write guidance manifest: %v", err)
	}
	stdout, stderr, err = stopShutdownRun(t, "start", "--project", projectRoot, "--no-wait", "--json", "service")
	if err != nil {
		t.Fatalf("guidance manifest start: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	manifest = "version: 1\nprocesses:\n  service:\n    argv: [/bin/sh, -c, 'sleep 31']\n"
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("rewrite guidance manifest: %v", err)
	}
	stdout, stderr, err = stopShutdownRun(t, "up", "--project", projectRoot, "--json", "--no-wait")
	if err == nil || manifestCLIExitCode(err) != 1 {
		t.Fatalf("guidance project drift: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	guidance := manifestCLILaunchResults(t, stdout)
	wantGuidance := projectCommand(selector, "restart service")
	if len(guidance) != 1 || guidance[0].Guidance != wantGuidance {
		t.Fatalf("stable project guidance = %#v, want %q", guidance, wantGuidance)
	}
	if _, _, err := stopShutdownRun(t, "stop", "--project", projectRoot, "service"); err != nil {
		t.Fatalf("stop guidance process: %v", err)
	}

	stdout, _, err = stopShutdownRun(t, "init", "--json")
	if err != nil {
		t.Fatalf("plain init: %v", err)
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode plain init: %v", err)
	}
	if result.NextCommand != initNextCommand {
		t.Fatalf("plain next command = %q, want unchanged %q", result.NextCommand, initNextCommand)
	}
}

func TestProjectDirDocs(t *testing.T) {
	for path, phrases := range map[string][]string{
		"../../README.md":      {"hum --project /path/to/checkout up", "-C DIR", "relative selector", "nearest Git root", "`-d` means"},
		"../../docs/design.md": {"hum [--project DIR|-C DIR]", "nearest-Git-root-or-directory-fallback", "`run` accepts them after the process name", "Command-local `-d`"},
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, phrase := range phrases {
			if !strings.Contains(string(content), phrase) {
				t.Errorf("%s missing %q", path, phrase)
			}
		}
	}
}

func captureProjectDirCommand(t *testing.T, args ...string) (value string, set bool, runName string, runArgv []string, err error) {
	t.Helper()
	root := NewRootCommand("test", "test", &bytes.Buffer{}, &bytes.Buffer{})
	root.ExitErrHandler = func(context.Context, *urfavecli.Command, error) {}
	// Inspect inherited persistent flags without contacting a daemon.
	for _, command := range root.Commands {
		if command.Name != "list" && command.Name != "run" && command.Name != "serve" {
			continue
		}
		command.Action = func(_ context.Context, cmd *urfavecli.Command) error {
			if cmd.Name == "run" {
				runName, runArgv, err = parseRunArgs(cmd)
				if err != nil {
					return err
				}
			}
			value, set = cmd.String("project"), cmd.IsSet("project")
			return nil
		}
	}
	err = root.Run(context.Background(), append([]string{"hum"}, args...))
	return value, set, runName, runArgv, err
}

func projectDirChdir(t *testing.T, dir string) string {
	t.Helper()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	return oldwd
}

func projectDirRestore(t *testing.T, oldwd string) {
	t.Helper()
	if err := os.Chdir(oldwd); err != nil {
		t.Errorf("restore working directory: %v", err)
	}
}

func projectDirCanonical(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve project path %q: %v", path, err)
	}
	return filepath.Clean(resolved)
}
