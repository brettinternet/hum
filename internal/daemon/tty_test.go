package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"hum/internal/protocol"
)

func TestTTYRemove(t *testing.T) {
	runtimeDir := t.TempDir()
	root := t.TempDir()
	server, err := NewServer(Config{RuntimeDir: runtimeDir, StopGrace: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	client, err := Dial(ctx, server.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Start(ctx, StartRequest{
		Name: "cat", Cwd: root, Root: root, Argv: []string{"/bin/sh", "-c", "cat"},
		Env: []string{"PATH=/bin:/usr/bin"}, TTY: true,
	}); err != nil {
		t.Fatal(err)
	}
	session, err := client.InputAttach(ctx, InputAttachRequest{Op: protocol.OpInputAttach, Name: "cat", Cwd: root, Root: root, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Release()
	if err := client.Remove(ctx, RemoveRequest{Name: "cat", Cwd: root}); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := session.Next(ctx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("input session remained open after remove")
			}
			break
		}
	}
	if err := session.Release(); err != nil {
		t.Fatal(err)
	}
	shutdownClient, err := Dial(ctx, server.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdownClient.Shutdown(ctx, ShutdownRequest{Force: true}); err != nil {
		t.Fatal(err)
	}
	_ = shutdownClient.Close()
	select {
	case <-serveDone:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

func TestTTYInputTransport(t *testing.T) {
	runtimeDir := t.TempDir()
	root := t.TempDir()
	server, err := NewServer(Config{RuntimeDir: runtimeDir, StopGrace: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	client, err := Dial(ctx, server.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	started, err := client.Start(ctx, StartRequest{Name: "cat", Cwd: root, Root: root, Argv: []string{"/bin/sh", "-c", "read line; printf done"}, Env: []string{"PATH=/bin:/usr/bin"}, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	if !started.TTY {
		t.Fatalf("started snapshot = %+v", started)
	}
	session, err := client.InputAttach(ctx, InputAttachRequest{Op: protocol.OpInputAttach, Name: "cat", Cwd: root, Root: root, TTY: true, Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	state, cursor := session.State()
	if state != "running" || cursor != protocol.Cursor(started.LaunchCursor) {
		t.Fatalf("input state = %s/%d", state, cursor)
	}
	other, err := Dial(ctx, server.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.InputAttach(ctx, InputAttachRequest{Op: protocol.OpInputAttach, Name: "cat", Cwd: root, Root: root, TTY: true}); err == nil {
		t.Fatal("second input owner was accepted")
	} else {
		var wire *protocol.WireError
		if !errors.As(err, &wire) || wire.Code != protocol.ErrorInputConflict {
			t.Fatalf("conflict error = %v", err)
		}
	}
	if err := session.Write(ctx, []byte("abc\n")); err != nil {
		t.Fatalf("input write: %v", err)
	}
	wait, err := client.Wait(ctx, WaitRequest{Name: "cat", Cwd: root, TimeoutMS: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if string(wait.Outcome) != "exited" {
		t.Fatalf("wait outcome = %+v", wait)
	}
	shutdownClient, err := Dial(ctx, server.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdownClient.Shutdown(ctx, ShutdownRequest{Force: true}); err != nil {
		t.Fatal(err)
	}
	_ = shutdownClient.Close()
	select {
	case <-serveDone:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
	}
}
