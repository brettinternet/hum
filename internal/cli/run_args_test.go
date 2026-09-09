package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	urfavecli "github.com/urfave/cli/v3"
	"hum/internal/daemon"
)

type runArgsResult struct {
	name   string
	argv   []string
	detach bool
	json   bool
	err    error
}

func parseRunArgsFor(t *testing.T, args ...string) runArgsResult {
	t.Helper()
	root := NewRootCommand("test", "test", os.Stdout, os.Stderr)
	SetInvocationArgs(root, append([]string{"hum", "run"}, args...))
	root.ExitErrHandler = func(context.Context, *urfavecli.Command, error) {}
	var result runArgsResult
	for _, command := range root.Commands {
		if command.Name != "run" {
			continue
		}
		command.Action = func(_ context.Context, cmd *urfavecli.Command) error {
			result.name, result.argv, result.err = parseRunArgs(cmd)
			result.detach = cmd.Bool("detach")
			result.json = cmd.Bool("json")
			return nil
		}
	}
	if err := root.Run(context.Background(), append([]string{"hum", "run"}, args...)); err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return result
}

func TestParseRunArgsRejectsCommandWithoutName(t *testing.T) {
	got := parseRunArgsFor(t, "--", "npm", "run", "dev")
	if got.err == nil || !strings.Contains(got.err.Error(), "run requires a process name before --") {
		t.Fatalf("run -- npm run dev error = %v, want missing name guidance", got.err)
	}

	got = parseRunArgsFor(t, "--detach", "--", "npm", "run", "dev")
	if got.err == nil || !strings.Contains(got.err.Error(), "run requires a process name before --") {
		t.Fatalf("run --detach -- npm run dev error = %v, want missing name guidance", got.err)
	}
}

func TestParseRunArgsAcceptsOptionsAfterName(t *testing.T) {
	got := parseRunArgsFor(t, "web", "--detach")
	if got.err != nil || got.name != "web" || got.argv != nil || !got.detach {
		t.Fatalf("run web --detach = %+v, want detached resolved run", got)
	}
	got = parseRunArgsFor(t, "web", "-d", "--json")
	if got.err != nil || got.name != "web" || !got.detach || !got.json {
		t.Fatalf("run web -d --json = %+v, want detach and json set", got)
	}
	got = parseRunArgsFor(t, "web", "--json", "--", "sleep", "1")
	if got.err != nil || got.name != "web" || !got.json || strings.Join(got.argv, " ") != "sleep 1" {
		t.Fatalf("run web --json -- sleep 1 = %+v", got)
	}
	got = parseRunArgsFor(t, "web", "--project", ".", "--", "sleep", "1")
	if got.err != nil || got.name != "web" || strings.Join(got.argv, " ") != "sleep 1" {
		t.Fatalf("run web --project . -- sleep 1 = %+v", got)
	}
	got = parseRunArgsFor(t, "web", "-C", ".", "--", "sleep", "1")
	if got.err != nil || got.name != "web" || strings.Join(got.argv, " ") != "sleep 1" {
		t.Fatalf("run web -C . -- sleep 1 = %+v", got)
	}
	got = parseRunArgsFor(t, "web", "--detach", "sleep")
	if got.err == nil || !strings.Contains(got.err.Error(), "run requires -- before the command") {
		t.Fatalf("run web --detach sleep error = %v, want separator guidance", got.err)
	}
	got = parseRunArgsFor(t, "web", "--project")
	if got.err == nil || !strings.Contains(got.err.Error(), "--project requires a value") {
		t.Fatalf("run web --project error = %v", got.err)
	}
	got = parseRunArgsFor(t, "web", "--tty")
	if got.err == nil || !strings.Contains(got.err.Error(), "--tty requires an ad-hoc command after --") {
		t.Fatalf("run web --tty error = %v", got.err)
	}
	got = parseRunArgsFor(t, "web", "--bogus")
	if got.err == nil || !strings.Contains(got.err.Error(), `unknown run option "--bogus"`) {
		t.Fatalf("run web --bogus error = %v", got.err)
	}
}

