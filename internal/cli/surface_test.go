package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"hum/internal/skill"
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
	quickstartSection := readme[quickstart:quickstartEnd]
	for _, command := range []string{"hum run", "hum up", "Ctrl+C", "hum down"} {
		if !strings.Contains(quickstartSection, command) {
			t.Errorf("README.md Quickstart missing %q", command)
		}
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

func TestMCPHelpScopeSchema(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)
	if err := root.Run(context.Background(), []string{"hum", "mcp", "--help"}); err != nil {
		t.Fatalf("mcp help: %v", err)
	}
	help := strings.Join(strings.Fields(strings.ToLower(output.String())), " ")
	for _, want := range []string{
		"project scope requires an absolute existing project_root",
		"global scope forbids project_root",
		"up supports project scope only",
		"list all is available only from project scope",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("mcp help missing %q: %q", want, output.String())
		}
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
		"init":     true,
		"mcp":      true,
		"skill":    true,
		"run":      true,
		"start":    true,
		"up":       true,
		"down":     true,
		"list":     true,
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

func TestHelpWordBudgets(t *testing.T) {
	tests := []struct {
		name  string
		limit int
	}{
		{name: "", limit: 250},
		{name: "version", limit: 200},
		{name: "serve", limit: 200},
		{name: "init", limit: 200},
		{name: "mcp", limit: 200},
		{name: "skill", limit: 200},
		{name: "run", limit: 200},
		{name: "start", limit: 200},
		{name: "up", limit: 200},
		{name: "down", limit: 200},
		{name: "list", limit: 200},
		{name: "status", limit: 200},
		{name: "attach", limit: 200},
		{name: "logs", limit: 200},
		{name: "wait", limit: 200},
		{name: "input", limit: 200},
		{name: "restart", limit: 200},
		{name: "signal", limit: 200},
		{name: "stop", limit: 200},
		{name: "remove", limit: 200},
		{name: "shutdown", limit: 200},
		{name: "completion", limit: 200},
	}
	for _, tt := range tests {
		label := tt.name
		if label == "" {
			label = "root"
		}
		t.Run(label, func(t *testing.T) {
			help := renderHelp(t, tt.name)
			if words := len(strings.Fields(help)); words > tt.limit {
				t.Errorf("%s help has %d words, want at most %d\n%s", label, words, tt.limit, help)
			}
		})
	}
}

func TestHelpAdvertisesOnlySupportedScopeFlags(t *testing.T) {
	tests := []struct {
		name        string
		wantProject bool
		wantGlobal  bool
	}{
		{name: "version"},
		{name: "serve"},
		{name: "mcp"},
		{name: "skill"},
		{name: "shutdown"},
		{name: "completion"},
		{name: "init", wantProject: true},
		{name: "up", wantProject: true},
		{name: "run", wantProject: true, wantGlobal: true},
		{name: "start", wantProject: true, wantGlobal: true},
		{name: "down", wantProject: true, wantGlobal: true},
		{name: "list", wantProject: true, wantGlobal: true},
		{name: "status", wantProject: true, wantGlobal: true},
		{name: "attach", wantProject: true, wantGlobal: true},
		{name: "logs", wantProject: true, wantGlobal: true},
		{name: "wait", wantProject: true, wantGlobal: true},
		{name: "input", wantProject: true, wantGlobal: true},
		{name: "restart", wantProject: true, wantGlobal: true},
		{name: "signal", wantProject: true, wantGlobal: true},
		{name: "stop", wantProject: true, wantGlobal: true},
		{name: "remove", wantProject: true, wantGlobal: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			help := renderHelp(t, tt.name)
			if got := strings.Contains(help, "--project"); got != tt.wantProject {
				t.Errorf("--project advertised = %t, want %t\n%s", got, tt.wantProject, help)
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
	for _, want := range []string{"hum attach name", "--tail", "currently running", "without starting", "raw input", "--tail 0", "hum run", "hum logs"} {
		if !strings.Contains(help, strings.ToLower(want)) {
			t.Errorf("attach help missing %q: %q", want, output.String())
		}
	}
	for _, unwanted := range []string{"--stream", "--json", "--match", "--limit-bytes", "--after-cursor"} {
		if strings.Contains(help, unwanted) {
			t.Errorf("attach help advertises unsupported flag %q: %q", unwanted, output.String())
		}
	}
	for path, wants := range map[string][]string{
		"../../README.md":      {"hum attach NAME", "--tail 0", "hum run", "hum logs --follow"},
		"../../docs/design.md": {"hum attach <name>", "--tail 0", "never starts or restarts", "hum run", "| `-n` | `--tail` | `attach`, `logs` |"},
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		content := strings.ToLower(string(data))
		for _, want := range wants {
			if !strings.Contains(content, strings.ToLower(want)) {
				t.Errorf("%s missing %q", path, want)
			}
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
	for _, want := range []string{"shell-only", "fallback", "mcp-capable", "hum mcp"} {
		if !strings.Contains(help, want) {
			t.Errorf("skill help missing %q: %q", want, output.String())
		}
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errorOutput.String())
	}
}

func TestSkillReferencesMatchRootCommandsAndFlags(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)
	content := skill.Content()

	type commandFlag struct {
		command string
		flag    string
	}
	var references []commandFlag
	inline := regexp.MustCompile("`([^`\\n]+)`")
	commandReference := regexp.MustCompile(`\bhum\s+([a-z][a-z0-9-]*)\b`)
	flagReference := regexp.MustCompile(`--([a-z][a-z0-9-]*)\b`)
	commandReferenceCount := 0
	for _, match := range inline.FindAllStringSubmatch(content, -1) {
		command := commandReference.FindStringSubmatch(match[1])
		if len(command) == 0 {
			continue
		}
		commandReferenceCount++
		name := command[1]
		found := false
		for _, command := range root.Commands {
			if command != nil && command.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("skill references missing root command %q", name)
		}
		for _, flag := range flagReference.FindAllStringSubmatch(match[1], -1) {
			references = append(references, commandFlag{command: name, flag: flag[1]})
		}
	}
	if commandReferenceCount == 0 {
		t.Fatal("skill contains no hum command references")
	}

	for _, reference := range references {
		var commandFound bool
		var flagFound bool
		for _, command := range root.Commands {
			if command == nil || command.Name != reference.command {
				continue
			}
			commandFound = true
			for _, flag := range command.Flags {
				for _, name := range flag.Names() {
					if name == reference.flag {
						flagFound = true
					}
				}
			}
			break
		}
		if !commandFound {
			t.Errorf("skill flag --%s references missing root command %q", reference.flag, reference.command)
		} else if !flagFound {
			t.Errorf("skill flag --%s is not a flag on hum %s", reference.flag, reference.command)
		}
	}
	if len(references) == 0 {
		t.Fatal("skill contains no command-scoped flag references")
	}
}

type skillErrorWriter struct {
	err error
}

func (w skillErrorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestWaitHelpDescribesExitAndReadiness(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)

	if err := root.Run(context.Background(), []string{"hum", "wait", "--help"}); err != nil {
		t.Fatalf("wait help: %v", err)
	}
	help := strings.ToLower(output.String())
	for _, want := range []string{"without --match", "process incarnation to exit", "stopped sessions", "next launch", "starting a daemon if needed", "--after-cursor", "--match", "--timeout", "--json", "exit codes"} {
		if !strings.Contains(help, want) {
			t.Errorf("wait help missing %q: %q", want, output.String())
		}
	}
}

func TestLogsAggregateDocs(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)
	if err := root.Run(context.Background(), []string{"hum", "logs", "--help"}); err != nil {
		t.Fatalf("logs help: %v", err)
	}
	help := strings.ToLower(output.String())
	for _, want := range []string{
		"[name...]",
		"named processes",
		"per process",
		"--after-cursor",
		"--follow",
		"without signaling",
		"docs/design.md",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("logs aggregate help missing %q: %q", want, output.String())
		}
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("logs help stderr = %q", errorOutput.String())
	}
	for path, want := range map[string][]string{
		"../../README.md":      {"hum up", "hum logs --follow", "docs/design.md"},
		"../../docs/design.md": {"hum up", "hum logs --follow", "single explicit", "unchanged", "named ndjson", "one follower", "per-session", "daemon loss"},
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		lower := strings.ToLower(string(content))
		for _, phrase := range want {
			if !strings.Contains(lower, phrase) {
				t.Errorf("%s missing %q", path, phrase)
			}
		}
	}
}

func TestLifecycleHelp(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []string
		notWant []string
	}{
		{
			name: "root",
			args: []string{"hum", "--help"},
			want: []string{
				"supervise local development processes",
				"hum up",
				"hum run",
				"hum logs",
				"docs/design.md",
			},
		},
		{
			name: "serve",
			args: []string{"hum", "serve", "--help"},
			want: []string{
				"foreground",
				"diagnostics to stderr",
				"--daemon",
				"detached daemon",
				"waits for readiness",
				"pid and socket",
			},
		},
		{
			name: "init",
			args: []string{"hum", "init", "--help"},
			want: []string{
				"create hum.yaml",
				"strict project discovery",
				"without starting a daemon",
				"single candidate",
				"commented template",
				"--force",
				"--json",
			},
		},
		{
			name: "run",
			args: []string{"hum", "run", "--help"},
			want: []string{
				"automatically starting a detached daemon",
				"without --detach",
				"stream raw child output",
				"ctrl+c stops it",
				"with --detach",
				"return immediately",
				"daemon keeps owning it",
				"ad-hoc commands require --",
			},
		},
		{
			name: "start",
			args: []string{"hum", "start", "--help"},
			want: []string{
				"named sessions",
				"idempotently",
				"hum.yaml or conventional discovery",
				"never pulls in prerequisites",
				"--no-wait",
				"--timeout",
				"--json",
				"docs/design.md",
			},
		},
		{
			name: "up",
			args: []string{"hum", "up", "--help"},
			want: []string{
				"manifest processes",
				"independent roots concurrently",
				"gate dependents on readiness",
				"continue after failures",
				"--no-wait",
				"--timeout",
				"--json",
				"docs/design.md",
			},
		},
		{
			name: "down",
			args: []string{"hum", "down", "--help"},
			want: []string{
				"every process",
				"current project",
				"resolved manifest",
				"ad-hoc",
				"concurrently",
				"not running",
				"idempotent",
				"never starts",
				"shuts down the daemon",
				"--json",
			},
		},
		{
			name: "list",
			args: []string{"hum", "list", "--help"},
			want: []string{
				"read-only",
				"does not start an empty daemon",
				"--all",
				"every project",
				"followers count",
				"unfollowed human output is unchanged",
			},
		},
		{
			name: "status",
			args: []string{"hum", "status", "--help"},
			want: []string{
				"without name",
				"compact table",
				"unlaunched manifest declarations",
				"with name",
				"detailed status",
				"followers",
				"read-only",
				"never starts a daemon",
			},
		},
		{
			name: "logs",
			args: []string{"hum", "logs", "--help"},
			want: []string{
				"bounded retained output",
				"named processes",
				"filters and limits apply per process",
				"--follow",
				"cancels reading",
				"without signaling processes",
				"docs/design.md",
			},
		},
		{
			name: "wait",
			args: []string{"hum", "wait", "--help"},
			want: []string{
				"without --match",
				"one process incarnation",
				"stopped sessions",
				"next launch",
				"starting a daemon if needed",
				"30s",
				"exit codes: 0 for a match or unfiltered exit",
			},
			notWant: []string{"(default: 0)"},
		},
		{
			name: "restart",
			args: []string{"hum", "restart", "--help"},
			want: []string{
				"graceful stop and relaunch by name",
				"restart is not the daemon",
				"--no-wait skips readiness",
				"later names continue",
				"errors stop the remaining restarts",
				"exit codes: 0 success",
			},
		},
		{
			name: "stop",
			args: []string{"hum", "stop", "--help"},
			want: []string{
				"multiple names",
				"one result per name",
				"already-stopped",
				"idempotent",
				"does not shut down the daemon",
			},
		},
		{
			name: "remove",
			args: []string{"hum", "remove", "--help"},
			want: []string{"supervision sessions", "stops each running incarnation", "closes attached followers", "discards retained output", "followers count never warns, prompts, or blocks removal", "never edits hum.yaml"},
		},
		{
			name: "shutdown",
			args: []string{"hum", "shutdown", "--help"},
			want: []string{
				"daemon lifetime",
				"by default it refuses",
				"managed processes are active",
				"lists their names",
				"--stop-processes",
				"every managed process",
				"no daemon is running",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output, errorOutput bytes.Buffer
			root := NewRootCommand("dev", "unknown", &output, &errorOutput)
			if err := root.Run(context.Background(), tt.args); err != nil {
				t.Fatalf("help: %v", err)
			}
			help := strings.Join(strings.Fields(strings.ToLower(output.String())), " ")
			for _, want := range tt.want {
				if !strings.Contains(help, strings.ToLower(want)) {
					t.Errorf("help missing %q: %q", want, output.String())
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(help, strings.ToLower(notWant)) {
					t.Errorf("help contains misleading %q: %q", notWant, output.String())
				}
			}
		})
	}
}

func TestOutputByteDocs(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)
	if err := root.Run(context.Background(), []string{"hum", "--help"}); err != nil {
		t.Fatalf("root help: %v", err)
	}
	help := strings.ToLower(output.String())
	for _, want := range []string{"output-bytes", "retained bytes per process", "docs/design.md"} {
		if !strings.Contains(help, want) {
			t.Errorf("root help missing %q: %q", want, output.String())
		}
	}
	content, err := os.ReadFile("../../docs/design.md")
	if err != nil {
		t.Fatalf("read docs/design.md: %v", err)
	}
	docs := strings.ToLower(string(content))
	for _, want := range []string{"len(text)+128", "--output-bytes", "--limit-bytes", "text bytes only", "not an exact rss cap"} {
		if !strings.Contains(docs, want) {
			t.Errorf("docs/design.md missing %q", want)
		}
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errorOutput.String())
	}
}

