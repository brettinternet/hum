package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	urfavecli "github.com/urfave/cli/v3"
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
	got = parseRunArgsFor(t, "web", "--detach", "sleep")
	if got.err == nil || !strings.Contains(got.err.Error(), "run requires -- before the command") {
		t.Fatalf("run web --detach sleep error = %v, want separator guidance", got.err)
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
	if err == nil || !strings.Contains(err.Error(), "Run hum list --all to see known processes.") || !isNotFound(err) {
		t.Fatalf("status of unknown name error = %v, want typed not-found with guidance", err)
	}
}
