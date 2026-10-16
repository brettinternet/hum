package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"hum/internal/protocol"
)

type portLookupTestClient struct {
	*fakeClient
	processes  []protocol.Process
	incomplete []protocol.PortLookupIncomplete
	requests   []protocol.PortLookupRequest
}

func (c *portLookupTestClient) PortLookup(_ context.Context, request protocol.PortLookupRequest) ([]protocol.Process, []protocol.PortLookupIncomplete, error) {
	c.requests = append(c.requests, request)
	return c.processes, c.incomplete, nil
}

func TestPortLookupToolStructuredContent(t *testing.T) {
	root := t.TempDir()
	client := &portLookupTestClient{
		fakeClient: &fakeClient{},
		processes: []protocol.Process{{
			Name: "web", Scope: protocol.ScopeProject, Root: "/work/b", State: protocol.StateRunning,
			Ports: &protocol.PortInspection{State: "available", Listeners: []protocol.PortEndpoint{{Transport: "tcp", Address: "127.0.0.1", Port: 3000, PIDs: []int{4190}}}},
		}},
		incomplete: []protocol.PortLookupIncomplete{{Name: "uncertain", Scope: protocol.ScopeGlobal, State: "denied", Diagnostic: "permission denied"}},
	}
	server := NewServer(Options{
		Resolver:      fakeResolver{resolution: Resolution{Root: root}},
		ClientFactory: func(context.Context, bool) (Client, error) { return client, nil },
	})
	arguments, err := json.Marshal(callToolParams{Name: "list", Arguments: args(root, "port", 3000, "all", true)})
	if err != nil {
		t.Fatal(err)
	}
	value, rpcErr := server.handleRequest(context.Background(), rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`"list"`), Method: "tools/call", Params: arguments})
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	result, ok := value.(callToolResult)
	if !ok || result.IsError {
		t.Fatalf("MCP port lookup result = %#v", value)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var structured portLookupResult
	if err := json.Unmarshal(encoded, &structured); err != nil {
		t.Fatal(err)
	}
	if len(structured.Processes) != 1 || structured.Processes[0].Name != "web" || structured.Processes[0].Ports == nil || structured.Processes[0].Ports.Listeners[0].PIDs[0] != 4190 {
		t.Fatalf("MCP holder structuredContent = %#v", structured.Processes)
	}
	if len(structured.Incomplete) != 1 || structured.Incomplete[0].Name != "uncertain" || structured.Incomplete[0].State != "denied" {
		t.Fatalf("MCP incomplete structuredContent = %#v", structured.Incomplete)
	}
	if len(client.requests) != 1 || client.requests[0].Port != 3000 {
		t.Fatalf("MCP lookup requests = %#v", client.requests)
	}
}