func TestRunSelectionSemantics(t *testing.T) {
	runtimeDir := cliServeRunRuntimeDir(t)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	cliServeRunStartDaemon(t, runtimeDir)

	missing := cliServeRunStartClient(t, "run", "missing")
	if err := missing.wait(5 * time.Second); cliServeRunExitCode(err) != 1 || !strings.Contains(missing.stderr(), "run missing requires a command after --") {
		t.Fatalf("unresolved argv-free run = %v (code %d), stderr %q", err, cliServeRunExitCode(err), missing.stderr())
	}
	client, err := daemon.Dial(context.Background(), daemon.NewRuntimePaths(runtimeDir).Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), daemon.GetRequest{Name: "missing", Cwd: cwd}); !isNotFound(err) {
		t.Fatalf("selection failure mutated daemon: %v", err)
	}

	marker := filepath.Join(t.TempDir(), "selection-running")
	if _, _, err := cliServeRunInvokeForTest(cliServeRunWithFixtureArgs([]string{"run", "busy", "--detach"}, "stream", marker)...); err != nil {
		t.Fatal(err)
	}
	if err := cliServeRunWaitForFile(marker + ".started"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"run", "busy"}, {"run", "busy", "--", "/bin/true"}} {
		refused := cliServeRunStartClient(t, args...)
		if err := refused.wait(5 * time.Second); cliServeRunExitCode(err) != 1 || !strings.Contains(refused.stderr(), "busy is already running") || !strings.Contains(refused.stderr(), "hum attach busy") || !strings.Contains(refused.stderr(), "hum stop busy") {
			t.Fatalf("running selection %v = %v (code %d), stderr %q", args, err, cliServeRunExitCode(err), refused.stderr())
		}
	}
	if err := cliServeRunStop(t, "busy"); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte("version: 1\nprocesses:\n  api:\n    argv: [/bin/true]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldwd) }()
	declared := cliServeRunStartClientInDir(t, projectRoot, "run", "api", "--", "/bin/true")
	if err := declared.wait(5 * time.Second); cliServeRunExitCode(err) != 1 || !strings.Contains(declared.stderr(), "declared in hum.yaml") || !strings.Contains(declared.stderr(), "hum run api") || !strings.Contains(declared.stderr(), "hum start api") {
		t.Fatalf("declared argv run = %v (code %d), stderr %q", err, cliServeRunExitCode(err), declared.stderr())
	}
	for _, args := range [][]string{{"web", "--detach"}, {"web", "-d", "--json"}, {"web", "--project", ".", "--", "sleep", "1"}, {"web", "-C", ".", "--", "sleep", "1"}} {
		parsed := parseRunArgsFor(t, args...)
		if parsed.err != nil || parsed.name != "web" {
			t.Fatalf("flag placement %v = %+v", args, parsed)
		}
	}
}

func TestStatusDeclaredButUnstartedWithDaemon(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	manifest := "version: 1\nprocesses:\n  web:\n    argv: [sleep, \"30\"]\n"
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := stopShutdownRun(t, "status", "web")
	if err != nil {
		t.Fatalf("status of declared but unstarted process: %v (stderr=%q)", err, stderr)
	}
	if !strings.Contains(stdout, "stopped") || !strings.Contains(stdout, "web") {
		t.Fatalf("status stdout = %q, want stopped web", stdout)
	}
	stdout, _, err = stopShutdownRun(t, "status", "web", "--json")
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}
	if got := statusDecodeJSON(t, stdout); got.State != "stopped" || got.Name != "web" {
		t.Fatalf("status --json = %+v, want stopped web", got)
	}

	_, _, err = stopShutdownRun(t, "status", "ghost")
	if err == nil || !strings.Contains(err.Error(), "Run hum list --all to see every scope.") || !isNotFound(err) {
		t.Fatalf("status of unknown name error = %v, want typed not-found with guidance", err)
	}
}
