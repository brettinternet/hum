package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"hum/internal/skill"

	urfavecli "github.com/urfave/cli/v3"
)

func TestREADMEQuickstartStructure(t *testing.T) {
	content, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	readme := string(content)

	headings := markdownH2Positions(readme)
	install, installOK := headings["Install"]
	quickstart, quickstartOK := headings["Quickstart"]
	codingAgents, codingAgentsOK := headings["Coding agents"]
	if !installOK || !quickstartOK || !codingAgentsOK {
		t.Fatalf("README.md heading positions: Install=%d Quickstart=%d Coding agents=%d", install, quickstart, codingAgents)
	}
	if !(install < quickstart && quickstart < codingAgents) {
		t.Errorf("README.md headings out of order: Install=%d Quickstart=%d Coding agents=%d", install, quickstart, codingAgents)
	}

	quickstartEnd := len(readme)
	for _, position := range headings {
		if position > quickstart && position < quickstartEnd {
			quickstartEnd = position
		}
	}
	if quickstartEnd == len(readme) {
		t.Fatal("README.md Quickstart has no following top-level section")
	}
}

func markdownH2Positions(markdown string) map[string]int {
	positions := make(map[string]int)
	inFence := false
	position := 0
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		} else if !inFence && strings.HasPrefix(line, "## ") && !strings.HasPrefix(line, "### ") {
			positions[strings.TrimSpace(strings.TrimPrefix(line, "## "))] = position
		}
		position += len(line) + 1
	}
	return positions
}

func TestMarkdownH2PositionsIgnoresFences(t *testing.T) {
	markdown := "```md\n## Install\n```\n## Quickstart\n~~~\n## Coding agents\n~~~\n"
	got := markdownH2Positions(markdown)
	if _, ok := got["Install"]; ok {
		t.Error("fenced Install heading was treated as a section")
	}
	if _, ok := got["Coding agents"]; ok {
		t.Error("fenced Coding agents heading was treated as a section")
	}
	if _, ok := got["Quickstart"]; !ok {
		t.Error("unfenced Quickstart heading was not found")
	}
}

func TestMCPHelpKeepsWireSchemaOutOfGuidance(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)
	if err := root.Run(context.Background(), []string{"hum", "mcp", "--help"}); err != nil {
		t.Fatalf("mcp help: %v", err)
	}
	help := output.String()
	for _, unwanted := range []string{"project_root", "ad_hoc", "-32001", "-32600", "-32800"} {
		if strings.Contains(help, unwanted) {
			t.Errorf("mcp help exposes wire detail %q: %q", unwanted, help)
		}
	}
	if !strings.Contains(help, "13") || !strings.Contains(help, "events") || !strings.Contains(help, "Examples:") || !strings.Contains(help, "hum mcp") {
		t.Errorf("mcp help lacks purpose or example: %q", help)
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("mcp help stderr = %q", errorOutput.String())
	}
}

func TestStatusAndWaitSurface(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)

	want := map[string]bool{
		"version":  true,
		"serve":    true,
		"doctor":   true,
		"init":     true,
		"mcp":      true,
		"skill":    true,
		"run":      true,
		"start":    true,
		"up":       true,
		"down":     true,
		"list":     true,
		"events":   true,
		"status":   true,
		"attach":   true,
		"logs":     true,
		"wait":     true,
		"input":    true,
		"restart":  true,
		"stop":     true,
		"remove":   true,
		"signal":   true,
		"shutdown": true,
	}
	got := make(map[string]bool, len(root.Commands))
	for _, command := range root.Commands {
		if command == nil {
			continue
		}
		got[command.Name] = true
	}
	if len(got) != len(want) {
		t.Fatalf("root command names = %v, want exactly %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("root command is missing %q", name)
		}
	}
}