func TestPortLookupToolValidationAndOrdinaryList(t *testing.T) {
	root := t.TempDir()
	client := &portLookupTestClient{fakeClient: &fakeClient{}}
	factoryCalls := 0
	server := NewServer(Options{
		Resolver: fakeResolver{resolution: Resolution{Root: root}},
		ClientFactory: func(context.Context, bool) (Client, error) {
			factoryCalls++
			return client, nil
		},
	})
	for _, raw := range []json.RawMessage{
		args(root, "port", 0),
		args(root, "port", 65536),
		args(root, "port", 3000, "all", false),
		json.RawMessage(`{"scope":"global","port":3000}`),
	} {
		if _, err := server.callTool(context.Background(), "list", raw); mapError(err).Code != "invalid_request" {
			t.Fatalf("invalid list port arguments %s error = %v", raw, err)
		}
		if factoryCalls != 0 {
			t.Fatalf("invalid lookup contacted daemon %d times", factoryCalls)
		}
	}
	if _, err := server.callTool(context.Background(), "list", args(root)); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 || factoryCalls != 1 {
		t.Fatalf("ordinary list triggered lookup: requests=%#v factories=%d", client.requests, factoryCalls)
	}
	if _, err := server.callTool(context.Background(), "list", args(root, "port", 3000)); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 || client.requests[0].Port != 3000 || factoryCalls != 2 {
		t.Fatalf("valid lookup request = %#v factories=%d", client.requests, factoryCalls)
	}

	var list toolDefinition
	for _, definition := range server.toolDefinitions() {
		if definition.Name == "list" {
			list = definition
			break
		}
	}
	inputProperties := list.InputSchema["properties"].(map[string]any)
	port := inputProperties["port"].(map[string]any)
	if port["minimum"] != 1 || port["maximum"] != 65535 {
		t.Fatalf("MCP list port schema = %#v", port)
	}
	outputProperties := list.OutputSchema["properties"].(map[string]any)
	if _, ok := outputProperties["incomplete"]; !ok {
		t.Fatalf("MCP list output schema omits incomplete: %#v", outputProperties)
	}
	holderProperties := outputProperties["processes"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if _, ok := holderProperties["ports"]; !ok {
		t.Fatalf("MCP list holder schema omits ports: %#v", holderProperties)
	}
}

func TestStatusPorts(t *testing.T) {
	inspection := &protocol.PortInspection{
		State: "partial", Diagnostic: "one process could not be inspected",
		Listeners: []protocol.PortEndpoint{{Transport: "tcp", Address: "127.0.0.1", Port: 43123, PIDs: []int{41, 42}}},
	}
	client := &fakeClient{processes: map[string]protocol.Process{
		"api": {Name: "api", Root: "/work", Cwd: "/work", State: "running", Ports: inspection},
	}}
	server, root, _ := newTestServer(t, nil, client)
	value, err := server.callTool(context.Background(), "status", args(root, "name", "api"))
	if err != nil {
		t.Fatal(err)
	}
	status := value.(protocol.Process)
	if len(client.gets) != 1 || !client.gets[0].Ports {
		t.Fatalf("status get requests = %#v, want automatic ports inspection", client.gets)
	}
	if status.Ports == nil || status.Ports.State != "partial" || status.Ports.Diagnostic != inspection.Diagnostic || len(status.Ports.Listeners) != 1 || status.Ports.Listeners[0].Port != 43123 {
		t.Fatalf("status ports = %#v, want partial listener snapshot", status.Ports)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	ports, ok := fields["ports"].(map[string]any)
	if !ok || ports["state"] != "partial" || ports["diagnostic"] != inspection.Diagnostic {
		t.Fatalf("serialized status ports = %#v", fields["ports"])
	}
	listeners, ok := ports["listeners"].([]any)
	if !ok || len(listeners) != 1 {
		t.Fatalf("serialized status listeners = %#v", ports["listeners"])
	}
	listener, ok := listeners[0].(map[string]any)
	if !ok || listener["transport"] != "tcp" || listener["address"] != "127.0.0.1" || listener["port"] != float64(43123) {
		t.Fatalf("serialized listener = %#v", listeners[0])
	}

	plainClient := &fakeClient{processes: map[string]protocol.Process{"api": {Name: "api", State: "running"}}}
	plainServer, plainRoot, _ := newTestServer(t, nil, plainClient)
	plainValue, err := plainServer.callTool(context.Background(), "list", args(plainRoot))
	if err != nil {
		t.Fatal(err)
	}
	if len(plainClient.gets) != 0 {
		t.Fatalf("aggregate list inspected ports: process=%+v requests=%#v", plainValue, plainClient.gets)
	}

	// Logging and input must not request socket inspection.
	for _, tool := range []string{"logs", "input"} {
		t.Run(tool+" bypasses ports", func(t *testing.T) {
			internalClient := &fakeClient{processes: map[string]protocol.Process{"api": {Name: "api", State: "running", TTY: true}}}
			internalServer, internalRoot, _ := newTestServer(t, nil, internalClient)
			input := args(internalRoot, "name", "api")
			if tool == "input" {
				input = args(internalRoot, "name", "api", "text", "hello")
			}
			if _, err := internalServer.callTool(context.Background(), tool, input); err != nil {
				t.Fatal(err)
			}
			if tool == "input" && len(internalClient.gets) == 0 {
				t.Fatal("tool did not exercise Get")
			}
			for _, req := range internalClient.gets {
				if req.Ports {
					t.Fatalf("%s requested port inspection: %+v", tool, req)
				}
			}
		})
	}

	var statusTool toolDefinition
	for _, tool := range server.toolDefinitions() {
		if tool.Name == "status" {
			statusTool = tool
			break
		}
	}
	inputProps := statusTool.InputSchema["properties"].(map[string]any)
	if _, ok := inputProps["ports"]; ok {
		t.Fatalf("status ports input schema = %#v", inputProps["ports"])
	}
	outputProps := statusTool.OutputSchema["properties"].(map[string]any)
	portsSchema, ok := outputProps["ports"].(map[string]any)
	if !ok {
		t.Fatalf("status output schema omits ports: %#v", outputProps)
	}
	portsProps := portsSchema["properties"].(map[string]any)
	for _, name := range []string{"state", "listeners", "diagnostic"} {
		if _, ok := portsProps[name]; !ok {
			t.Errorf("ports output schema omits %q", name)
		}
	}
	listenerSchema := portsProps["listeners"].(map[string]any)["items"].(map[string]any)
	listenerProps := listenerSchema["properties"].(map[string]any)
	for _, name := range []string{"transport", "address", "port", "pids"} {
		if _, ok := listenerProps[name]; !ok {
			t.Errorf("listener output schema omits %q", name)
		}
	}
}
