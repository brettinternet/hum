package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hum/internal/protocol"
)

func TestRestartPolicyMCP(t *testing.T) {
	next := time.Date(2026, time.September, 5, 18, 0, 1, 0, time.UTC)
	client := &fakeClient{processes: map[string]protocol.Process{
		"adhoc":   {Name: "adhoc", Source: "ad_hoc", State: "exited", Restart: protocol.RestartOnFailure},
		"pending": {Name: "pending", Source: "manifest", State: "exited", Restart: protocol.RestartOnFailure, Relaunches: 2, NextLaunchAt: &next},
	}}
	server, root, _ := newTestServer(t, []Definition{
		{Name: "declared", Source: "manifest", Argv: []string{"server"}, Restart: protocol.RestartOnFailure},
		{Name: "discovered", Source: "package_json", Argv: []string{"npm", "run", "dev"}},
	}, client)

	startValue, err := server.callTool(context.Background(), "start", args(root, "name", "declared"))
	if err != nil {
		t.Fatal(err)
	}
	launch, ok := startValue.(launchResult)
	if !ok || launch.Process == nil {
		t.Fatalf("start result = %#v (type %T)", startValue, startValue)
	}
	if len(client.starts) != 1 || client.starts[0].Restart != protocol.RestartOnFailure || launch.Process.Restart != protocol.RestartOnFailure {
		t.Fatalf("start policy propagation: request=%#v process=%#v", client.starts, launch.Process)
	}

	listValue, err := server.callTool(context.Background(), "list", args(root))
	if err != nil {
		t.Fatal(err)
	}
	listed, ok := listValue.([]protocol.Process)
	if !ok {
		t.Fatalf("list result type = %T", listValue)
	}
	byName := make(map[string]protocol.Process, len(listed))
	for _, process := range listed {
		byName[process.Name] = process
	}
	if byName["declared"].Restart != protocol.RestartOnFailure || byName["discovered"].Restart != protocol.RestartNever || byName["adhoc"].Restart != protocol.RestartNever {
		t.Fatalf("list policy values = %#v", byName)
	}
	if byName["pending"].Relaunches != 2 || byName["pending"].NextLaunchAt == nil {
		t.Fatalf("pending list snapshot = %#v", byName["pending"])
	}

	statusValue, err := server.callTool(context.Background(), "status", args(root, "name", "pending"))
	if err != nil {
		t.Fatal(err)
	}
	status, ok := statusValue.(protocol.Process)
	if !ok || status.Restart != protocol.RestartOnFailure || status.Relaunches != 2 || status.NextLaunchAt == nil {
		t.Fatalf("status snapshot = %#v", statusValue)
	}

	if _, err := server.callTool(context.Background(), "restart", args(root, "name", "declared")); err != nil {
		t.Fatal(err)
	}
	if len(client.restarts) != 1 || client.restarts[0].Restart != protocol.RestartOnFailure {
		t.Fatalf("restart policy propagation: %#v", client.restarts)
	}

	if _, err := server.callTool(context.Background(), "down", args(root)); err != nil {
		t.Fatal(err)
	}
	foundPendingStop := false
	for _, request := range client.stops {
		foundPendingStop = foundPendingStop || request.Name == "pending"
	}
	if !foundPendingStop {
		t.Fatalf("down did not cancel pending process: %#v", client.stops)
	}

	definitions := server.toolDefinitions()
	var snapshot map[string]any
	for _, definition := range definitions {
		if definition.Name != "status" {
			continue
		}
		snapshot = definition.OutputSchema
		break
	}
	encoded, _ := json.Marshal(snapshot)
	for _, field := range []string{"restart", "relaunches", "next_launch_at"} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("status schema = %s, missing %q", encoded, field)
		}
	}
}
