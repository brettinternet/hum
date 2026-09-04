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
	stops           []protocol.StopRequest
	restarts        []protocol.RestartRequest
	waitHook        func(protocol.WaitRequest)
	keepStarting    bool
	waited          map[string]bool
	readyBeforeWait bool
}

func (f *fakeClient) Close() error { return nil }
func (f *fakeClient) Start(_ context.Context, req protocol.StartRequest) (protocol.Process, error) {
	f.starts = append(f.starts, req)
	if err := f.startErr[req.Name]; err != nil {
		return protocol.Process{}, err
	}
	p := protocol.Process{Name: req.Name, Source: req.Source, Root: req.Root, Cwd: req.Cwd, Argv: append([]string(nil), req.Argv...), State: "running", LaunchCursor: 7}
	if req.Ready != nil {
		p.Readiness = &protocol.Readiness{State: protocol.ReadinessStarting, Match: req.Ready.Match}
	}
	f.processes[req.Name] = p
	return p, nil
}
func (f *fakeClient) List(_ context.Context, req protocol.ListRequest) ([]protocol.Process, error) {
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
	if p.Readiness != nil && !f.keepStarting && (f.readyBeforeWait || f.waited[req.Name]) {
		p.Readiness = &protocol.Readiness{State: protocol.ReadinessReady, Cursor: p.NextCursor, Match: p.Readiness.Match}
	}
	return p, nil
}
func (f *fakeClient) Output(_ context.Context, req protocol.OutputRequest) (protocol.OutputResult, error) {
	f.outputs = append(f.outputs, req)
	return f.output, nil
}
func (f *fakeClient) Wait(_ context.Context, req protocol.WaitRequest) (protocol.WaitResponse, error) {
	if f.waitHook != nil {
		f.waitHook(req)
	}
	f.mu.Lock()
	f.waits = append(f.waits, req)
	if f.waited == nil {
		f.waited = make(map[string]bool)
	}
	f.waited[req.Name] = true
	f.mu.Unlock()
	if f.waitResult.Op == "" {
		return protocol.NewWaitResponse(protocol.WaitMatched, 9, nil), nil
	}
	return f.waitResult, nil
}
func (f *fakeClient) Stop(_ context.Context, req protocol.StopRequest) error {
	f.stops = append(f.stops, req)
	return f.stopErr[req.Name]
}
func (f *fakeClient) Remove(_ context.Context, req protocol.RemoveRequest) error {
	f.stops = append(f.stops, protocol.StopRequest{Op: protocol.OpStop, Name: req.Name, Cwd: req.Cwd})
	return f.stopErr[req.Name]
}
func (f *fakeClient) Restart(_ context.Context, req protocol.RestartRequest) (protocol.Process, error) {
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
	}
	want := []string{"start", "up", "down", "list", "status", "logs", "wait", "restart", "stop", "remove"}
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
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
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
	for _, tool := range []string{"status", "logs", "wait", "restart"} {
		if _, callErr := s.callTool(context.Background(), tool, args(root, "name", "raw")); mapError(callErr).Code != "unavailable" {
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
	if len(client.waits) != 1 || client.waits[0].TimeoutMS != defaultTimeoutMS || client.waits[0].After == nil || *client.waits[0].After != 7 {
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
	concurrentClient.waitHook = func(protocol.WaitRequest) {
		if len(concurrentClient.starts) != len(concurrentDefinitions) {
			t.Errorf("readiness wait began after only %d/%d launches", len(concurrentClient.starts), len(concurrentDefinitions))
		}
	}
	if _, err := concurrentServer.callTool(context.Background(), "up", args(concurrentRoot)); err != nil {
		t.Fatal(err)
	}
	timeouts := []int64{concurrentClient.waits[0].TimeoutMS, concurrentClient.waits[1].TimeoutMS}
	sort.Slice(timeouts, func(i, j int) bool { return timeouts[i] < timeouts[j] })
	if !reflect.DeepEqual(timeouts, []int64{17, defaultTimeoutMS}) {
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
	current, outcome, err := s.waitForReadiness(context.Background(), matchClient, Resolution{Root: root}, Definition{Name: "api", Ready: &protocol.ReadinessConfig{Match: "new"}}, initial, "already_running", defaultTimeoutMS)
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
	if err != nil || exitedValue.(launchResult).Outcome != "exited_before_ready" {
		t.Fatalf("exited start = %#v, %v", exitedValue, err)
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
