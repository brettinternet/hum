package daemon

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/process"
	"hum/internal/protocol"
)

const darwinUnixSocketPathLimit = 104

func shortRuntimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "h-")
	if err != nil {
		t.Fatalf("create short runtime root: %v", err)
	}
	socketPath := filepath.Join(dir, "hum-"+strconv.Itoa(os.Getuid()), "hum.sock")
	if len(socketPath) >= darwinUnixSocketPathLimit {
		_ = os.RemoveAll(dir)
		dir, err = os.MkdirTemp("/tmp", "h-")
		if err != nil {
			t.Fatalf("create short runtime root under /tmp: %v", err)
		}
	}
	socketPath = filepath.Join(dir, "hum-"+strconv.Itoa(os.Getuid()), "hum.sock")
	if len(socketPath) >= darwinUnixSocketPathLimit {
		_ = os.RemoveAll(dir)
		t.Fatalf("runtime socket path is too long: %q", socketPath)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func testServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	if cfg.RuntimeDir == "" {
		cfg.RuntimeDir = shortRuntimeDir(t)
	}
	if cfg.StopGrace == 0 {
		cfg.StopGrace = 20 * time.Millisecond
	}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(context.Background()) }()
	readyCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.WaitReady(readyCtx); err != nil {
		_ = server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server
}

func testStartRequest(root, name string, argv ...string) protocol.StartRequest {
	return protocol.NewStartRequest(name, argv, root, []string{"PATH=/usr/bin:/bin"})
}

func testShell(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Fatalf("required /bin/sh is unavailable: %v", err)
	}
	return "/bin/sh"
}

func TestPrivateSocket(t *testing.T) {
	t.Run("XDG", func(t *testing.T) {
		xdg := shortRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", "")
		t.Setenv("XDG_RUNTIME_DIR", xdg)
		paths := NewRuntimePaths("")
		if want := filepath.Join(xdg, "hum"); paths.Dir != want {
			t.Fatalf("runtime dir = %q, want %q", paths.Dir, want)
		}
		server := testServer(t, Config{RuntimeDir: paths.Dir})
		assertPrivateArtifacts(t, server.Paths())
	})
	t.Run("TempFallback", func(t *testing.T) {
		tmpRoot := shortRuntimeDir(t)
		t.Setenv("HUM_RUNTIME_DIR", "")
		t.Setenv("XDG_RUNTIME_DIR", "")
		t.Setenv("TMPDIR", tmpRoot)
		if got := os.TempDir(); got != tmpRoot {
			t.Fatalf("runtime fallback test requires os.TempDir to honor TMPDIR: got %q, want %q", got, tmpRoot)
		}
		paths := NewRuntimePaths("")
		want := filepath.Join(tmpRoot, "hum-"+strconv.Itoa(os.Getuid()))
		if paths.Dir != want {
			t.Fatalf("fallback runtime dir = %q, want %q", paths.Dir, want)
		}
		server := testServer(t, Config{RuntimeDir: paths.Dir})
		assertPrivateArtifacts(t, server.Paths())
	})
}

func assertPrivateArtifacts(t *testing.T, paths RuntimePaths) {
	t.Helper()
	for _, item := range []struct {
		name string
		path string
		mode os.FileMode
	}{{"directory", paths.Dir, 0o700}, {"socket", paths.Socket, 0o600}, {"pid", paths.PID, 0o600}, {"lock", paths.Lock, 0o600}, {"ready", paths.Ready, 0o600}, {"log", paths.Log, 0o600}} {
		info, err := os.Stat(item.path)
		if err != nil {
			t.Fatalf("stat %s: %v", item.name, err)
		}
		if info.Mode().Perm() != item.mode {
			t.Fatalf("%s mode = %o, want %o", item.name, info.Mode().Perm(), item.mode)
		}
	}
}

type daemonTestResponse struct {
	Op      protocol.Operation  `json:"op"`
	OK      bool                `json:"ok"`
	Version int                 `json:"version"`
	Error   *protocol.WireError `json:"error"`
}

type daemonTestReadConn struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (c *daemonTestReadConn) Read(p []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Read(p)
}

type daemonTestWriteConn struct {
	net.Conn
	writeCalls atomic.Uint64
	writeBytes atomic.Uint64
}

func (c *daemonTestWriteConn) Write(p []byte) (int, error) {
	c.writeCalls.Add(1)
	n, err := c.Conn.Write(p)
	c.writeBytes.Add(uint64(n))
	return n, err
}

