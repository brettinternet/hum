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
	for _, want := range []string{"without --match", "exit", "hum start <name>", "--after-cursor", "--match", "--timeout", "--json"} {
		if !strings.Contains(help, want) {
			t.Errorf("wait help missing %q: %q", want, output.String())
		}
	}
}
