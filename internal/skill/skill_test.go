package skill

import (
	"encoding/json"
	"os"
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
	lines := strings.Split(strings.ReplaceAll(Content(), "\r\n", "\n"), "\n")
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
	if description == "" {
		t.Fatal("SKILL.md frontmatter description must be non-empty")
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