func TestHelpAdvertisesOnlySupportedScopeFlags(t *testing.T) {
	tests := []struct {
		name        string
		wantProject bool
		wantFile    bool
		wantGlobal  bool
	}{
		{name: "version"},
		{name: "serve"},
		{name: "mcp"},
		{name: "skill"},
		{name: "shutdown"},
		{name: "completion"},
		{name: "doctor", wantProject: true, wantFile: true},
		{name: "init", wantProject: true, wantFile: true},
		{name: "up", wantProject: true, wantFile: true},
		{name: "run", wantProject: true, wantFile: true, wantGlobal: true},
		{name: "start", wantProject: true, wantFile: true, wantGlobal: true},
		{name: "down", wantProject: true, wantFile: true, wantGlobal: true},
		{name: "list", wantProject: true, wantFile: true, wantGlobal: true},
		{name: "status", wantProject: true, wantFile: true, wantGlobal: true},
		{name: "attach", wantProject: true, wantFile: true, wantGlobal: true},
		{name: "logs", wantProject: true, wantFile: true, wantGlobal: true},
		{name: "wait", wantProject: true, wantFile: true, wantGlobal: true},
		{name: "input", wantProject: true, wantFile: true, wantGlobal: true},
		{name: "restart", wantProject: true, wantFile: true, wantGlobal: true},
		{name: "signal", wantProject: true, wantFile: true, wantGlobal: true},
		{name: "stop", wantProject: true, wantFile: true, wantGlobal: true},
		{name: "remove", wantProject: true, wantFile: true, wantGlobal: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			help := renderHelp(t, tt.name)
			if got := strings.Contains(help, "--project"); got != tt.wantProject {
				t.Errorf("--project advertised = %t, want %t\n%s", got, tt.wantProject, help)
			}
			if got := strings.Contains(help, "--file"); got != tt.wantFile {
				t.Errorf("--file advertised = %t, want %t\n%s", got, tt.wantFile, help)
			}
			if got := strings.Contains(help, "--global"); got != tt.wantGlobal {
				t.Errorf("--global advertised = %t, want %t\n%s", got, tt.wantGlobal, help)
			}
		})
	}
}

func renderHelp(t *testing.T, command string) string {
	t.Helper()
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)
	args := []string{"hum"}
	if command != "" {
		args = append(args, command)
	}
	args = append(args, "--help")
	if err := root.Run(context.Background(), args); err != nil {
		t.Fatalf("help: %v", err)
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("help stderr = %q", errorOutput.String())
	}
	return output.String()
}

func TestAttachSurface(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)
	attach := root.Command("attach")
	if attach == nil || attach.Name != "attach" {
		t.Fatalf("attach command is missing: %#v", attach)
	}
	if logs := root.Command("logs"); logs == nil || logs == attach {
		t.Fatalf("attach must be distinct from logs: attach=%#v logs=%#v", attach, logs)
	}
	if err := root.Run(context.Background(), []string{"hum", "attach", "--help"}); err != nil {
		t.Fatalf("attach help: %v", err)
	}
	help := strings.ToLower(output.String())
	for _, unwanted := range []string{"--stream", "--json", "--match", "--limit-bytes", "--after-cursor"} {
		if strings.Contains(help, unwanted) {
			t.Errorf("attach help advertises unsupported flag %q: %q", unwanted, output.String())
		}
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("attach help stderr = %q", errorOutput.String())
	}
}

func TestSkillCommand(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)

	if err := root.Run(context.Background(), []string{"hum", "skill"}); err != nil {
		t.Fatalf("skill: %v", err)
	}
	if got, want := output.String(), skill.Content(); got != want {
		t.Fatalf("skill output differs from embedded content:\n got %q\nwant %q", got, want)
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errorOutput.String())
	}
}

func TestSkillRejectsPositionalArgs(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)

	err := root.Run(context.Background(), []string{"hum", "skill", "extra"})
	if err == nil || !strings.Contains(err.Error(), "skill accepts no positional arguments") {
		t.Fatalf("skill with positional argument error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("skill wrote output after rejecting argument: %q", output.String())
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errorOutput.String())
	}
}

