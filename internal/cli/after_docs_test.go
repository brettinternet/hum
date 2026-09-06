package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"hum/internal/skill"
)

func TestAfterDocs(t *testing.T) {
	documents := map[string]string{
		"README.md":                       afterDocsRead(t, "../../README.md"),
		"docs/design.md":                  afterDocsRead(t, "../../docs/design.md"),
		"docs/coding-agents.md":           afterDocsRead(t, "../../docs/coding-agents.md"),
		"internal/skill/SKILL.md":         afterDocsRead(t, "../skill/SKILL.md"),
		"plugins/hum/skills/hum/SKILL.md": afterDocsRead(t, "../../plugins/hum/skills/hum/SKILL.md"),
		"embedded skill":                  skill.Content(),
	}
	all := strings.ToLower(strings.Join([]string{
		documents["README.md"], documents["docs/design.md"], documents["docs/coding-agents.md"],
		documents["internal/skill/SKILL.md"], documents["plugins/hum/skills/hum/SKILL.md"],
	}, "\n"))
	for _, phrase := range []string{"after", "readiness", "timeout", "skipped", "blocked_by", "lexical", "no-wait", "explicit", "concurrent", "rerun", "on-failure"} {
		if !strings.Contains(all, phrase) {
			t.Errorf("after documentation missing %q", phrase)
		}
	}
	if strings.Contains(strings.ToLower(documents["docs/design.md"]), "no runtime settings, dependencies") {
		t.Fatal("design document still excludes dependencies from the manifest")
	}
	if skill.Content() != documents["internal/skill/SKILL.md"] {
		t.Fatal("embedded skill differs from source skill")
	}

	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	if err := root.Run(context.Background(), []string{"hum", "up", "--help"}); err != nil {
		t.Fatal(err)
	}
	help := strings.ToLower(stdout.String())
	for _, phrase := range []string{"after", "readiness", "concurrently", "skipped", "--no-wait", "daemon contact", "one invocation", "automatic prerequisite successor", "rerun hum up after recovery"} {
		if !strings.Contains(help, phrase) {
			t.Errorf("up help missing %q: %q", phrase, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("up help stderr = %q", stderr.String())
	}
}

func afterDocsRead(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}
