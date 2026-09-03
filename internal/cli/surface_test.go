package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestStatusAndWaitSurface(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)

	want := map[string]bool{
		"serve":    true,
		"run":      true,
		"start":    true,
		"up":       true,
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
	for _, name := range []string{"down"} {
		if root.Command(name) != nil {
			t.Errorf("root command unexpectedly exposes %q", name)
		}
	}
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
