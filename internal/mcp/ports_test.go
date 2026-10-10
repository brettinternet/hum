package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"hum/internal/protocol"
)

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