type daemonTestChild struct {
	pid  int
	done chan struct{}
	once sync.Once
}

func (c *daemonTestChild) PID() int { return c.pid }

func (c *daemonTestChild) PGID() int { return c.pid }

func (c *daemonTestChild) Done() <-chan struct{} { return c.done }

func (c *daemonTestChild) Wait() process.Result {
	<-c.done
	return process.Result{ExitCode: -1, ExitedAt: time.Now()}
}

func (c *daemonTestChild) Signal(os.Signal) error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func waitForDaemonTest(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func daemonTestProcessGroupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func assertShutdownArtifactsAbsent(t *testing.T, paths RuntimePaths) {
	t.Helper()
	for _, item := range []struct {
		name string
		path string
	}{{"socket", paths.Socket}, {"pid", paths.PID}, {"ready", paths.Ready}} {
		if _, err := os.Stat(item.path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s after shutdown = %v, want not found", item.name, err)
		}
	}
}

func TestClientDisconnect(t *testing.T) {
	server := testServer(t, Config{})
	root := t.TempDir()
	client, err := Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	process, err := client.Start(context.Background(), testStartRequest(root, "detached", testShell(t), "-c", "sleep 2"))
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	time.Sleep(30 * time.Millisecond)
	got, err := server.Supervisor().Get(root, process.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != app.StateRunning {
		t.Fatalf("process state after client disconnect = %s, want running", got.State)
	}
	_ = server.Supervisor().Stop(context.Background(), root, process.Name)

	t.Run("timed-out control request cannot be reused", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		recordingConn := &daemonTestWriteConn{Conn: clientConn}
		controlClient := NewClient(recordingConn)
		t.Cleanup(func() {
			_ = recordingConn.Close()
			_ = serverConn.Close()
		})
		const serverMaxLine = 512
		secondRequest := make(chan error, 1)
		serverErrors := make(chan error, 1)
		go func() {
			decoder := protocol.NewDecoder(serverConn, serverMaxLine)
			encoder := protocol.NewEncoder(serverConn, serverMaxLine)
			request, err := decoder.DecodeRequest()
			if err != nil {
				serverErrors <- err
				return
			}
			if request.Op != protocol.OpHello {
				serverErrors <- errors.New("fake server received non-hello handshake")
				return
			}
			if err := encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}); err != nil {
				serverErrors <- err
				return
			}
			_, err = decoder.DecodeRequest()
			if err == nil {
				serverErrors <- errors.New("fake server accepted oversized start request")
				return
			}
			if !errors.Is(err, protocol.ErrOversized) {
				serverErrors <- err
				return
			}
			go func() {
				_, err := decoder.DecodeRequest()
				secondRequest <- err
				_ = serverConn.Close()
			}()
			if err := encoder.EncodeResponse(protocol.ErrorResponse{
				Op:    "",
				OK:    false,
				Error: protocol.WireErrorForDecode(err),
			}); err != nil {
				serverErrors <- err
				_ = serverConn.Close()
			}
		}()

		helloCtx, cancelHello := context.WithTimeout(context.Background(), time.Second)
		if err := controlClient.Hello(helloCtx); err != nil {
			cancelHello()
			t.Fatal(err)
		}
		cancelHello()
		requestCtx, cancelRequest := context.WithTimeout(context.Background(), time.Second)
		_, err = controlClient.Start(requestCtx, testStartRequest(root, "timed", testShell(t), "-c", strings.Repeat("x", serverMaxLine)))
		cancelRequest()
		var wireErr *protocol.WireError
		if !errors.As(err, &wireErr) || wireErr.Code != protocol.ErrorOversized {
			t.Fatalf("oversized control request = %v, want protocol.WireError code %q", err, protocol.ErrorOversized)
		}
		writeCalls, writeBytes := recordingConn.writeCalls.Load(), recordingConn.writeBytes.Load()
		if writeCalls == 0 || writeBytes == 0 {
			t.Fatalf("oversized control request wrote calls=%d bytes=%d", writeCalls, writeBytes)
		}
		listCtx, cancelList := context.WithTimeout(context.Background(), time.Second)
		_, err = controlClient.List(listCtx, protocol.NewListRequest(root, false, false))
		cancelList()
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("list after terminal decoder error = %v, want net.ErrClosed", err)
		}
		if got := recordingConn.writeCalls.Load(); got != writeCalls {
			t.Fatalf("list after terminal decoder error made %d additional writes", got-writeCalls)
		}
		if got := recordingConn.writeBytes.Load(); got != writeBytes {
			t.Fatalf("list after terminal decoder error wrote %d additional bytes", got-writeBytes)
		}
		select {
		case serverErr := <-serverErrors:
			t.Fatalf("fake server: %v", serverErr)
		case requestErr := <-secondRequest:
			if requestErr == nil {
				t.Fatal("fake server observed second request after terminal decoder error")
			}
		case <-time.After(time.Second):
			t.Fatal("fake server did not observe client connection close")
		}
	})
}

