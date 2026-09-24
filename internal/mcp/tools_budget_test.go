package mcp

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestToolsListBudget(t *testing.T) {
	tools := NewServer(Options{}).toolDefinitions()
	if len(tools) != 13 {
		t.Fatalf("tools/list returned %d tools, want 13", len(tools))
	}
	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 40_000 {
		t.Errorf("result.tools is %d bytes, budget 40000", len(encoded))
	}

	withoutOutputs := make([]toolDefinition, len(tools))
	for i, tool := range tools {
		if utf8.RuneCountInString(tool.Description) > 300 || strings.TrimSpace(tool.Description) == "" {
			t.Errorf("%s description is %d characters (budget 300, nonempty)", tool.Name, utf8.RuneCountInString(tool.Description))
		}
		checkInputDescriptions(t, tool.Name+".inputSchema", tool.InputSchema)
		checkOutputDescriptions(t, tool.Name+".outputSchema", tool.OutputSchema)
		withoutOutputs[i] = tool
		withoutOutputs[i].OutputSchema = nil
	}
	encoded, err = json.Marshal(withoutOutputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 17_000 {
		t.Errorf("result.tools without outputSchema is %d bytes, budget 17000", len(encoded))
	}
}

func checkInputDescriptions(t *testing.T, path string, node any) {
	t.Helper()
	switch value := node.(type) {
	case map[string]any:
		if description, ok := value["description"].(string); ok && utf8.RuneCountInString(description) > 100 {
			t.Errorf("%s description is %d characters (budget 100)", path, utf8.RuneCountInString(description))
		}
		for key, child := range value {
			checkInputDescriptions(t, path+"."+key, child)
		}
	case []any:
		for _, child := range value {
			checkInputDescriptions(t, path, child)
		}
	}
}

func checkOutputDescriptions(t *testing.T, path string, node any) {
	t.Helper()
	switch value := node.(type) {
	case map[string]any:
		if _, ok := value["description"]; ok {
			t.Errorf("%s contains description", path)
		}
		for key, child := range value {
			checkOutputDescriptions(t, path+"."+key, child)
		}
	case []any:
		for _, child := range value {
			checkOutputDescriptions(t, path, child)
		}
	}
}
