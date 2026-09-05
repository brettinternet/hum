package skill

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
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
	if strings.Contains(content, "only when the developer asks you to stop that process") {
		t.Error("SKILL.md retains obsolete developer-request stop restriction")
	}
	for _, instruction := range []string{"Never use unbounded", "hum remove <name>", "durable session", "intermediate work"} {
		if !strings.Contains(content, instruction) {
			t.Errorf("SKILL.md missing lifecycle instruction %q", instruction)
		}
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

func TestPluginPackageWiresSkillAndMCP(t *testing.T) {
	var manifest struct {
		Name       string `json:"name"`
		Skills     string `json:"skills"`
		MCPServers string `json:"mcpServers"`
	}
	decodePluginJSON(t, "../../plugins/hum/.codex-plugin/plugin.json", &manifest)
	if manifest.Name != "hum" || manifest.Skills != "./skills/" || manifest.MCPServers != "./.mcp.json" {
		t.Fatalf("plugin manifest wiring = %+v", manifest)
	}

	var mcpConfig struct {
		Servers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	decodePluginJSON(t, "../../plugins/hum/.mcp.json", &mcpConfig)
	humServer, ok := mcpConfig.Servers["hum"]
	if !ok || humServer.Command != "hum" || !slices.Equal(humServer.Args, []string{"mcp"}) {
		t.Fatalf("hum MCP server wiring = %+v", humServer)
	}

	pluginSkill, err := os.ReadFile("../../plugins/hum/skills/hum/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, instruction := range []string{
		"name: hum",
		"project_root",
		"Use the bundled hum MCP tools",
		"bounded later condition",
		"before another client starts the name",
		"durable session keeps terminal observers attached",
		"Use `remove` only to discard the runtime session",
		"never edits `hum.yaml`",
		"later `up` restarts only resolved definitions",
		"Never use raw `hum run",
	} {
		if !strings.Contains(string(pluginSkill), instruction) {
			t.Errorf("plugin skill missing %q", instruction)
		}
	}
	if strings.Contains(string(pluginSkill), "only when the developer asks you to stop that process") {
		t.Error("plugin skill retains obsolete developer-request stop restriction")
	}
}

func TestPluginMarketplaceEntry(t *testing.T) {
	var marketplace struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source struct {
				Path string `json:"path"`
			} `json:"source"`
		} `json:"plugins"`
	}
	decodePluginJSON(t, "../../.agents/plugins/marketplace.json", &marketplace)
	if marketplace.Name != "hum" || len(marketplace.Plugins) != 1 || marketplace.Plugins[0].Name != "hum" || marketplace.Plugins[0].Source.Path != "./plugins/hum" {
		t.Fatalf("marketplace wiring = %+v", marketplace)
	}
}

func decodePluginJSON(t *testing.T, path string, target any) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func TestTTYInstructions(t *testing.T) {
	for name, content := range map[string]string{"embedded": Content(), "plugin": string(mustReadSkillFile(t, "../../plugins/hum/skills/hum/SKILL.md"))} {
		for _, want := range []string{"tty: true", "CLI TTY option with a command separator", "raw mode", "Ctrl-]", "terminal echo", "Ctrl-C", "logs --follow", "MCP", "shutdown"} {
			if !strings.Contains(content, want) {
				t.Errorf("%s skill missing %q", name, want)
			}
		}
	}
}

func mustReadSkillFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
