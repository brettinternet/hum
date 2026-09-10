package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hum/internal/protocol"
)

func TestStartupBudgetIncludesEveryRecordedGroup(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := NewRuntimePaths(runtimeDir)
	state := RuntimeState{
		Version: RuntimeStateVersion,
		Daemon:  RuntimeIdentity{PID: 2147483646, StartIdentity: "dead:1"},
		Groups: []RuntimeGroup{
			{ProjectRoot: t.TempDir(), Name: "one", LeaderPID: 2147483645, PGID: 2147483645, StartIdentity: "dead:2"},
			{ProjectRoot: t.TempDir(), Name: "two", LeaderPID: 2147483644, PGID: 2147483644, StartIdentity: "dead:3"},
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.State, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := StartupBudget(paths, 3*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if want := 17 * time.Second; got != want {
		t.Fatalf("StartupBudget() = %s, want %s", got, want)
	}

	got, err = StartupBudget(paths, 0, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if want := 5 * time.Second; got != want {
		t.Fatalf("StartupBudget() with zero grace = %s, want %s", got, want)
	}
}

func TestStartupBudgetWithoutStateIsDialSlack(t *testing.T) {
	got, err := StartupBudget(NewRuntimePaths(t.TempDir()), time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got != 5*time.Second {
		t.Fatalf("StartupBudget() = %s, want 5s", got)
	}
}

func TestInputAttachRejectsUnexpectedResponseOperations(t *testing.T) {
	for _, tc := range []struct {
		name      string
		responses []any
		wantOp    protocol.Operation
	}{
		{name: "attach response", responses: []any{protocol.GetResponse{Op: protocol.OpGet, OK: true}}, wantOp: protocol.OpGet},
		{name: "state event", responses: []any{protocol.InputAttachResponse{Op: protocol.OpInputAttach, OK: true}, protocol.InputStateEvent{Op: protocol.OpEvent, State: protocol.StateRunning, LaunchCursor: 1, TTY: true}}, wantOp: protocol.OpEvent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := filepath.Join(shortRuntimeDir(t), "input.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			serverDone := make(chan error, 1)
			go func() {
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					serverDone <- acceptErr
					return
				}
				defer conn.Close()
				decoder := protocol.NewDecoder(conn)
				encoder := protocol.NewEncoder(conn)
				if _, decodeErr := decoder.DecodeRequest(); decodeErr != nil {
					serverDone <- decodeErr
					return
				}
				if encodeErr := encoder.EncodeResponse(protocol.HelloResponse{Op: protocol.OpHello, Version: protocol.Version}); encodeErr != nil {
					serverDone <- encodeErr
					return
				}
				if _, decodeErr := decoder.DecodeRequest(); decodeErr != nil {
					serverDone <- decodeErr
					return
				}
				for _, response := range tc.responses {
					if encodeErr := encoder.EncodeResponse(response); encodeErr != nil {
						serverDone <- encodeErr
						return
					}
				}
				serverDone <- nil
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			client := &Client{socket: socket}
			_, err = client.InputAttach(ctx, InputAttachRequest{Name: "tty", Cwd: t.TempDir(), Root: t.TempDir(), TTY: true})
			if err == nil || !strings.Contains(err.Error(), string(tc.wantOp)) {
				t.Fatalf("InputAttach() error = %v, want unexpected %q operation", err, tc.wantOp)
			}
			if err := <-serverDone; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInputAckBlankOperationPreservesTypedError(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	session := &InputSession{client: NewClient(clientConn), acks: make(chan json.RawMessage, 1)}
	session.acks <- json.RawMessage(`{"ok":false,"error":{"code":"oversized","message":"too large"}}`)
	err := session.waitForAck(context.Background(), protocol.OpInputWrite)
	var wireErr *protocol.WireError
	if !errors.As(err, &wireErr) || wireErr.Code != protocol.ErrorOversized {
		t.Fatalf("waitForAck() error = %v, want typed oversized error", err)
	}
	if !session.client.closed {
		t.Fatal("blank-operation terminal error did not invalidate client")
	}
}
