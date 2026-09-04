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
		"stdio", "one-time", "project_root", "absolute existing", "start and up", "resolved", "status, logs, wait, restart, and stop",
		"ad_hoc", "hum run", "daemon shutdown or replacement", "argv-based environment activation",
		"nine tools", "run, serve, and shutdown are not mcp tools",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("mcp help missing %q: %q", want, output.String())
		}
	}
}
