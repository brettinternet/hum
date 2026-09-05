package skill

import (
	"os"
	"strings"
	"testing"
)

func TestTerminalControlDocs(t *testing.T) {
	for path, content := range map[string]string{
		"README.md":                       readTerminalControlDoc(t, "../../README.md"),
		"docs/design.md":                  readTerminalControlDoc(t, "../../docs/design.md"),
		"docs/coding-agents.md":           readTerminalControlDoc(t, "../../docs/coding-agents.md"),
		"embedded skill":                  Content(),
		"plugins/hum/skills/hum/SKILL.md": readTerminalControlDoc(t, "../../plugins/hum/skills/hum/SKILL.md"),
	} {
		lower := strings.ToLower(content)
		normalized := strings.Join(strings.Fields(lower), " ")
		for _, phrase := range []string{
			"terminal-control-stripped",
			"system entries remain raw",
			"raw esc bytes",
			"anchor now matches",
			"colourised output",
			"follow",
			"attached",
			"stored bytes",
			"cursor",
			"limit",
			"control-only",
			"empty text",
			"split",
			"carriage-return",
			"raw opt-out",
		} {
			if !strings.Contains(normalized, phrase) {
				t.Errorf("%s missing terminal-control guidance %q", path, phrase)
			}
		}
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
