package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"hum/internal/daemon"
	"hum/internal/orchestrate"
	"hum/internal/output"
	"hum/internal/project"
	"hum/internal/protocol"
)

func TestMCPManifestEnvironmentContract(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileSpec := &project.EnvironmentSpec{Inherit: true, Files: []string{".env"}, Values: map[string]*string{}, BaseDir: root, Root: root}
	resolution := Resolution{Root: root, Definitions: []Definition{{Name: "api", Source: "manifest", Argv: []string{"api"}, Cwd: root, Environment: fileSpec}}}

	t.Run("configured baseline captured once", func(t *testing.T) {
		calls := 0
		server := NewServer(Options{Environment: func() []string {
			calls++
			return []string{"PATH=/bin", "TOKEN=baseline-secret"}
		}})
		prepared, err := server.prepareEnvironments(resolution)
		if err != nil {
			t.Fatal(err)
		}
		if calls != 1 || !reflect.DeepEqual(prepared["api"], []string{"PATH=/bin", "TOKEN=file-secret"}) {
			t.Fatalf("calls=%d prepared=%v", calls, prepared["api"])
		}
	})

	t.Run("inherit false and null", func(t *testing.T) {
		value := "only"
		spec := &project.EnvironmentSpec{Inherit: false, Values: map[string]*string{"ONLY": &value, "TOKEN": nil}, BaseDir: root, Root: root}
		server := NewServer(Options{Environment: func() []string { return []string{"TOKEN=baseline-secret"} }})
		prepared, err := server.prepareEnvironments(Resolution{Root: root, Definitions: []Definition{{Name: "isolated", Environment: spec}}})
		if err != nil || !reflect.DeepEqual(prepared["isolated"], []string{"ONLY=only"}) {
			t.Fatalf("prepared=%v error=%v", prepared, err)
		}
	})

	t.Run("concurrent requests do not mutate global environment", func(t *testing.T) {
		t.Setenv("HUM_MCP_ENVIRONMENT_SENTINEL", "global")
		server := NewServer(Options{Environment: func() []string { return []string{"HUM_MCP_ENVIRONMENT_SENTINEL=baseline"} }})
		var group sync.WaitGroup
		errors := make(chan error, 16)
		for index := 0; index < cap(errors); index++ {
			group.Add(1)
			go func() {
				defer group.Done()
				_, err := server.prepareEnvironments(resolution)
				errors <- err
			}()
		}
		group.Wait()
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatal(err)
			}
		}
		if got := os.Getenv("HUM_MCP_ENVIRONMENT_SENTINEL"); got != "global" {
			t.Fatalf("global environment mutated to %q", got)
		}
	})

	t.Run("preflight precedes client contact", func(t *testing.T) {
		contacts := 0
		server := NewServer(Options{
			Environment: func() []string { return []string{"PATH=/bin"} },
			ClientFactory: func(context.Context, bool) (Client, error) {
				contacts++
				return nil, errors.New("unexpected client contact")
			},
		})
		missing := Resolution{Root: root, Definitions: []Definition{{Name: "bad", Source: "manifest", Argv: []string{"bad"}, Cwd: root, Environment: &project.EnvironmentSpec{Inherit: true, Files: []string{"missing.env"}, Values: map[string]*string{}, BaseDir: root, Root: root}}}}
		if _, err := server.start(context.Background(), missing, commonInput{Name: "bad", NoWait: true}); err == nil || contacts != 0 {
			t.Fatalf("missing-file error=%v contacts=%d", err, contacts)
		}

		encodedLarge := strings.Repeat("\n", (4<<20)-7)
		oversized := Resolution{Root: root, Definitions: []Definition{{Name: "bad", Source: "manifest", Argv: []string{"bad"}, Cwd: root, Environment: &project.EnvironmentSpec{Inherit: false, Values: map[string]*string{"VALUE": &encodedLarge}, BaseDir: root, Root: root}}}}
		if _, err := server.start(context.Background(), oversized, commonInput{Name: "bad", NoWait: true}); err == nil || contacts != 0 {
			t.Fatalf("oversized error=%v contacts=%d", err, contacts)
		}
	})

	t.Run("request carries environment but response does not", func(t *testing.T) {
		client := &fakeClient{processes: map[string]protocol.Process{}, startErr: map[string]error{}}
		server := NewServer(Options{
			Environment:   func() []string { return []string{"PATH=/bin", "TOKEN=baseline-secret"} },
			ClientFactory: func(context.Context, bool) (Client, error) { return client, nil },
		})
		value, err := server.start(context.Background(), resolution, commonInput{Name: "api", NoWait: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(client.starts) != 1 || !reflect.DeepEqual(client.starts[0].Env, []string{"PATH=/bin", "TOKEN=file-secret"}) {
			t.Fatalf("start requests=%#v", client.starts)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{`"env"`, "TOKEN", "file-secret", "baseline-secret"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("response exposed %q: %s", secret, encoded)
			}
		}
	})
}

func TestStartupReconciliationWarnings(t *testing.T) {
	warnings := []protocol.StartupWarning{{Project: "/project", Name: "api", Outcome: "unresolved", Message: "identity mismatch"}}
	for _, name := range []string{"up", "list", "status"} {
		t.Run(name, func(t *testing.T) {
			value := any([]protocol.Process{})
			if name == "status" {
				value = map[string]any{"name": "api"}
			}
			encoded, err := json.Marshal(structuredToolContent(name, value, warnings))
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &object); err != nil {
				t.Fatal(err)
			}
			if _, ok := object["warnings"]; !ok {
				t.Fatalf("%s structured content has no top-level warnings: %s", name, encoded)
			}
		})
	}
	encoded, err := json.Marshal(structuredToolContent("list", []protocol.Process{}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"warnings"`)) {
		t.Fatalf("clean startup emitted warnings: %s", encoded)
	}
}

type fakeResolver struct {
	resolution Resolution
	err        error
}

func (f fakeResolver) Resolve(context.Context, string) (Resolution, error) {
	return f.resolution, f.err
}

type countingResolver struct {
	calls      int
	resolution Resolution
}

func (r *countingResolver) Resolve(context.Context, string) (Resolution, error) {
	r.calls++
	return r.resolution, nil
}

func (r *countingResolver) ResolveManifest(context.Context, string, string) (Resolution, error) {
	r.calls++
	return r.resolution, nil
}

type blockingResolver struct {
	resolution Resolution
	entered    chan struct{}
	release    chan struct{}
}

func (r blockingResolver) Resolve(context.Context, string) (Resolution, error) {
	close(r.entered)
	<-r.release
	return r.resolution, nil
}

type fakeClient struct {
	mu                     sync.Mutex
	processes              map[string]protocol.Process
	output                 protocol.OutputResult
	waitResult             protocol.WaitResponse
	startErr               map[string]error
	stopErr                map[string]error
	starts                 []protocol.StartRequest
	lists                  []protocol.ListRequest
	gets                   []protocol.GetRequest
	outputs                []protocol.OutputRequest
	waits                  []protocol.WaitRequest
	inputs                 []InputRequest
	inputResult            InputResult
	inputErr               error
	stops                  []protocol.StopRequest
	restarts               []protocol.RestartRequest
	signals                []protocol.SignalRequest
	signalResult           protocol.SignalResult
	signalErr              error
	waitHook               func(protocol.WaitRequest)
	keepStarting           bool
	waited                 map[string]bool
	readyBeforeWait        bool
	readinessDiagnostic    string
	retainReadinessOnStart bool
}

func (f *fakeClient) Close() error { return nil }
func (f *fakeClient) Start(_ context.Context, req protocol.StartRequest) (protocol.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, req)
	if err := f.startErr[req.Name]; err != nil {
		return protocol.Process{}, err
	}
	p := protocol.Process{Name: req.Name, Source: req.Source, Root: req.Root, Cwd: req.Cwd, Argv: append([]string(nil), req.Argv...), State: "running", LaunchCursor: 7, Restart: req.Restart, StopGraceInherited: req.StopGrace == nil}
	if req.StopGrace != nil {
		p.StopGrace = *req.StopGrace
	}
	if req.Ready == nil && f.retainReadinessOnStart {
		if retained := f.processes[req.Name].Readiness; retained != nil {
			copy := *retained
			copy.Argv = append([]string(nil), retained.Argv...)
			p.Readiness = &copy
		}
	}
	if req.Ready != nil {
		p.Readiness = &protocol.Readiness{Method: req.Ready.Method, Argv: append([]string(nil), req.Ready.Argv...), Interval: req.Ready.Interval, State: protocol.ReadinessStarting, Match: req.Ready.Match, Diagnostic: f.readinessDiagnostic}
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
		p.Readiness = &protocol.Readiness{Method: p.Readiness.Method, Argv: append([]string(nil), p.Readiness.Argv...), Interval: p.Readiness.Interval, State: protocol.ReadinessReady, Cursor: p.NextCursor, Match: p.Readiness.Match, Diagnostic: p.Readiness.Diagnostic}
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
func (f *fakeClient) SignalResult(_ context.Context, req protocol.SignalRequest) (protocol.SignalResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signals = append(f.signals, req)
	if f.signalErr != nil {
		return protocol.SignalResult{}, f.signalErr
	}
	if f.signalResult.Name == "" {
		return protocol.SignalResult{Name: req.Name, Signal: protocol.SignalInfo{Name: req.Signal, Number: 1}, Status: "sent"}, nil
	}
	return f.signalResult, nil
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
	if req.Update {
		p.Source, p.Root, p.Cwd = req.Source, req.Root, req.Cwd
		p.StopGraceInherited = req.StopGrace == nil
		if req.StopGrace != nil {
			p.StopGrace = *req.StopGrace
		} else {
			p.StopGrace = 0
		}
		p.Argv = append([]string(nil), req.Argv...)
		p.Readiness = nil
		if req.Ready != nil {
			p.Readiness = &protocol.Readiness{Method: req.Ready.Method, Argv: append([]string(nil), req.Ready.Argv...), Interval: req.Ready.Interval, State: protocol.ReadinessStarting, Match: req.Ready.Match, Diagnostic: f.readinessDiagnostic}
		}
	}
	f.processes[req.Name] = p
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
	for name, process := range client.processes {
		if process.StopGrace == 0 && !process.StopGraceInherited {
			process.StopGraceInherited = true
			client.processes[name] = process
		}
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

func TestSignalExitSnapshots(t *testing.T) {
	exitAt := time.Unix(172, 0).UTC()
	signal := &protocol.SignalInfo{Name: "SIGTERM", Number: 15}
	exited := protocol.Process{
		Name: "signal", Source: "manifest", Root: "/project", Cwd: "/project", Argv: []string{"sleep", "30"},
		State: protocol.StateExited, Exit: &protocol.Exit{Code: -1, Time: exitAt, Signal: signal}, ExitCode: -1, ExitedAt: exitAt,
		Restart: protocol.RestartOnFailure, Relaunches: 5,
	}
	call := func(t *testing.T, server *Server, name, root string) callToolResult {
		t.Helper()
		arguments := args(root, "name", "signal")
		if name == "list" || name == "up" {
			arguments = args(root)
		}
		params, err := json.Marshal(callToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		value, rpcErr := server.handleRequest(context.Background(), rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`"signal-exit"`), Method: "tools/call", Params: params})
		if rpcErr != nil {
			t.Fatalf("%s RPC: %#v", name, rpcErr)
		}
		result, ok := value.(callToolResult)
		if !ok || result.IsError || len(result.Content) != 1 {
			t.Fatalf("%s result = %#v", name, value)
		}
		return result
	}
	assertSignal := func(t *testing.T, name string, result callToolResult) {
		t.Helper()
		structured, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		for _, content := range []string{result.Content[0].Text, string(structured)} {
			if !strings.Contains(content, `"signal":{"name":"SIGTERM","number":15}`) {
				t.Fatalf("%s MCP content = %s, want signal", name, content)
			}
		}
	}

	listClient := &fakeClient{processes: map[string]protocol.Process{"signal": exited}}
	listServer, root, _ := newTestServer(t, nil, listClient)
	assertSignal(t, "list", call(t, listServer, "list", root))

	statusClient := &fakeClient{processes: map[string]protocol.Process{"signal": exited}}
	statusServer, statusRoot, _ := newTestServer(t, nil, statusClient)
	assertSignal(t, "status", call(t, statusServer, "status", statusRoot))

	upClient := &fakeClient{processes: map[string]protocol.Process{"signal": exited}}
	upServer, upRoot, _ := newTestServer(t, nil, upClient)
	exited.Root, exited.Cwd = upRoot, upRoot
	upClient.processes["signal"] = exited
	assertSignal(t, "up", call(t, upServer, "up", upRoot))

	waitClient := &fakeClient{processes: map[string]protocol.Process{"signal": exited}, waitResult: protocol.NewWaitResponse(protocol.WaitExited, 8, &protocol.Exit{Code: -1, Time: exitAt, Signal: signal})}
	waitServer, waitRoot, _ := newTestServer(t, nil, waitClient)
	assertSignal(t, "wait", call(t, waitServer, "wait", waitRoot))

	stoppedClient := &fakeClient{processes: map[string]protocol.Process{"signal": {Name: "signal", State: protocol.StateStopped}}}
	stoppedServer, stoppedRoot, _ := newTestServer(t, nil, stoppedClient)
	stoppedResult := call(t, stoppedServer, "status", stoppedRoot)
	if strings.Contains(stoppedResult.Content[0].Text, `"signal":`) {
		t.Fatalf("operator-stopped MCP content = %s, must omit signal", stoppedResult.Content[0].Text)
	}
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

func TestGlobalScopeTools(t *testing.T) {
	global, err := decodeInput(json.RawMessage(`{"scope":"global","name":"proxy"}`))
	if err != nil || global.Scope != protocol.ScopeGlobal || global.ProjectRoot != "" {
		t.Fatalf("global input = %+v err=%v", global, err)
	}
	if _, err := decodeInput(json.RawMessage(`{"scope":"global","project_root":"","name":"proxy"}`)); err == nil {
		t.Fatal("global input accepted project_root")
	}
	if _, err := decodeInput(json.RawMessage(`{"name":"proxy"}`)); err == nil {
		t.Fatal("project input accepted without project_root")
	}
	got := normalizeProcess(protocol.Process{Name: "proxy", Scope: protocol.ScopeGlobal, State: protocol.StateExited})
	if got.Scope != protocol.ScopeGlobal || got.Root != "" {
		t.Fatalf("normalized global process = %+v", got)
	}
	project := protocol.Process{Name: "proxy", Scope: protocol.ScopeProject, Root: "/project"}
	if listProcessKey(got) == listProcessKey(project) {
		t.Fatal("global and project list keys collide")
	}
	for _, definition := range NewServer(Options{}).toolDefinitions() {
		if definition.Name == "up" {
			if !strings.Contains(definition.Description, "project scope only") || !strings.Contains(definition.Description, "project_root") {
				t.Fatalf("up description omits project-only scope contract")
			}
			continue
		}
		if !strings.Contains(definition.Description, "global") || !strings.Contains(definition.Description, "project_root") {
			t.Fatalf("%s description omits global scope contract", definition.Name)
		}
	}
}

func TestProcessStopGraceSyntheticDefinition(t *testing.T) {
	explicit := 750 * time.Millisecond
	process := stoppedProcess("/project", Definition{Name: "db", Source: "manifest", Argv: []string{"db"}, StopGrace: &explicit})
	if process.StopGrace != explicit || process.StopGraceInherited {
		t.Fatalf("explicit synthetic snapshot = %#v, want %s and inherited=false", process, explicit)
	}
	inherited := stoppedProcess("/project", Definition{Name: "api", Source: "manifest", Argv: []string{"api"}})
	if inherited.StopGrace != 0 || !inherited.StopGraceInherited {
		t.Fatalf("inherited synthetic snapshot = %#v, want inherited=true", inherited)
	}
}

func TestProcessStopGraceMCPProcessResultParity(t *testing.T) {
	grace := 700 * time.Millisecond
	client := &fakeClient{}
	server, root, _ := newTestServer(t, []Definition{
		{Name: "explicit", Source: "manifest", Cwd: ".", Argv: []string{"explicit"}, StopGrace: &grace},
		{Name: "inherited", Source: "manifest", Cwd: ".", Argv: []string{"inherited"}},
	}, client)
	value, err := server.callTool(context.Background(), "up", args(root, "no_wait", true))
	if err != nil {
		t.Fatal(err)
	}
	results := value.([]launchResult)
	if len(results) != 2 {
		t.Fatalf("MCP up results = %#v, want two processes", results)
	}
	for _, result := range results {
		if result.Process == nil {
			t.Fatalf("MCP result omitted process: %#v", result)
		}
		switch result.Name {
		case "explicit":
			if result.Process.StopGrace != grace || result.Process.StopGraceInherited {
				t.Fatalf("explicit MCP process = %#v", result.Process)
			}
		case "inherited":
			if !result.Process.StopGraceInherited {
				t.Fatalf("inherited MCP process = %#v", result.Process)
			}
		default:
			t.Fatalf("unexpected MCP process result = %#v", result)
		}
	}
	listedValue, err := server.callTool(context.Background(), "list", args(root))
	if err != nil {
		t.Fatal(err)
	}
	listed := listedValue.([]protocol.Process)
	if len(listed) != 2 {
		t.Fatalf("MCP list = %#v, want two processes", listed)
	}
	for _, process := range listed {
		if process.Name == "explicit" && (process.StopGrace != grace || process.StopGraceInherited) {
			t.Fatalf("MCP list explicit = %#v", process)
		}
		if process.Name == "inherited" && !process.StopGraceInherited {
			t.Fatalf("MCP list inherited = %#v", process)
		}
	}
	statusValue, err := server.callTool(context.Background(), "status", args(root, "name", "explicit"))
	if err != nil {
		t.Fatal(err)
	}
	status := statusValue.(protocol.Process)
	if status.StopGrace != grace || status.StopGraceInherited {
		t.Fatalf("MCP status = %#v", status)
	}
	restartedValue, err := server.callTool(context.Background(), "restart", args(root, "name", "explicit", "no_wait", true))
	if err != nil {
		t.Fatal(err)
	}
	restarted := restartedValue.(restartResult)
	if restarted.StopGrace != grace || restarted.StopGraceInherited {
		t.Fatalf("MCP restart = %#v", restarted)
	}
}

func TestToolSchemas(t *testing.T) {
	s := NewServer(Options{})
	defs := s.toolDefinitions()
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
		req := d.InputSchema["required"].([]string)
		if d.Name == "up" {
			if !contains(req, "project_root") {
				t.Errorf("up does not require project_root")
			}
			if _, ok := d.InputSchema["allOf"]; ok {
				t.Errorf("up advertises unsupported global scope rules")
			}
		} else {
			if contains(req, "project_root") {
				t.Errorf("%s unconditionally requires project_root", d.Name)
			}
			rules, ok := d.InputSchema["allOf"].([]any)
			wantRules := 2
			if d.Name == "list" {
				wantRules = 3
			}
			if !ok || len(rules) != wantRules {
				t.Errorf("%s scope rules = %#v, want %d", d.Name, d.InputSchema["allOf"], wantRules)
			}
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
	want := []string{"start", "up", "down", "list", "status", "logs", "wait", "input", "restart", "stop", "remove", "signal"}
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

func TestWaitTimeoutExplainsNeverObserved(t *testing.T) {
	guidance := `no process named "api" was observed during the wait; check the name or start it first.`
	for _, test := range []struct {
		name   string
		observ bool
	}{
		{name: "false", observ: false},
		{name: "true", observ: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{waitResult: protocol.WaitResponse{Op: protocol.OpWait, OK: true, Outcome: protocol.WaitTimedOut, Cursor: 3, ProcessObserved: test.observ}}
			server, root, _ := newTestServer(t, nil, client)
			params, err := json.Marshal(callToolParams{Name: "wait", Arguments: args(root, "name", "api", "timeout_ms", 20)})
			if err != nil {
				t.Fatal(err)
			}
			value, rpcErr := server.handleRequest(context.Background(), rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`"wait"`), Method: "tools/call", Params: params})
			if rpcErr != nil {
				t.Fatalf("wait RPC: %#v", rpcErr)
			}
			result, ok := value.(callToolResult)
			if !ok || result.IsError || len(result.Content) != 1 {
				t.Fatalf("wait RPC result = %#v", value)
			}
			structured, ok := result.StructuredContent.(protocol.WaitResponse)
			if !ok {
				t.Fatalf("wait structured content = %T, want protocol.WaitResponse", result.StructuredContent)
			}
			if structured.ProcessObserved != test.observ {
				t.Fatalf("structured process_observed = %v, want %v", structured.ProcessObserved, test.observ)
			}
			if test.observ {
				if structured.Message != "" {
					t.Fatalf("observed timeout message = %q, want empty", structured.Message)
				}
			} else if structured.Message != guidance {
				t.Fatalf("unobserved timeout message = %q, want %q", structured.Message, guidance)
			}
			if !strings.Contains(result.Content[0].Text, `"process_observed":`) {
				t.Fatalf("wait text omitted process_observed: %q", result.Content[0].Text)
			}
			var text protocol.WaitResponse
			if err := json.Unmarshal([]byte(result.Content[0].Text), &text); err != nil {
				t.Fatalf("decode wait text = %q: %v", result.Content[0].Text, err)
			}
			if !test.observ && text.Message != guidance {
				t.Fatalf("wait text guidance = %q, want %q", text.Message, guidance)
			}
			if len(client.waits) != 1 || len(client.gets) != 0 {
				t.Fatalf("wait round trips = waits=%d gets=%d, want one wait and no get", len(client.waits), len(client.gets))
			}
		})
	}
}

func TestToolInputSchemaRuntimeConformance(t *testing.T) {
	root := t.TempDir()
	knownFields := map[string]any{
		"scope": protocol.ScopeProject, "project_root": root, "all": true, "name": "alpha",
		"no_wait": true, "timeout_ms": 1, "after": 1, "since_ms": 1, "tail": 1,
		"max_entries": 1, "max_bytes": 1, "match": "ready", "text": "x",
		"base64": "eA==", "signal": "TERM",
	}
	base := map[string]map[string]any{
		"start": {"name": "alpha"}, "up": {}, "down": {}, "list": {},
		"status": {"name": "alpha"}, "logs": {"name": "alpha"},
		"wait": {"name": "alpha"}, "input": {"name": "alpha", "text": "x"},
		"restart": {"name": "alpha"}, "stop": {"name": "alpha"},
		"remove": {"name": "alpha"}, "signal": {"name": "alpha", "signal": "TERM"},
	}

	for _, definition := range NewServer(Options{}).toolDefinitions() {
		properties := definition.InputSchema["properties"].(map[string]any)
		for field, value := range knownFields {
			if _, applicable := properties[field]; applicable {
				continue
			}
			t.Run(definition.Name+"/rejects_"+field, func(t *testing.T) {
				resolver := &countingResolver{resolution: Resolution{Root: root}}
				clientCalls := 0
				server := NewServer(Options{
					Resolver: resolver,
					ClientFactory: func(context.Context, bool) (Client, error) {
						clientCalls++
						return &fakeClient{}, nil
					},
				})
				arguments := map[string]any{"project_root": root}
				for key, item := range base[definition.Name] {
					arguments[key] = item
				}
				arguments[field] = value
				raw, err := json.Marshal(arguments)
				if err != nil {
					t.Fatal(err)
				}
				_, callErr := server.callTool(context.Background(), definition.Name, raw)
				if mapped := mapError(callErr); mapped.Code != "invalid_request" {
					t.Fatalf("error = %v, want invalid_request", callErr)
				}
				if resolver.calls != 0 || clientCalls != 0 {
					t.Fatalf("rejected input reached resolver/client: %d/%d", resolver.calls, clientCalls)
				}
			})
		}
	}
}

func TestRejectsUnsupportedAggregateInputs(t *testing.T) {
	root := t.TempDir()
	resolver := &countingResolver{resolution: Resolution{Root: root}}
	client := &fakeClient{processes: map[string]protocol.Process{"alpha": {Name: "alpha", State: protocol.StateRunning}}}
	clientCalls := 0
	server := NewServer(Options{
		Resolver: resolver,
		ClientFactory: func(context.Context, bool) (Client, error) {
			clientCalls++
			return client, nil
		},
	})
	cases := []struct {
		name string
		tool string
		raw  json.RawMessage
	}{
		{name: "named up", tool: "up", raw: args(root, "name", "alpha")},
		{name: "named down", tool: "down", raw: args(root, "name", "alpha")},
		{name: "global up", tool: "up", raw: json.RawMessage(`{"scope":"global"}`)},
		{name: "global list all", tool: "list", raw: json.RawMessage(`{"scope":"global","all":true}`)},
		{name: "empty scope down", tool: "down", raw: args(root, "scope", "")},
		{name: "ambiguous remove", tool: "remove", raw: args(root, "name", "", "all", true)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := server.callTool(context.Background(), test.tool, test.raw)
			if mapped := mapError(err); mapped.Code != "invalid_request" {
				t.Fatalf("error = %v, want invalid_request", err)
			}
		})
	}
	if resolver.calls != 0 || clientCalls != 0 {
		t.Fatalf("rejected aggregate input reached resolver/client: %d/%d", resolver.calls, clientCalls)
	}
	if len(client.starts) != 0 || len(client.stops) != 0 || len(client.lists) != 0 {
		t.Fatalf("rejected aggregate input mutated lifecycle: starts=%#v stops=%#v lists=%#v", client.starts, client.stops, client.lists)
	}
}

func TestMCPScopeSchema(t *testing.T) {
	definitions := NewServer(Options{}).toolDefinitions()
	for _, definition := range definitions {
		properties := definition.InputSchema["properties"].(map[string]any)
		scope := properties["scope"].(map[string]any)
		if definition.Name == "up" {
			if scope["const"] != protocol.ScopeProject || !contains(definition.InputSchema["required"].([]string), "project_root") {
				t.Fatalf("up scope schema = %#v", definition.InputSchema)
			}
			continue
		}
		if _, ok := scope["enum"]; !ok {
			t.Errorf("%s scope schema does not advertise project/global", definition.Name)
		}
		rules, ok := definition.InputSchema["allOf"].([]any)
		if !ok || len(rules) < 2 {
			t.Errorf("%s schema lacks conditional project_root rules", definition.Name)
		}
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
	}{
		{protocol.NewWireError(protocol.ErrorNotFound, "gone", map[string]any{"name": "x"}), "not_found"},
		{ErrDaemonUnavailable, "unavailable"},
		// Shutdown abandons in-flight requests; that is not an adapter defect,
		// so it must not be indistinguishable from one.
		{context.Canceled, "cancelled"},
		{fmt.Errorf("up api: %w", context.Canceled), "cancelled"},
		{context.DeadlineExceeded, "cancelled"},
		{errors.New("boom"), "internal"},
	} {
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

func TestRetainedExecutableReadinessStartWaitsWithoutOutput(t *testing.T) {
	retained := protocol.Process{
		Name: "removed", Source: "manifest", State: "exited", Cwd: "/work", Argv: []string{"server"}, LaunchCursor: 11,
		Readiness: &protocol.Readiness{Method: "exec", Argv: []string{"probe", "--service", "removed"}, Interval: 10 * time.Millisecond, State: protocol.ReadinessStarting, Diagnostic: "status 1"},
	}
	client := &fakeClient{processes: map[string]protocol.Process{"removed": retained}, readyBeforeWait: true, retainReadinessOnStart: true}
	server, root, _ := newTestServer(t, nil, client)
	value, err := server.start(context.Background(), Resolution{Root: root, Scope: protocol.ScopeProject}, commonInput{Name: "removed"})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(launchResult)
	if !ok || result.Process == nil || result.Outcome != "started" {
		t.Fatalf("retained start result = %#v", value)
	}
	readiness := result.Process.Readiness
	if readiness == nil || readiness.State != protocol.ReadinessReady || readiness.Method != "exec" || !reflect.DeepEqual(readiness.Argv, retained.Readiness.Argv) || readiness.Interval != retained.Readiness.Interval || readiness.Diagnostic != retained.Readiness.Diagnostic {
		t.Fatalf("retained start readiness = %#v, want durable executable readiness", readiness)
	}
	if len(client.waits) != 0 {
		t.Fatalf("retained executable readiness used output waits: %#v", client.waits)
	}

	noWaitClient := &fakeClient{processes: map[string]protocol.Process{"removed": retained}, readyBeforeWait: true, retainReadinessOnStart: true}
	noWaitServer, noWaitRoot, _ := newTestServer(t, nil, noWaitClient)
	value, err = noWaitServer.start(context.Background(), Resolution{Root: noWaitRoot, Scope: protocol.ScopeProject}, commonInput{Name: "removed", NoWait: true})
	if err != nil {
		t.Fatal(err)
	}
	noWaitResult, ok := value.(launchResult)
	if !ok || noWaitResult.Outcome != "started" || noWaitResult.Process == nil || noWaitResult.Process.Readiness == nil || noWaitResult.Process.Readiness.State != protocol.ReadinessStarting {
		t.Fatalf("retained no_wait start result = %#v", value)
	}
	if len(noWaitClient.waits) != 0 {
		t.Fatalf("retained no_wait issued output waits: %#v", noWaitClient.waits)
	}
}

func TestExecutableReadiness(t *testing.T) {
	root := t.TempDir()
	argv := []string{"probe", "--service", "api"}
	ready := &protocol.ReadinessConfig{Method: "exec", Argv: argv, Interval: 20 * time.Millisecond, Timeout: time.Second}
	definitions := []Definition{{Name: "api", Source: "manifest", Argv: []string{"server"}, Cwd: root, Ready: ready}}
	client := &fakeClient{readyBeforeWait: true, readinessDiagnostic: "status 1"}
	server, resolvedRoot, _ := newTestServer(t, definitions, client)
	input := commonInput{Name: "api"}

	value, err := server.start(context.Background(), Resolution{Root: resolvedRoot, Scope: protocol.ScopeProject, Definitions: definitions}, input)
	if err != nil {
		t.Fatal(err)
	}
	startResult, ok := value.(launchResult)
	if !ok || startResult.Process == nil || startResult.Process.Readiness == nil {
		t.Fatalf("MCP start result=%#v", value)
	}
	if startResult.Outcome != "started" || startResult.Process.Readiness.Method != "exec" || !reflect.DeepEqual(startResult.Process.Readiness.Argv, argv) || startResult.Process.Readiness.Interval != ready.Interval || startResult.Process.Readiness.State != protocol.ReadinessReady {
		t.Fatalf("MCP start result=%#v, want ready executable snapshot", startResult)
	}
	if len(client.waits) != 0 {
		t.Fatalf("MCP exec readiness issued output waits: %#v", client.waits)
	}

	upValue, err := server.up(context.Background(), Resolution{Root: resolvedRoot, Scope: protocol.ScopeProject, Definitions: definitions}, input)
	if err != nil {
		t.Fatal(err)
	}
	upResults, ok := upValue.([]launchResult)
	if !ok || len(upResults) != 1 || upResults[0].Outcome != "already_running" || upResults[0].Process == nil || upResults[0].Process.Readiness == nil || upResults[0].Process.Readiness.Method != "exec" {
		t.Fatalf("MCP up result=%#v, want parity with start", upValue)
	}

	restartValue, err := server.restart(context.Background(), Resolution{Root: resolvedRoot, Scope: protocol.ScopeProject, Definitions: definitions}, input)
	if err != nil {
		t.Fatal(err)
	}
	restartResult, ok := restartValue.(restartResult)
	if !ok || restartResult.Outcome != "restarted" || restartResult.Readiness != protocol.ReadinessReady || len(client.restarts) != 1 || client.restarts[0].Ready == nil || client.restarts[0].Ready.Method != "exec" || !reflect.DeepEqual(client.restarts[0].Ready.Argv, argv) {
		t.Fatalf("MCP restart result=%#v request=%#v, want executable readiness parity", restartValue, client.restarts)
	}
	if len(client.waits) != 0 {
		t.Fatalf("MCP restart exec readiness issued output waits: %#v", client.waits)
	}
	assertReadiness := func(label string, process protocol.Process) {
		t.Helper()
		if process.Readiness == nil || process.Readiness.Method != "exec" || !reflect.DeepEqual(process.Readiness.Argv, argv) || process.Readiness.Interval != ready.Interval || process.Readiness.Diagnostic != "status 1" {
			t.Fatalf("MCP %s process=%#v, want executable readiness fields", label, process)
		}
	}
	assertReadiness("start", *startResult.Process)
	if upResults[0].Process == nil {
		t.Fatalf("MCP up result=%#v, missing process", upResults[0])
	}
	assertReadiness("up", *upResults[0].Process)
	if restartResult.ReadinessMethod != "exec" || !reflect.DeepEqual(restartResult.ReadinessArgv, argv) || restartResult.ReadinessInterval != ready.Interval || restartResult.ReadinessDiagnostic != "status 1" {
		t.Fatalf("MCP restart result=%#v, want executable readiness fields", restartResult)
	}
	statusValue, err := server.status(context.Background(), Resolution{Root: resolvedRoot, Scope: protocol.ScopeProject, Definitions: definitions}, "api")
	if err != nil {
		t.Fatal(err)
	}
	statusProcess, ok := statusValue.(protocol.Process)
	if !ok {
		t.Fatalf("MCP status result type=%T, want protocol.Process", statusValue)
	}
	assertReadiness("status", statusProcess)
	listValue, err := server.list(context.Background(), Resolution{Root: resolvedRoot, Scope: protocol.ScopeProject, Definitions: definitions}, input)
	if err != nil {
		t.Fatal(err)
	}
	listed, ok := listValue.([]protocol.Process)
	if !ok {
		t.Fatalf("MCP list result type=%T, want []protocol.Process", listValue)
	}
	var listedAPI protocol.Process
	for _, process := range listed {
		if process.Name == "api" {
			listedAPI = process
			break
		}
	}
	if listedAPI.Name == "" {
		t.Fatalf("MCP list result=%#v, missing api", listed)
	}
	assertReadiness("list", listedAPI)

	execDriftDefinition := definitions[0]
	execDriftDefinition.Ready = &protocol.ReadinessConfig{Method: "exec", Argv: []string{"probe", "different"}, Interval: ready.Interval, Timeout: ready.Timeout}
	execDrift := orchestrate.DefinitionDriftResult(resolvedRoot, mcpDefinition(execDriftDefinition), orchestrateProcess(*startResult.Process))
	if execDrift.Outcome != "definition_drift" || !reflect.DeepEqual(execDrift.ChangedFields, []string{"readiness_exec"}) {
		t.Fatalf("MCP executable readiness drift=%#v, want readiness_exec", execDrift)
	}
	matchDefinition := Definition{Name: "api", Source: "manifest", Argv: []string{"server"}, Cwd: root, Ready: &protocol.ReadinessConfig{Method: "match", Match: "ready"}}
	matchProcess := protocol.Process{Name: "api", Source: "manifest", Root: resolvedRoot, Cwd: root, Argv: []string{"server"}, State: "running", StopGraceInherited: true, Readiness: &protocol.Readiness{Method: "match", Match: "ready", State: protocol.ReadinessStarting}}
	matchDrift := orchestrate.DefinitionDriftResult(resolvedRoot, mcpDefinition(matchDefinition), orchestrateProcess(matchProcess))
	if len(matchDrift.ChangedFields) != 0 {
		t.Fatalf("unchanged MCP match readiness drift=%#v, want no changed fields", matchDrift)
	}
	matchToExec := orchestrate.DefinitionDriftResult(resolvedRoot, mcpDefinition(definitions[0]), orchestrateProcess(matchProcess))
	if !reflect.DeepEqual(matchToExec.ChangedFields, []string{"readiness_exec"}) {
		t.Fatalf("MCP match-to-exec readiness drift=%#v, want readiness_exec", matchToExec)
	}
	execToMatch := orchestrate.DefinitionDriftResult(resolvedRoot, mcpDefinition(matchDefinition), orchestrateProcess(*startResult.Process))
	if !reflect.DeepEqual(execToMatch.ChangedFields, []string{"readiness_exec"}) {
		t.Fatalf("MCP exec-to-match readiness drift=%#v, want readiness_exec", execToMatch)
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
	if len(client.waits) != 1 || client.waits[0].TimeoutMS > 100 || client.waits[0].After != nil {
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
	if timeouts[0] < 1 || timeouts[0] > 17 || timeouts[1] < 1 || timeouts[1] > 100 {
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
		{name: "stopped", state: "stopped"},
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
	client := &fakeClient{processes: map[string]protocol.Process{
		"descendants": {Name: "descendants", Root: "/root", State: protocol.StateDescendants},
		"transient":   {Name: "transient", Root: "/root", State: protocol.StateRunning},
	}}
	s, root, ensures := newTestServer(t, defs, client)
	got, err := s.callTool(context.Background(), "down", args(root))
	if err != nil {
		t.Fatal(err)
	}
	results := got.([]stopResult)
	if len(results) != 3 || results[0].Name != "declared" || results[0].State != "not_running" || results[1].Name != "descendants" || results[1].State != "stopped" || results[2].Name != "transient" || results[2].State != "stopped" {
		t.Fatalf("down=%#v", results)
	}
	if len(client.stops) != 2 || client.stops[0].Name != "descendants" || client.stops[1].Name != "transient" {
		t.Fatalf("stops=%#v", client.stops)
	}
	if (*ensures)[0] {
		t.Fatal("down created daemon")
	}
}

func TestTerminalStateSnapshots(t *testing.T) {
	exitedAt := time.Date(2026, time.September, 6, 12, 34, 56, 0, time.UTC)
	client := &fakeClient{processes: map[string]protocol.Process{
		"stopped": {Name: "stopped", State: protocol.StateStopped},
		"zero":    {Name: "zero", State: protocol.StateExited, Exit: &protocol.Exit{Code: 0, Time: exitedAt}, ExitCode: 0, ExitedAt: exitedAt},
		"failed":  {Name: "failed", State: protocol.StateExited, Exit: &protocol.Exit{Code: 7, Time: exitedAt}, ExitCode: 7, ExitedAt: exitedAt},
		"signal":  {Name: "signal", State: protocol.StateExited, Exit: &protocol.Exit{Code: -1, Time: exitedAt}, ExitCode: -1, ExitedAt: exitedAt},
	}}
	s, root, _ := newTestServer(t, nil, client)
	listedValue, err := s.callTool(context.Background(), "list", args(root))
	if err != nil {
		t.Fatal(err)
	}
	listed := listedValue.([]protocol.Process)
	if len(listed) != 4 {
		t.Fatalf("terminal list = %#v, want four snapshots", listed)
	}
	for _, want := range client.processes {
		statusValue, statusErr := s.callTool(context.Background(), "status", args(root, "name", want.Name))
		if statusErr != nil {
			t.Fatal(statusErr)
		}
		got := statusValue.(protocol.Process)
		if got.State != want.State || got.ExitCode != want.ExitCode || !got.ExitedAt.Equal(want.ExitedAt) || !reflect.DeepEqual(got.Exit, want.Exit) {
			t.Fatalf("status %q = %#v, want %#v", want.Name, got, want)
		}
	}
	for _, got := range listed {
		want, ok := client.processes[got.Name]
		if !ok || got.State != want.State || got.ExitCode != want.ExitCode || !got.ExitedAt.Equal(want.ExitedAt) || !reflect.DeepEqual(got.Exit, want.Exit) {
			t.Fatalf("list snapshot = %#v, want matching terminal state", got)
		}
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

func TestSignalTool(t *testing.T) {
	client := &fakeClient{processes: map[string]protocol.Process{"api": {Name: "api", State: protocol.StateRunning}}}
	server, root, _ := newTestServer(t, nil, client)
	value, err := server.callTool(context.Background(), "signal", args(root, "name", "api", "signal", "hup"))
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(protocol.SignalResult)
	if !ok || result.Name != "api" || result.Signal.Name != "SIGHUP" || result.Signal.Number != 1 || result.Status != "sent" {
		t.Fatalf("signal result = %#v", value)
	}
	if len(client.signals) != 1 || client.signals[0].Signal != "SIGHUP" {
		t.Fatalf("signal requests = %#v", client.signals)
	}
	descendants := client.processes["api"]
	descendants.State = protocol.StateDescendants
	client.processes["api"] = descendants
	if _, err := server.callTool(context.Background(), "signal", args(root, "name", "api", "signal", "TERM")); err != nil {
		t.Fatalf("signal descendants: %v", err)
	}
	if len(client.signals) != 2 || client.signals[1].Signal != "SIGTERM" {
		t.Fatalf("descendant signal requests = %#v", client.signals)
	}
	for _, input := range []string{"", "0", "-1", "SIGUSR3"} {
		before := len(client.signals)
		_, err := server.callTool(context.Background(), "signal", args(root, "name", "api", "signal", input))
		if mapError(err).Code != string(protocol.ErrorInvalidSignal) {
			t.Fatalf("signal %q error = %v", input, err)
		}
		if len(client.signals) != before {
			t.Fatalf("invalid signal %q was delivered", input)
		}
	}
	stopped := client.processes["api"]
	stopped.State = protocol.StateStopped
	client.processes["api"] = stopped
	if _, err := server.callTool(context.Background(), "signal", args(root, "name", "api", "signal", "HUP")); mapError(err).Code != string(protocol.ErrorNotRunning) {
		t.Fatalf("stopped signal error = %v", err)
	}
	if _, err := server.callTool(context.Background(), "signal", args(root, "name", "missing", "signal", "HUP")); mapError(err).Code != string(protocol.ErrorNotFound) {
		t.Fatalf("missing signal error = %v", err)
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

func TestLogsSystemStream(t *testing.T) {
	var logsDefinition toolDefinition
	for _, definition := range NewServer(Options{}).toolDefinitions() {
		if definition.Name == "logs" {
			logsDefinition = definition
			break
		}
	}
	streamProperty, ok := logsDefinition.InputSchema["properties"].(map[string]any)["stream"].(map[string]any)
	if !ok {
		t.Fatalf("logs stream schema missing: %#v", logsDefinition.InputSchema)
	}
	wantEnum := []string{string(protocol.StreamStdout), string(protocol.StreamStderr), string(protocol.StreamSystem), string(protocol.StreamBoth)}
	if !reflect.DeepEqual(streamProperty["enum"], wantEnum) || streamProperty["default"] != protocol.StreamBoth {
		t.Fatalf("logs stream schema = %#v, want enum %#v and both default", streamProperty, wantEnum)
	}
	if !strings.Contains(logsDefinition.Description, "supervision-only system") || !strings.Contains(logsDefinition.Description, "both includes all three") {
		t.Fatalf("logs description does not explain system/both: %q", logsDefinition.Description)
	}

	next := protocol.Cursor(9)
	oldest := protocol.Cursor(2)
	latest := protocol.Cursor(12)
	client := &fakeClient{
		processes: map[string]protocol.Process{"raw": {Name: "raw", State: "running"}},
		output: protocol.OutputResult{
			Entries: []protocol.OutputEntry{{Cursor: 8, Stream: protocol.StreamSystem, Text: "raw supervision entry\n"}},
			Next:    &next, Oldest: &oldest, Latest: &latest, Truncated: true, More: true,
		},
	}
	server, root, _ := newTestServer(t, nil, client)

	value, err := server.callTool(context.Background(), "logs", args(root, "name", "raw", "stream", "system", "tail", 1, "max_entries", 1, "max_bytes", 64))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.outputs) != 1 || client.outputs[0].Stream != protocol.StreamSystem {
		t.Fatalf("system output requests = %#v", client.outputs)
	}
	result := value.(protocol.OutputResult)
	if len(result.Entries) != 1 || result.Entries[0].Stream != protocol.StreamSystem || result.Next == nil || *result.Next != next || result.Oldest == nil || *result.Oldest != oldest || result.Latest == nil || *result.Latest != latest || !result.Truncated || !result.More {
		t.Fatalf("system logs result = %#v, want unchanged bounded metadata", result)
	}

	if _, err := server.callTool(context.Background(), "logs", args(root, "name", "raw")); err != nil {
		t.Fatal(err)
	}
	if client.outputs[1].Stream != protocol.StreamBoth {
		t.Fatalf("omitted stream request = %#v, want both", client.outputs[1])
	}
	before := len(client.outputs)
	if _, err := server.callTool(context.Background(), "logs", args(root, "name", "raw", "stream", "invalid")); mapError(err).Code != "invalid_request" {
		t.Fatalf("invalid stream error = %v", err)
	}
	if len(client.outputs) != before {
		t.Fatalf("invalid stream contacted daemon: %#v", client.outputs[before:])
	}
}

func TestLogsMatchContext(t *testing.T) {
	cursor := protocol.Cursor(3)
	client := &fakeClient{
		processes: map[string]protocol.Process{"api": {Name: "api", State: "running"}},
		output: protocol.OutputResult{Entries: []protocol.OutputEntry{
			{Cursor: 1, Stream: protocol.StreamSystem, Text: "before\n"},
			{Cursor: 2, Stream: protocol.StreamSystem, Text: "ERROR\n"},
			{Cursor: 3, Stream: protocol.StreamSystem, Text: "after\n"},
		}, Next: &cursor},
	}
	s, root, _ := newTestServer(t, nil, client)

	var logsDefinition toolDefinition
	for _, definition := range s.toolDefinitions() {
		if definition.Name == "logs" {
			logsDefinition = definition
			break
		}
	}
	properties := logsDefinition.InputSchema["properties"].(map[string]any)
	if properties["stream"] == nil || properties["match"] == nil || properties["context"] == nil || logsDefinition.InputSchema["dependentRequired"] == nil {
		t.Fatalf("logs schema = %#v, want stream, match, context, and dependency", logsDefinition.InputSchema)
	}

	value, err := s.callTool(context.Background(), "logs", args(root, "name", "api", "stream", "system", "match", "ERROR", "context", 1, "max_entries", 3))
	if err != nil {
		t.Fatal(err)
	}
	request := client.outputs[len(client.outputs)-1]
	if request.Stream != protocol.StreamSystem || request.Match != "ERROR" || request.Context != 1 || request.MaxEntries != 3 {
		t.Fatalf("match context request = %#v", request)
	}
	if got := value.(protocol.OutputResult); !reflect.DeepEqual(got, client.output) {
		t.Fatalf("MCP output = %#v, want protocol/CLI-compatible %#v", got, client.output)
	}

	before := len(client.outputs)
	for _, test := range []struct {
		name string
		args json.RawMessage
	}{
		{"negative", args(root, "name", "api", "match", "ERROR", "context", -1)},
		{"without match", args(root, "name", "api", "context", 1)},
		{"zero without match", args(root, "name", "api", "context", 0)},
		{"invalid match", args(root, "name", "api", "match", "[", "context", 1)},
	} {
		if _, err := s.callTool(context.Background(), "logs", test.args); err == nil || mapError(err).Code != "invalid_request" {
			t.Fatalf("%s error = %v, want invalid_request", test.name, err)
		}
	}
	if len(client.outputs) != before {
		t.Fatalf("invalid match-context requests contacted daemon: before=%d after=%d", before, len(client.outputs))
	}
	if _, err := s.callTool(context.Background(), "logs", args(root, "name", "api", "match", "ERROR", "context", 0)); err != nil {
		t.Fatalf("zero context with match: %v", err)
	}
}

func TestLogsDefaultNewestWindow(t *testing.T) {
	newest := protocol.Cursor(201)
	client := &fakeClient{
		processes: map[string]protocol.Process{"raw": {Name: "raw", State: "running", LaunchCursor: 5}},
		output:    protocol.OutputResult{Entries: []protocol.OutputEntry{{Cursor: newest, Text: "newest"}}},
	}
	s, root, _ := newTestServer(t, nil, client)

	value, err := s.callTool(context.Background(), "logs", args(root, "name", "raw"))
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(protocol.OutputResult)
	if !ok || len(result.Entries) != 1 || result.Entries[0].Cursor != newest {
		t.Fatalf("default logs result = %#v, want newest retained entry", value)
	}
	defaultRequest := client.outputs[len(client.outputs)-1]
	if defaultRequest.After != nil || defaultRequest.Tail != protocol.DefaultReadEntries || defaultRequest.MaxEntries != protocol.DefaultReadEntries {
		t.Fatalf("default logs request = %#v, want newest default tail=%d", defaultRequest, protocol.DefaultReadEntries)
	}

	if _, err := s.callTool(context.Background(), "logs", args(root, "name", "raw", "after", 50)); err != nil {
		t.Fatal(err)
	}
	forwardRequest := client.outputs[len(client.outputs)-1]
	if forwardRequest.After == nil || *forwardRequest.After != 50 || forwardRequest.Tail != 0 || forwardRequest.MaxEntries != 0 {
		t.Fatalf("explicit after logs request = %#v, want unchanged forward paging", forwardRequest)
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
	if result, ok := got.(restartResult); !ok || result.Name != "raw" || result.Outcome != "running_unverified" || result.Readiness != "running_unverified" || client.restarts[0].Update || len(client.restarts[0].Env) != 0 {
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

func TestRemoveAllProcessTools(t *testing.T) {
	client := &fakeClient{processes: map[string]protocol.Process{
		"zeta":  {Name: "zeta", State: "running"},
		"alpha": {Name: "alpha", State: "stopped"},
	}}
	s, root, _ := newTestServer(t, nil, client)

	got, err := s.callTool(context.Background(), "remove", args(root, "all", true))
	if err != nil {
		t.Fatal(err)
	}
	results, ok := got.([]stopResult)
	if !ok || len(results) != 2 || results[0].Name != "alpha" || results[1].Name != "zeta" {
		t.Fatalf("remove all result = %#v", got)
	}
	if len(client.lists) != 1 || !client.lists[0].IncludeCompleted || client.lists[0].All {
		t.Fatalf("remove all list request = %#v", client.lists)
	}
	if len(client.stops) != 2 || client.stops[0].Name != "alpha" || client.stops[1].Name != "zeta" {
		t.Fatalf("remove all calls = %#v", client.stops)
	}
	for _, fields := range []map[string]any{
		{"project_root": root},
		{"project_root": root, "all": false},
		{"project_root": root, "name": "alpha", "all": true},
	} {
		raw, marshalErr := json.Marshal(fields)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, callErr := s.callTool(context.Background(), "remove", raw); mapError(callErr).Code != "invalid_request" {
			t.Fatalf("remove input %v error = %v", fields, callErr)
		}
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
	if !ok || contains(inputRequired, "project_root") || !contains(inputRequired, "name") {
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
		if !ok || contains(required, "project_root") || !contains(required, "name") {
			t.Fatalf("input branch %d required = %#v", index, branchSchema["required"])
		}
		if rules, ok := branchSchema["allOf"].([]any); !ok || len(rules) != 2 {
			t.Fatalf("input branch %d lacks conditional root rules", index)
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

func TestRestartWaitsForReadiness(t *testing.T) {
	tests := []struct {
		name          string
		definition    *Definition
		process       protocol.Process
		waitResult    protocol.WaitResponse
		readyBefore   bool
		keepStarting  bool
		noWait        bool
		timeoutMS     int64
		wantOutcome   string
		wantReadiness string
		wantMessage   string
		wantWaits     int
	}{
		{
			name:        "ready",
			definition:  &Definition{Name: "api", Source: "hum.yaml", Cwd: "/work", Argv: []string{"api"}, Ready: &protocol.ReadinessConfig{Match: "ready"}},
			process:     protocol.Process{Name: "api", Source: "old", State: "running", PID: 41, LaunchCursor: 7},
			readyBefore: true,
			wantOutcome: "restarted", wantReadiness: protocol.ReadinessReady,
		},
		{
			name:        "running_unverified",
			process:     protocol.Process{Name: "raw", Source: "ad_hoc", State: "running", PID: 42, LaunchCursor: 8, Argv: []string{"raw"}},
			wantOutcome: protocol.ReadinessRunningUnverified, wantReadiness: protocol.ReadinessRunningUnverified,
		},
		{
			name:         "retained empty matcher after declaration removal",
			process:      protocol.Process{Name: "removed", Source: "manifest", State: "running", PID: 46, LaunchCursor: 12, Argv: []string{"removed"}, Readiness: &protocol.Readiness{State: protocol.ReadinessStarting}},
			waitResult:   protocol.WaitResponse{Op: protocol.OpWait, Outcome: protocol.WaitTimedOut},
			keepStarting: true,
			timeoutMS:    25,
			wantOutcome:  "timed_out", wantReadiness: protocol.ReadinessStarting, wantMessage: "readiness timed out", wantWaits: 1,
		},
		{
			name:        "exited_before_ready",
			definition:  &Definition{Name: "api", Source: "hum.yaml", Cwd: "/work", Argv: []string{"api"}, Ready: &protocol.ReadinessConfig{Match: "ready"}},
			process:     protocol.Process{Name: "api", Source: "old", State: "running", PID: 43, LaunchCursor: 9},
			waitResult:  protocol.WaitResponse{Op: protocol.OpWait, Outcome: protocol.WaitExited, Exit: &protocol.Exit{Code: 7}},
			wantOutcome: "exited_before_ready", wantMessage: "process exited before readiness", wantWaits: 1,
		},
		{
			name:         "timed_out",
			definition:   &Definition{Name: "api", Source: "hum.yaml", Cwd: "/work", Argv: []string{"api"}, Ready: &protocol.ReadinessConfig{Match: "ready"}},
			process:      protocol.Process{Name: "api", Source: "old", State: "running", PID: 44, LaunchCursor: 10},
			waitResult:   protocol.WaitResponse{Op: protocol.OpWait, Outcome: protocol.WaitTimedOut},
			keepStarting: true,
			timeoutMS:    25,
			wantOutcome:  "timed_out", wantReadiness: protocol.ReadinessStarting, wantMessage: "readiness timed out", wantWaits: 1,
		},
		{
			name:        "no_wait",
			definition:  &Definition{Name: "api", Source: "hum.yaml", Cwd: "/work", Argv: []string{"api"}, Ready: &protocol.ReadinessConfig{Match: "ready"}},
			process:     protocol.Process{Name: "api", Source: "old", State: "running", PID: 45, LaunchCursor: 11},
			noWait:      true,
			wantOutcome: "restarted", wantReadiness: protocol.ReadinessStarting,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var definitions []Definition
			if test.definition != nil {
				definitions = []Definition{*test.definition}
			}
			client := &fakeClient{
				processes:       map[string]protocol.Process{},
				waitResult:      test.waitResult,
				keepStarting:    test.keepStarting,
				readyBeforeWait: test.readyBefore,
			}
			client.processes[test.process.Name] = test.process
			server, root, _ := newTestServer(t, definitions, client)
			inputValues := []any{"name", test.process.Name}
			if test.noWait {
				inputValues = append(inputValues, "no_wait", true)
			}
			if test.timeoutMS != 0 {
				inputValues = append(inputValues, "timeout_ms", test.timeoutMS)
			}
			got, err := server.callTool(context.Background(), "restart", args(root, inputValues...))
			if err != nil {
				t.Fatalf("restart: %v", err)
			}
			result, ok := got.(restartResult)
			if !ok {
				t.Fatalf("restart result type = %T, want restartResult", got)
			}
			if result.Name != test.process.Name || result.Outcome != test.wantOutcome || result.Readiness != test.wantReadiness || result.PID == 0 && test.wantOutcome != "exited_before_ready" || result.LaunchCursor != test.process.LaunchCursor || result.Message != test.wantMessage {
				t.Fatalf("restart result = %#v, want outcome=%s readiness=%s message=%q", result, test.wantOutcome, test.wantReadiness, test.wantMessage)
			}
			if len(client.waits) != test.wantWaits {
				t.Fatalf("wait calls = %d, want %d (%#v)", len(client.waits), test.wantWaits, client.waits)
			}
			if test.timeoutMS != 0 && len(client.waits) == 1 && (client.waits[0].TimeoutMS < 1 || client.waits[0].TimeoutMS > test.timeoutMS) {
				t.Fatalf("wait timeout = %d, want positive value no greater than %d", client.waits[0].TimeoutMS, test.timeoutMS)
			}

			arguments := args(root, inputValues...)
			value, rpcErr := server.handleRequest(context.Background(), rpcRequest{
				JSONRPC: "2.0", ID: json.RawMessage(`"restart"`), Method: "tools/call",
				Params: func() json.RawMessage {
					encoded, _ := json.Marshal(callToolParams{Name: "restart", Arguments: arguments})
					return encoded
				}(),
			})
			if rpcErr != nil {
				t.Fatalf("restart RPC: %#v", rpcErr)
			}
			call, ok := value.(callToolResult)
			if !ok || call.IsError || len(call.Content) != 1 {
				t.Fatalf("restart RPC result = %#v", value)
			}
			var textResult restartResult
			if err := json.Unmarshal([]byte(call.Content[0].Text), &textResult); err != nil {
				t.Fatalf("decode restart text = %q: %v", call.Content[0].Text, err)
			}
			structured, ok := call.StructuredContent.(restartResult)
			if !ok {
				t.Fatalf("structured restart type = %T, want restartResult", call.StructuredContent)
			}
			if !reflect.DeepEqual(textResult, structured) || structured.Name != test.process.Name || structured.Outcome != test.wantOutcome || structured.Readiness != test.wantReadiness {
				t.Fatalf("text/structured restart mismatch: text=%#v structured=%#v", textResult, structured)
			}
		})
	}

	t.Run("rejects explicit zero timeout", func(t *testing.T) {
		client := &fakeClient{processes: map[string]protocol.Process{"raw": {Name: "raw", Source: "ad_hoc", State: "running", PID: 47}}}
		server, root, _ := newTestServer(t, nil, client)
		_, err := server.callTool(context.Background(), "restart", args(root, "name", "raw", "timeout_ms", 0))
		var toolErr *ToolError
		if !errors.As(err, &toolErr) || toolErr.Code != "invalid_request" || !strings.Contains(toolErr.Message, "positive") {
			t.Fatalf("explicit zero timeout error = %#v", err)
		}
		if len(client.restarts) != 0 {
			t.Fatalf("explicit zero timeout contacted restart: %#v", client.restarts)
		}
	})
}

func TestLogsSince(t *testing.T) {
	t.Run("captures before slow resolution", func(t *testing.T) {
		root := t.TempDir()
		client := &fakeClient{output: protocol.OutputResult{}}
		resolver := blockingResolver{resolution: Resolution{Root: root}, entered: make(chan struct{}), release: make(chan struct{})}
		server := NewServer(Options{Resolver: resolver, ClientFactory: func(context.Context, bool) (Client, error) { return client, nil }})
		resultDone := make(chan error, 1)
		go func() {
			_, err := server.callTool(context.Background(), "logs", args(root, "name", "api", "since_ms", 1000))
			resultDone <- err
		}()
		select {
		case <-resolver.entered:
		case <-time.After(time.Second):
			t.Fatal("resolver did not block")
		}
		resolverEntered := time.Now()
		time.Sleep(1100 * time.Millisecond)
		close(resolver.release)
		if err := <-resultDone; err != nil {
			t.Fatal(err)
		}
		if len(client.outputs) != 1 {
			t.Fatalf("slow-resolution logs requests = %#v, want one request", client.outputs)
		}
		cutoff := time.Unix(0, client.outputs[0].SinceUnixNano)
		if !cutoff.Before(resolverEntered) {
			t.Fatalf("MCP cutoff = %v, was captured after resolver entered at %v", cutoff, resolverEntered)
		}
	})
	t.Run("live daemon boundary and composition", testMCPLogsSinceLive)

	client := &fakeClient{output: protocol.OutputResult{Entries: []protocol.OutputEntry{{Cursor: 2, Text: "since\n"}}}}
	server, root, _ := newTestServer(t, nil, client)
	before := time.Now().Add(-1234*time.Millisecond - 20*time.Millisecond)
	value, err := server.callTool(context.Background(), "logs", args(root, "name", "api", "after", 1, "since_ms", 1234, "tail", 2, "max_entries", 2, "max_bytes", 32))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.outputs) != 1 {
		t.Fatalf("MCP logs requests = %#v, want one request", client.outputs)
	}
	request := client.outputs[0]
	if request.SinceUnixNano == 0 || request.After == nil || *request.After != 1 || request.Tail != 2 || request.MaxEntries != 2 || request.MaxBytes != 32 {
		t.Fatalf("MCP logs request = %#v, want since/cursor/tail/entry/byte composition", request)
	}
	cutoff := time.Unix(0, request.SinceUnixNano)
	if cutoff.Before(before) || cutoff.After(time.Now().Add(-1234*time.Millisecond+20*time.Millisecond)) {
		t.Fatalf("MCP since cutoff = %v, want one request-time cutoff near 1234ms ago", cutoff)
	}
	result, ok := value.(protocol.OutputResult)
	if !ok || len(result.Entries) != 1 || result.Entries[0].Text != "since\n" {
		t.Fatalf("MCP since result = %#v, want bounded output result", value)
	}
	for _, since := range []int64{0, -1, maxSinceMilliseconds + 1} {
		if _, err := server.callTool(context.Background(), "logs", args(root, "name", "api", "since_ms", since)); mapError(err).Code != "invalid_request" {
			t.Fatalf("since_ms=%d error = %v, want invalid_request", since, err)
		}
	}
	if len(client.outputs) != 1 {
		t.Fatalf("invalid MCP since requests contacted daemon: %#v", client.outputs)
	}
	var sinceProperty map[string]any
	for _, definition := range server.toolDefinitions() {
		if definition.Name == "logs" {
			sinceProperty = definition.InputSchema["properties"].(map[string]any)["since_ms"].(map[string]any)
		}
	}
	if minimum, ok := sinceProperty["minimum"].(int); !ok || minimum != 1 {
		t.Fatalf("MCP since schema minimum = %#v, want 1", sinceProperty["minimum"])
	}
}

func testMCPLogsSinceLive(t *testing.T) {
	runtimeDir, err := os.MkdirTemp("/tmp", "h-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	daemonServer, err := daemon.NewServer(daemon.Config{RuntimeDir: runtimeDir})
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- daemonServer.Serve(context.Background()) }()
	readyContext, cancelReady := context.WithTimeout(context.Background(), time.Second)
	if err := daemonServer.WaitReady(readyContext); err != nil {
		cancelReady()
		t.Fatal(err)
	}
	cancelReady()
	t.Cleanup(func() {
		_ = daemonServer.Shutdown(context.Background(), true)
		select {
		case err := <-serveDone:
			if err != nil {
				t.Errorf("live MCP daemon: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("live MCP daemon did not stop")
		}
	})

	root := t.TempDir()
	mcpServer := NewServer(Options{
		Resolver: fakeResolver{resolution: Resolution{Root: root}},
		ClientFactory: func(ctx context.Context, _ bool) (Client, error) {
			client, err := daemon.Dial(ctx, daemonServer.SocketPath())
			if err != nil {
				return nil, err
			}
			return &liveMCPOutputClient{fakeClient: &fakeClient{}, client: client}, nil
		},
	})
	client, err := daemon.Dial(context.Background(), daemonServer.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	start := protocol.NewStartRequest("api", []string{"/bin/sh", "-c", "printf 'old-mcp\\n'; sleep 2; printf 'new-mcp\\n'"}, root, nil)
	start.Root, start.Source = root, "ad_hoc"
	started, err := client.Start(context.Background(), start)
	if err != nil {
		t.Fatal(err)
	}

	var latest protocol.OutputResult
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		value, err := mcpServer.callTool(context.Background(), "logs", args(root, "name", "api"))
		if err != nil {
			t.Fatal(err)
		}
		latest = value.(protocol.OutputResult)
		found := false
		for _, entry := range latest.Entries {
			if entry.Text == "new-mcp\n" {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var newCursor protocol.Cursor
	foundNew := false
	for _, entry := range latest.Entries {
		if entry.Text == "new-mcp\n" {
			newCursor, foundNew = entry.Cursor, true
		}
	}
	if !foundNew {
		t.Fatalf("live MCP logs = %#v, never observed new entry", latest)
	}

	value, err := mcpServer.callTool(context.Background(), "logs", args(root, "name", "api", "since_ms", 1000, "after", uint64(0), "tail", 1, "max_entries", 1, "max_bytes", 64))
	if err != nil {
		t.Fatal(err)
	}
	result := value.(protocol.OutputResult)
	if len(result.Entries) != 1 || result.Entries[0].Text != "new-mcp\n" {
		t.Fatalf("live MCP since result = %#v, want only newest entry", result)
	}
	if result.Next == nil || *result.Next != newCursor {
		t.Fatalf("live MCP since next = %v, want new cursor %d", result.Next, newCursor)
	}

	value, err = mcpServer.callTool(context.Background(), "logs", args(root, "name", "api", "since_ms", 1000, "after", uint64(newCursor), "tail", 1, "max_entries", 1, "max_bytes", 64))
	if err != nil {
		t.Fatal(err)
	}
	result = value.(protocol.OutputResult)
	if len(result.Entries) != 0 || result.Next == nil || *result.Next != newCursor {
		t.Fatalf("live MCP after/since result = %#v, want empty result at cursor boundary", result)
	}
	launchCursor := protocol.Cursor(started.LaunchCursor)
	wait, err := client.Wait(context.Background(), daemon.WaitRequest{Name: "api", Cwd: root, After: &launchCursor, TimeoutMS: 4000})
	if err != nil || string(wait.Outcome) != string(protocol.WaitExited) {
		t.Fatalf("live MCP process wait = %+v, err=%v", wait, err)
	}
}

type liveMCPOutputClient struct {
	*fakeClient
	client *daemon.Client
}

func (c *liveMCPOutputClient) Close() error { return c.client.Close() }

func (c *liveMCPOutputClient) Output(ctx context.Context, request protocol.OutputRequest) (protocol.OutputResult, error) {
	result, err := c.client.Output(ctx, request)
	if err != nil {
		return protocol.OutputResult{}, err
	}
	entries := make([]protocol.OutputEntry, 0, len(result.Entries))
	for _, entry := range result.Entries {
		stream := protocol.StreamSystem
		switch entry.Stream {
		case output.Stdout:
			stream = protocol.StreamStdout
		case output.Stderr:
			stream = protocol.StreamStderr
		}
		entries = append(entries, protocol.OutputEntry{Cursor: protocol.Cursor(entry.Cursor), Stream: stream, Time: entry.Time, Text: entry.Text})
	}
	return protocol.OutputResult{Entries: entries, Next: protocolCursor(result.Next), Oldest: protocolCursor(result.Oldest), Latest: protocolCursor(result.Latest), EvictedThrough: protocolCursor(result.EvictedThrough), Truncated: result.Truncated, More: result.More}, nil
}

func protocolCursor(cursor *output.Cursor) *protocol.Cursor {
	if cursor == nil {
		return nil
	}
	value := protocol.Cursor(*cursor)
	return &value
}

func TestScopeMCP(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	input, err := decodeInput(json.RawMessage(fmt.Sprintf(`{"project_root":%q,"all":true}`, alias)))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if input.ProjectRoot != canonical || !input.All {
		t.Fatalf("decoded scope = %#v, want %q/all", input, canonical)
	}
	var list toolDefinition
	for _, definition := range NewServer(Options{}).toolDefinitions() {
		if definition.Name == "list" {
			list = definition
		}
	}
	properties := list.InputSchema["properties"].(map[string]any)
	if _, ok := properties["all"]; !ok {
		t.Fatal("list schema omits all")
	}
	client := &fakeClient{processes: map[string]protocol.Process{"web": {Name: "web", Scope: "project", Root: canonical}}}
	server, selectedRoot, _ := newTestServer(t, nil, client)
	if _, err := server.callTool(context.Background(), "list", args(selectedRoot, "all", true)); err != nil {
		t.Fatal(err)
	}
	if len(client.lists) != 1 || !client.lists[0].All || client.lists[0].Cwd != selectedRoot {
		t.Fatalf("MCP list all request = %+v", client.lists)
	}
	mapped := mapError(&protocol.WireError{Code: protocol.ErrorNotFound, Message: "missing", Details: map[string]any{
		"scope": "project", "project_root": "/work/linked", "other_scopes": []any{map[string]any{"scope": "project", "project_root": "/work/main"}},
	}})
	if mapped.Code != string(protocol.ErrorNotFound) {
		t.Fatalf("mapped error code = %q", mapped.Code)
	}
	details, ok := mapped.Details.(map[string]any)
	if !ok || details["other_scopes"] == nil {
		t.Fatalf("mapped error details = %#v", mapped.Details)
	}
}

func TestScopeDocs(t *testing.T) {
	definitions := NewServer(Options{}).toolDefinitions()
	blob, err := json.Marshal(definitions)
	if err != nil {
		t.Fatal(err)
	}
	text := string(blob)
	for _, phrase := range []string{`"scope"`, `"project_root"`, "canonical", "every project scope"} {
		if !strings.Contains(text, phrase) {
			t.Errorf("tool descriptions/schemas omit %q", phrase)
		}
	}
}

func TestManifestSelection(t *testing.T) {
	root := t.TempDir()
	definitions := []Definition{
		{Name: "api", Source: "manifest:hum.dev.yaml", Cwd: root, Argv: []string{"new"}},
		{Name: "worker", Source: "manifest:hum.dev.yaml", Cwd: root, Argv: []string{"worker"}},
	}
	newServer := func(client *fakeClient) *Server {
		resolver := &countingResolver{resolution: Resolution{Root: root, Definitions: definitions}}
		return NewServer(Options{Resolver: resolver, ClientFactory: func(context.Context, bool) (Client, error) { return client, nil }})
	}

	t.Run("selected identity and declarations", func(t *testing.T) {
		client := &fakeClient{processes: map[string]protocol.Process{}}
		server := newServer(client)
		value, err := server.callTool(context.Background(), "start", args(root, "manifest", "hum.dev.yaml", "name", "api", "no_wait", true))
		if err != nil {
			t.Fatal(err)
		}
		result := value.(launchResult)
		if len(client.starts) != 1 || client.starts[0].Source != "manifest:hum.dev.yaml" || result.Process == nil || result.Process.Source != "manifest:hum.dev.yaml" {
			t.Fatalf("selected start identity: request=%#v result=%#v", client.starts, result)
		}
		if _, err := server.callTool(context.Background(), "restart", args(root, "manifest", "hum.dev.yaml", "name", "api", "no_wait", true)); err != nil {
			t.Fatal(err)
		}
		if len(client.restarts) != 1 || !client.restarts[0].Update || client.restarts[0].Source != "manifest:hum.dev.yaml" {
			t.Fatalf("selected restart identity: %#v", client.restarts)
		}
	})

	t.Run("retained fallback", func(t *testing.T) {
		client := &fakeClient{processes: map[string]protocol.Process{
			"retained": {Name: "retained", Root: root, Cwd: root, Source: "manifest:hum.yaml", Argv: []string{"old"}, State: protocol.StateExited},
		}}
		server := newServer(client)
		started, err := server.callTool(context.Background(), "start", args(root, "manifest", "hum.dev.yaml", "name", "retained", "no_wait", true))
		if err != nil {
			t.Fatal(err)
		}
		if got := started.(launchResult); got.Outcome != "started" || len(client.starts) != 1 || client.starts[0].Name != "retained" {
			t.Fatalf("retained start fallback: result=%#v requests=%#v", got, client.starts)
		}
		client = &fakeClient{processes: map[string]protocol.Process{"retained": {Name: "retained", Root: root, Cwd: root, Source: "manifest:hum.yaml", Argv: []string{"old"}, State: protocol.StateExited}}}
		server = newServer(client)
		restarted, err := server.callTool(context.Background(), "restart", args(root, "manifest", "hum.dev.yaml", "name", "retained", "no_wait", true))
		if err != nil {
			t.Fatal(err)
		}
		if got := restarted.(restartResult); got.Name != "retained" || len(client.restarts) != 1 || client.restarts[0].Update {
			t.Fatalf("retained restart fallback: result=%#v requests=%#v", got, client.restarts)
		}
	})

	t.Run("list retained precedence and bounded response", func(t *testing.T) {
		client := &fakeClient{processes: map[string]protocol.Process{"api": {Name: "api", Scope: protocol.ScopeProject, Root: root, Source: "manifest:hum.yaml", Argv: []string{"old"}, State: protocol.StateRunning}}}
		server := newServer(client)
		value, err := server.callTool(context.Background(), "list", args(root, "manifest", "hum.dev.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		processes := value.([]protocol.Process)
		if len(processes) != 2 || processes[0].Name != "api" || processes[0].Source != "manifest:hum.yaml" || !reflect.DeepEqual(processes[0].Argv, []string{"old"}) || processes[1].Name != "worker" || processes[1].Source != "manifest:hum.dev.yaml" {
			t.Fatalf("list precedence/declarations: %#v", processes)
		}
		arguments, _ := json.Marshal(callToolParams{Name: "list", Arguments: args(root, "manifest", "hum.dev.yaml")})
		value, rpcErr := server.handleRequest(context.Background(), rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`"list"`), Method: "tools/call", Params: arguments})
		if rpcErr != nil {
			t.Fatal(rpcErr)
		}
		call := value.(callToolResult)
		if call.IsError || len(call.Content) != 1 {
			t.Fatalf("bounded list response: %#v", call)
		}
		var textProcesses []protocol.Process
		if err := json.Unmarshal([]byte(call.Content[0].Text), &textProcesses); err != nil {
			t.Fatalf("decode bounded list text: %v", err)
		}
		structured, ok := call.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("bounded list structured type=%T", call.StructuredContent)
		}
		structuredProcesses, ok := structured["processes"].([]protocol.Process)
		if !ok || len(textProcesses) != len(structuredProcesses) || len(textProcesses) != 2 {
			t.Fatalf("bounded list text/structured mismatch: text=%#v structured=%#v", textProcesses, structured)
		}
		for index := range textProcesses {
			if textProcesses[index].Name != structuredProcesses[index].Name || textProcesses[index].Source != structuredProcesses[index].Source || !reflect.DeepEqual(textProcesses[index].Argv, structuredProcesses[index].Argv) {
				t.Fatalf("bounded list identity mismatch: text=%#v structured=%#v", textProcesses, structuredProcesses)
			}
		}
	})

	t.Run("up removed definition", func(t *testing.T) {
		client := &fakeClient{processes: map[string]protocol.Process{"removed": {Name: "removed", Root: root, Source: "manifest:hum.yaml", Argv: []string{"old"}, State: protocol.StateRunning}}}
		server := newServer(client)
		value, err := server.callTool(context.Background(), "up", args(root, "manifest", "hum.dev.yaml", "no_wait", true))
		if err != nil {
			t.Fatal(err)
		}
		results := value.([]launchResult)
		found := false
		for _, result := range results {
			if result.Name == "removed" {
				found = true
				if result.Outcome != "removed_definition" || result.Process == nil || !strings.Contains(result.Guidance, "hum stop removed") {
					t.Fatalf("removed result=%#v", result)
				}
			}
		}
		if !found {
			t.Fatalf("up omitted removed definition: %#v", results)
		}
	})

	clientCalls := 0
	resolver := &countingResolver{resolution: Resolution{Root: root, Definitions: definitions}}
	server := NewServer(Options{Resolver: resolver, ClientFactory: func(context.Context, bool) (Client, error) { clientCalls++; return &fakeClient{}, nil }})
	before := clientCalls
	for _, tool := range []string{"down", "status", "logs", "wait", "input", "stop", "remove", "signal"} {
		raw := args(root, "manifest", "hum.dev.yaml", "name", "api")
		if tool == "input" {
			raw = args(root, "manifest", "hum.dev.yaml", "name", "api", "text", "x")
		}
		if _, err := server.callTool(context.Background(), tool, raw); mapError(err).Code != "invalid_request" {
			t.Fatalf("%s accepted manifest: %v", tool, err)
		}
	}
	if clientCalls != before {
		t.Fatalf("rejected manifest contacted client: %d -> %d", before, clientCalls)
	}
	if _, err := server.callTool(context.Background(), "list", json.RawMessage(`{"scope":"global","manifest":"hum.dev.yaml"}`)); mapError(err).Code != "invalid_request" {
		t.Fatalf("global manifest error = %v", err)
	}
}
