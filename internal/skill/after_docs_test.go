package skill

import (
	"os"
	"strings"
	"testing"
)

func TestAfterDocs(t *testing.T) {
	paths := []string{"../../README.md", "../../docs/design.md", "../../docs/coding-agents.md", "SKILL.md", "../../plugins/hum/skills/hum/SKILL.md"}
	var all strings.Builder
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := strings.ToLower(string(contents))
		for _, phrase := range []string{"after", "readiness", "skipped", "blocked_by", "lexical", "no-wait", "concurrent", "rerun"} {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s missing after guidance %q", path, phrase)
			}
		}
		all.WriteString(text)
	}
	if Content() != string(mustReadSkillDocs(t, "SKILL.md")) {
		t.Fatal("embedded skill differs from source skill")
	}
	for _, phrase := range []string{"direct", "timeout", "on-failure", "success 0", "start", "down"} {
		if !strings.Contains(all.String(), phrase) {
			t.Errorf("after guidance missing %q", phrase)
		}
	}
}
