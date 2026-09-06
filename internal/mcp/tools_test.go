package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"hum/internal/protocol"
)

type fakeResolver struct {
	resolution Resolution
	err        error
}

func (f fakeResolver) Resolve(context.Context, string) (Resolution, error) {
	return f.resolution, f.err
}

type fakeClient struct {
	mu              sync.Mutex
	processes       map[string]protocol.Process
	output          protocol.OutputResult
	waitResult      protocol.WaitResponse
	startErr        map[string]error
	stopErr         map[string]error
	starts          []protocol.StartRequest
	lists           []protocol.ListRequest
	gets            []protocol.GetRequest
	outputs         []protocol.OutputRequest
	waits           []protocol.WaitRequest
	inputs          []InputRequest
	inputResult     InputResult
	inputErr        error
	stops           []protocol.StopRequest
	restarts        []protocol.RestartRequest
	waitHook        func(protocol.WaitRequest)
	keepStarting    bool
	waited          map[string]bool
	readyBeforeWait bool
}

func (f *fakeClient) Close() error { return nil }
func (f *fakeClient) Start(_ context.Context, req protocol.StartRequest) (protocol.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, req)
	if err := f.startErr[req.Name]; err != nil {
		return protocol.Process{}, err
	}
	p := protocol.Process{Name: req.Name, Source: req.Source, Root: req.Root, Cwd: req.Cwd, Argv: append([]string(nil), req.Argv...), State: "running", LaunchCursor: 7, Restart: req.Restart}
	if req.Ready != nil {
		p.Readiness = &protocol.Readiness{State: protocol.ReadinessStarting, Match: req.Ready.Match}
	}
	f.processes[req.Name] = p
	return p, nil
}
func (f *fakeClient) List(_ context.Context, req protocol.ListRequest) ([]protocol.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists = append(f.lists, req)
	out := make([]protocol.Process, 0, len(f.processes))
	for _, p := range f.processes {
		out = append(out, p)
	}
	return out, nil
}
func (f *fakeClient) Get(_ context.Context, req protocol.GetRequest) (protocol.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets = append(f.gets, req)
	p, ok := f.processes[req.Name]
	if !ok {
		return protocol.Process{}, protocol.NewWireError(protocol.ErrorNotFound, "not found", nil)
	}
	if f.waitResult.Outcome == protocol.WaitExited && f.waited[req.Name] {
		// Mirror a daemon that has already recorded the child's exit by the
		// time Wait reports it, so tests can prove the snapshot returned to
		// the caller is fresh rather than the stale pre-wait record.
		p.State = "exited"
		p.Readiness = nil
		p.PID = 0
		if f.waitResult.Exit != nil {
			p.ExitCode = f.waitResult.Exit.Code
		}
		f.processes[req.Name] = p
		return p, nil
	}
	if p.Readiness != nil && !f.keepStarting && (f.readyBeforeWait || f.waited[req.Name]) {
		p.Readiness = &protocol.Readiness{State: protocol.ReadinessReady, Cursor: p.NextCursor, Match: p.Readiness.Match}
	}
	return p, nil
}
func (f *fakeClient) Output(_ context.Context, req protocol.OutputRequest) (protocol.OutputResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outputs = append(f.outputs, req)
	return f.output, nil
}
func (f *fakeClient) Wait(_ context.Context, req protocol.WaitRequest) (protocol.WaitResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.waitHook != nil {
		f.waitHook(req)
	}
	f.waits = append(f.waits, req)
	if f.waited == nil {
		f.waited = make(map[string]bool)
	}
	f.waited[req.Name] = true
	if f.waitResult.Op == "" {
		return protocol.NewWaitResponse(protocol.WaitMatched, 9, nil), nil
	}
	return f.waitResult, nil
}
func (f *fakeClient) Input(_ context.Context, req InputRequest) (InputResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, req)
	if f.inputErr != nil {
		return InputResult{}, f.inputErr
	}
	if process, ok := f.processes[req.Name]; ok && process.State != "" && process.State != "running" && process.State != "starting" {
		return InputResult{}, &SessionNotRunningError{Name: req.Name}
	}
	if f.inputResult.Name == "" {
		return InputResult{Name: req.Name, Bytes: len(req.Data), LaunchCursor: f.processes[req.Name].LaunchCursor}, nil
	}
	return f.inputResult, nil
}
func (f *fakeClient) Stop(_ context.Context, req protocol.StopRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops = append(f.stops, req)
	return f.stopErr[req.Name]
}
func (f *fakeClient) Remove(_ context.Context, req protocol.RemoveRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops = append(f.stops, protocol.StopRequest{Op: protocol.OpStop, Name: req.Name, Cwd: req.Cwd})
	return f.stopErr[req.Name]
}
func (f *fakeClient) Restart(_ context.Context, req protocol.RestartRequest) (protocol.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarts = append(f.restarts, req)
	p, ok := f.processes[req.Name]
	if !ok {
		return protocol.Process{}, protocol.NewWireError(protocol.ErrorNotFound, "not found", nil)
	}
	p.State = "running"
	p.RestartCount++
	return p, nil
}

func newTestServer(t *testing.T, definitions []Definition, client *fakeClient) (*Server, string, *[]bool) {
	t.Helper()
	root := t.TempDir()
	if client.processes == nil {
		client.processes = map[string]protocol.Process{}
	}
	if client.startErr == nil {
		client.startErr = map[string]error{}
	}
	if client.stopErr == nil {
		client.stopErr = map[string]error{}
	}
	ensures := []bool{}
	s := NewServer(Options{Resolver: fakeResolver{resolution: Resolution{Root: root, Definitions: definitions}}, ClientFactory: func(_ context.Context, ensure bool) (Client, error) {
		ensures = append(ensures, ensure)
		return client, nil
	}, Environment: func() []string { return []string{"TOKEN=secret"} }, Version: "test"})
	return s, root, &ensures
}
func args(root string, values ...any) json.RawMessage {
	m := map[string]any{"project_root": root}
	for i := 0; i < len(values); i += 2 {
		m[values[i].(string)] = values[i+1]
	}
	b, _ := json.Marshal(m)
	return b
}

func TestNoInProcessSupervisor(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", ".")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"hum/internal/app", "hum/internal/process", "hum/internal/output"} {
		for _, line := range strings.Split(string(out), "\n") {
			if line == forbidden {
				t.Fatalf("forbidden dependency %s", forbidden)
			}
		}
	}
}

