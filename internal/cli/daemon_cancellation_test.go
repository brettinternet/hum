//go:build !windows

package cli

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"hum/internal/daemon"
	"hum/internal/protocol"
)

// stalledRunDaemon completes the handshake and startup handoff, then leaves
// the chosen unary request unanswered until the client closes the socket.
func stalledRunDaemon(t *testing.T, stall protocol.Operation) (string, <-chan protocol.Operation) {
	t.Helper()
	return stalledDaemonListing(t, stall, []protocol.Process{{Name: "demo", Scope: "global", State: "running", StopGrace: 100 * time.Millisecond}})
}

// stalledDaemonListing is stalledRunDaemon with a caller-supplied list response.
func stalledDaemonListing(t *testing.T, stall protocol.Operation, listed []protocol.Process) (string, <-chan protocol.Operation) {
	t.Helper()
	runtimeDir, err := os.MkdirTemp("/tmp", "h-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	listener, err := net.Listen("unix", daemon.NewRuntimePaths(runtimeDir).Socket)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan protocol.Operation, 8)
	var mu sync.Mutex
	var connections []net.Conn
	var workers sync.WaitGroup
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		for _, connection := range connections {
			_ = connection.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				decoder := protocol.NewDecoder(conn)
				encoder := protocol.NewEncoder(conn)
				request, decodeErr := decoder.DecodeRequest()
				if decodeErr != nil || request.Op != protocol.OpHello {
					return
				}
				if encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}) != nil {
					return
				}
				for {
					request, decodeErr = decoder.DecodeRequest()
					if decodeErr != nil {
						return
					}
					switch request.Op {
					case protocol.OpGet:
						_ = encoder.EncodeResponse(protocol.NewErrorResponse(protocol.OpGet, protocol.NewWireError(protocol.ErrorNotFound, "not found", nil)))
					case protocol.OpList:
						_ = encoder.EncodeResponse(protocol.NewListResponse(listed))
					case protocol.OpFollow:
						if encoder.EncodeResponse(protocol.NewReadyEvent(nil)) != nil {
							return
						}
					case protocol.OpStart:
						if stall == request.Op {
							requests <- request.Op
							_, _ = decoder.DecodeRequest()
							return
						}
						process := protocol.Process{Name: "demo", State: "running", StopGrace: 100 * time.Millisecond}
						_ = encoder.EncodeResponse(protocol.NewStartResponse(process))
						requests <- request.Op
					case protocol.OpStop:
						requests <- request.Op
						_, _ = decoder.DecodeRequest()
						return
					default:
						return
					}
				}
			}()
		}
	}()
	return runtimeDir, requests
}

func awaitStalledRequest(t *testing.T, requests <-chan protocol.Operation, want protocol.Operation) {
	t.Helper()
	select {
	case got := <-requests:
		if got != want {
			t.Fatalf("request = %s; want %s", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("no %s request", want)
	}
}

func TestUnresponsiveDaemonHonorsTermination(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			runtimeDir, requests := stalledRunDaemon(t, protocol.OpStart)
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			client := cliServeRunStartClient(t, "--stop-grace", "100ms", "run", "demo", "--detach", "--", "true")
			awaitStalledRequest(t, requests, protocol.OpStart)
			start := time.Now()
			if err := client.cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			err := client.wait(2 * time.Second)
			if err == nil || strings.Contains(err.Error(), "did not exit") || time.Since(start) >= 2*time.Second {
				t.Fatalf("client should exit nonzero within 2s: %v; stderr=%q", err, client.stderr())
			}
			if !strings.Contains(client.stderr(), "start request") {
				t.Fatalf("missing unanswered request in stderr: %q", client.stderr())
			}
		})
	}
}

func TestDownStalledStopHonorsTermination(t *testing.T) {
	runtimeDir, requests := stalledRunDaemon(t, protocol.OpStop)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	client := cliServeRunStartClient(t, "--stop-grace", "100ms", "--global", "down")
	awaitStalledRequest(t, requests, protocol.OpStop)
	if err := client.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := client.wait(2 * time.Second); err == nil || strings.Contains(err.Error(), "did not exit") {
		t.Fatalf("down should exit nonzero on termination: %v; stderr=%q", err, client.stderr())
	}
	if !strings.Contains(client.stderr(), "stop request") {
		t.Fatalf("missing stop request error: %q", client.stderr())
	}
}

func TestDownTerminationSharesOneCleanupDeadline(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeManifestCLITestFile(t, root, `version: 1
processes:
  a:
    argv: [a]
    ready: {match: a}
  b:
    argv: [b]
    ready: {match: b}
    after: [a]
  c:
    argv: [c]
    after: [b]
`)
	var listed []protocol.Process
	for _, name := range []string{"a", "b", "c"} {
		listed = append(listed, protocol.Process{Name: name, Source: "manifest:hum.yaml", Scope: "project", Root: root, Cwd: root, Argv: []string{name}, State: "running", StopGrace: 100 * time.Millisecond})
	}
	runtimeDir, requests := stalledDaemonListing(t, protocol.OpStop, listed)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	client := cliServeRunStartClientInDir(t, root, "--stop-grace", "100ms", "down")
	awaitStalledRequest(t, requests, protocol.OpStop)
	start := time.Now()
	if err := client.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	// c's stop is canceled; b and a must share one grace-plus-slack budget
	// rather than each receiving a fresh one.
	awaitStalledRequest(t, requests, protocol.OpStop)
	limit := 100*time.Millisecond + daemonCleanupSlack + 600*time.Millisecond
	if err := client.wait(limit); err == nil || strings.Contains(err.Error(), "did not exit") || time.Since(start) > limit {
		t.Fatalf("down should exit nonzero within one cleanup budget: %v; elapsed=%s stderr=%q", err, time.Since(start), client.stderr())
	}
}

func TestTerminationStopRequestIsBounded(t *testing.T) {
	runtimeDir, requests := stalledRunDaemon(t, protocol.OpStop)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	client := cliServeRunStartClient(t, "--stop-grace", "100ms", "run", "demo", "--", "true")
	awaitStalledRequest(t, requests, protocol.OpStart)
	// Let the client enter its follow loop after receiving the start response.
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	if err := client.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	awaitStalledRequest(t, requests, protocol.OpStop)
	if err := client.wait(100*time.Millisecond + daemonCleanupSlack + 400*time.Millisecond); err == nil || strings.Contains(err.Error(), "did not exit") || time.Since(start) > 100*time.Millisecond+daemonCleanupSlack+400*time.Millisecond {
		t.Fatalf("client should exit nonzero after bounded stop: %v; stderr=%q", err, client.stderr())
	}
	if !strings.Contains(client.stderr(), "stop request") {
		t.Fatalf("missing stop request error: %q", client.stderr())
	}
}

// Live requests must not expire on the CLI's grace: the daemon applies each
// process's admitted grace, which may be longer. Only cleanup is time-bounded.
func TestBoundedDaemonRequestOnlyBoundsCleanup(t *testing.T) {
	live, cancelLive := boundedDaemonRequest(context.Background(), 0)
	defer cancelLive()
	if _, ok := live.Deadline(); ok {
		t.Fatal("live request has a deadline")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	cleanup, cancelCleanup := boundedDaemonRequest(canceled, time.Second)
	defer cancelCleanup()
	deadline, ok := cleanup.Deadline()
	if !ok || cleanup.Err() != nil || time.Until(deadline) > time.Second+daemonCleanupSlack {
		t.Fatalf("cleanup deadline = %v, %v, err=%v", deadline, ok, cleanup.Err())
	}
}
