package cli

import (
	"context"
	"os"
	"strings"
	"testing"

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