func TestPinnedToolchainDocs(t *testing.T) {
	miseContent, err := os.ReadFile("../../mise.toml")
	if err != nil {
		t.Fatalf("read mise.toml: %v", err)
	}
	mise := string(miseContent)
	for _, want := range []string{`go = "1.27.1"`, `staticcheck = "2026.2.1"`} {
		if !strings.Contains(mise, want) {
			t.Errorf("mise.toml missing pinned tool %q", want)
		}
	}
	for _, tool := range []string{"go", "staticcheck"} {
		for _, line := range strings.Split(mise, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, tool+" =") && strings.Contains(trimmed, "latest") {
				t.Errorf("mise.toml leaves %s on latest: %q", tool, line)
			}
		}
	}

	goModContent, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	goDirective := ""
	for _, line := range strings.Split(string(goModContent), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "go ") {
			goDirective = strings.TrimSpace(line)
			break
		}
	}
	// The directive tracks the pinned toolchain minor; there is no separate
	// minimum supported Go version because hum ships only as release binaries.
	if goDirective != "go 1.27" {
		t.Errorf("go.mod directive = %q, want %q", goDirective, "go 1.27")
	}

	docsContent, err := os.ReadFile("../../docs/development.md")
	if err != nil {
		t.Fatalf("read docs/development.md: %v", err)
	}
	docs := strings.ToLower(string(docsContent))
	for _, want := range []string{
		"toolchain policy",
		"go 1.27.1",
		"staticcheck 2026.2.1",
		"no separate minimum supported go version",
		"task ci",
		"upgrade",
	} {
		if !strings.Contains(docs, want) {
			t.Errorf("docs/development.md missing %q", want)
		}
	}
}
