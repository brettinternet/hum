package skill

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestInputDocs(t *testing.T) {
	for _, path := range []string{"SKILL.md", "../../plugins/hum/skills/hum/SKILL.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		for _, phrase := range []string{"hum input", "--base64", "without a newline", "strict padded base64", "without whitespace", "wait --match", "observe", "answer", "confirm", "ownership conflict", "never starts", "retains"} {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s missing input guidance %q", path, phrase)
			}
		}
	}
}

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
		"Try bounded `hum up --detach` first",
		"interactive plain `hum up`",
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
	foregroundContract := "`hum run NAME -- COMMAND` owns exactly one incarnation: it streams raw child output, propagates the child exit status, stops on Ctrl+C or SIGTERM, and detaches on SIGHUP. Use `hum run NAME --detach -- COMMAND` for daemon ownership, and `hum attach NAME` or `hum logs NAME --follow` for durable observation."
	rawRunCommand := regexp.MustCompile(`\bhum[[:space:]]+run\b`)
	withoutContract := strings.ReplaceAll(strings.ReplaceAll(content, rawRunWarning, ""), foregroundContract, "")
	if rawRunCommand.MatchString(withoutContract) {
		t.Error("SKILL.md must not instruct a raw hum run command outside its human-facing lifecycle contract")
	}

	packageManagerCommand := regexp.MustCompile(`(^|[^[:alnum:]_-])(npm|bun|yarn|pnpm)[[:space:]]+[^[:space:]]+`)
	if packageManagerCommand.MatchString(content) {
		t.Error("SKILL.md must not contain package-manager development commands")
	}
}

func TestPluginPackageWiresSkillAndMCP(t *testing.T) {
	var codexManifest struct {
		Name       string `json:"name"`
		Version    string `json:"version"`
		Skills     string `json:"skills"`
		MCPServers string `json:"mcpServers"`
	}
	decodePluginJSON(t, "../../plugins/hum/.codex-plugin/plugin.json", &codexManifest)
	if codexManifest.Name != "hum" || codexManifest.Skills != "./skills/" || codexManifest.MCPServers != "./.mcp.json" {
		t.Fatalf("Codex plugin manifest wiring = %+v", codexManifest)
	}

	var claudeManifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	decodePluginJSON(t, "../../plugins/hum/.claude-plugin/plugin.json", &claudeManifest)
	if claudeManifest.Name != "hum" || claudeManifest.Version != codexManifest.Version {
		t.Fatalf("Claude plugin manifest wiring = %+v; Codex version = %q", claudeManifest, codexManifest.Version)
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
	var codexMarketplace struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source struct {
				Path string `json:"path"`
			} `json:"source"`
		} `json:"plugins"`
	}
	decodePluginJSON(t, "../../.agents/plugins/marketplace.json", &codexMarketplace)
	if codexMarketplace.Name != "hum" || len(codexMarketplace.Plugins) != 1 || codexMarketplace.Plugins[0].Name != "hum" || codexMarketplace.Plugins[0].Source.Path != "./plugins/hum" {
		t.Fatalf("Codex marketplace wiring = %+v", codexMarketplace)
	}

	var claudeMarketplace struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"plugins"`
	}
	decodePluginJSON(t, "../../.claude-plugin/marketplace.json", &claudeMarketplace)
	if claudeMarketplace.Name != "hum" || len(claudeMarketplace.Plugins) != 1 || claudeMarketplace.Plugins[0].Name != "hum" || claudeMarketplace.Plugins[0].Source != "./plugins/hum" {
		t.Fatalf("Claude marketplace wiring = %+v", claudeMarketplace)
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

func TestScopeDocs(t *testing.T) {
	// The plugin skill ships to agents alongside the embedded one, so scope,
	// global-namespace, foreground-run, and current operational guidance must reach both.
	for name, text := range map[string]string{"embedded": Content(), "plugin": string(mustReadSkillFile(t, "../../plugins/hum/skills/hum/SKILL.md"))} {
		for _, phrase := range []string{
			"canonical", "symlink", "worktree", "--project", "-C", "removed", "list --all", "scope", "project_root",
			"--global", "-g", "ad-hoc",
			"hum run NAME --detach -- COMMAND", "SIGHUP",
			"ready.exec", "stop_grace", "--stream system", "--context",
		} {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s skill missing %q", name, phrase)
			}
		}
	}
}