func TestMultipleFollowers(t *testing.T) {
	server := testServer(t, Config{})
	root := t.TempDir()
	client, err := Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.Start(context.Background(), testStartRequest(root, "logs", testShell(t), "-c", "printf 'one\\n'; sleep .1; printf 'two\\n'; sleep .1"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.Follow(context.Background(), protocol.NewFollowRequest("logs", root))
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Follow(context.Background(), protocol.NewFollowRequest("logs", root))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	defer second.Close()
	for _, follower := range []*Follower{first, second} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		var seenOne, seenTwo, exited bool
		for !exited {
			event, err := follower.Next(ctx)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			if event.Read != nil {
				for _, entry := range event.Read.Entries {
					seenOne = seenOne || strings.Contains(entry.Text, "one")
					seenTwo = seenTwo || strings.Contains(entry.Text, "two")
				}
			}
			if event.Exit != nil {
				exited = true
			}
		}
		cancel()
		if !seenOne || !seenTwo {
			t.Fatalf("follower missed output: one=%v two=%v", seenOne, seenTwo)
		}
	}

	t.Run("blank-op terminal error preserves typed error and closes follower", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		follower := &Follower{client: newClient(clientConn, "")}
		t.Cleanup(func() {
			_ = follower.Close()
			_ = serverConn.Close()
		})
		serverDone := make(chan error, 1)
		go func() {
			serverDone <- protocol.NewEncoder(serverConn).EncodeResponse(protocol.ErrorResponse{
				Op:    "",
				OK:    false,
				Error: protocol.NewWireError(protocol.ErrorOutput, "terminal follower", nil),
			})
		}()

		nextCtx, cancelNext := context.WithTimeout(context.Background(), time.Second)
		_, err := follower.Next(nextCtx)
		cancelNext()
		var wireErr *protocol.WireError
		if !errors.As(err, &wireErr) || wireErr.Code != protocol.ErrorOutput || wireErr.Message != "terminal follower" {
			t.Fatalf("blank-op follower error = %v, want typed output error", err)
		}
		select {
		case serverErr := <-serverDone:
			if serverErr != nil {
				t.Fatalf("fake follower server: %v", serverErr)
			}
		case <-time.After(time.Second):
			t.Fatal("fake follower server did not send terminal error")
		}

		secondDone := make(chan error, 1)
		go func() {
			_, err := follower.Next(context.Background())
			secondDone <- err
		}()
		select {
		case err := <-secondDone:
			if !errors.Is(err, net.ErrClosed) {
				t.Fatalf("second follower Next after blank-op error = %v, want net.ErrClosed", err)
			}
		case <-time.After(time.Second):
			t.Fatal("second follower Next after blank-op error did not fail fast")
		}
	})

	t.Run("Next cancellation without deadline", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		readConn := &daemonTestReadConn{Conn: clientConn, entered: make(chan struct{})}
		follower := &Follower{client: newClient(readConn, "")}
		t.Cleanup(func() {
			_ = clientConn.Close()
			_ = serverConn.Close()
		})
		nextDone := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			_, err := follower.Next(ctx)
			nextDone <- err
		}()
		select {
		case <-readConn.entered:
		case <-time.After(time.Second):
			t.Fatal("follower Next did not begin its blocking read")
		}
		cancel()
		select {
		case err := <-nextDone:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled follower Next = %v, want context canceled", err)
			}
		case <-time.After(time.Second):
			t.Fatal("follower Next with canceled context remained blocked")
		}
	})

	t.Run("concurrent Close interrupts blocked Next", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		readConn := &daemonTestReadConn{Conn: clientConn, entered: make(chan struct{})}
		follower := &Follower{client: newClient(readConn, "")}
		t.Cleanup(func() {
			_ = clientConn.Close()
			_ = serverConn.Close()
		})
		nextDone := make(chan error, 1)
		go func() {
			_, err := follower.Next(context.Background())
			nextDone <- err
		}()
		select {
		case <-readConn.entered:
		case <-time.After(time.Second):
			t.Fatal("follower Next did not begin its blocking read")
		}
		closeDone := make(chan error, 1)
		go func() { closeDone <- follower.Close() }()
		select {
		case <-closeDone:
		case <-time.After(time.Second):
			t.Fatal("follower Close remained blocked behind Next")
		}
		select {
		case err := <-nextDone:
			if err == nil {
				t.Fatal("follower Next succeeded after concurrent Close")
			}
		case <-time.After(time.Second):
			t.Fatal("follower Next remained blocked after concurrent Close")
		}
	})
}

