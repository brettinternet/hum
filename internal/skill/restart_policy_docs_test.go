package skill

import (
	"os"
	"strings"
	"testing"
)

func TestRestartPolicyDocs(t *testing.T) {
	paths := []string{"../../README.md", "../../docs/design.md", "../../docs/coding-agents.md", "SKILL.md", "../../plugins/hum/skills/hum/SKILL.md"}
	var all strings.Builder
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := strings.ToLower(string(content))
		for _, phrase := range []string{"restart: on-failure", "never", "five", "30", "spawn", "retained", "failing", "output"} {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s missing restart-policy guidance %q", path, phrase)
			}
		}
		all.WriteString(text)
	}
	if Content() != string(mustReadSkillDocs(t, "SKILL.md")) {
		t.Fatal("embedded skill differs from internal/skill/SKILL.md")
	}
	for _, phrase := range []string{"1s", "2s", "4s", "8s", "16s", "relaunches", "next_launch_at", "followers", "last effective", "operator", "gave up"} {
		if !strings.Contains(all.String(), phrase) {
			t.Errorf("restart-policy documentation missing %q", phrase)
		}
	}
}

func mustReadSkillDocs(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
