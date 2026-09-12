package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hum/internal/protocol"
)

func TestAlternateManifestStopGrace(t *testing.T) {
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