func TestSkillPropagatesWriteErrors(t *testing.T) {
	wantErr := errors.New("skill write failed")
	var errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", skillErrorWriter{err: wantErr}, &errorOutput)

	if err := root.Run(context.Background(), []string{"hum", "skill"}); !errors.Is(err, wantErr) {
		t.Fatalf("skill write error = %v, want %v", err, wantErr)
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errorOutput.String())
	}
}

func TestSkillHelp(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)

	if err := root.Run(context.Background(), []string{"hum", "skill", "--help"}); err != nil {
		t.Fatalf("skill help: %v", err)
	}
	help := strings.ToLower(output.String())
	for _, want := range []string{"instructions", "coding agents", "shell", "mcp", "hum mcp"} {
		if !strings.Contains(help, want) {
			t.Errorf("skill help missing %q: %q", want, output.String())
		}
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errorOutput.String())
	}
}

func TestDocsReferenceRealCommandsAndFlags(t *testing.T) {
	root := NewRootCommand("dev", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	type commandSurface struct {
		flags map[string]bool
	}
	commands := make(map[string]commandSurface)
	for _, command := range root.Commands {
		if command == nil {
			continue
		}
		flags := make(map[string]bool)
		for _, flag := range command.Flags {
			for _, name := range flag.Names() {
				flags[name] = true
			}
		}
		for _, flag := range command.VisibleFlags() {
			for _, name := range flag.Names() {
				flags[name] = true
			}
		}
		surface := commandSurface{flags: flags}
		commands[command.Name] = surface
		for _, alias := range command.Aliases {
			commands[alias] = surface
		}
	}
	// urfave/cli installs the shell completion command during command setup.
	commands["completion"] = commandSurface{flags: map[string]bool{}}
	rootFlags := make(map[string]bool)
	rootValueFlags := make(map[string]bool)
	for _, flag := range root.Flags {
		valued, _ := flag.(urfavecli.DocGenerationFlag)
		for _, name := range flag.Names() {
			rootFlags[name] = true
			rootValueFlags[name] = valued != nil && valued.TakesValue()
		}
	}
	for _, flag := range root.VisibleFlags() {
		for _, name := range flag.Names() {
			rootFlags[name] = true
		}
	}

	documents := []struct {
		name    string
		content string
	}{
		{name: "embedded skill", content: skill.Content()},
		{name: "plugins/hum/skills/hum/SKILL.md", content: readDocsSurfaceFile(t, "../../plugins/hum/skills/hum/SKILL.md")},
		{name: "README.md", content: readDocsSurfaceFile(t, "../../README.md")},
		{name: "docs/design.md", content: readDocsSurfaceFile(t, "../../docs/design.md")},
		{name: "docs/coding-agents.md", content: readDocsSurfaceFile(t, "../../docs/coding-agents.md")},
	}
	exampleDocs, err := filepath.Glob("../../examples/*/README.md")
	if err != nil || len(exampleDocs) == 0 {
		t.Fatalf("example READMEs = %v, %v", exampleDocs, err)
	}
	for _, path := range append([]string{"../../examples/README.md"}, exampleDocs...) {
		documents = append(documents, struct {
			name    string
			content string
		}{name: strings.TrimPrefix(path, "../../"), content: readDocsSurfaceFile(t, path)})
	}
	inline := regexp.MustCompile("`([^`\\n]+)`")
	// A trailing hyphen stays in the captured flag so `--detach-` is rejected.
	flagReference := regexp.MustCompile(`--([a-z][a-z0-9-]*)`)
	for _, document := range documents {
		for _, unit := range markdownCommandUnits(document.content, inline) {
			commandMatches := docCommandReferences(unit, rootFlags, rootValueFlags)
			for _, match := range commandMatches {
				if _, ok := commands[match.name]; !ok {
					t.Errorf("%s references missing root command %q in %q", document.name, match.name, unit)
				}
			}
			activeCommand := ""
			commandIndex := 0
			for _, match := range flagReference.FindAllStringSubmatchIndex(unit, -1) {
				for commandIndex < len(commandMatches) && commandMatches[commandIndex].offset < match[0] {
					activeCommand = commandMatches[commandIndex].name
					commandIndex++
				}
				flag := unit[match[2]:match[3]]
				if rootFlags[flag] {
					continue
				}
				if activeCommand == "" && len(commandMatches) > 0 {
					activeCommand = commandMatches[0].name
				}
				if activeCommand == "" {
					continue
				}
				command, ok := commands[activeCommand]
				if ok && !command.flags[flag] {
					t.Errorf("%s flag --%s is not supported by hum %s in %q", document.name, flag, activeCommand, unit)
				}
			}
		}
	}
}

func TestDocsCoverEveryCommand(t *testing.T) {
	design := readDocsSurfaceFile(t, "../../docs/design.md")
	root := NewRootCommand("dev", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	var visibleCommands []string
	completionVisible := false
	for _, command := range root.Commands {
		if command == nil || command.Hidden {
			continue
		}
		visibleCommands = append(visibleCommands, command.Name)
		completionVisible = completionVisible || command.Name == "completion"
	}
	// urfave/cli installs the visible shell completion command during setup.
	if !completionVisible {
		visibleCommands = append(visibleCommands, "completion")
	}
	for _, name := range visibleCommands {
		reference := regexp.MustCompile(`\bhum\s+` + regexp.QuoteMeta(name) + `\b`)
		if !reference.MatchString(design) {
			t.Errorf("docs/design.md does not reference visible root command %q as hum %s", name, name)
		}
	}
}

type docCommandReference struct {
	name   string
	offset int
}

var (
	docHumInvocation = regexp.MustCompile(`\bhum\s+`)
	docCommandToken  = regexp.MustCompile(`^([a-z][a-z0-9-]*)(?:[^A-Za-z0-9_-].*)?$`)
)

// docCommandReferences finds the command named by each `hum` invocation in
// unit, skipping root selectors such as `--project DIR`, `-C DIR`, and the
// bracketed synopsis form `[--project DIR|-C DIR]`. Invocations whose command
// position holds a placeholder such as COMMAND are ignored.
func docCommandReferences(unit string, rootFlags, rootValueFlags map[string]bool) []docCommandReference {
	var references []docCommandReference
	for _, match := range docHumInvocation.FindAllStringIndex(unit, -1) {
		position := match[1]
		skipValue := false
		for position < len(unit) {
			rest := unit[position:]
			trimmed := strings.TrimLeft(rest, " \t")
			position += len(rest) - len(trimmed)
			if trimmed == "" {
				break
			}
			if strings.HasPrefix(trimmed, "[") {
				end := strings.Index(trimmed, "]")
				if end < 0 {
					break
				}
				position += end + 1
				continue
			}
			token, _, _ := strings.Cut(trimmed, " ")
			if skipValue {
				skipValue = false
				position += len(token)
				continue
			}
			if strings.HasPrefix(token, "-") {
				name, _, hasValue := strings.Cut(strings.TrimLeft(token, "-"), "=")
				if !rootFlags[name] {
					break
				}
				skipValue = rootValueFlags[name] && !hasValue
				position += len(token)
				continue
			}
			if command := docCommandToken.FindStringSubmatch(token); command != nil {
				references = append(references, docCommandReference{name: command[1], offset: position})
			}
			break
		}
	}
	return references
}

func readDocsSurfaceFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func markdownCommandUnits(content string, inline *regexp.Regexp) []string {
	units := make([]string, 0)
	for _, match := range inline.FindAllStringSubmatch(content, -1) {
		units = append(units, match[1])
	}
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence && strings.HasPrefix(trimmed, "hum ") {
			units = append(units, trimmed)
		}
	}
	return units
}

type skillErrorWriter struct {
	err error
}

func (w skillErrorWriter) Write([]byte) (int, error) {
	return 0, w.err
}
