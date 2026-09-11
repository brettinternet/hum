package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/protocol"
)

func TestGlobalScopeSelection(t *testing.T) {
	t.Parallel()
	root := NewRootCommand("test", "test", &bytes.Buffer{}, &bytes.Buffer{})
	SetInvocationArgs(root, []string{"hum", "run", "--global", "proxy", "--detach", "--", "echo"})
	if !rawScopeFlag(root, "global", "g") {
		t.Fatal("global invocation flag was not retained after run name")
	}
	// A selector spelling that the parser consumes as another flag's value must
	// not re-scope the command.
	for _, probe := range []struct {
		args []string
		want bool
	}{
		{[]string{"hum", "wait", "api", "--match", "-g", "--timeout", "1s"}, false},
		{[]string{"hum", "logs", "api", "--match", "--global"}, false},
		{[]string{"hum", "wait", "api", "--global", "--match", "ready"}, true},
		{[]string{"hum", "run", "api", "-g", "--detach", "--", "echo", "-g"}, true},
	} {
		command := NewRootCommand("test", "test", &bytes.Buffer{}, &bytes.Buffer{})
		SetInvocationArgs(command, probe.args)
		if got := rawScopeFlag(command, "global", "g"); got != probe.want {
			t.Errorf("rawScopeFlag(%v) = %v, want %v", probe.args, got, probe.want)
		}
	}
	wantCommands := map[string]bool{"run": true, "start": true, "down": true, "list": true, "status": true, "attach": true, "logs": true, "wait": true, "input": true, "signal": true, "restart": true, "stop": true, "remove": true}
	for _, command := range root.Commands {
		if !wantCommands[command.Name] {
			continue
		}
		found := false
		for _, flag := range cliCommandFlags(command) {
			found = found || strings.Join(flag.Names(), ",") == "global,g"
		}
		if !found {
			t.Errorf("%s lacks --global/-g", command.Name)
		}
		delete(wantCommands, command.Name)
	}
	if len(wantCommands) != 0 {
		t.Fatalf("commands missing global selection: %v", wantCommands)
	}
	for _, args := range [][]string{
		{"hum", "list", "--global", "--all"},
		{"hum", "--global", "list", "--all"},
		{"hum", "--global", "--project", ".", "status"},
		{"hum", "--global", "init"},
		{"hum", "init", "--global"},
		{"hum", "--global", "up"},
		{"hum", "up", "-g"},
	} {
		command := NewRootCommand("test", "test", &bytes.Buffer{}, &bytes.Buffer{})
		SetInvocationArgs(command, args)
		if err := command.Run(context.Background(), args); err == nil || !strings.Contains(err.Error(), "global") {
			t.Fatalf("%v error=%v", args, err)
		}
	}
}

func TestGlobalScopeDiscovery(t *testing.T) {
	t.Parallel()
	description := NewRootCommand("test", "test", &bytes.Buffer{}, &bytes.Buffer{}).Description
	for _, phrase := range []string{"--project", "--global", "machine-wide"} {
		if !strings.Contains(description, phrase) {
			t.Fatalf("root help omits %q", phrase)
		}
	}
	processes := []app.Process{{Name: "proxy", Scope: app.ScopeProject, Root: "/project", State: app.StateRunning}, {Name: "proxy", Scope: app.ScopeGlobal, State: app.StateRunning}}
	var human bytes.Buffer
	if err := renderListHuman(&human, processes, true); err != nil {
		t.Fatal(err)
	}
	if output := human.String(); !strings.Contains(output, "Global: (hum --global)") || !strings.Contains(output, "Project: /project") {
		t.Fatalf("all-scope list = %q", output)
	}
	encoded, err := json.Marshal(processListJSON(processes, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"scope":"global"`) || strings.Contains(string(encoded), `"scope":"global","root":"","project_root"`) {
		t.Fatalf("all-scope JSON = %s", encoded)
	}
	err = &daemon.WireError{Code: protocol.ErrorNotFound, Message: "not found", Details: map[string]any{"scope": "project", "project_root": "/project", "other_scopes": []any{map[string]any{"scope": "global"}}}}
	if got := crossScopeNotFoundMessage(err, "logs proxy"); !strings.Contains(got, "hum --global logs proxy") {
		t.Fatalf("global guidance = %q", got)
	}
}

func TestGlobalScopeDocs(t *testing.T) {
	for _, path := range []string{"../../README.md", "../../docs/design.md", "../../docs/coding-agents.md", "../../internal/skill/SKILL.md"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, phrase := range []string{"--global", "-g", "global", "ad-hoc", "list --all", "project_root"} {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s missing %q", path, phrase)
			}
		}
	}
}
