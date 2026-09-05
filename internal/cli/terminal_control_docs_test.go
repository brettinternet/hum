package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"hum/internal/skill"
)

func TestTerminalControlDocs(t *testing.T) {
	for path, content := range map[string]string{
		"README.md":                       readTerminalControlDoc(t, "../../README.md"),
		"docs/design.md":                  readTerminalControlDoc(t, "../../docs/design.md"),
		"docs/coding-agents.md":           readTerminalControlDoc(t, "../../docs/coding-agents.md"),
		"internal/skill/SKILL.md":         readTerminalControlDoc(t, "../skill/SKILL.md"),
		"plugins/hum/skills/hum/SKILL.md": readTerminalControlDoc(t, "../../plugins/hum/skills/hum/SKILL.md"),
		"embedded skill":                  skill.Content(),
	} {
		lower := strings.ToLower(content)
		normalized := strings.Join(strings.Fields(lower), " ")
		for _, phrase := range []string{
			"terminal-control-stripped",
			"system entries remain raw",
			"raw esc bytes",
			"anchor now matches",
			"colourised output",
			"stored bytes",
			"cursor",
			"limit",
			"control-only bounded",
			"empty text",
			"attached",
			"follow --match",
			"selected raw",
			"raw opt-out",
			"per entry",
			"split",
			"carriage-return",
			"follow",
		} {
			if !strings.Contains(normalized, phrase) {
				t.Errorf("%s missing terminal-control guidance %q", path, phrase)
			}
		}
	}

	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	if err := root.Run(context.Background(), []string{"hum", "logs", "--help"}); err != nil {
		t.Fatal(err)
	}
	help := strings.ToLower(stdout.String())
	for _, phrase := range []string{"terminal-control-stripped", "system entries remain raw", "raw esc bytes", "a ^ anchor now matches colourised", "stored bytes, cursors, and limit accounting remain raw", "control-only bounded child entries remain present with empty text", "follow --match", "attached run output is also raw", "selected entries are emitted raw", "per entry", "split sequences", "carriage-return redraw", "no --raw flag"} {
		if !strings.Contains(help, phrase) {
			t.Errorf("CLI logs help missing %q: %q", phrase, stdout.String())
		}
	}
	for _, command := range root.Commands {
		for _, flag := range command.Flags {
			for _, name := range flag.Names() {
				if name == "raw" {
					t.Errorf("%s command exposes a raw opt-out flag", command.Name)
				}
			}
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("CLI logs help stderr = %q", stderr.String())
	}
}

func readTerminalControlDoc(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
