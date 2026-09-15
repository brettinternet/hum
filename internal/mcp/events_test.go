package mcp

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"hum/internal/daemon"
	"hum/internal/protocol"
)

type eventHistoryClient struct {
	*fakeClient
	requests []protocol.EventsRequest
	response protocol.EventsResponse
}

func (c *eventHistoryClient) Events(_ context.Context, request protocol.EventsRequest) (protocol.EventsResponse, error) {
	c.requests = append(c.requests, request)
	return c.response, nil
}

type eventMetadataClient struct {
	*fakeClient
	metadata EventOperationMetadata
}

func (c *eventMetadataClient) Stop(ctx context.Context, request protocol.StopRequest) error {
	c.metadata = EventOperationFromContext(ctx)
	return c.fakeClient.Stop(ctx, request)
}

func TestEvents(t *testing.T) {
	t.Run("bounded tool surface", func(t *testing.T) {
		var found bool
		for _, definition := range NewServer(Options{}).toolDefinitions() {
			if definition.Name == "events" {
				found = true
				if !strings.Contains(strings.ToLower(definition.Description), "never follow") {
					t.Fatal("events description does not document bounded semantics")
				}
			}
			if strings.Contains(definition.Name, "follow") {
				t.Fatalf("unbounded follow tool exposed: %s", definition.Name)
			}
		}
		if !found {
			t.Fatal("events tool missing")
		}
	})

	t.Run("selection filters cursor and structured response", func(t *testing.T) {
		root := t.TempDir()
		after := uint64(7)
		client := &eventHistoryClient{fakeClient: &fakeClient{}, response: protocol.NewEventsResponse([]protocol.HistoryEvent{{Cursor: 8, Time: time.Unix(8, 0).UTC(), Kind: protocol.EventOperation, Name: "api", Event: "start", OperationID: "op-8", Origin: "mcp", Outcome: "failure"}}, 8, true, true)}
		server := NewServer(Options{ClientFactory: func(context.Context, bool) (Client, error) { return client, nil }})
		raw, err := json.Marshal(map[string]any{"project_root": root, "names": []string{"api", "web"}, "kinds": []string{"operation"}, "failed": true, "match": "api|boom", "since_ms": 1000, "tail": 17, "after_cursor": after, "max_bytes": 4096})
		if err != nil {
			t.Fatal(err)
		}
		value, err := server.callTool(context.Background(), "events", raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 1 {
			t.Fatalf("requests=%#v", client.requests)
		}
		request := client.requests[0]
		if request.Scope != protocol.ScopeProject || len(request.Names) != 2 || len(request.Kinds) != 1 || request.Kinds[0] != protocol.EventOperation || !request.Failed || request.Match != "api|boom" || request.SinceUnixNano == 0 || request.Tail != 17 || request.AfterCursor == nil || *request.AfterCursor != 7 || request.MaxBytes != 4096 {
			t.Fatalf("request=%#v", request)
		}
		result := value.(map[string]any)
		events := result["events"].([]protocol.HistoryEvent)
		if len(events) != 1 || events[0].Origin != "mcp" || events[0].OperationID != "op-8" || result["next_cursor"] != protocol.Cursor(8) || result["truncated"] != true || result["has_more"] != true {
			t.Fatalf("structured result=%#v", result)
		}
	})

	t.Run("offline reader applies byte bounded forward paging", func(t *testing.T) {
		root := t.TempDir()
		history := daemon.NewEventHistory(t.TempDir(), protocol.ScopeProject, root)
		for i := 0; i < 6; i++ {
			if _, err := history.Append(protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "launch", Detail: strings.Repeat("x", 100)}); err != nil {
				t.Fatal(err)
			}
		}
		readMetadata := EventOperationMetadata{Name: "unexpected"}
		server := NewServer(Options{
			ClientFactory: func(context.Context, bool) (Client, error) { return nil, ErrDaemonUnavailable },
			EventReader: func(ctx context.Context, request protocol.EventsRequest) (protocol.EventsResponse, error) {
				readMetadata = EventOperationFromContext(ctx)
				var match *regexp.Regexp
				if request.Match != "" {
					match = regexp.MustCompile(request.Match)
				}
				page, err := history.Read(request.Names, time.Unix(0, request.SinceUnixNano), request.Kinds, request.Failed, match, request.Tail, request.AfterCursor, request.MaxBytes)
				return protocol.NewEventsResponse(page.Events, page.NextCursor, page.Truncated, page.HasMore), err
			},
		})
		zero := uint64(0)
		value, err := server.events(context.Background(), commonInput{Scope: protocol.ScopeProject, ProjectRoot: root, Tail: 2000, AfterCursor: &zero, MaxBytes: 300})
		if err != nil {
			t.Fatal(err)
		}
		result := value.(map[string]any)
		if len(result["events"].([]protocol.HistoryEvent)) >= 6 || result["has_more"] != true {
			t.Fatalf("byte bounded result=%#v", result)
		}
		if readMetadata != (EventOperationMetadata{}) {
			t.Fatalf("read-only events carried operation metadata: %#v", readMetadata)
		}
	})

	t.Run("mcp controls carry shared operation metadata", func(t *testing.T) {
		root := t.TempDir()
		client := &eventMetadataClient{fakeClient: &fakeClient{processes: map[string]protocol.Process{"api": {Name: "api", State: protocol.StateRunning}}, stopErr: map[string]error{}}}
		server := NewServer(Options{Resolver: fakeResolver{resolution: Resolution{Root: root, Scope: protocol.ScopeProject}}, ClientFactory: func(context.Context, bool) (Client, error) { return client, nil }})
		raw, _ := json.Marshal(map[string]any{"project_root": root, "name": "api"})
		if _, err := server.callTool(context.Background(), "stop", raw); err != nil {
			t.Fatal(err)
		}
		if client.metadata.Name != "stop" || client.metadata.ID == "" || client.metadata.Origin != "mcp" {
			t.Fatalf("operation metadata=%#v", client.metadata)
		}
	})

	t.Run("malformed filters fail before daemon contact", func(t *testing.T) {
		root := t.TempDir()
		contacts := 0
		server := NewServer(Options{ClientFactory: func(context.Context, bool) (Client, error) {
			contacts++
			return &eventHistoryClient{fakeClient: &fakeClient{}}, nil
		}})
		cases := []map[string]any{
			{"project_root": root, "tail": 2001},
			{"project_root": root, "match": "["},
			{"project_root": root, "kinds": []string{"unknown"}},
			{"project_root": root, "since_ms": -1},
		}
		for _, input := range cases {
			raw, _ := json.Marshal(input)
			if _, err := server.callTool(context.Background(), "events", raw); err == nil {
				t.Fatalf("input %#v unexpectedly succeeded", input)
			}
		}
		if contacts != 0 {
			t.Fatalf("malformed filters contacted daemon %d times", contacts)
		}
	})
}
