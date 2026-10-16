//go:build !windows

package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/process"
	"hum/internal/protocol"
)

type serverPortLookupChild struct {
	*daemonTestChild
	result      process.PortsResult
	inspections atomic.Int32
}

func (c *serverPortLookupChild) InspectPorts(context.Context) process.PortsResult {
	c.inspections.Add(1)
	return c.result
}

func TestPortLookupRoundTripAcrossScopes(t *testing.T) {
	t.Parallel()
	rootA, rootB := t.TempDir(), t.TempDir()
	for _, root := range []*string{&rootA, &rootB} {
		if err := os.Mkdir(filepath.Join(*root, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		canonical, err := filepath.EvalSymlinks(*root)
		if err != nil {
			t.Fatal(err)
		}
		*root = canonical
	}
	children := map[string]*serverPortLookupChild{
		"web-a": {daemonTestChild: &daemonTestChild{pid: 9001, done: make(chan struct{})}, result: process.PortsResult{State: process.PortsAvailable, Listeners: []process.Port{
			{Transport: "tcp", Address: "127.0.0.1", Port: 3000, PIDs: []int{9101}},
			{Transport: "tcp", Address: "127.0.0.1", Port: 3001, PIDs: []int{9101}},
		}}},
		"web-b":  {daemonTestChild: &daemonTestChild{pid: 9002, done: make(chan struct{})}, result: process.PortsResult{State: process.PortsPartial, Diagnostic: "one member was unreadable", Listeners: []process.Port{{Transport: "tcp", Address: "::1", Port: 3000, PIDs: []int{9102}}}}},
		"global": {daemonTestChild: &daemonTestChild{pid: 9003, done: make(chan struct{})}, result: process.PortsResult{State: process.PortsDenied, Diagnostic: "permission denied", Listeners: []process.Port{}}},
	}
	supervisor, err := app.New(app.Options{StartProcess: func(spec process.Spec) (app.Child, error) {
		return children[spec.Argv[0]], nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := testServer(t, Config{Supervisor: supervisor})
	client, err := Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, request := range []protocol.StartRequest{
		{Name: "web-a", Root: rootA, Cwd: rootA, Argv: []string{"web-a"}},
		{Name: "web-b", Root: rootB, Cwd: rootB, Argv: []string{"web-b"}},
		{Name: "global", Scope: app.ScopeGlobal, Cwd: rootA, Argv: []string{"global"}},
	} {
		if _, err := client.Start(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.List(context.Background(), protocol.NewListRequest(rootA, false, true)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.List(context.Background(), protocol.NewListRequest(rootA, true, true)); err != nil {
		t.Fatal(err)
	}
	for name, child := range children {
		if child.inspections.Load() != 0 {
			t.Fatalf("ordinary list inspected %s sockets %d times", name, child.inspections.Load())
		}
	}

	lookup, err := client.PortLookup(context.Background(), protocol.PortLookupRequest{Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if len(lookup.Processes) != 2 || lookup.Processes[0].Name != "web-a" || lookup.Processes[1].Name != "web-b" {
		t.Fatalf("port lookup holders = %#v", lookup.Processes)
	}
	for _, holder := range lookup.Processes {
		if holder.Ports == nil || len(holder.Ports.Listeners) != 1 || holder.Ports.Listeners[0].Port != 3000 {
			t.Fatalf("holder %s listener filter = %#v", holder.Name, holder.Ports)
		}
	}
	if lookup.Processes[1].Root != rootB || lookup.Processes[1].Ports.State != process.PortsPartial {
		t.Fatalf("cross-project partial holder = %#v", lookup.Processes[1])
	}
	if len(lookup.Incomplete) != 1 || lookup.Incomplete[0].Name != "global" || lookup.Incomplete[0].Scope != app.ScopeGlobal || lookup.Incomplete[0].State != process.PortsDenied || lookup.Incomplete[0].Diagnostic != "permission denied" {
		t.Fatalf("incomplete global inspection = %#v", lookup.Incomplete)
	}
}

func TestAlternateManifestStopGrace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	argv := []string{os.Args[0], "-test.run=^$"}
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte("version: 1\nprocesses:\n  web:\n    argv: [echo]\n    stop_grace: 900ms\n  default:\n    argv: [echo]\n    stop_grace: 900ms\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hum.dev.yaml"), []byte("version: 1\nprocesses:\n  web:\n    argv: [echo]\n    stop_grace: 125ms\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := testServer(t, Config{RuntimeDir: filepath.Join(shortRuntimeDir(t), "runtime"), StopGrace: 3 * time.Second})
	startResponse, _ := server.dispatch(&protocol.Request{Op: protocol.OpStart, Start: &protocol.StartRequest{Op: protocol.OpStart, Name: "web", Scope: protocol.ScopeProject, Root: root, Cwd: root, Argv: argv, Source: "manifest:hum.dev.yaml"}})
	started, ok := startResponse.(protocol.StartResponse)
	if !ok || started.Process == nil {
		t.Fatalf("alternate start response = %#v", startResponse)
	}
	if started.Process.StopGrace != 125*time.Millisecond || started.Process.StopGraceInherited {
		t.Fatalf("alternate start stop grace = %#v", started.Process)
	}
	restartResponse, _ := server.dispatch(&protocol.Request{Op: protocol.OpRestart, Restart: &protocol.RestartRequest{Op: protocol.OpRestart, Name: "web", Scope: protocol.ScopeProject, Root: root, Cwd: root, Argv: argv, Source: "manifest:hum.dev.yaml", Update: true}})
	restarted, ok := restartResponse.(protocol.RestartResponse)
	if !ok || restarted.Process == nil {
		t.Fatalf("alternate restart response = %#v", restartResponse)
	}
	if restarted.Process.StopGrace != 125*time.Millisecond || restarted.Process.StopGraceInherited {
		t.Fatalf("alternate restart stop grace = %#v", restarted.Process)
	}
	defaultResponse, _ := server.dispatch(&protocol.Request{Op: protocol.OpStart, Start: &protocol.StartRequest{Op: protocol.OpStart, Name: "default", Scope: protocol.ScopeProject, Root: root, Cwd: root, Argv: argv, Source: "manifest"}})
	defaultStarted, ok := defaultResponse.(protocol.StartResponse)
	if !ok || defaultStarted.Process == nil {
		t.Fatalf("default start response = %#v", defaultResponse)
	}
	if defaultStarted.Process.StopGrace != 900*time.Millisecond || defaultStarted.Process.StopGraceInherited {
		t.Fatalf("default manifest behavior changed = %#v", defaultStarted.Process)
	}
}

func TestSinceProtocolVersionNegotiation(t *testing.T) {
	t.Parallel()
	server := testServer(t, Config{WireVersion: protocol.Version - 1})
	client, err := Dial(context.Background(), server.Paths().Socket)
	if client == nil {
		t.Fatalf("legacy daemon dial returned nil client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	var mismatch *VersionMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("since request against legacy daemon hello error = %v, want version mismatch", err)
	}
	if mismatch.ClientVersion != protocol.Version || mismatch.DaemonVersion != protocol.Version-1 {
		t.Fatalf("since protocol mismatch = client %d daemon %d, want client %d daemon %d", mismatch.ClientVersion, mismatch.DaemonVersion, protocol.Version, protocol.Version-1)
	}
	if !strings.Contains(mismatch.Error(), "hum shutdown --stop-processes") {
		t.Fatalf("protocol mismatch error missing actionable shutdown guidance: %q", mismatch.Error())
	}
	_, outputErr := client.Output(context.Background(), protocol.OutputRequest{
		Op:            protocol.OpOutput,
		Name:          "api",
		Cwd:           t.TempDir(),
		SinceUnixNano: time.Now().Add(-time.Second).UnixNano(),
	})
	if !errors.As(outputErr, &mismatch) {
		t.Fatalf("since output against legacy daemon = %v, want cached version mismatch", outputErr)
	}
	if err := client.Shutdown(context.Background(), protocol.NewShutdownRequest(false)); err != nil {
		t.Fatalf("legacy daemon shutdown: %v", err)
	}
	if err := server.Wait(); err != nil {
		t.Fatalf("legacy daemon wait: %v", err)
	}
}
