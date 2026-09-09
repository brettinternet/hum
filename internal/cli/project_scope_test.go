package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	urfavecli "github.com/urfave/cli/v3"
	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/protocol"
)

func TestProjectScopeSelection(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "worktree")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	oldwd := projectDirChdir(t, parent)
	defer projectDirRestore(t, oldwd)

	var selected projectSelection
	capture := func(args ...string) error {
		commandName := args[0]
		commandRoot := NewRootCommand("test", "test", &strings.Builder{}, &strings.Builder{})
		for _, command := range commandRoot.Commands {
			if command.Name == commandName {
				command.Action = func(_ context.Context, cmd *urfavecli.Command) error {
					var err error
					selected, err = selectedProjectDirectory(cmd)
					return err
				}
			}
		}
		return commandRoot.Run(context.Background(), append([]string{"hum"}, args...))
	}
	if err := capture("list", "--project", "alias"); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	physicalParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		t.Fatal(err)
	}
	lexicalAlias := filepath.Join(physicalParent, "alias")
	if selected.root != canonical || selected.cwd != lexicalAlias || selected.selector != "--project "+canonical {
		t.Fatalf("selection = %#v, canonical=%q alias=%q selector=%q", selected, canonical, alias, "--project "+canonical)
	}

	removed := filepath.Join(parent, "removed")
	if err := capture("list", "--project", removed); err != nil {
		t.Fatalf("removed observation: %v", err)
	}
	lexicalRemoved := removed
	canonicalRemoved := filepath.Join(physicalParent, "removed")
	if selected.cwd != lexicalRemoved || selected.root != canonicalRemoved {
		t.Fatalf("removed selection = %#v", selected)
	}
	launches := [][]string{
		{"run", "--project", removed, "name", "--", "echo"},
		{"start", "--project", removed, "name"},
		{"restart", "--project", removed, "name"},
		{"up", "--project", removed},
		{"init", "--project", removed},
	}
	for _, args := range launches {
		if err := capture(args...); err == nil || !strings.Contains(err.Error(), "directory") {
			t.Fatalf("removed launch %v error = %v, want directory validation", args, err)
		}
	}
	testRemovedWorktreeScopeTargeting(t)
	t.Run("project lifecycle and lexical cwd", testProjectDirFlag)
	t.Run("current-scope name completion", testNameCompletion)
}

