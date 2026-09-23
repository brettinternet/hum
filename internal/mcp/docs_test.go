package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"hum/internal/cli"
	"hum/internal/mcp"
)

func TestDocsCoverEveryTool(t *testing.T) {
	// tools/list is the public projection of the toolDefinitions surface.
	var serverOutput bytes.Buffer
	request := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"
	if err := mcp.NewServer(mcp.Options{}).Serve(context.Background(), strings.NewReader(request), &serverOutput); err != nil {
		t.Fatalf("list MCP tools: %v", err)
	}
	responses := strings.Split(strings.TrimSpace(serverOutput.String()), "\n")
	if len(responses) == 0 {
		t.Fatal("MCP tools/list returned no response")
	}
	var listing struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(responses[len(responses)-1]), &listing); err != nil {
		t.Fatalf("decode MCP tools/list: %v", err)
	}
	if len(listing.Result.Tools) == 0 {
		t.Fatal("MCP tools/list returned no tools")
	}

	content, err := os.ReadFile("../../docs/coding-agents.md")
	if err != nil {
		t.Fatalf("read docs/coding-agents.md: %v", err)
	}
	docs := string(content)
	for _, tool := range listing.Result.Tools {
		if !strings.Contains(docs, "`"+tool.Name+"`") {
			t.Errorf("docs/coding-agents.md does not reference MCP tool %q in backticks", tool.Name)
		}
	}

	var helpOutput, helpError bytes.Buffer
	root := cli.NewRootCommand("test", "test", &helpOutput, &helpError)
	if err := root.Run(context.Background(), []string{"hum", "mcp", "--help"}); err != nil {
		t.Fatalf("mcp help: %v", err)
	}
	if helpError.Len() != 0 {
		t.Fatalf("mcp help stderr = %q", helpError.String())
	}
	for _, tool := range listing.Result.Tools {
		name := regexp.MustCompile(`\b` + regexp.QuoteMeta(tool.Name) + `\b`)
		if !name.MatchString(helpOutput.String()) {
			t.Errorf("hum mcp --help does not name MCP tool %q", tool.Name)
		}
	}
}