func TestSocketOwnership(t *testing.T) {
	runtimeDir := shortRuntimeDir(t)
	first, err := NewServer(Config{RuntimeDir: runtimeDir})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewServer(Config{RuntimeDir: runtimeDir})
	if second != nil {
		_ = second.Close()
		t.Fatal("second server unexpectedly acquired runtime")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second startup error = %v, want ErrAlreadyRunning", err)
	}
}

func TestStaleRuntimeRecovery(t *testing.T) {
	runtimeDir := shortRuntimeDir(t)
	paths := NewRuntimePaths(runtimeDir)
	if err := os.WriteFile(paths.PID, []byte(strconv.Itoa(1<<30)), 0o600); err != nil {
		t.Fatal(err)
	}
	stale, err := net.Listen("unix", paths.Socket)
	if err != nil {
		t.Fatal(err)
	}
	_ = stale.Close()
	server := testServer(t, Config{RuntimeDir: runtimeDir})
	if _, err := os.Stat(server.Paths().Socket); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentStartup(t *testing.T) {
	runtimeDir := shortRuntimeDir(t)
	const contenders = 8
	servers := make(chan *Server, contenders)
	errs := make(chan error, contenders)
	var wg sync.WaitGroup
	for range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			server, err := NewServer(Config{RuntimeDir: runtimeDir})
			if err != nil {
				errs <- err
				return
			}
			servers <- server
		}()
	}
	wg.Wait()
	close(servers)
	close(errs)
	var winners []*Server
	for server := range servers {
		winners = append(winners, server)
	}
	if len(winners) != 1 {
		for _, server := range winners {
			_ = server.Close()
		}
		t.Fatalf("concurrent startup winners = %d, want 1", len(winners))
	}
	_ = winners[0].Close()
	for err := range errs {
		if !errors.Is(err, ErrAlreadyRunning) && !errors.Is(err, ErrRuntimeOwned) {
			t.Fatalf("contender error = %v", err)
		}
	}

	t.Run("Start and Shutdown(false) admission", func(t *testing.T) {
		runtimeDir := shortRuntimeDir(t)
		root := t.TempDir()
		startEntered := make(chan struct{})
		releaseStart := make(chan struct{})
		child := &daemonTestChild{pid: 424242, done: make(chan struct{})}
		supervisor, err := app.New(app.Options{
			StopGrace: time.Millisecond,
			StartProcess: func(process.Spec) (app.Child, error) {
				close(startEntered)
				<-releaseStart
				return child, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		server, err := NewServer(Config{RuntimeDir: runtimeDir, Supervisor: supervisor})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = server.Close() })

		startRequest := testStartRequest(root, "race", testShell(t), "-c", "sleep 1")
		startDone := make(chan wireResponse, 1)
		go func() {
			response, _ := server.dispatch(wireRequest{
				Op: string(protocol.OpStart), Name: startRequest.Name, Argv: startRequest.Argv,
				Cwd: startRequest.Cwd, Env: startRequest.Env,
			})
			startDone <- response
		}()
		select {
		case <-startEntered:
		case <-time.After(time.Second):
			t.Fatal("start did not reach the process admission barrier")
		}
		shutdownDone := make(chan wireResponse, 1)
		go func() {
			response, _ := server.dispatch(wireRequest{Op: string(protocol.OpShutdown), Force: false})
			shutdownDone <- response
		}()
		close(releaseStart)

		var startResponse, shutdownResponse wireResponse
		select {
		case startResponse = <-startDone:
		case <-time.After(time.Second):
			t.Fatal("start remained blocked after admission barrier release")
		}
		select {
		case shutdownResponse = <-shutdownDone:
		case <-time.After(time.Second):
			t.Fatal("non-forced shutdown remained blocked after start admission")
		}
		switch {
		case startResponse.Error == nil:
			if startResponse.Process == nil {
				t.Fatal("successful start response omitted process")
			}
			if shutdownResponse.Error == nil || shutdownResponse.Error.Code != string(protocol.ErrorActiveProcesses) {
				t.Fatalf("start admitted without shutdown refusal: shutdown response = %+v", shutdownResponse)
			}
		case startResponse.Error.Code == string(protocol.ErrorSupervisorClosed):
			if shutdownResponse.Error != nil || shutdownResponse.Op != string(protocol.OpShutdown) {
				t.Fatalf("shutdown won without start rejection: shutdown response = %+v", shutdownResponse)
			}
		default:
			t.Fatalf("unexpected start response during concurrent shutdown: %+v", startResponse)
		}
	})
}

