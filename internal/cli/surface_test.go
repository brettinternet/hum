package cli

import (
	"bytes"
	"testing"
)

func TestStatusWithoutWaitOrRestartYet(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)

	want := map[string]bool{
		"serve":    true,
		"run":      true,
		"list":     true,
		"status":   true,
		"logs":     true,
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
	for _, name := range []string{"wait", "restart", "down"} {
		if root.Command(name) != nil {
			t.Errorf("root command unexpectedly exposes %q", name)
		}
	}
}
