package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/process"
	"hum/internal/protocol"
)

type blockedInputChild struct {
	*daemonTestChild
	started    chan struct{}
	startOnce  sync.Once
	writeCalls atomic.Int32
}

func (c *blockedInputChild) Resize(uint16, uint16) error { return nil }

func (c *blockedInputChild) Write(p []byte) (int, error) {
	return c.WriteContext(context.Background(), p)
}

func (c *blockedInputChild) WriteContext(ctx context.Context, p []byte) (int, error) {
	c.writeCalls.Add(1)
	c.startOnce.Do(func() { close(c.started) })
	<-ctx.Done()
	return 0, ctx.Err()
}

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

func TestOneShotInputWrite(t *testing.T) {
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

	live, err := client.Start(ctx, StartRequest{
		Name: "live", Cwd: root, Root: root, Argv: []string{"/bin/sh", "-c", "sleep 5"},
		Env: []string{"PATH=/bin:/usr/bin"}, TTY: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	liveResult, err := client.Input(ctx, InputRequest{Name: "live", Cwd: root, Root: root, Data: []byte("hello")})
	if err != nil || liveResult.Bytes != len("hello") || liveResult.LaunchCursor != protocol.Cursor(live.LaunchCursor) {
		t.Fatalf("live one-shot input = %+v, err=%v, start=%+v", liveResult, err, live)
	}
	// The one-shot must wait for the server's input_release acknowledgement;
	// this attach is intentionally immediate and would race an EOF-only
	// release implementation.
	liveOwner, err := client.InputAttach(ctx, InputAttachRequest{Name: "live", Cwd: root, Root: root, TTY: true})
	if err != nil {
		t.Fatalf("immediate reattach after one-shot = %v", err)
	}
	liveState, liveCursor := liveOwner.InitialState()
	if liveState != "running" || liveCursor != protocol.Cursor(live.LaunchCursor) {
		t.Fatalf("immediate reattach state = %s/%d, start=%+v", liveState, liveCursor, live)
	}
	if err := liveOwner.Release(); err != nil {
		t.Fatalf("release immediate reattach: %v", err)
	}
	if err := client.Stop(ctx, StopRequest{Name: "live", Cwd: root}); err != nil {
		t.Fatal(err)
	}

	started, err := client.Start(ctx, StartRequest{
		Name: "prompt", Cwd: root, Root: root, Argv: []string{"/bin/sh", "-c", "read -r line; printf 'got:%s' \"$line\""},
		Env: []string{"PATH=/bin:/usr/bin"}, TTY: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Input(ctx, InputRequest{Name: "prompt", Cwd: root, Root: root, Data: []byte("hello\n")})
	if err != nil {
		t.Fatalf("one-shot input: %v", err)
	}
	if result.Bytes != len("hello\n") || result.LaunchCursor != protocol.Cursor(started.LaunchCursor) {
		t.Fatalf("one-shot result = %+v, start = %+v", result, started)
	}
	wait, err := client.Wait(ctx, WaitRequest{Name: "prompt", Cwd: root, TimeoutMS: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if string(wait.Outcome) != string(protocol.WaitExited) {
		t.Fatalf("one-shot wait = %+v", wait)
	}
	if _, err := client.Input(ctx, InputRequest{Name: "prompt", Cwd: root, Root: root, Data: []byte("again")}); err == nil {
		t.Fatal("stopped one-shot input succeeded")
	} else {
		var stopped *SessionNotRunningError
		if !errors.As(err, &stopped) {
			t.Fatalf("stopped one-shot input error = %v", err)
		}
	}
	stoppedOwner, err := client.InputAttach(ctx, InputAttachRequest{Name: "prompt", Cwd: root, Root: root, TTY: true})
	if err != nil {
		t.Fatalf("immediate reattach after stopped one-shot = %v", err)
	}
	stoppedState, _ := stoppedOwner.InitialState()
	if stoppedState != "stopped" {
		t.Fatalf("stopped reattach state = %q", stoppedState)
	}
	if err := stoppedOwner.Release(); err != nil {
		t.Fatalf("release stopped reattach: %v", err)
	}

	owned, err := client.Start(ctx, StartRequest{
		Name: "owned", Cwd: root, Root: root, Argv: []string{"/bin/sh", "-c", "sleep 5"},
		Env: []string{"PATH=/bin:/usr/bin"}, TTY: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := client.InputAttach(ctx, InputAttachRequest{Name: "owned", Cwd: root, Root: root, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Input(ctx, InputRequest{Name: "owned", Cwd: root, Root: root, Data: []byte("conflict")}); err == nil {
		t.Fatal("one-shot input ignored an occupied lease")
	} else {
		var wire *protocol.WireError
		if !errors.As(err, &wire) || wire.Code != protocol.ErrorInputConflict {
			t.Fatalf("occupied one-shot input error = %v", err)
		}
	}
	if err := owner.Release(); err != nil {
		t.Fatalf("release occupied owner: %v", err)
	}
	if _, err := client.Input(ctx, InputRequest{Name: "owned", Cwd: root, Root: root, Data: []byte("released")}); err != nil {
		t.Fatalf("one-shot input after owner release: %v", err)
	}
	if err := client.Stop(ctx, StopRequest{Name: owned.Name, Cwd: root}); err != nil {
		t.Fatal(err)
	}

	closed, err := client.Start(ctx, StartRequest{
		Name: "closed", Cwd: root, Root: root, Argv: []string{"/bin/sh", "-c", "sleep 5"},
		Env: []string{"PATH=/bin:/usr/bin"}, TTY: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	closedSession, err := client.InputAttach(ctx, InputAttachRequest{Name: "closed", Cwd: root, Root: root, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	closedCursor := protocol.Cursor(closed.LaunchCursor)
	if err := client.Stop(ctx, StopRequest{Name: "closed", Cwd: root}); err != nil {
		t.Fatal(err)
	}
	closedEventCtx, closedEventCancel := context.WithTimeout(ctx, time.Second)
	for {
		event, eventErr := closedSession.Next(closedEventCtx)
		if eventErr != nil {
			closedEventCancel()
			t.Fatalf("closed input state: %v", eventErr)
		}
		if event.State == "stopped" {
			break
		}
	}
	closedEventCancel()
	if err := closedSession.WriteAt(ctx, closedCursor, []byte("discarded")); err == nil {
		t.Fatal("closed incarnation accepted input")
	} else {
		var wire *protocol.WireError
		if !errors.As(err, &wire) || wire.Code != protocol.ErrorInputClosed {
			t.Fatalf("closed incarnation input error = %v", err)
		}
	}
	if err := closedSession.Release(); err != nil {
		t.Fatalf("release closed session: %v", err)
	}

	stale, err := client.Start(ctx, StartRequest{
		Name: "stale", Cwd: root, Root: root, Argv: []string{"/bin/sh", "-c", "sleep 5"},
		Env: []string{"PATH=/bin:/usr/bin"}, TTY: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	staleSession, err := client.InputAttach(ctx, InputAttachRequest{Name: "stale", Cwd: root, Root: root, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	staleCursor := protocol.Cursor(stale.LaunchCursor)
	if _, err := client.Restart(ctx, RestartRequest{Name: "stale", Cwd: root}); err != nil {
		t.Fatal(err)
	}
	if err := staleSession.WriteAt(ctx, staleCursor, []byte("old incarnation")); err == nil {
		t.Fatal("stale cursor input succeeded")
	} else {
		var wire *protocol.WireError
		if !errors.As(err, &wire) || wire.Code != protocol.ErrorInputStale {
			t.Fatalf("stale cursor input error = %v", err)
		}
	}
	if err := staleSession.Release(); err != nil {
		t.Fatalf("release stale session: %v", err)
	}
	if err := client.Stop(ctx, StopRequest{Name: "stale", Cwd: root}); err != nil {
		t.Fatal(err)
	}

	cancelled, err := client.Start(ctx, StartRequest{
		Name: "cancelled", Cwd: root, Root: root, Argv: []string{"/bin/sh", "-c", "sleep 5"},
		Env: []string{"PATH=/bin:/usr/bin"}, TTY: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cancelledSession, err := client.InputAttach(ctx, InputAttachRequest{Name: "cancelled", Cwd: root, Root: root, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	cancelledCtx, cancelInput := context.WithCancel(ctx)
	cancelInput()
	if err := cancelledSession.WriteAt(cancelledCtx, protocol.Cursor(cancelled.LaunchCursor), []byte("not sent")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled input error = %v, want context canceled", err)
	}
	if err := cancelledSession.Release(); err != nil {
		t.Fatalf("release cancelled session: %v", err)
	}
	cancelledOwner, err := client.InputAttach(ctx, InputAttachRequest{Name: "cancelled", Cwd: root, Root: root, TTY: true})
	if err != nil {
		t.Fatalf("reattach after cancelled input: %v", err)
	}
	if err := cancelledOwner.Release(); err != nil {
		t.Fatalf("release cancelled reattach: %v", err)
	}
	if err := client.Stop(ctx, StopRequest{Name: "cancelled", Cwd: root}); err != nil {
		t.Fatal(err)
	}

	t.Run("lost daemon acknowledgement releases lease without resend", func(t *testing.T) {
		child := &blockedInputChild{
			daemonTestChild: &daemonTestChild{pid: 9910, done: make(chan struct{})},
			started:         make(chan struct{}),
		}
		supervisor, err := app.New(app.Options{StartProcess: func(processSpec process.Spec) (app.Child, error) {
			return child, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		runtimeDir := shortRuntimeDir(t)
		lostServer, err := NewServer(Config{RuntimeDir: runtimeDir, Supervisor: supervisor})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = lostServer.Close() })
		serveDone := make(chan error, 1)
		go func() { serveDone <- lostServer.Serve(context.Background()) }()
		readyCtx, readyCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer readyCancel()
		if err := lostServer.WaitReady(readyCtx); err != nil {
			t.Fatal(err)
		}
		lostClient, err := Dial(readyCtx, lostServer.SocketPath())
		if err != nil {
			t.Fatal(err)
		}
		defer lostClient.Close()
		started, err := lostClient.Start(readyCtx, StartRequest{Name: "lost", Cwd: root, Root: root, Argv: []string{"fake"}, TTY: true})
		if err != nil {
			t.Fatal(err)
		}
		session, err := lostClient.InputAttach(readyCtx, InputAttachRequest{Name: "lost", Cwd: root, Root: root, TTY: true})
		if err != nil {
			t.Fatal(err)
		}
		writeDone := make(chan error, 1)
		go func() {
			writeDone <- session.WriteAt(context.Background(), protocol.Cursor(started.LaunchCursor), []byte("once"))
		}()
		select {
		case <-child.started:
		case <-time.After(time.Second):
			t.Fatal("lost-ack write did not reach child")
		}
		// Closing the owner transport cancels the blocked server-side write. The
		// server then attempts the acknowledgement on the closed connection and
		// must release the lease without retrying the payload.
		_ = session.client.Close()
		select {
		case err := <-writeDone:
			var wire *protocol.WireError
			if !errors.As(err, &wire) || wire.Code != protocol.ErrorInputClosed {
				t.Fatalf("lost-ack write error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("lost-ack write remained blocked")
		}
		if err := session.Release(); err != nil {
			t.Fatalf("release lost-ack session: %v", err)
		}
		if got := child.writeCalls.Load(); got != 1 {
			t.Fatalf("lost-ack child writes = %d, want 1", got)
		}
		var next *InputSession
		deadline := time.Now().Add(time.Second)
		for {
			next, err = lostClient.InputAttach(readyCtx, InputAttachRequest{Name: "lost", Cwd: root, Root: root, TTY: true})
			if err == nil {
				break
			}
			var wire *protocol.WireError
			if !errors.As(err, &wire) || wire.Code != protocol.ErrorInputConflict || time.Now().After(deadline) {
				t.Fatalf("reattach after lost ack: %v", err)
			}
			time.Sleep(time.Millisecond)
		}
		if err := next.Release(); err != nil {
			t.Fatalf("release successor after lost ack: %v", err)
		}
		shutdownClient, err := Dial(readyCtx, lostServer.SocketPath())
		if err != nil {
			t.Fatal(err)
		}
		if err := shutdownClient.Shutdown(readyCtx, ShutdownRequest{Force: true}); err != nil {
			t.Fatal(err)
		}
		_ = shutdownClient.Close()
		select {
		case <-serveDone:
		case <-time.After(3 * time.Second):
			t.Fatal("lost-ack daemon did not stop")
		}
	})

	t.Run("lost write acknowledgement never resends", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		t.Cleanup(func() {
			_ = serverConn.Close()
			_ = clientConn.Close()
		})
		inputClient := NewClient(clientConn)
		session := &InputSession{
			client: inputClient, events: make(chan protocol.InputStateEvent), eventNotify: make(chan struct{}, 1),
			acks: make(chan json.RawMessage, 4), done: make(chan struct{}), releaseDone: make(chan struct{}),
		}
		go session.readInputEvents()
		requests := make(chan protocol.Request, 1)
		go func() {
			decoder := protocol.NewDecoder(serverConn, protocol.DefaultMaxLineBytes)
			request, decodeErr := decoder.DecodeRequest()
			if decodeErr == nil {
				requests <- request
			}
			_ = serverConn.Close()
		}()
		if err := session.WriteAt(context.Background(), 0, []byte("exactly once")); err == nil {
			t.Fatal("write without acknowledgement succeeded")
		}
		select {
		case request := <-requests:
			if request.Op != protocol.OpInputWrite {
				t.Fatalf("lost-ack request op = %q", request.Op)
			}
		case <-time.After(time.Second):
			t.Fatal("lost-ack server did not receive input_write")
		}
		if err := session.Release(); err != nil {
			t.Fatalf("lost-ack release: %v", err)
		}
	})

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
