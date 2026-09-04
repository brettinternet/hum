package cli

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"hum/internal/skill"
)

func TestStatusAndWaitSurface(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)

	want := map[string]bool{
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
		"logs":     true,
		"wait":     true,
		"restart":  true,
		"stop":     true,
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
	for _, want := range []string{"without --match", "exit", "hum run <name> -- <command>", "wait --match", "future resolved-process commands", "hum start <name>", "--after-cursor", "default: current launch cursor", "--match", "--timeout", "--json"} {
		if !strings.Contains(help, want) {
			t.Errorf("wait help missing %q: %q", want, output.String())
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
				"automatically starts a detached daemon",
				"stays attached by default",
				"serve --daemon runs it detached",
				"manifest projects use hum start",
				"read/control commands (excluding explicit hum serve) do not start an empty daemon",
				"hum run <name> -- <command>",
				"stopping named processes and shutting down the daemon are separate operations",
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
				"create a hum.yaml manifest",
				"strict project discovery",
				"without starting a daemon",
				"single candidate",
				"commented template",
				"absolute path",
				"next command hum up",
				"--json",
			},
		},
		{
			name: "run",
			args: []string{"hum", "run", "--help"},
			want: []string{
				"automatically starts a detached daemon",
				"without --detach",
				"attached",
				"streams the managed process",
				"with --detach",
				"returns immediately",
				"daemon keeps owning it",
				"stable json for detached runs",
				"attached runs stream raw child output",
			},
		},
		{
			name: "start",
			args: []string{"hum", "start", "--help"},
			want: []string{
				"manifest",
				"one or more",
				"--no-wait",
				"--timeout",
				"--json",
				"readiness",
			},
		},
		{
			name: "up",
			args: []string{"hum", "up", "--help"},
			want: []string{
				"every manifest process",
				"lexical order",
				"continues after launch failures",
				"concurrently",
				"--no-wait",
				"--timeout",
				"--json",
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
			},
		},
		{
			name: "status",
			args: []string{"hum", "status", "--help"},
			want: []string{
				"read-only",
				"never starts a daemon",
				"hum run <name> -- <command>",
			},
		},
		{
			name: "logs",
			args: []string{"hum", "logs", "--help"},
			want: []string{
				"bounded retained output",
				"--follow",
				"read-only",
				"cancels only the follower",
				"never signals the managed process",
				"hum run <name> -- <command>",
			},
		},
		{
			name: "wait",
			args: []string{"hum", "wait", "--help"},
			want: []string{
				"without --match",
				"current incarnation's launch cursor",
				"default: current launch cursor",
				"30s",
				"exit code is 0",
				"3 when --match",
				"2 on timeout",
				"wait --match",
				"future resolved-process commands",
				"hum start <name>",
				"hum run <name> -- <command>",
				"non-empty regular expression",
			},
			notWant: []string{"(default: 0)"},
		},
		{
			name: "restart",
			args: []string{"hum", "restart", "--help"},
			want: []string{
				"by name",
				"graceful stop",
				"names are attempted in order",
				"first error stops the remaining restarts",
				"only successful attempts",
				"new pid",
				"not the daemon",
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
