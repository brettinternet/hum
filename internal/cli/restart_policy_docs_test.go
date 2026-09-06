package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"hum/internal/skill"
)

func TestRestartPolicyDocs(t *testing.T) {
	documents := map[string]string{
		"README.md":                       restartPolicyDoc(t, "../../README.md"),
		"docs/design.md":                  restartPolicyDoc(t, "../../docs/design.md"),
		"docs/coding-agents.md":           restartPolicyDoc(t, "../../docs/coding-agents.md"),
		"internal/skill/SKILL.md":         restartPolicyDoc(t, "../skill/SKILL.md"),
		"plugins/hum/skills/hum/SKILL.md": restartPolicyDoc(t, "../../plugins/hum/skills/hum/SKILL.md"),
		"embedded skill":                  skill.Content(),
	}
	for path, content := range documents {
		lower := strings.ToLower(content)
		for _, phrase := range []string{"restart: on-failure", "never", "five", "30", "spawn", "retained", "failing", "output"} {
			if !strings.Contains(lower, phrase) {
				t.Errorf("%s missing restart-policy guidance %q", path, phrase)
			}
		}
	}
	all := strings.ToLower(strings.Join([]string{
		documents["README.md"], documents["docs/design.md"], documents["docs/coding-agents.md"],
		documents["internal/skill/SKILL.md"], documents["plugins/hum/skills/hum/SKILL.md"],
	}, "\n"))
	for _, phrase := range []string{"1s", "2s", "4s", "8s", "16s", "relaunches", "next_launch_at", "followers", "last effective", "generation", "operator", "gave up"} {
		if !strings.Contains(all, phrase) {
			t.Errorf("restart-policy documentation missing %q", phrase)
		}
	}

	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	if err := root.Run(context.Background(), []string{"hum", "--help"}); err != nil {
		t.Fatal(err)
	}
	help := strings.ToLower(stdout.String())
	for _, phrase := range []string{"restart: on-failure", "1s", "2s", "4s", "8s", "16s", "five times", "spawn failures", "30-second", "retained failing output"} {
		if !strings.Contains(help, phrase) {
			t.Errorf("CLI help missing %q: %q", phrase, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("CLI help stderr = %q", stderr.String())
	}
}

func restartPolicyDoc(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