func TestCrossWorktreeScopeDiscovery(t *testing.T) {
	processes := []app.Process{
		{Name: "web", Scope: "project", Root: "/work/main", State: app.StateRunning},
		{Name: "web", Scope: "project", Root: "/work/linked", State: app.StateRunning},
	}
	var human bytes.Buffer
	if err := renderListHuman(&human, processes, true); err != nil {
		t.Fatal(err)
	}
	output := human.String()
	for _, want := range []string{"Project: /work/linked (hum --project /work/linked)\n", "Project: /work/main (hum --project /work/main)\n"} {
		if !strings.Contains(output, want) {
			t.Fatalf("list --all output missing %q: %s", want, output)
		}
	}
	if strings.Contains(output, `\\n`) {
		t.Fatalf("list --all rendered a literal newline escape: %q", output)
	}
	encoded, err := json.Marshal(processListJSON(processes, nil))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Processes []map[string]any `json:"processes"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Processes) != 2 || decoded.Processes[0]["scope"] != "project" || decoded.Processes[0]["project_root"] == nil {
		t.Fatalf("JSON scope fields = %#v", decoded.Processes)
	}
	wire := &daemon.WireError{Code: protocol.ErrorNotFound, Message: "not_found: web in /work/linked", Details: map[string]any{"scope": "project", "project_root": "/work/linked", "other_scopes": []any{map[string]any{"scope": "project", "project_root": "/work/main"}}}}
	guidance := crossScopeNotFoundMessage(wire, "logs web")
	if !strings.Contains(guidance, "hum --project /work/main logs web") || !strings.Contains(guidance, "hum list --all") {
		t.Fatalf("cross-scope guidance = %q", guidance)
	}

	server, runtimeDir := stopShutdownTestServer(t, 50*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	projectsDir := t.TempDir()
	mainRoot := filepath.Join(projectsDir, "main")
	linkedRoot := filepath.Join(projectsDir, "linked")
	if err := os.Mkdir(mainRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", mainRoot},
		{"-C", mainRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial"},
		{"-C", mainRoot, "worktree", "add", "-b", "linked", linkedRoot},
	} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	mainRoot, err = filepath.EvalSymlinks(mainRoot)
	if err != nil {
		t.Fatal(err)
	}
	linkedRoot, err = filepath.EvalSymlinks(linkedRoot)
	if err != nil {
		t.Fatal(err)
	}
	client, err := daemon.Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Start(context.Background(), daemon.StartRequest{Name: "web", Root: mainRoot, Cwd: mainRoot, Argv: []string{"/bin/sh", "-c", "sleep 30"}}); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	stdout, _, err := stopShutdownRun(t, "list", "--project", linkedRoot)
	if err != nil || !strings.Contains(stdout, "Nothing is running in "+linkedRoot) || !strings.Contains(stdout, "hum list --all") {
		t.Fatalf("empty linked scope guidance: stdout=%q err=%v", stdout, err)
	}
	stdout, _, err = stopShutdownRun(t, "list", "--project", linkedRoot, "--json")
	if err != nil || strings.Contains(stdout, `"name":"web"`) {
		t.Fatalf("default linked scope leaked main record: stdout=%q err=%v", stdout, err)
	}
	stdout, _, err = stopShutdownRun(t, "list", "--all", "--project", linkedRoot, "--json")
	if err != nil || !strings.Contains(stdout, `"name":"web"`) || !strings.Contains(stdout, `"scope":"project"`) || !strings.Contains(stdout, `"project_root":"`+mainRoot+`"`) {
		t.Fatalf("list --all discovery: stdout=%q err=%v", stdout, err)
	}
	stdout, _, err = stopShutdownRun(t, "status", "-C", mainRoot, "web", "--json")
	if err != nil || !strings.Contains(stdout, `"name":"web"`) {
		t.Fatalf("-C cross-scope access: stdout=%q err=%v", stdout, err)
	}
	guidanceCases := []struct {
		args   []string
		action string
	}{
		{[]string{"logs", "--project", linkedRoot, "web"}, "logs web"},
		{[]string{"wait", "--project", linkedRoot, "web", "--after-cursor", "0", "--timeout", "10ms"}, "wait web"},
		{[]string{"remove", "--project", linkedRoot, "web"}, "remove web"},
	}
	for _, test := range guidanceCases {
		_, _, err = stopShutdownRun(t, test.args...)
		want := "hum --project " + mainRoot + " " + test.action
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "hum list --all") {
			t.Fatalf("%v cross-scope guidance = %v, want %q", test.args, err, want)
		}
	}
	check, err := daemon.Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	if _, err := check.Get(context.Background(), daemon.GetRequest{Name: "web", Cwd: mainRoot}); err != nil {
		t.Fatalf("guided local miss mutated main scope: %v", err)
	}
}

func TestScopeDocs(t *testing.T) {
	for _, path := range []string{"../../README.md", "../../docs/design.md", "../../docs/coding-agents.md", "../../internal/skill/SKILL.md"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, phrase := range []string{"canonical", "symlink", "worktree", "--project", "-C", "removed", "list --all", "scope", "project_root"} {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s missing %q", path, phrase)
			}
		}
	}
}

func TestRemovedWorktreeScopeTargeting(t *testing.T) {
	testRemovedWorktreeScopeTargeting(t)
}

func testRemovedWorktreeScopeTargeting(t *testing.T) {
	t.Helper()
	server, runtimeDir := stopShutdownTestServer(t, 50*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	root := filepath.Join(t.TempDir(), "removed-worktree")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	client, err := daemon.Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Start(context.Background(), daemon.StartRequest{Name: "web", Root: root, Cwd: root, Argv: []string{"/bin/sh", "-c", "sleep 30"}}); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if stdout, _, err := stopShutdownRun(t, "status", "--project", root, "web", "--json"); err != nil || !strings.Contains(stdout, `"project_root":"`+canonical+`"`) {
		t.Fatalf("status removed worktree: stdout=%q err=%v", stdout, err)
	}
	if _, _, err := stopShutdownRun(t, "stop", "--project", root, "web"); err != nil {
		t.Fatalf("stop removed worktree: %v", err)
	}
	if _, _, err := stopShutdownRun(t, "remove", "--project", root, "web"); err != nil {
		t.Fatalf("remove removed worktree: %v", err)
	}
}
