package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestMCPHelp(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)
	if err := root.Run(context.Background(), []string{"hum", "mcp", "--help"}); err != nil {
		t.Fatalf("mcp help: %v", err)
	}
	help := strings.ToLower(output.String())
	for _, want := range []string{
		"stdio", "one-time", "project_root", "absolute existing", "start and up", "resolved", "status, logs, wait, input, restart, stop, remove, and signal",
		"ad_hoc", "hum run", "daemon shutdown or replacement", "argv-based environment activation",
		"twelve tools", "run, serve, and shutdown are not mcp tools",
		"64", "-32001", "-32600", "-32800", "notifications/cancelled", "serialized", "parent cancellation",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("mcp help missing %q: %q", want, output.String())
		}
	}
}

func TestMCPConcurrencyDescription(t *testing.T) {
	description := strings.ToLower(mcpCLICommand("dev", "unknown", &bytes.Buffer{}).Description)
	for _, want := range []string{"64", "-32001", "-32600", "-32800", "notifications/cancelled", "serialized", "parent cancellation"} {
		if !strings.Contains(description, want) {
			t.Errorf("mcp description missing %q: %q", want, description)
		}
	}
}