func TestReadinessHandshake(t *testing.T) {
	runtimeDir := shortRuntimeDir(t)
	server, err := NewServer(Config{RuntimeDir: runtimeDir})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() { _ = server.Serve(context.Background()) }()
	select {
	case <-server.ready:
		t.Fatal("server reported readiness before accepting a connection")
	default:
	}
	if _, err := os.Stat(server.Paths().Ready); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ready artifact before accepted connection = %v, want not found", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(server.Paths().Ready); err != nil {
		t.Fatalf("ready artifact after handshake: %v", err)
	}
}

func TestShutdown(t *testing.T) {
	t.Run("refuses active processes", func(t *testing.T) {
		server := testServer(t, Config{})
		root := t.TempDir()
		client, err := Dial(context.Background(), server.Paths().Socket)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		_, err = client.Start(context.Background(), testStartRequest(root, "active", testShell(t), "-c", "sleep 2"))
		if err != nil {
			t.Fatal(err)
		}
		err = client.Shutdown(context.Background(), protocol.NewShutdownRequest(false))
		var active *ActiveProcessesError
		if !errors.As(err, &active) || len(active.Names) != 1 || !strings.Contains(active.Names[0], "active") {
			t.Fatalf("shutdown refusal = %v, want named active process", err)
		}
		if _, err := os.Stat(server.Paths().Socket); err != nil {
			t.Fatalf("socket after refusal: %v", err)
		}
	})
	t.Run("forced waits and removes", func(t *testing.T) {
		runtimeDir := shortRuntimeDir(t)
		server := testServer(t, Config{RuntimeDir: runtimeDir, StopGrace: 300 * time.Millisecond})
		root := t.TempDir()
		client, err := Dial(context.Background(), server.Paths().Socket)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		descendantPath := filepath.Join(root, "descendant.pid")
		termPath := filepath.Join(root, "descendant.term")
		script := `( 
			trap '' HUP
			trap 'printf term > "$2"' TERM
			while :; do sleep 1; done
		) &
		printf '%s' "$!" > "$1"
		exit 0`
		managed, err := client.Start(context.Background(), testStartRequest(root, "active", testShell(t), "-c", script, "sh", descendantPath, termPath))
		if err != nil {
			t.Fatal(err)
		}
		var descendantPID int
		waitForDaemonTest(t, 3*time.Second, "shell-created descendant", func() bool {
			data, readErr := os.ReadFile(descendantPath)
			if readErr != nil {
				return false
			}
			descendantPID, readErr = strconv.Atoi(strings.TrimSpace(string(data)))
			return readErr == nil && descendantPID > 0
		})
		t.Cleanup(func() {
			if daemonTestProcessGroupAlive(managed.PGID) {
				_ = syscall.Kill(-managed.PGID, syscall.SIGKILL)
			}
		})
		waitForDaemonTest(t, 3*time.Second, "shell leader exit", func() bool {
			return !processAlive(managed.PID)
		})
		if !processAlive(descendantPID) {
			t.Fatalf("shell-created descendant %d exited before forced shutdown", descendantPID)
		}

		shutdownDone := make(chan error, 1)
		go func() {
			shutdownDone <- client.Shutdown(context.Background(), protocol.NewShutdownRequest(true))
		}()
		waitForDaemonTest(t, 3*time.Second, "descendant TERM handler", func() bool {
			_, err := os.Stat(termPath)
			return err == nil
		})
		if !daemonTestProcessGroupAlive(managed.PGID) {
			t.Fatal("process group disappeared before forced shutdown cleanup barrier")
		}
		for _, item := range []struct {
			name string
			path string
		}{{"socket", server.Paths().Socket}, {"pid", server.Paths().PID}, {"ready", server.Paths().Ready}} {
			if _, err := os.Stat(item.path); err != nil {
				t.Fatalf("%s disappeared while descendant %d was alive: %v", item.name, descendantPID, err)
			}
		}
		select {
		case err := <-shutdownDone:
			if err != nil {
				t.Fatalf("forced shutdown: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("forced shutdown remained blocked")
		}
		waitForDaemonTest(t, 3*time.Second, "managed process group exit", func() bool {
			return !daemonTestProcessGroupAlive(managed.PGID)
		})
		if err := server.Wait(); err != nil {
			t.Fatalf("daemon Serve after forced shutdown: %v", err)
		}
		assertShutdownArtifactsAbsent(t, server.Paths())
	})
}

func TestHelloVersion(t *testing.T) {
	t.Run("rejects version 0, missing version, and wrong op", func(t *testing.T) {
		server := testServer(t, Config{})
		tests := []struct {
			name    string
			request any
		}{
			{"version 0", protocol.Hello{Op: protocol.OpHello, Version: 0}},
			{"missing version", map[string]any{"op": string(protocol.OpHello)}},
			{"wrong op", map[string]any{"op": string(protocol.OpStart), "version": protocol.Version}},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				conn, err := net.Dial("unix", server.Paths().Socket)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				if err := protocol.NewEncoder(conn).EncodeRequest(test.request); err != nil {
					t.Fatal(err)
				}
				var response daemonTestResponse
				if err := protocol.NewDecoder(conn).Decode(&response); err != nil {
					t.Fatal(err)
				}
				if response.Op != protocol.OpHello {
					t.Fatalf("rejected hello response op = %q, want hello", response.Op)
				}
				if response.Error == nil {
					t.Fatal("invalid hello unexpectedly succeeded")
				}
				if test.name != "wrong op" && response.Error.Code != protocol.ErrorVersionMismatch {
					t.Fatalf("invalid hello error code = %q, want version_mismatch", response.Error.Code)
				}
			})
		}
	})
	t.Run("blank-op terminal error preserves typed error and closes client", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		controlClient := NewClient(clientConn)
		t.Cleanup(func() {
			_ = controlClient.Close()
			_ = serverConn.Close()
		})
		serverDone := make(chan error, 1)
		go func() {
			decoder := protocol.NewDecoder(serverConn)
			if _, err := decoder.DecodeRequest(); err != nil {
				serverDone <- err
				return
			}
			serverDone <- protocol.NewEncoder(serverConn).EncodeResponse(protocol.ErrorResponse{
				Op:    "",
				OK:    false,
				Error: protocol.NewWireError(protocol.ErrorInternal, "terminal hello", nil),
			})
		}()

		helloCtx, cancelHello := context.WithTimeout(context.Background(), time.Second)
		err := controlClient.Hello(helloCtx)
		cancelHello()
		var wireErr *protocol.WireError
		if !errors.As(err, &wireErr) || wireErr.Code != protocol.ErrorInternal || wireErr.Message != "terminal hello" {
			t.Fatalf("blank-op hello error = %v, want typed internal error", err)
		}
		select {
		case serverErr := <-serverDone:
			if serverErr != nil {
				t.Fatalf("fake hello server: %v", serverErr)
			}
		case <-time.After(time.Second):
			t.Fatal("fake hello server did not send terminal error")
		}

		listCtx, cancelList := context.WithTimeout(context.Background(), time.Second)
		_, err = controlClient.List(listCtx, protocol.NewListRequest(t.TempDir(), false, false))
		cancelList()
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("list after blank-op hello error = %v, want net.ErrClosed", err)
		}
	})

	t.Run("same-op hello error preserves client for retry", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		controlClient := NewClient(clientConn)
		t.Cleanup(func() {
			_ = controlClient.Close()
			_ = serverConn.Close()
		})
		serverDone := make(chan error, 1)
		go func() {
			decoder := protocol.NewDecoder(serverConn)
			encoder := protocol.NewEncoder(serverConn)

			request, err := decoder.DecodeRequest()
			if err != nil {
				serverDone <- err
				return
			}
			if request.Op != protocol.OpHello || request.Hello == nil || request.Hello.Version != protocol.Version {
				serverDone <- errors.New("first request was not the current hello")
				return
			}
			if err := encoder.EncodeResponse(protocol.NewErrorResponse(
				protocol.OpHello,
				protocol.NewWireError(protocol.ErrorInternal, "retryable hello", nil),
			)); err != nil {
				serverDone <- err
				return
			}

			request, err = decoder.DecodeRequest()
			if err != nil {
				serverDone <- err
				return
			}
			if request.Op != protocol.OpHello || request.Hello == nil || request.Hello.Version != protocol.Version {
				serverDone <- errors.New("retry request was not the current hello")
				return
			}
			serverDone <- encoder.EncodeResponse(protocol.NewHello())
		}()

		helloCtx, cancelHello := context.WithTimeout(context.Background(), time.Second)
		err := controlClient.Hello(helloCtx)
		cancelHello()
		var wireErr *protocol.WireError
		if !errors.As(err, &wireErr) || wireErr.Code != protocol.ErrorInternal || wireErr.Message != "retryable hello" {
			t.Fatalf("same-op hello error = %v, want typed internal error", err)
		}

		controlClient.stateMu.Lock()
		closedAfterError := controlClient.closed
		helloOKAfterError := controlClient.helloOK
		helloErrAfterError := controlClient.helloErr
		controlClient.stateMu.Unlock()
		if closedAfterError {
			t.Fatal("same-op hello error closed the client")
		}
		if helloOKAfterError {
			t.Fatal("same-op hello error marked hello successful")
		}
		if helloErrAfterError != nil {
			t.Fatalf("same-op hello error was cached: %v", helloErrAfterError)
		}

		retryCtx, cancelRetry := context.WithTimeout(context.Background(), time.Second)
		err = controlClient.Hello(retryCtx)
		cancelRetry()
		if err != nil {
			t.Fatalf("retry hello: %v", err)
		}
		select {
		case serverErr := <-serverDone:
			if serverErr != nil {
				t.Fatalf("fake hello server: %v", serverErr)
			}
		case <-time.After(time.Second):
			t.Fatal("fake hello server did not receive retry hello")
		}

		controlClient.stateMu.Lock()
		closedAfterRetry := controlClient.closed
		helloOKAfterRetry := controlClient.helloOK
		controlClient.stateMu.Unlock()
		if closedAfterRetry {
			t.Fatal("successful retry hello left the client closed")
		}
		if !helloOKAfterRetry {
			t.Fatal("successful retry hello did not mark hello successful")
		}
	})

	t.Run("repeat Hello is a no-op and connection remains usable", func(t *testing.T) {
		server := testServer(t, Config{})
		client, err := Dial(context.Background(), server.Paths().Socket)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if err := client.Hello(context.Background()); err != nil {
			t.Fatalf("repeated Hello: %v", err)
		}
		items, err := client.List(context.Background(), protocol.NewListRequest(t.TempDir(), false, false))
		if err != nil {
			t.Fatalf("list after repeated Hello: %v", err)
		}
		if len(items) != 0 {
			t.Fatalf("list after repeated Hello = %d processes, want 0", len(items))
		}
	})

	t.Run("newer client to older daemon permits frozen shutdown", func(t *testing.T) {
		server := testServer(t, Config{Version: strconv.Itoa(protocol.Version)})
		conn, err := net.Dial("unix", server.Paths().Socket)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		encoder := protocol.NewEncoder(conn)
		decoder := protocol.NewDecoder(conn)
		if err := encoder.EncodeRequest(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version + 1}); err != nil {
			t.Fatal(err)
		}
		var mismatch daemonTestResponse
		if err := decoder.Decode(&mismatch); err != nil {
			t.Fatal(err)
		}
		if mismatch.Op != protocol.OpHello || mismatch.Error == nil || mismatch.Error.Code != protocol.ErrorVersionMismatch {
			t.Fatalf("newer-client hello response = %+v, want version_mismatch", mismatch)
		}
		details, ok := mismatch.Error.Details.(map[string]any)
		if !ok {
			t.Fatalf("version mismatch details = %T, want object", mismatch.Error.Details)
		}
		clientVersion, clientOK := details["client"].(float64)
		daemonVersion, daemonOK := details["daemon"].(float64)
		if !clientOK || int(clientVersion) != protocol.Version+1 || !daemonOK || int(daemonVersion) != protocol.Version {
			t.Fatalf("version mismatch direction = %#v, want client %d daemon %d", details, protocol.Version+1, protocol.Version)
		}
		if err := encoder.EncodeRequest(protocol.NewShutdownRequest(false)); err != nil {
			t.Fatal(err)
		}
		var shutdown daemonTestResponse
		if err := decoder.Decode(&shutdown); err != nil {
			t.Fatal(err)
		}
		if shutdown.Op != protocol.OpShutdown || !shutdown.OK || shutdown.Error != nil {
			t.Fatalf("frozen shutdown response = %+v, want successful shutdown", shutdown)
		}
		if err := server.Wait(); err != nil {
			t.Fatalf("daemon Serve after frozen shutdown: %v", err)
		}
		assertShutdownArtifactsAbsent(t, server.Paths())
	})
}

