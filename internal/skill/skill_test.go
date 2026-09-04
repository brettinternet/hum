package skill

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestSkillContentMatchesFileByteForByte(t *testing.T) {
	want, err := os.ReadFile("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := Content(); got != string(want) {
		t.Fatal("Content does not match SKILL.md byte-for-byte")
	}
}

func TestSkillContentHasRequiredFrontmatter(t *testing.T) {
	lines := strings.Split(Content(), "\n")
	if len(lines) < 4 || lines[0] != "---" {
		t.Fatal("SKILL.md must begin with YAML frontmatter")
	}
	end := -1
	for index := 1; index < len(lines); index++ {
		if lines[index] == "---" {
			end = index
			break
		}
	}
	if end < 0 {
		t.Fatal("SKILL.md frontmatter is not closed")
	}
	fields := make(map[string]string, end-1)
	for _, line := range lines[1:end] {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" {
			t.Fatalf("invalid frontmatter line %q", line)
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if fields["name"] != "hum" {
		t.Fatalf("frontmatter name = %q, want hum", fields["name"])
	}
	description := fields["description"]
	if description == "" || strings.Count(description, ".") != 1 {
		t.Fatalf("frontmatter description must be one non-empty sentence: %q", description)
	}
	if !strings.Contains(description, "shell-only") || !strings.Contains(description, "MCP") {
		t.Fatalf("description must explain shell-only fallback use: %q", description)
	}
}

func TestResolvedProjectInstructions(t *testing.T) {
	content := Content()
	lineCount := len(strings.Split(strings.TrimSuffix(content, "\n"), "\n"))
	if lineCount >= 80 {
		t.Fatalf("SKILL.md has %d lines, want fewer than 80", lineCount)
	}

	for _, instruction := range []string{
		"Use MCP as the primary integration",
		"Try `hum up` first",
		"waits for readiness by default",
		"hum start <name>",
		"hum list",
		"source and readiness",
		"hum logs --tail 100 <name>",
		"hum logs --after-cursor <cursor> --json <name>",
		"hum wait <name>",
		"hum restart <name>",
		"hum down",
		"everything in the current project",
		"absent `hum.yaml` is normal",
		"conservative discovery",
		"exactly one candidate named `dev`",
		"no candidate or is ambiguous",
		"multiple commands",
		"custom cwd",
		"readiness",
		"ask the developer to run `hum init`",
		"commit the resulting `hum.yaml`",
		"Do not run `hum init` yourself",
		"Never derive or run underlying development commands",
		"including npm, bun, yarn, or pnpm-style commands",
		"Never use raw `hum run ... -- ...`",
	} {
		if !strings.Contains(content, instruction) {
			t.Errorf("SKILL.md missing instruction %q", instruction)
		}
	}
	if !strings.Contains(content, "`hum stop <name>`") {
		t.Error("SKILL.md missing exact hum stop invocation")
	}
	if !strings.Contains(content, "only when the developer asks you to stop that process") {
		t.Error("SKILL.md missing developer-request stop restriction")
	}
	rawRunWarning := "Never use raw `hum run ... -- ...`"
	rawRunCommand := regexp.MustCompile(`\bhum[[:space:]]+run\b`)
	if rawRunCommand.MatchString(strings.ReplaceAll(content, rawRunWarning, "")) {
		t.Error("SKILL.md must not instruct a raw hum run command")
	}

	packageManagerCommand := regexp.MustCompile(`(^|[^[:alnum:]_-])(npm|bun|yarn|pnpm)[[:space:]]+[^[:space:]]+`)
	if packageManagerCommand.MatchString(content) {
		t.Error("SKILL.md must not contain package-manager development commands")
	}
}
