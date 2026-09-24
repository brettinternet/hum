package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"hum/internal/cli"
	"hum/internal/mcp"
)

func TestDocsCoverEveryTool(t *testing.T) {
	// Keep stdin open until the asynchronous tools/list response arrives: EOF
	// cancels in-flight requests and need not preserve response order.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	inputReader, input := io.Pipe()
	output, outputWriter := io.Pipe()
	defer input.Close()
	defer output.Close()
	stopOnDeadline := context.AfterFunc(ctx, func() {
		_ = input.Close()
		_ = output.Close()
	})
	defer stopOnDeadline()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- mcp.NewServer(mcp.Options{}).Serve(ctx, inputReader, outputWriter)
		_ = outputWriter.Close()
	}()
	request := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"
	if _, err := io.WriteString(input, request); err != nil {
		t.Fatalf("send tools/list: %v", err)
	}
	var listing struct {
		ID     int `json:"id"`
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	decoder := json.NewDecoder(output)
	for {
		if err := decoder.Decode(&listing); err != nil {
			t.Fatalf("read tools/list response: %v", err)
		}
		if listing.ID == 2 {
			break
		}
	}
	_ = input.Close()
	if err := <-serveDone; err != nil {
		t.Fatalf("list MCP tools: %v", err)
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