func TestDaemonSignal(t *testing.T) {
	t.Run("unsupported signal returns invalid_signal", func(t *testing.T) {
		server := testServer(t, Config{})
		client, err := Dial(context.Background(), server.Paths().Socket)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		err = client.Signal(context.Background(), protocol.NewSignalRequest("missing", t.TempDir(), "SIGUSR1"))
		var wireErr *protocol.WireError
		if !errors.As(err, &wireErr) || wireErr.Code != protocol.ErrorInvalidSignal {
			t.Fatalf("unsupported signal error = %v, want invalid_signal", err)
		}
	})

	runSignal := func(t *testing.T, daemonSignal syscall.Signal) {
		t.Helper()
		server := testServer(t, Config{})
		root := t.TempDir()
		client, err := Dial(context.Background(), server.Paths().Socket)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		descendantPath := filepath.Join(root, "descendant.pid")
		script := `(sleep 30) &
		printf '%s' "$!" > "$1"
		wait`
		managed, err := client.Start(context.Background(), testStartRequest(root, "signal", testShell(t), "-c", script, "sh", descendantPath))
		if err != nil {
			t.Fatal(err)
		}
		var descendantPID int
		waitForDaemonTest(t, 3*time.Second, "signal-test descendant", func() bool {
			data, readErr := os.ReadFile(descendantPath)
			if readErr != nil {
				return false
			}
			descendantPID, readErr = strconv.Atoi(strings.TrimSpace(string(data)))
			return readErr == nil && descendantPID > 0
		})
		t.Cleanup(func() {
			if daemonTestProcessGroupAlive(managed.PGID) {
				_ = syscall.Kill(-managed.PGID, syscall.SIGKILL)
			}
		})
		if !processAlive(descendantPID) {
			t.Fatalf("signal-test descendant %d exited before daemon signal", descendantPID)
		}
		if err := syscall.Kill(os.Getpid(), daemonSignal); err != nil {
			t.Fatalf("send daemon %s: %v", daemonSignal, err)
		}
		serveDone := make(chan error, 1)
		go func() { serveDone <- server.Wait() }()
		select {
		case err := <-serveDone:
			if err != nil {
				t.Fatalf("daemon Serve after %s: %v", daemonSignal, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("daemon Serve remained blocked after %s", daemonSignal)
		}
		waitForDaemonTest(t, 3*time.Second, "signal-test process group exit", func() bool {
			return !daemonTestProcessGroupAlive(managed.PGID)
		})
		if processAlive(descendantPID) {
			t.Fatalf("signal-test descendant %d remained alive after %s", descendantPID, daemonSignal)
		}
		assertShutdownArtifactsAbsent(t, server.Paths())
	}

	t.Run("SIGTERM", func(t *testing.T) { runSignal(t, syscall.SIGTERM) })
	t.Run("SIGINT", func(t *testing.T) { runSignal(t, syscall.SIGINT) })
}

func TestProtocolSurfaceUsesNoEnvironment(t *testing.T) {
	// This focused compile-time/runtime check keeps the daemon's response path
	// honest when protocol DTOs evolve: a response containing env must be
	// rejected by the shared encoder before it can reach a client.
	var sink strings.Builder
	encoder := protocol.NewEncoder(&sink)
	if err := encoder.EncodeResponse(struct {
		Op  protocol.Operation `json:"op"`
		Env []string           `json:"env"`
	}{Op: protocol.OpStart, Env: []string{"SECRET=x"}}); err == nil {
		t.Fatal("response environment unexpectedly encoded")
	}
	if sink.Len() != 0 {
		t.Fatalf("environment response wrote %d bytes", sink.Len())
	}
}