func TestToolSchemas(t *testing.T) {
	s := NewServer(Options{})
	defs := s.toolDefinitions()
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
		req := d.InputSchema["required"].([]string)
		if !contains(req, "project_root") {
			t.Errorf("%s does not require project_root", d.Name)
		}
		props := d.InputSchema["properties"].(map[string]any)
		if _, ok := props["project_root"]; !ok {
			t.Errorf("%s lacks project_root property", d.Name)
		}
		if d.OutputSchema == nil {
			t.Errorf("%s lacks output schema", d.Name)
		}
		assertSchemaPropertiesDescribed(t, d.Name, d.InputSchema)
	}
	want := []string{"start", "up", "down", "list", "status", "logs", "wait", "input", "restart", "stop", "remove"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tools=%v want %v", names, want)
	}
	blob, _ := json.Marshal(defs)
	for _, forbidden := range []string{`"run"`, `"serve"`, `"shutdown"`} {
		if strings.Contains(string(blob), forbidden) {
			t.Errorf("schema exposes %s", forbidden)
		}
	}
	for _, requiredField := range []string{`"source"`, `"readiness"`, `"launch_cursor"`} {
		if !strings.Contains(string(blob), requiredField) {
			t.Errorf("output schemas omit %s", requiredField)
		}
	}
	inputDefinition := defs[7]
	if inputDefinition.Name != "input" {
		t.Fatalf("input tool position = %d (%s)", 7, inputDefinition.Name)
	}
	if inputDefinition.InputSchema["additionalProperties"] != false {
		t.Fatal("input schema is not closed")
	}
	branches, ok := inputDefinition.InputSchema["oneOf"].([]any)
	if !ok || len(branches) != 2 {
		t.Fatalf("input schema oneOf = %#v", inputDefinition.InputSchema["oneOf"])
	}
	textBranch := branches[0].(map[string]any)["properties"].(map[string]any)
	base64Branch := branches[1].(map[string]any)["properties"].(map[string]any)
	if _, ok := textBranch["base64"]; ok {
		t.Fatal("text input branch accepts base64")
	}
	if _, ok := base64Branch["text"]; ok {
		t.Fatal("base64 input branch accepts text")
	}
	statusProperties := defs[4].OutputSchema["properties"].(map[string]any)
	exited := protocol.Process{
		Name: "api", Root: "/tmp", Cwd: "/tmp", Argv: []string{"api"}, State: "exited",
		Exit: &protocol.Exit{Code: 3, Time: time.Now(), Error: "failed"}, ExitCode: 3, ExitedAt: time.Now(),
	}
	encoded, err := json.Marshal(exited)
	if err != nil {
		t.Fatal(err)
	}
	var processFields map[string]any
	if err := json.Unmarshal(encoded, &processFields); err != nil {
		t.Fatal(err)
	}
	for field := range processFields {
		if _, ok := statusProperties[field]; !ok {
			t.Errorf("status schema rejects serialized process field %q", field)
		}
	}
	exitProperties := statusProperties["exit"].(map[string]any)["properties"].(map[string]any)
	for _, field := range []string{"code", "time", "error"} {
		if _, ok := exitProperties[field]; !ok {
			t.Errorf("exit schema omits %q", field)
		}
	}
	var in bytes.Buffer
	for _, line := range []string{`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`} {
		in.WriteString(line + "\n")
	}
	var out bytes.Buffer
	if err := s.Serve(context.Background(), &in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"protocolVersion":"2025-06-18"`) || !strings.Contains(out.String(), `"tools"`) {
		t.Fatalf("stdio output=%s", out.String())
	}
}

// assertSchemaPropertiesDescribed fails if any property in a tool's input
// schema, including properties nested under oneOf branches, lacks a
// non-empty description. An agent choosing arguments only ever sees this
// schema, so every property needs its own explanation.
func assertSchemaPropertiesDescribed(t *testing.T, toolName string, schema map[string]any) {
	t.Helper()
	if props, ok := schema["properties"].(map[string]any); ok {
		for name, raw := range props {
			property, ok := raw.(map[string]any)
			if !ok {
				t.Errorf("%s.%s is not an object schema: %#v", toolName, name, raw)
				continue
			}
			description, _ := property["description"].(string)
			if description == "" {
				t.Errorf("%s.%s input schema property lacks a description", toolName, name)
			}
		}
	}
	if branches, ok := schema["oneOf"].([]any); ok {
		for _, branch := range branches {
			if branchSchema, ok := branch.(map[string]any); ok {
				assertSchemaPropertiesDescribed(t, toolName, branchSchema)
			}
		}
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func TestUpPreservesCrashRecovery(t *testing.T) {
	next := time.Date(2026, time.September, 6, 5, 0, 1, 0, time.UTC)
	client := &fakeClient{processes: map[string]protocol.Process{
		"pending": {
			Name: "pending", Source: "manifest", State: "exited", Argv: []string{"pending"},
			Readiness:    &protocol.Readiness{State: protocol.ReadinessStarting, Match: "ready"},
			LaunchCursor: 11, Restart: protocol.RestartOnFailure, Relaunches: 2, NextLaunchAt: &next,
		},
		"exhausted": {
			Name: "exhausted", Source: "manifest", State: "exited", Argv: []string{"exhausted"},
			Readiness:    &protocol.Readiness{State: protocol.ReadinessStarting, Match: "ready"},
			LaunchCursor: 19, Restart: protocol.RestartOnFailure, Relaunches: 5,
		},
	}}
	ready := &protocol.ReadinessConfig{Match: "ready", Timeout: time.Second}
	server, root, _ := newTestServer(t, []Definition{
		{Name: "pending", Source: "manifest", Cwd: ".", Argv: []string{"pending"}, Ready: ready, Restart: protocol.RestartOnFailure},
		{Name: "exhausted", Source: "manifest", Cwd: ".", Argv: []string{"exhausted"}, Ready: ready, Restart: protocol.RestartOnFailure},
	}, client)
	for name := range client.processes {
		process := client.processes[name]
		process.Root, process.Cwd = root, root
		client.processes[name] = process
	}

	for attempt := 0; attempt < 2; attempt++ {
		value, err := server.callTool(context.Background(), "up", args(root))
		if err != nil {
			t.Fatalf("up attempt %d: %v", attempt+1, err)
		}
		results, ok := value.([]launchResult)
		if !ok || len(results) != 2 {
			t.Fatalf("up attempt %d result = %#v (type %T), want two results", attempt+1, value, value)
		}
		if results[0].Name != "exhausted" || results[1].Name != "pending" {
			t.Fatalf("up attempt %d order = %#v, want lexical order", attempt+1, results)
		}
		for index, result := range results {
			if result.Process == nil {
				t.Fatalf("up attempt %d result %q omitted process", attempt+1, result.Name)
			}
			process := result.Process
			if process.State != "exited" || process.Source != "manifest" || process.Restart != protocol.RestartOnFailure || process.Readiness == nil || process.Readiness.Match != "ready" {
				t.Fatalf("up attempt %d process %q = %#v, want exited manifest on-failure with retained readiness matcher", attempt+1, result.Name, process)
			}
			if result.Name == "exhausted" {
				if result.Outcome != "recovery_exhausted" || process.Relaunches != 5 || process.NextLaunchAt != nil || process.LaunchCursor != 19 {
					t.Fatalf("up attempt %d exhausted result = %#v", attempt+1, result)
				}
			} else {
				if result.Outcome != "recovery_pending" || process.Relaunches != 2 || process.NextLaunchAt == nil || !process.NextLaunchAt.Equal(next) || process.LaunchCursor != 11 {
					t.Fatalf("up attempt %d pending result = %#v", attempt+1, result)
				}
			}
			if (index == 0 && result.Name != "exhausted") || (index == 1 && result.Name != "pending") {
				t.Fatalf("up attempt %d result index %d = %#v", attempt+1, index, result)
			}
		}
	}
	if len(client.starts) != 0 {
		t.Fatalf("up sent start requests: %#v", client.starts)
	}
	if len(client.waits) != 0 {
		t.Fatalf("up waited on exited recovery: %#v", client.waits)
	}
}

func TestUpReportsManifestRuntimeDrift(t *testing.T) {
	next := time.Date(2026, time.September, 6, 5, 0, 1, 0, time.UTC)
	baseProcesses := map[string]protocol.Process{
		"running":   {Name: "running", Source: "manifest", State: "running", Argv: []string{"running"}, Readiness: &protocol.Readiness{State: protocol.ReadinessStarting, Match: "old"}},
		"pending":   {Name: "pending", Source: "manifest", State: "exited", Argv: []string{"pending"}, Readiness: &protocol.Readiness{State: protocol.ReadinessStarting, Match: "old"}, Restart: protocol.RestartOnFailure, Relaunches: 2, NextLaunchAt: &next},
		"exhausted": {Name: "exhausted", Source: "manifest", State: "exited", Argv: []string{"exhausted"}, Readiness: &protocol.Readiness{State: protocol.ReadinessStarting, Match: "old"}, Restart: protocol.RestartOnFailure, Relaunches: 5},
	}
	definitions := []Definition{
		{Name: "exhausted", Source: "manifest", Cwd: ".", Argv: []string{"exhausted"}, Ready: &protocol.ReadinessConfig{Match: "old"}, Restart: protocol.RestartOnFailure},
		{Name: "pending", Source: "manifest", Cwd: ".", Argv: []string{"pending"}, Ready: &protocol.ReadinessConfig{Match: "old"}, Restart: protocol.RestartOnFailure},
		{Name: "running", Source: "manifest", Cwd: ".", Argv: []string{"running"}, Ready: &protocol.ReadinessConfig{Match: "old"}},
	}
	client := &fakeClient{processes: baseProcesses}
	server, root, _ := newTestServer(t, definitions, client)
	for name, process := range client.processes {
		process.Root, process.Cwd = root, root
		client.processes[name] = process
	}
	value, err := server.callTool(context.Background(), "up", args(root, "no_wait", true))
	if err != nil {
		t.Fatal(err)
	}
	matching := value.([]launchResult)
	if len(matching) != 3 || matching[0].Outcome != "recovery_exhausted" || matching[1].Outcome != "recovery_pending" || matching[2].Outcome != "already_running" {
		t.Fatalf("matching MCP up = %#v", matching)
	}

	for _, name := range []string{"running", "pending", "exhausted"} {
		changed := append([]Definition(nil), definitions...)
		for index := range changed {
			if changed[index].Name == name {
				changed[index].Ready = &protocol.ReadinessConfig{Match: "new"}
			}
		}
		changedClient := &fakeClient{processes: make(map[string]protocol.Process, len(baseProcesses))}
		for processName, process := range baseProcesses {
			process.Root, process.Cwd = root, root
			changedClient.processes[processName] = process
		}
		changedServer, changedRoot, _ := newTestServer(t, changed, changedClient)
		for processName, process := range changedClient.processes {
			process.Root, process.Cwd = changedRoot, changedRoot
			changedClient.processes[processName] = process
		}
		changedValue, changedErr := changedServer.callTool(context.Background(), "up", args(changedRoot, "no_wait", true))
		if changedErr != nil {
			t.Fatalf("%s changed up: %v", name, changedErr)
		}
		changedResults := changedValue.([]launchResult)
		for _, result := range changedResults {
			if result.Name == name {
				if result.Outcome != "definition_drift" || !reflect.DeepEqual(result.ChangedFields, []string{"readiness_match"}) || result.Guidance != "hum restart "+name || result.Process == nil || result.Process.Readiness == nil || result.Process.Readiness.Match != "old" {
					t.Fatalf("%s changed result = %#v", name, result)
				}
			}
		}
		startValue, startErr := changedServer.callTool(context.Background(), "start", args(changedRoot, "name", name, "no_wait", true))
		if startErr != nil {
			t.Fatalf("%s changed start: %v", name, startErr)
		}
		startResult := startValue.(launchResult)
		if startResult.Outcome != "definition_drift" || !reflect.DeepEqual(startResult.ChangedFields, []string{"readiness_match"}) || len(changedClient.starts) != 0 {
			t.Fatalf("%s changed start result = %#v starts=%#v", name, startResult, changedClient.starts)
		}
	}
}

func TestUpRejectsDriftedReadinessGate(t *testing.T) {
	client := &fakeClient{processes: map[string]protocol.Process{
		"db": {Name: "db", Source: "manifest", State: "running", Argv: []string{"db"}, Readiness: &protocol.Readiness{State: protocol.ReadinessStarting, Match: "old"}},
	}}
	definitions := []Definition{
		{Name: "api", Source: "manifest", Cwd: ".", Argv: []string{"api"}, Ready: &protocol.ReadinessConfig{Match: "api-ready"}, After: []string{"db"}},
		{Name: "db", Source: "manifest", Cwd: ".", Argv: []string{"db"}, Ready: &protocol.ReadinessConfig{Match: "new"}},
	}
	server, root, _ := newTestServer(t, definitions, client)
	process := client.processes["db"]
	process.Root, process.Cwd = root, root
	client.processes["db"] = process
	value, err := server.callTool(context.Background(), "up", args(root))
	if err != nil {
		t.Fatal(err)
	}
	results := value.([]launchResult)
	if len(results) != 2 || results[0].Name != "api" || results[0].Outcome != "skipped" || !reflect.DeepEqual(results[0].BlockedBy, []string{"db"}) || results[1].Name != "db" || results[1].Outcome != "definition_drift" {
		t.Fatalf("drifted MCP gate results = %#v", results)
	}
	if len(client.starts) != 0 {
		t.Fatalf("drifted MCP gate starts = %#v", client.starts)
	}
}

func TestUpReportsRemovedManifestSessions(t *testing.T) {
	next := time.Date(2026, time.September, 6, 5, 0, 1, 0, time.UTC)
	client := &fakeClient{processes: map[string]protocol.Process{
		"current":    {Name: "current", Source: "manifest", State: "running", Argv: []string{"current"}},
		"running":    {Name: "running", Source: "manifest", State: "running", Argv: []string{"running"}},
		"pending":    {Name: "pending", Source: "manifest", State: "exited", Argv: []string{"pending"}, Restart: protocol.RestartOnFailure, Relaunches: 2, NextLaunchAt: &next},
		"exhausted":  {Name: "exhausted", Source: "manifest", State: "exited", Argv: []string{"exhausted"}, Restart: protocol.RestartOnFailure, Relaunches: 5},
		"stopped":    {Name: "stopped", Source: "manifest", State: "exited", Argv: []string{"stopped"}},
		"ad_hoc":     {Name: "ad_hoc", Source: "ad_hoc", State: "running", Argv: []string{"ad_hoc"}},
		"discovered": {Name: "discovered", Source: "package_json", State: "running", Argv: []string{"discovered"}},
	}}
	definitions := []Definition{{Name: "current", Source: "manifest", Cwd: ".", Argv: []string{"current"}}}
	server, root, _ := newTestServer(t, definitions, client)
	for name, process := range client.processes {
		process.Root, process.Cwd = root, root
		client.processes[name] = process
	}
	value, err := server.callTool(context.Background(), "up", args(root, "no_wait", true))
	if err != nil {
		t.Fatal(err)
	}
	results := value.([]launchResult)
	if len(results) != 4 {
		t.Fatalf("removed MCP results = %#v", results)
	}
	for index, name := range []string{"current", "exhausted", "pending", "running"} {
		if results[index].Name != name {
			t.Fatalf("removed MCP order = %#v", results)
		}
	}
	if results[0].Outcome != "already_running" {
		t.Fatalf("current MCP result = %#v", results[0])
	}
	for _, result := range results[1:] {
		if result.Outcome != "removed_definition" || !strings.Contains(result.Guidance, "hum stop "+result.Name) || !strings.Contains(result.Guidance, "hum remove "+result.Name) || result.Process == nil {
			t.Fatalf("removed MCP result = %#v", result)
		}
	}
}

func TestUpAdapterParity(t *testing.T) {
	if got := durationToMCPTimeout(time.Nanosecond); got != 1 {
		t.Fatalf("positive sub-millisecond readiness timeout = %dms, want 1ms", got)
	}
	ready := &protocol.ReadinessConfig{Match: "new"}
	next := time.Now().Add(time.Minute)
	client := &fakeClient{processes: map[string]protocol.Process{
		"db": {
			Name: "db", Source: "manifest", State: "running", Argv: []string{"db"},
			Readiness: &protocol.Readiness{State: protocol.ReadinessStarting, Match: "old"},
		},
		"pending": {
			Name: "pending", Source: "manifest", State: "exited", Argv: []string{"pending"},
			Readiness: &protocol.Readiness{State: protocol.ReadinessStarting, Match: "ready"},
			Restart:   protocol.RestartOnFailure, Relaunches: 2, NextLaunchAt: &next,
		},
	}}
	definitions := []Definition{
		{Name: "api", Source: "manifest", Argv: []string{"api"}, Cwd: ".", Ready: ready, After: []string{"db"}},
		{Name: "db", Source: "manifest", Argv: []string{"db"}, Cwd: ".", Ready: ready},
		{Name: "pending", Source: "manifest", Argv: []string{"pending"}, Cwd: ".", Ready: &protocol.ReadinessConfig{Match: "ready"}, Restart: protocol.RestartOnFailure},
	}
	server, root, _ := newTestServer(t, definitions, client)
	for name, process := range client.processes {
		process.Root, process.Cwd = root, root
		client.processes[name] = process
	}
	value, err := server.callTool(context.Background(), "up", args(root))
	if err != nil {
		t.Fatal(err)
	}
	results := value.([]launchResult)
	if got := []string{results[0].Name, results[1].Name, results[2].Name}; !reflect.DeepEqual(got, []string{"api", "db", "pending"}) {
		t.Fatalf("adapter ordering=%v", got)
	}
	if results[0].Outcome != "skipped" || !reflect.DeepEqual(results[0].BlockedBy, []string{"db"}) || results[0].Guidance != "" {
		t.Fatalf("adapter blocked result=%#v", results[0])
	}
	if results[1].Outcome != "definition_drift" || !reflect.DeepEqual(results[1].ChangedFields, []string{"readiness_match"}) || results[1].Guidance != "hum restart db" {
		t.Fatalf("adapter drift result=%#v", results[1])
	}
	if results[2].Outcome != "recovery_pending" || results[2].Process == nil || results[2].Process.Readiness == nil || results[2].Process.Readiness.Match != "ready" || results[2].Process.NextLaunchAt == nil {
		t.Fatalf("adapter recovery result=%#v", results[2])
	}
}

func TestUpReportsRemovedManifestSessionsWithoutDefinitions(t *testing.T) {
	next := time.Date(2026, time.September, 6, 5, 0, 1, 0, time.UTC)
	client := &fakeClient{processes: map[string]protocol.Process{
		"removed": {
			Name: "removed", Source: "manifest", State: "exited", Argv: []string{"removed"},
			Restart: protocol.RestartOnFailure, Relaunches: 2, NextLaunchAt: &next,
		},
	}}
	server, root, _ := newTestServer(t, nil, client)
	process := client.processes["removed"]
	process.Root, process.Cwd = root, root
	client.processes["removed"] = process

	value, err := server.callTool(context.Background(), "up", args(root, "no_wait", true))
	if err != nil {
		t.Fatal(err)
	}
	results := value.([]launchResult)
	if len(results) != 1 || results[0].Name != "removed" || results[0].Outcome != "removed_definition" || results[0].Process == nil || !strings.Contains(results[0].Guidance, "hum stop removed") || !strings.Contains(results[0].Guidance, "hum remove removed") {
		t.Fatalf("removed MCP up without definitions = %#v", results)
	}
}

func TestToolValidation(t *testing.T) {
	client := &fakeClient{}
	s, root, _ := newTestServer(t, []Definition{{Name: "api", Source: "hum.yaml", Argv: []string{"api"}, Cwd: "/tmp"}}, client)
	fileRoot := filepath.Join(root, "file")
	if err := os.WriteFile(fileRoot, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		raw  json.RawMessage
	}{
		{"relative", json.RawMessage(`{"project_root":"."}`)},
		{"nonexistent", args(filepath.Join(root, "missing"))},
		{"file_root", args(fileRoot)},
		{"missing_name", args(root)},
		{"unknown", args(root, "name", "x", "extra", true)},
		{"timeout", args(root, "name", "x", "timeout_ms", -1)},
		{"match", args(root, "name", "x", "match", "[")},
	}
	tools := []string{"list", "list", "list", "status", "status", "start", "wait"}
	for i, tc := range cases {
		if _, err := s.callTool(context.Background(), tools[i], tc.raw); err == nil {
			t.Errorf("%s accepted", tc.name)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{protocol.NewWireError(protocol.ErrorNotFound, "gone", map[string]any{"name": "x"}), "not_found"}, {ErrDaemonUnavailable, "unavailable"}, {errors.New("boom"), "internal"}} {
		if got := mapError(tc.err); got.Code != tc.code {
			t.Errorf("mapError(%v)=%s", tc.err, got.Code)
		}
	}
	root := t.TempDir()
	s := NewServer(Options{Resolver: fakeResolver{resolution: Resolution{Root: root}}, ClientFactory: func(context.Context, bool) (Client, error) { return nil, ErrDaemonUnavailable }})
	got, err := s.callTool(context.Background(), "list", args(root))
	if err != nil || len(got.([]protocol.Process)) != 0 {
		t.Fatalf("unavailable list=%#v err=%v", got, err)
	}
	for _, tool := range []string{"status", "logs", "wait", "input", "restart"} {
		arguments := args(root, "name", "raw")
		if tool == "input" {
			arguments = args(root, "name", "raw", "text", "x")
		}
		if _, callErr := s.callTool(context.Background(), tool, arguments); mapError(callErr).Code != "unavailable" {
			t.Errorf("%s unavailable error = %v", tool, callErr)
		}
	}
	for _, tool := range []string{"down", "stop"} {
		arguments := args(root)
		if tool == "stop" {
			arguments = args(root, "name", "raw")
		}
		if _, callErr := s.callTool(context.Background(), tool, arguments); callErr != nil {
			t.Errorf("%s should succeed without daemon: %v", tool, callErr)
		}
	}
}

func TestStartUp(t *testing.T) {
	ready := &protocol.ReadinessConfig{Match: "ready"}
	defs := []Definition{{Name: "api", Source: "hum.yaml", Argv: []string{"api"}, Cwd: "/tmp", Ready: ready}, {Name: "web", Source: "package.json", Argv: []string{"web"}, Cwd: "/tmp"}}
	client := &fakeClient{}
	s, root, ensures := newTestServer(t, defs, client)
	got, err := s.callTool(context.Background(), "start", args(root, "name", "api"))
	if err != nil {
		t.Fatal(err)
	}
	launch := got.(launchResult)
	if launch.Outcome != "started" || launch.Process == nil {
		t.Fatalf("start=%#v", launch)
	}
	p := *launch.Process
	if p.Readiness == nil || p.Readiness.State != protocol.ReadinessReady {
		t.Fatalf("start=%#v", p)
	}
	if len(client.waits) != 1 || client.waits[0].TimeoutMS < defaultTimeoutMS-1000 || client.waits[0].TimeoutMS > defaultTimeoutMS || client.waits[0].After != nil {
		t.Fatalf("wait=%#v", client.waits)
	}
	if len(client.starts[0].Env) != 1 {
		t.Fatal("start omitted server env")
	}
	if _, err = s.callTool(context.Background(), "start", args(root, "name", "api")); err != nil {
		t.Fatalf("repeat start: %v", err)
	}
	if len(client.starts) != 1 || len(client.waits) != 1 {
		t.Fatalf("repeat start launched or waited again: starts=%#v waits=%#v", client.starts, client.waits)
	}
	collision := client.processes["api"]
	collision.Source = ""
	client.processes["api"] = collision
	if _, err = s.callTool(context.Background(), "start", args(root, "name", "api", "no_wait", true)); mapError(err).Code != string(protocol.ErrorNameInUse) {
		t.Fatalf("ad hoc collision error = %v", err)
	}
	collisionValue, err := s.callTool(context.Background(), "up", args(root, "no_wait", true))
	if err != nil {
		t.Fatal(err)
	}
	collisionResults := collisionValue.([]launchResult)
	if collisionResults[0].Outcome != "error" || collisionResults[0].Error == nil || collisionResults[0].Error.Code != string(protocol.ErrorNameInUse) {
		t.Fatalf("up collision = %#v", collisionResults[0])
	}
	collision.Source = "hum.yaml"
	client.processes["api"] = collision
	got, err = s.callTool(context.Background(), "up", args(root, "no_wait", true))
	if err != nil {
		t.Fatal(err)
	}
	results := got.([]launchResult)
	if len(results) != 2 {
		t.Fatalf("up=%#v", results)
	}
	if results[1].Process == nil || results[1].Process.Readiness.State != protocol.ReadinessRunningUnverified {
		t.Fatalf("discovered=%#v", results[1])
	}
	for _, ensure := range *ensures {
		if !ensure {
			t.Fatalf("launch used ensure=false: %v", *ensures)
		}
	}
	concurrentReady := &protocol.ReadinessConfig{Match: "ready", Timeout: 17 * time.Millisecond}
	concurrentClient := &fakeClient{}
	concurrentDefinitions := []Definition{
		{Name: "api", Source: "hum.yaml", Argv: []string{"api"}, Cwd: "/tmp", Ready: concurrentReady},
		{Name: "db", Source: "hum.yaml", Argv: []string{"db"}, Cwd: "/tmp", Ready: &protocol.ReadinessConfig{Match: "ready"}},
	}
	concurrentServer, concurrentRoot, _ := newTestServer(t, concurrentDefinitions, concurrentClient)
	concurrentClient.waitHook = func(req protocol.WaitRequest) {
		process, started := concurrentClient.processes[req.Name]
		if !started || process.State != "running" {
			t.Errorf("readiness wait for %q began before its process launch: %#v", req.Name, concurrentClient.starts)
		}
	}
	if _, err := concurrentServer.callTool(context.Background(), "up", args(concurrentRoot)); err != nil {
		t.Fatal(err)
	}
	timeouts := []int64{concurrentClient.waits[0].TimeoutMS, concurrentClient.waits[1].TimeoutMS}
	sort.Slice(timeouts, func(i, j int) bool { return timeouts[i] < timeouts[j] })
	if timeouts[0] < 1 || timeouts[0] > 17 || timeouts[1] < defaultTimeoutMS-1000 || timeouts[1] > defaultTimeoutMS {
		t.Fatalf("readiness timeouts = %v", timeouts)
	}
	timeoutClient := &fakeClient{keepStarting: true, waitResult: protocol.NewWaitResponse(protocol.WaitTimedOut, 9, nil)}
	immediateClient := &fakeClient{readyBeforeWait: true}
	immediateServer, immediateRoot, _ := newTestServer(t, []Definition{{Name: "fast", Source: "hum.yaml", Argv: []string{"fast"}, Cwd: "/tmp", Ready: ready}}, immediateClient)
	immediateValue, err := immediateServer.callTool(context.Background(), "start", args(immediateRoot, "name", "fast"))
	if err != nil || immediateValue.(launchResult).Outcome != "started" || len(immediateClient.waits) != 0 {
		t.Fatalf("ready-before-wait start = %#v waits=%#v err=%v", immediateValue, immediateClient.waits, err)
	}
	matchClient := &fakeClient{keepStarting: true, processes: map[string]protocol.Process{
		"api": {Name: "api", Source: "hum.yaml", Root: root, State: "running", Readiness: &protocol.Readiness{State: protocol.ReadinessStarting, Match: "new"}},
	}}
	initial := protocol.Process{Name: "api", Source: "hum.yaml", Root: root, State: "running", Readiness: &protocol.Readiness{State: protocol.ReadinessStarting, Match: "old"}}
	current, outcome, err := s.mcpWaitForReadiness(context.Background(), matchClient, Resolution{Root: root}, Definition{Name: "api", Ready: &protocol.ReadinessConfig{Match: "new"}}, initial, "already_running", defaultTimeoutMS)
	if err != nil || outcome != "already_running" || current.Readiness.Match != "new" || len(matchClient.waits) != 0 {
		t.Fatalf("changed readiness match = %#v outcome=%q waits=%#v err=%v", current, outcome, matchClient.waits, err)
	}
	timeoutServer, timeoutRoot, _ := newTestServer(t, []Definition{{Name: "slow", Source: "hum.yaml", Argv: []string{"slow"}, Cwd: "/tmp", Ready: ready}}, timeoutClient)
	timeoutValue, err := timeoutServer.callTool(context.Background(), "start", args(timeoutRoot, "name", "slow"))
	if err != nil || timeoutValue.(launchResult).Outcome != "timed_out" {
		t.Fatalf("timed out start = %#v, %v", timeoutValue, err)
	}
	timeoutUp, err := timeoutServer.callTool(context.Background(), "up", args(timeoutRoot))
	if err != nil || timeoutUp.([]launchResult)[0].Outcome != "timed_out" {
		t.Fatalf("timed out up = %#v, %v", timeoutUp, err)
	}
	exitedClient := &fakeClient{waitResult: protocol.NewWaitResponse(protocol.WaitExited, 9, &protocol.Exit{Code: 3})}
	exitedServer, exitedRoot, _ := newTestServer(t, []Definition{{Name: "short", Source: "hum.yaml", Argv: []string{"short"}, Cwd: "/tmp", Ready: ready}}, exitedClient)
	exitedValue, err := exitedServer.callTool(context.Background(), "start", args(exitedRoot, "name", "short"))
	exitedResult, _ := exitedValue.(launchResult)
	if err != nil || exitedResult.Outcome != "exited_before_ready" || exitedResult.Process == nil || exitedResult.Process.State != "exited" || exitedResult.Process.ExitCode != 3 {
		t.Fatalf("exited start = %#v, %v, want fresh state=exited exit_code=3", exitedValue, err)
	}
}

func TestUpOrdersByAfter(t *testing.T) {
	ready := &protocol.ReadinessConfig{Match: "ready"}
	client := &fakeClient{}
	defs := []Definition{
		{Name: "web", Source: "hum.yaml", Argv: []string{"web"}, Cwd: "/tmp", Ready: ready, After: []string{"api"}},
		{Name: "api", Source: "hum.yaml", Argv: []string{"api"}, Cwd: "/tmp", Ready: ready, After: []string{"db"}},
		{Name: "db", Source: "hum.yaml", Argv: []string{"db"}, Cwd: "/tmp", Ready: ready},
	}
	server, root, _ := newTestServer(t, defs, client)
	value, err := server.callTool(context.Background(), "up", args(root))
	if err != nil {
		t.Fatal(err)
	}
	results := value.([]launchResult)
	if got := []string{results[0].Name, results[1].Name, results[2].Name}; !reflect.DeepEqual(got, []string{"api", "db", "web"}) {
		t.Fatalf("MCP up order = %v", got)
	}
	for _, result := range results {
		if result.Outcome != "started" || result.Process == nil || result.Process.Readiness == nil || result.Process.Readiness.State != protocol.ReadinessReady {
			t.Fatalf("MCP up result = %#v", result)
		}
	}
	if got := []string{client.starts[0].Name, client.starts[1].Name, client.starts[2].Name}; !reflect.DeepEqual(got, []string{"db", "api", "web"}) {
		t.Fatalf("MCP launch order = %v", got)
	}
	if _, err := server.callTool(context.Background(), "up", args(root)); err != nil {
		t.Fatalf("idempotent MCP up: %v", err)
	}
	if len(client.starts) != 3 {
		t.Fatalf("idempotent MCP up relaunched: %#v", client.starts)
	}

	noWaitClient := &fakeClient{}
	noWaitServer, noWaitRoot, noWaitEnsures := newTestServer(t, []Definition{{Name: "api", Source: "hum.yaml", Argv: []string{"api"}, Cwd: "/tmp", Ready: ready, After: []string{"db"}}, {Name: "db", Source: "hum.yaml", Argv: []string{"db"}, Cwd: "/tmp", Ready: ready}}, noWaitClient)
	if _, err := noWaitServer.callTool(context.Background(), "up", args(noWaitRoot, "no_wait", true)); err == nil {
		t.Fatal("MCP up no_wait accepted after dependency")
	}
	if len(*noWaitEnsures) != 0 {
		t.Fatalf("MCP no_wait contacted daemon: %v", *noWaitEnsures)
	}

	blockedClient := &fakeClient{startErr: map[string]error{"db": errors.New("request failed"), "queue": errors.New("queue failed")}}
	blockedServer, blockedRoot, _ := newTestServer(t, []Definition{
		{Name: "web", Source: "hum.yaml", Argv: []string{"web"}, Cwd: "/tmp", Ready: ready, After: []string{"api"}},
		{Name: "api", Source: "hum.yaml", Argv: []string{"api"}, Cwd: "/tmp", Ready: ready, After: []string{"queue", "db"}},
		{Name: "db", Source: "hum.yaml", Argv: []string{"db"}, Cwd: "/tmp", Ready: ready},
		{Name: "queue", Source: "hum.yaml", Argv: []string{"queue"}, Cwd: "/tmp", Ready: ready},
	}, blockedClient)
	blockedValue, err := blockedServer.callTool(context.Background(), "up", args(blockedRoot))
	if err != nil {
		t.Fatal(err)
	}
	blockedResults := blockedValue.([]launchResult)
	if !reflect.DeepEqual(blockedResults[0].BlockedBy, []string{"db", "queue"}) || blockedResults[0].Outcome != "skipped" {
		t.Fatalf("direct blockers = %#v", blockedResults[0])
	}
	if !reflect.DeepEqual(blockedResults[3].BlockedBy, []string{"api"}) || blockedResults[3].Outcome != "skipped" {
		t.Fatalf("cascade blockers = %#v", blockedResults[3])
	}

	matchedThenExited := &fakeClient{keepStarting: true}
	matchedThenExited.waitHook = func(req protocol.WaitRequest) {
		process := matchedThenExited.processes[req.Name]
		process.State = "exited"
		matchedThenExited.processes[req.Name] = process
	}
	matchedServer, matchedRoot, _ := newTestServer(t, []Definition{
		{Name: "api", Source: "hum.yaml", Argv: []string{"api"}, Cwd: "/tmp", Ready: ready, After: []string{"db"}},
		{Name: "db", Source: "hum.yaml", Argv: []string{"db"}, Cwd: "/tmp", Ready: ready},
	}, matchedThenExited)
	matchedValue, err := matchedServer.callTool(context.Background(), "up", args(matchedRoot))
	if err != nil {
		t.Fatal(err)
	}
	matchedResults := matchedValue.([]launchResult)
	if matchedResults[1].Outcome != "started" || matchedResults[1].Process == nil || matchedResults[1].Process.Readiness == nil || matchedResults[1].Process.Readiness.State != protocol.ReadinessReady {
		t.Fatalf("matched-then-exited prerequisite = %#v, want started/ready", matchedResults[1])
	}
	if len(matchedThenExited.starts) != 2 || matchedThenExited.starts[1].Name != "api" {
		t.Fatalf("matched-then-exited starts = %#v, want dependent api launched", matchedThenExited.starts)
	}
}

func TestUpReportsBlockedExistingState(t *testing.T) {
	ready := &protocol.ReadinessConfig{Match: "ready"}
	definitions := []Definition{
		{Name: "web", Source: "hum.yaml", Argv: []string{"web"}, Cwd: "/tmp", Ready: ready, After: []string{"api"}},
		{Name: "api", Source: "hum.yaml", Argv: []string{"api"}, Cwd: "/tmp", Ready: ready, After: []string{"db"}},
		{Name: "db", Source: "hum.yaml", Argv: []string{"db"}, Cwd: "/tmp", Ready: ready},
	}
	nextLaunch := time.Now().Add(time.Second).UTC().Truncate(time.Millisecond)
	for _, test := range []struct {
		name  string
		state string
		pid   int
	}{
		{name: "running", state: "running", pid: 41},
		{name: "exited", state: "exited"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{
				processes: map[string]protocol.Process{
					"api": {Name: "api", Source: "manifest", Root: "/root", State: test.state, PID: test.pid, LaunchCursor: 12, Restart: "on-failure", Relaunches: 2, NextLaunchAt: &nextLaunch},
				},
				startErr: map[string]error{"db": errors.New("request failed")},
			}
			server, root, _ := newTestServer(t, definitions, client)
			upSchema := server.toolDefinitions()[1].OutputSchema
			upProperties := upSchema["properties"].(map[string]any)
			resultsSchema := upProperties["results"].(map[string]any)
			upItems := resultsSchema["items"].(map[string]any)
			upResultProperties := upItems["properties"].(map[string]any)
			if _, ok := upResultProperties["existing_state"]; !ok || upItems["additionalProperties"] != false {
				t.Fatalf("up result schema omits closed existing_state: %#v", upItems)
			}
			value, err := server.callTool(context.Background(), "up", args(root))
			if err != nil {
				t.Fatal(err)
			}
			results := value.([]launchResult)
			if results[0].Outcome != "skipped" || results[0].ExistingState != test.state || results[0].Process == nil || results[0].Process.LaunchCursor != 12 || results[0].Process.Restart != "on-failure" || results[0].Process.Relaunches != 2 || results[0].Process.NextLaunchAt == nil {
				t.Fatalf("blocked %s api = %#v", test.state, results[0])
			}
			if test.state == "running" && results[0].Process.PID != test.pid {
				t.Fatalf("blocked running PID = %d, want %d", results[0].Process.PID, test.pid)
			}
			if !reflect.DeepEqual(results[0].BlockedBy, []string{"db"}) || results[2].Outcome != "skipped" || !reflect.DeepEqual(results[2].BlockedBy, []string{"api"}) || results[2].ExistingState != "" || results[2].Process != nil {
				t.Fatalf("blocked chain = %#v", results)
			}
			if len(client.starts) != 1 || client.starts[0].Name != "db" {
				t.Fatalf("blocked lifecycle requests = %#v, want only db start", client.starts)
			}
		})
	}
}

func TestDown(t *testing.T) {
	defs := []Definition{{Name: "declared", Source: "hum.yaml", Argv: []string{"x"}, Cwd: "/tmp"}}
	client := &fakeClient{processes: map[string]protocol.Process{"transient": {Name: "transient", Root: "/root", State: "running"}}}
	s, root, ensures := newTestServer(t, defs, client)
	got, err := s.callTool(context.Background(), "down", args(root))
	if err != nil {
		t.Fatal(err)
	}
	results := got.([]stopResult)
	if len(results) != 2 || results[0].Name != "declared" || results[0].State != "not_running" || results[1].State != "stopped" {
		t.Fatalf("down=%#v", results)
	}
	if len(client.stops) != 1 || client.stops[0].Name != "transient" {
		t.Fatalf("stops=%#v", client.stops)
	}
	if (*ensures)[0] {
		t.Fatal("down created daemon")
	}
}

func TestStatusListFollowers(t *testing.T) {
	client := &fakeClient{processes: map[string]protocol.Process{
		"watched": {Name: "watched", State: "running", Followers: 2},
	}}
	s, root, _ := newTestServer(t, nil, client)

	listedValue, err := s.callTool(context.Background(), "list", args(root))
	if err != nil {
		t.Fatal(err)
	}
	listed := listedValue.([]protocol.Process)
	if len(listed) != 1 || listed[0].Followers != 2 {
		t.Fatalf("list followers = %#v, want 2", listed)
	}
	statusValue, err := s.callTool(context.Background(), "status", args(root, "name", "watched"))
	if err != nil {
		t.Fatal(err)
	}
	if got := statusValue.(protocol.Process).Followers; got != 2 {
		t.Fatalf("status followers = %d, want 2", got)
	}
	properties := s.toolDefinitions()[4].OutputSchema["properties"].(map[string]any)
	followers, ok := properties["followers"].(map[string]any)
	if !ok || followers["type"] != "integer" {
		t.Fatalf("followers schema = %#v, want integer", properties["followers"])
	}
}

func TestObservationTools(t *testing.T) {
	cursor := protocol.Cursor(12)
	client := &fakeClient{processes: map[string]protocol.Process{"raw": {Name: "raw", State: "running", LaunchCursor: 5}}, output: protocol.OutputResult{Next: &cursor}}
	defs := []Definition{{Name: "api", Source: "hum.yaml", Argv: []string{"api"}, Cwd: "/tmp"}}
	s, root, ensures := newTestServer(t, defs, client)
	got, err := s.callTool(context.Background(), "list", args(root))
	if err != nil {
		t.Fatal(err)
	}
	listed := got.([]protocol.Process)
	if len(listed) != 2 || listed[1].Source != "ad_hoc" {
		t.Fatalf("list=%#v", listed)
	}
	if _, err = s.callTool(context.Background(), "status", args(root, "name", "raw")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.callTool(context.Background(), "logs", args(root, "name", "raw", "tail", 2, "max_entries", 3, "max_bytes", 100)); err != nil {
		t.Fatal(err)
	}
	if client.outputs[0].Tail != 2 || client.outputs[0].MaxEntries != 3 {
		t.Fatalf("output req=%#v", client.outputs[0])
	}
	if _, err = s.callTool(context.Background(), "logs", args(root, "name", "raw", "tail", 200)); err != nil {
		t.Fatal(err)
	}
	if got := client.outputs[len(client.outputs)-1]; got.Tail != 200 || got.MaxEntries != 200 {
		t.Fatalf("tail-only output req = %#v, want max_entries defaulted to tail", got)
	}
	if _, err = s.callTool(context.Background(), "wait", args(root, "name", "raw")); err != nil {
		t.Fatal(err)
	}
	if client.waits[len(client.waits)-1].After != nil {
		t.Fatalf("default session wait unexpectedly set after cursor: %#v", client.waits)
	}
	for _, ensure := range *ensures {
		if ensure {
			t.Fatalf("observation created daemon: %v", *ensures)
		}
	}
}

func TestRemoveAdHocProcessTools(t *testing.T) {
	client := &fakeClient{processes: map[string]protocol.Process{"raw": {Name: "raw", Argv: []string{"sleep", "1"}, Cwd: "/tmp", State: "running"}, "api": {Name: "api", Source: "old", Argv: []string{"old"}, State: "running"}}}
	defs := []Definition{{Name: "api", Source: "hum.yaml", Argv: []string{"new"}, Cwd: "/work"}}
	s, root, ensures := newTestServer(t, defs, client)
	got, err := s.callTool(context.Background(), "restart", args(root, "name", "raw"))
	if err != nil {
		t.Fatal(err)
	}
	if got.(protocol.Process).Source != "ad_hoc" || client.restarts[0].Update || len(client.restarts[0].Env) != 0 {
		t.Fatalf("ad hoc restart=%#v result=%#v", client.restarts[0], got)
	}
	if _, err = s.callTool(context.Background(), "restart", args(root, "name", "api")); err != nil {
		t.Fatal(err)
	}
	resolved := client.restarts[1]
	if !resolved.Update || !reflect.DeepEqual(resolved.Argv, []string{"new"}) || !reflect.DeepEqual(resolved.Env, []string{"TOKEN=secret"}) {
		t.Fatalf("resolved restart=%#v", resolved)
	}
	if _, err = s.callTool(context.Background(), "stop", args(root, "name", "raw")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.callTool(context.Background(), "remove", args(root, "name", "raw")); err != nil {
		t.Fatal(err)
	}
	if len(client.stops) != 2 {
		t.Fatalf("stop/remove calls=%#v", client.stops)
	}
	for _, ensure := range *ensures {
		if ensure {
			t.Fatal("control created daemon")
		}
	}
	data, _ := json.Marshal(got)
	if strings.Contains(string(data), "TOKEN=secret") {
		t.Fatal("response exposed environment")
	}
	delete(client.processes, "raw")
	if _, err = s.callTool(context.Background(), "restart", args(root, "name", "raw")); mapError(err).Code != string(protocol.ErrorNotFound) {
		t.Fatalf("evicted ad hoc restart error = %v", err)
	}
}

func TestSortedToolNames(t *testing.T) {
	defs := NewServer(Options{}).toolDefinitions()
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	if len(names) != len(sorted) {
		t.Fatal("unreachable")
	}
}

func TestInputTool(t *testing.T) {
	client := &fakeClient{processes: map[string]protocol.Process{}, startErr: map[string]error{}, stopErr: map[string]error{}}
	server, root, _ := newTestServer(t, []Definition{{Name: "prompt", Source: "manifest", Argv: []string{"prompt"}, TTY: true}}, client)
	client.processes["prompt"] = protocol.Process{Name: "prompt", Root: root, Cwd: root, TTY: true, State: "running", LaunchCursor: 12}

	inputDefinition := server.toolDefinitions()[7]
	if inputDefinition.Name != "input" || inputDefinition.InputSchema["additionalProperties"] != false {
		t.Fatalf("input schema = %#v", inputDefinition)
	}
	inputRequired, ok := inputDefinition.InputSchema["required"].([]string)
	if !ok || !contains(inputRequired, "project_root") || !contains(inputRequired, "name") {
		t.Fatalf("input required fields = %#v", inputDefinition.InputSchema["required"])
	}
	branches, ok := inputDefinition.InputSchema["oneOf"].([]any)
	if !ok || len(branches) != 2 {
		t.Fatalf("input oneOf = %#v", inputDefinition.InputSchema["oneOf"])
	}
	for index, branch := range branches {
		branchSchema, ok := branch.(map[string]any)
		if !ok || branchSchema["additionalProperties"] != false {
			t.Fatalf("input branch %d = %#v", index, branch)
		}
		required, ok := branchSchema["required"].([]string)
		if !ok || !contains(required, "project_root") || !contains(required, "name") {
			t.Fatalf("input branch %d required = %#v", index, branchSchema["required"])
		}
		properties, ok := branchSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("input branch %d properties = %#v", index, branchSchema["properties"])
		}
		if index == 0 {
			if !contains(required, "text") || properties["text"].(map[string]any)["minLength"] != 1 {
				t.Fatalf("text branch = %#v", branchSchema)
			}
			if _, exists := properties["base64"]; exists {
				t.Fatal("text branch accepts base64")
			}
		} else {
			base64Property := properties["base64"].(map[string]any)
			if !contains(required, "base64") || base64Property["pattern"] != "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$" || !strings.Contains(base64Property["description"].(string), "without whitespace") {
				t.Fatalf("base64 branch = %#v", branchSchema)
			}
			if _, exists := properties["text"]; exists {
				t.Fatal("base64 branch accepts text")
			}
		}
	}
	outputProperties, ok := inputDefinition.OutputSchema["properties"].(map[string]any)
	outputRequired, requiredOK := inputDefinition.OutputSchema["required"].([]string)
	if !ok || !requiredOK || inputDefinition.OutputSchema["additionalProperties"] != false {
		t.Fatalf("input output schema = %#v", inputDefinition.OutputSchema)
	}
	for _, field := range []string{"name", "bytes", "launch_cursor"} {
		if !contains(outputRequired, field) {
			t.Fatalf("input output schema does not require %q", field)
		}
		if _, exists := outputProperties[field]; !exists {
			t.Fatalf("input output schema missing %q", field)
		}
	}

	textPayload := "hé\x00"
	value, err := server.callTool(context.Background(), "input", args(root, "name", "prompt", "text", textPayload))
	if err != nil {
		t.Fatalf("input text: %v", err)
	}
	result, ok := value.(InputResult)
	if !ok || result.Name != "prompt" || result.Bytes != len([]byte(textPayload)) || result.LaunchCursor != 12 {
		t.Fatalf("input result = %#v", value)
	}
	if len(client.inputs) != 1 || !bytes.Equal(client.inputs[0].Data, []byte(textPayload)) || client.inputs[0].Root != root || client.inputs[0].Cwd != root {
		t.Fatalf("input request = %#v", client.inputs)
	}
	value, err = server.callTool(context.Background(), "input", args(root, "name", "prompt", "base64", "AP8="))
	if err != nil {
		t.Fatalf("input base64: %v", err)
	}
	result, ok = value.(InputResult)
	if !ok || result.Bytes != 2 || result.LaunchCursor != 12 || !bytes.Equal(client.inputs[1].Data, []byte{0, 255}) {
		t.Fatalf("input base64 result=%#v requests=%#v", value, client.inputs)
	}

	for _, tc := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"missing payload", args(root, "name", "prompt")},
		{"both payloads", args(root, "name", "prompt", "text", "x", "base64", "eA==")},
		{"empty text", args(root, "name", "prompt", "text", "")},
		{"bad base64", args(root, "name", "prompt", "base64", "not base64")},
		{"un-padded base64", args(root, "name", "prompt", "base64", "eA")},
		{"base64 whitespace", args(root, "name", "prompt", "base64", "eA==\n")},
		{"oversized base64", args(root, "name", "prompt", "base64", strings.Repeat("A", 43692))},
		{"unknown input field", args(root, "name", "prompt", "text", "x", "timeout_ms", 1)},
		{"oversized", args(root, "name", "prompt", "text", strings.Repeat("x", protocol.MaxInputBytes+1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(client.inputs)
			if _, err := server.callTool(context.Background(), "input", tc.raw); err == nil {
				t.Fatal("input accepted invalid payload")
			}
			if len(client.inputs) != before {
				t.Fatalf("invalid payload invoked client: %#v", client.inputs)
			}
		})
	}

	stopped := client.processes["prompt"]
	stopped.State = "exited"
	client.processes["prompt"] = stopped
	if _, err := server.callTool(context.Background(), "input", args(root, "name", "prompt", "text", "again")); mapError(err).Code != "session_not_running" {
		t.Fatalf("stopped input error = %v", err)
	}
	if len(client.inputs) != 3 {
		t.Fatalf("stopped input client calls = %#v", client.inputs)
	}

	nonTTY := stopped
	nonTTY.State, nonTTY.TTY = "running", false
	client.processes["prompt"] = nonTTY
	if _, err := server.callTool(context.Background(), "input", args(root, "name", "prompt", "text", "again")); mapError(err).Code != string(protocol.ErrorInputNotTTY) {
		t.Fatalf("non-tty input error = %v", err)
	}

	for _, tc := range []struct {
		name      string
		code      protocol.ErrorCode
		process   protocol.Process
		wantCalls int
	}{
		{name: "not found", code: protocol.ErrorNotFound, wantCalls: 0},
		{name: "conflict", code: protocol.ErrorInputConflict, process: protocol.Process{State: "running", TTY: true}, wantCalls: 1},
		{name: "closed", code: protocol.ErrorInputClosed, process: protocol.Process{State: "running", TTY: true}, wantCalls: 1},
		{name: "stale", code: protocol.ErrorInputStale, process: protocol.Process{State: "running", TTY: true}, wantCalls: 1},
		{name: "not running", code: "session_not_running", process: protocol.Process{State: "exited", TTY: true}, wantCalls: 1},
		{name: "not tty", code: protocol.ErrorInputNotTTY, process: protocol.Process{State: "running", TTY: false}, wantCalls: 0},
		{name: "too large", code: protocol.ErrorInputTooLarge, process: protocol.Process{State: "running", TTY: true}, wantCalls: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caseClient := &fakeClient{processes: map[string]protocol.Process{}, startErr: map[string]error{}, stopErr: map[string]error{}}
			caseServer, caseRoot, _ := newTestServer(t, []Definition{{Name: "prompt", Source: "manifest", Argv: []string{"prompt"}, TTY: true}}, caseClient)
			targetName := "prompt"
			if tc.name == "not found" {
				targetName = "missing"
			}
			if tc.process.State != "" {
				tc.process.Name, tc.process.Root, tc.process.Cwd = targetName, caseRoot, caseRoot
				caseClient.processes[targetName] = tc.process
			}
			if tc.name == "too large" {
				_, err := caseServer.callTool(context.Background(), "input", args(caseRoot, "name", targetName, "text", strings.Repeat("x", protocol.MaxInputBytes+1)))
				if mapError(err).Code != string(tc.code) {
					t.Fatalf("too large error = %v", err)
				}
				if len(caseClient.inputs) != tc.wantCalls {
					t.Fatalf("too large client calls = %d, want %d", len(caseClient.inputs), tc.wantCalls)
				}
				return
			}
			if tc.code == protocol.ErrorInputConflict || tc.code == protocol.ErrorInputClosed || tc.code == protocol.ErrorInputStale {
				caseClient.inputErr = protocol.NewWireError(tc.code, string(tc.code), nil)
			}
			_, err := caseServer.callTool(context.Background(), "input", args(caseRoot, "name", targetName, "text", "x"))
			if mapError(err).Code != string(tc.code) {
				t.Fatalf("%s error = %v", tc.name, err)
			}
			if len(caseClient.inputs) != tc.wantCalls {
				t.Fatalf("%s client calls = %d, want %d", tc.name, len(caseClient.inputs), tc.wantCalls)
			}
		})
	}

	unavailableRoot := t.TempDir()
	unavailableServer := NewServer(Options{Resolver: fakeResolver{resolution: Resolution{Root: unavailableRoot}}, ClientFactory: func(context.Context, bool) (Client, error) {
		return nil, ErrDaemonUnavailable
	}})
	if _, err := unavailableServer.callTool(context.Background(), "input", args(unavailableRoot, "name", "prompt", "text", "x")); mapError(err).Code != "unavailable" {
		t.Fatalf("unavailable input error = %v", err)
	}
}

func TestTTYMCP(t *testing.T) {
	client := &fakeClient{processes: map[string]protocol.Process{}, startErr: map[string]error{}, stopErr: map[string]error{}}
	server, root, _ := newTestServer(t, []Definition{{Name: "dev", Source: "manifest", Argv: []string{"dev"}, TTY: true}}, client)
	result, err := server.callTool(context.Background(), "start", args(root, "name", "dev", "no_wait", true))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.starts) != 1 || !client.starts[0].TTY {
		t.Fatalf("start requests = %+v", client.starts)
	}
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), `"tty"`) {
		t.Fatalf("TTY MCP result = %s, err %v", encoded, err)
	}
	for _, tool := range server.toolDefinitions() {
		if tool.Name != "start" && tool.Name != "up" && tool.Name != "list" && tool.Name != "status" && tool.Name != "restart" {
			continue
		}
		blob, _ := json.Marshal(tool)
		if !strings.Contains(string(blob), "tty") {
			t.Fatalf("tool %s omits tty schema", tool.Name)
		}
	}

	runningWithoutTTY := client.processes["dev"]
	runningWithoutTTY.TTY = false
	client.processes["dev"] = runningWithoutTTY
	value, err := server.callTool(context.Background(), "start", args(root, "name", "dev", "no_wait", true))
	if err != nil {
		t.Fatalf("running non-tty drift error = %v", err)
	}
	drift, ok := value.(launchResult)
	if !ok || drift.Outcome != "definition_drift" || !reflect.DeepEqual(drift.ChangedFields, []string{"tty"}) || !strings.Contains(drift.Guidance, "hum restart dev") {
		t.Fatalf("running non-tty drift result = %#v", value)
	}
}

func TestInitializeNegotiatesProtocolVersion(t *testing.T) {
	cases := map[string]string{
		`{"protocolVersion":"2024-11-05"}`: "2024-11-05",
		`{"protocolVersion":"2025-03-26"}`: "2025-03-26",
		`{"protocolVersion":"2025-06-18"}`: "2025-06-18",
		`{"protocolVersion":"1999-01-01"}`: "2025-06-18",
		`{}`:                               "2025-06-18",
	}
	for params, want := range cases {
		var in, out bytes.Buffer
		in.WriteString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":` + params + "}\n")
		if err := NewServer(Options{}).Serve(context.Background(), &in, &out); err != nil {
			t.Fatalf("params %s: %v", params, err)
		}
		if !strings.Contains(out.String(), `"protocolVersion":"`+want+`"`) {
			t.Errorf("params %s: response %s, want protocolVersion %s", params, out.String(), want)
		}
	}
}
