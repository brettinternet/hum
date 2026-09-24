package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"time"

	"hum/internal/protocol"
)

// checkOutputSchema checks the JSON Schema keywords used by hum's output
// schemas, against JSON values (rather than Go struct representations).
func checkOutputSchema(schema map[string]any, value any, path string) error {
	if choices, ok := schema["oneOf"].([]any); ok {
		matches := 0
		for _, choice := range choices {
			if checkOutputSchema(choice.(map[string]any), value, path) == nil {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("%s: matched %d oneOf branches, want 1", path, matches)
		}
	}
	if constant, ok := schema["const"]; ok && !reflect.DeepEqual(value, constant) {
		return fmt.Errorf("%s: value %v does not match const %v", path, value, constant)
	}
	if choices, ok := schema["enum"].([]any); ok {
		found := false
		for _, choice := range choices {
			found = found || reflect.DeepEqual(value, choice)
		}
		if !found {
			return fmt.Errorf("%s: value %v not in enum %v", path, value, choices)
		}
	}
	if alternatives, ok := schema["type"].([]any); ok {
		for _, alternative := range alternatives {
			variant := make(map[string]any, len(schema))
			for key, field := range schema {
				variant[key] = field
			}
			variant["type"] = alternative
			if checkOutputSchema(variant, value, path) == nil {
				return nil
			}
		}
		return fmt.Errorf("%s: value %v matches none of types %v", path, value, alternatives)
	}
	switch schema["type"] {
	case "null":
		if value != nil {
			return fmt.Errorf("%s: expected null, got %T", path, value)
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object, got %T", path, value)
		}
		properties, _ := schema["properties"].(map[string]any)
		if required, ok := schema["required"].([]any); ok {
			for _, field := range required {
				if _, present := object[field.(string)]; !present {
					return fmt.Errorf("%s: missing required %s", path, field)
				}
			}
		}
		for key, child := range object {
			property, defined := properties[key]
			if !defined {
				if schema["additionalProperties"] == false {
					return fmt.Errorf("%s: unexpected property %s", path, key)
				}
				continue
			}
			if err := checkOutputSchema(property.(map[string]any), child, path+"."+key); err != nil {
				return err
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s: expected array, got %T", path, value)
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for index, item := range items {
				if err := checkOutputSchema(itemSchema, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: expected string, got %T", path, value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: expected boolean, got %T", path, value)
		}
	case "integer", "number":
		number, ok := value.(float64)
		if !ok || (schema["type"] == "integer" && number != float64(int64(number))) {
			return fmt.Errorf("%s: expected %s, got %v", path, schema["type"], value)
		}
		if min, ok := schema["minimum"].(float64); ok && number < min {
			return fmt.Errorf("%s: %v below minimum %v", path, number, min)
		}
		if max, ok := schema["maximum"].(float64); ok && number > max {
			return fmt.Errorf("%s: %v above maximum %v", path, number, max)
		}
	default:
		if schema["type"] != nil {
			return fmt.Errorf("%s: unsupported schema type %v", path, schema["type"])
		}
	}
	return nil
}

func TestOutputSchemaCheckerRejectsUnknownType(t *testing.T) {
	if err := checkOutputSchema(map[string]any{"type": "strng"}, "value", "test"); err == nil {
		t.Fatal("checker accepted an unsupported schema type")
	}
}

func outputJSONValue(t *testing.T, value any) any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestOutputSchemasAcceptStructuredContent(t *testing.T) {
	for _, method := range []string{"match", "exec", "http", "tcp", "exit", "none"} {
		t.Run(method, func(t *testing.T) {
			var ready *protocol.ReadinessConfig
			switch method {
			case "match":
				ready = &protocol.ReadinessConfig{Method: method, Match: "ready"}
			case "exec":
				ready = &protocol.ReadinessConfig{Method: method, Argv: []string{"probe"}, Interval: time.Second}
			case "http":
				ready = &protocol.ReadinessConfig{Method: method, Target: "http://127.0.0.1:1/ready"}
			case "tcp":
				ready = &protocol.ReadinessConfig{Method: method, Target: "127.0.0.1:1"}
			case "exit":
				ready = &protocol.ReadinessConfig{Method: method}
			}
			client := &fakeClient{readyBeforeWait: true}
			server, root, _ := newTestServer(t, []Definition{{Name: "api", Source: "manifest", Cwd: ".", Argv: []string{"api"}, Ready: ready}}, client)
			for _, name := range []string{"start", "up", "list", "status", "restart"} {
				fields := map[string]any{}
				if name == "start" || name == "status" || name == "restart" {
					fields["name"] = "api"
				}
				checkToolResult(t, server, root, name, fields)
			}
		})
	}

	// Every other advertised tool gets a successful call; use a running TTY
	// session so input and signal exercise their result schemas too.
	client := &fakeClient{processes: map[string]protocol.Process{
		"api": {Name: "api", Source: "manifest", State: "running", TTY: true, Cwd: ".", Argv: []string{"api"}, LaunchCursor: 1, StopGraceInherited: true},
	}, output: protocol.OutputResult{Entries: []protocol.OutputEntry{{Cursor: 1, Stream: protocol.StreamStdout, Time: time.Now(), Text: "hello"}}}}
	server, root, _ := newTestServer(t, []Definition{{Name: "api", Source: "manifest", Cwd: ".", Argv: []string{"api"}, TTY: true}}, client)
	process := client.processes["api"]
	process.Root, process.Cwd = root, root
	client.processes["api"] = process
	for _, call := range []struct {
		name   string
		fields map[string]any
	}{
		{"logs", map[string]any{"name": "api"}},
		{"wait", map[string]any{"name": "api"}},
		{"input", map[string]any{"name": "api", "text": "x"}},
		{"signal", map[string]any{"name": "api", "signal": "TERM"}},
		{"stop", map[string]any{"name": "api"}},
		{"down", nil},
		{"remove", map[string]any{"name": "api"}},
		{"remove", map[string]any{"all": true}},
	} {
		checkToolResult(t, server, root, call.name, call.fields)
	}
	// A session created by a follower before its first launch has no argv.
	process.Argv = nil
	client.processes["api"] = process
	checkToolResult(t, server, root, "list", nil)
	checkToolResult(t, server, root, "status", map[string]any{"name": "api"})
	// Per-process stop failures keep wire error details.
	client.stopErr = map[string]error{"api": protocol.NewWireError(protocol.ErrorCodeNotFound, "process not found", map[string]any{"name": "api"})}
	checkToolResult(t, server, root, "down", nil)
	client.stopErr = nil
	events := &eventHistoryClient{fakeClient: client, response: protocol.NewEventsResponse([]protocol.HistoryEvent{{Cursor: 1, Time: time.Now(), Kind: protocol.EventOperation, Name: "api", Event: "start"}}, 1, false, false)}
	server.opts.ClientFactory = func(context.Context, bool) (Client, error) { return events, nil }
	checkToolResult(t, server, root, "events", nil)
	client.output.Entries = nil
	checkToolResult(t, server, root, "logs", map[string]any{"name": "api"})
	events.response = protocol.NewEventsResponse(nil, 0, false, false)
	checkToolResult(t, server, root, "events", nil)
}

func checkToolResult(t *testing.T, server *Server, root, name string, fields map[string]any) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		params := outputJSONValue(t, toolsCallParams(root, name, fields))
		encoded, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		value, rpcErr := server.handleRequest(context.Background(), rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`"schema"`), Method: "tools/call", Params: encoded})
		if rpcErr != nil {
			t.Fatalf("RPC: %#v", rpcErr)
		}
		result, ok := value.(callToolResult)
		if name == "signal" && runtime.GOOS == "windows" {
			if !ok || !result.IsError || result.StructuredContent == nil || result.StructuredContent.(*ToolError).Code != string(protocol.ErrorInvalidSignal) {
				t.Fatalf("unsupported Windows signal result: %#v", value)
			}
			return
		}
		if !ok || result.IsError || result.StructuredContent == nil {
			t.Fatalf("tool result: %#v", value)
		}
		for _, definition := range server.toolDefinitions() {
			if definition.Name == name {
				schema := outputJSONValue(t, definition.OutputSchema).(map[string]any)
				content := outputJSONValue(t, result.StructuredContent)
				if err := checkOutputSchema(schema, content, name); err != nil {
					t.Fatalf("%v; structuredContent=%#v", err, content)
				}
				return
			}
		}
		t.Fatalf("missing output schema for %s", name)
	})
}
