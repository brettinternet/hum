package daemon

import (
	"context"
	"encoding/json"
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
	"hum/internal/output"
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

func TestStatusGetTransportsNextCursorAndTypedErrors(t *testing.T) {
	root := t.TempDir()
	var store *output.Store
	supervisor, err := app.New(app.Options{
		StartProcess: func(spec process.Spec) (app.Child, error) {
			store = spec.Output
			return &daemonTestChild{pid: 9001, done: make(chan struct{})}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := testServer(t, Config{Supervisor: supervisor})
	client, err := Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if _, err := supervisor.Start(app.StartRequest{Name: "status", Cwd: root, Argv: []string{"fake"}}); err != nil {
		t.Fatal(err)
	}
	if store == nil {
		t.Fatal("fake process did not expose output store")
	}
	empty, err := client.Get(context.Background(), protocol.NewGetRequest("status", root))
	if err != nil {
		t.Fatalf("empty status get: %v", err)
	}
	if empty.NextCursor != 0 {
		t.Fatalf("empty status next cursor = %d, want 0", empty.NextCursor)
	}
	for _, text := range []string{"first\n", "second\n"} {
		if _, err := store.Append(output.Stdout, time.Unix(1, 0), text); err != nil {
			t.Fatal(err)
		}
	}

	got, err := client.Get(context.Background(), protocol.NewGetRequest("status", root))
	if err != nil {
		t.Fatal(err)
	}
	if got.NextCursor != output.Cursor(2) {
		t.Fatalf("status next cursor = %d, want 2", got.NextCursor)
	}

	_, err = client.Get(context.Background(), protocol.NewGetRequest("bad/name", root))
	var wireErr *protocol.WireError
	if !errors.As(err, &wireErr) || wireErr.Code != protocol.ErrorInvalidRequest {
		t.Fatalf("invalid status name error = %v, want typed invalid request", err)
	}

	_, err = client.Get(context.Background(), protocol.NewGetRequest("missing", root))
	wireErr = nil
	if !errors.As(err, &wireErr) || wireErr.Code != protocol.ErrorNotFound {
		t.Fatalf("missing status process error = %v, want typed not found", err)
	}
}

func TestStatusFollowers(t *testing.T) {
	root := t.TempDir()
	server := testServer(t, Config{})
	client, err := Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	follower, err := client.Follow(context.Background(), protocol.NewFollowRequest("watched", root))
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Get(context.Background(), protocol.NewGetRequest("watched", root))
	if err != nil || got.Followers != 1 {
		t.Fatalf("attached status followers = %d, err %v, want 1", got.Followers, err)
	}
	if err := follower.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, err = client.Get(context.Background(), protocol.NewGetRequest("watched", root))
		if errors.Is(err, app.ErrProcessNotFound) || got.Followers == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("detached status followers = %d, err %v, want zero or removed pre-launch session", got.Followers, err)
}

func TestStatusResponseShapes(t *testing.T) {
	initialCursor := uint64(19)
	item := wireProcess{
		Name:       "status",
		Root:       "/work/project",
		PID:        42,
		PGID:       42,
		Cwd:        "/work/project",
		Argv:       []string{"tool"},
		NextCursor: &initialCursor,
	}
	encode := func(t *testing.T, response wireResponse) []byte {
		t.Helper()
		var sink strings.Builder
		if err := writeProtocolResponse(protocol.NewEncoder(&sink), response); err != nil {
			t.Fatalf("write %s response: %v", response.Op, err)
		}
		return []byte(sink.String())
	}
	assertOmitted := func(t *testing.T, response wireResponse) {
		t.Helper()
		if encoded := string(encode(t, response)); strings.Contains(encoded, `"next_cursor"`) {
			t.Fatalf("%s response unexpectedly includes next_cursor: %s", response.Op, encoded)
		}
	}

	t.Run("start omits next cursor", func(t *testing.T) {
		assertOmitted(t, wireResponse{Op: "start", OK: true, Process: &item})
	})
	t.Run("list omits next cursor", func(t *testing.T) {
		assertOmitted(t, wireResponse{Op: "list", OK: true, Processes: []wireProcess{item}})
	})
	t.Run("stop omits next cursor", func(t *testing.T) {
		assertOmitted(t, wireResponse{Op: "stop", OK: true, Process: &item})
	})
	for _, test := range []struct {
		name   string
		cursor uint64
	}{
		{name: "nonzero", cursor: 19},
		{name: "zero", cursor: 0},
	} {
		t.Run("get includes exact next cursor "+test.name, func(t *testing.T) {
			nextCursor := test.cursor
			item.NextCursor = &nextCursor
			encoded := encode(t, wireResponse{Op: "get", OK: true, Process: &item})
			var got protocol.GetResponse
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatalf("decode get response: %v", err)
			}
			if got.Process == nil || got.Process.NextCursor == nil {
				t.Fatalf("get response next_cursor = %#v, want pointer to %d", got.Process, test.cursor)
			}
			if got := uint64(*got.Process.NextCursor); got != test.cursor {
				t.Fatalf("get response next_cursor = %d, want %d", got, test.cursor)
			}
		})
	}
}

func TestStatusGetRejectsOmittedNextCursor(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	client := NewClient(clientConn)
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverConn.Close()
	})

	serverDone := make(chan error, 1)
	go func() {
		decoder := protocol.NewDecoder(serverConn)
		request, err := decoder.DecodeRequest()
		if err != nil {
			serverDone <- err
			return
		}
		if request.Op != protocol.OpGet {
			serverDone <- errors.New("fake server received non-get request")
			return
		}
		process := protocol.Process{
			Name: "status", Root: "/work/project", PID: 42, PGID: 42,
			Cwd: "/work/project", Argv: []string{"tool"}, State: string(app.StateRunning),
		}
		serverDone <- protocol.NewEncoder(serverConn).EncodeResponse(protocol.GetResponse{
			Op: protocol.OpGet, OK: true, Process: &process,
		})
	}()

	got, err := client.Get(context.Background(), protocol.NewGetRequest("status", "/work/project"))
	if err == nil || err.Error() != "daemon get response omitted next_cursor" {
		t.Fatalf("legacy get response error = %v, want omitted next_cursor response-shape error", err)
	}
	if got.Name != "" || got.Root != "" || got.PID != 0 || got.Argv != nil || got.NextCursor != 0 || got.Exit != nil {
		t.Fatalf("legacy get returned successful process data: %#v", got)
	}
	select {
	case serverErr := <-serverDone:
		if serverErr != nil {
			t.Fatalf("fake legacy server: %v", serverErr)
		}
	case <-time.After(time.Second):
		t.Fatal("fake legacy server did not send get response")
	}
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
		var seenOne, seenTwo, waiting bool
		for !waiting {
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
			if event.Read != nil {
				for _, entry := range event.Read.Entries {
					waiting = waiting || strings.Contains(entry.Text, "waiting for next launch")
				}
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

func TestFollowAcrossOrdinaryStartReplacement(t *testing.T) {
	server := testServer(t, Config{})
	root := t.TempDir()
	client, err := Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	shell := testShell(t)
	if _, err := client.Start(context.Background(), testStartRequest(root, "restart", shell, "-c", "printf 'old\\n'; sleep .1")); err != nil {
		t.Fatal(err)
	}
	follower, err := client.Follow(context.Background(), protocol.NewFollowRequest("restart", root))
	if err != nil {
		t.Fatal(err)
	}
	defer follower.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	seenOld, seenWaiting := false, false
	for !seenWaiting {
		event, err := follower.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if event.Read != nil {
			for _, entry := range event.Read.Entries {
				seenOld = seenOld || strings.Contains(entry.Text, "old")
				seenWaiting = seenWaiting || strings.Contains(entry.Text, "waiting for next launch")
			}
		}
	}
	if !seenOld {
		t.Fatal("durable follower missed first incarnation")
	}
	if _, err := client.Start(context.Background(), testStartRequest(root, "restart", shell, "-c", "printf 'new\\n'; sleep .1")); err != nil {
		t.Fatal(err)
	}
	seenLaunch, seenNew := false, false
	for !seenNew {
		event, err := follower.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if event.Read != nil {
			for _, entry := range event.Read.Entries {
				seenLaunch = seenLaunch || strings.Contains(entry.Text, "launched")
				seenNew = seenNew || strings.Contains(entry.Text, "new")
			}
		}
	}
	if !seenLaunch {
		t.Fatal("durable follower missed successor launch boundary")
	}
}

func TestFollowRetainsOutputAfterCompletedEviction(t *testing.T) {
	root := t.TempDir()
	aReady := make(chan struct{})
	var aStore *output.Store
	var aChild *daemonTestChild

	supervisor, err := app.New(app.Options{
		CompletedLimit: 1,
		Now:            func() time.Time { return time.Unix(0, 0) },
		StartProcess: func(spec process.Spec) (app.Child, error) {
			if len(spec.Argv) < 2 {
				return nil, errors.New("fake process missing name")
			}
			child := &daemonTestChild{pid: 1000, done: make(chan struct{})}
			switch spec.Argv[1] {
			case "A":
				aStore = spec.Output
				aChild = child
				close(aReady)
			case "B":
				spec.Output.NotifyExit(output.Exit{Code: -1, Time: time.Unix(2, 0)})
				child.once.Do(func() { close(child.done) })
			default:
				return nil, errors.New("unexpected fake process")
			}
			return child, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := testServer(t, Config{Supervisor: supervisor})
	client, err := Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	startCtx, cancelStart := context.WithTimeout(context.Background(), 3*time.Second)
	_, err = client.Start(startCtx, testStartRequest(root, "A", "fake", "A"))
	cancelStart()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-aReady:
	case <-time.After(3 * time.Second):
		t.Fatal("fake process A did not reach its start barrier")
	}
	if aStore == nil || aChild == nil {
		t.Fatal("fake process A did not expose output and child")
	}
	if _, err := aStore.Append(output.Stdout, time.Unix(1, 0), "A first\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := aStore.Append(output.Stdout, time.Unix(1, 1), "A second\n"); err != nil {
		t.Fatal(err)
	}
	aStore.NotifyExit(output.Exit{Code: -1, Time: time.Unix(1, 2)})
	aChild.once.Do(func() { close(aChild.done) })
	waitForDaemonTest(t, 3*time.Second, "process A to exit", func() bool {
		got, err := server.Supervisor().Get(root, "A")
		return err == nil && got.State == app.StateExited
	})

	followRequest := protocol.NewFollowRequest("A", root)
	followRequest.MaxEntries = 1
	followCtx, cancelFollow := context.WithTimeout(context.Background(), 3*time.Second)
	follower, err := client.Follow(followCtx, followRequest)
	cancelFollow()
	if err != nil {
		t.Fatal(err)
	}
	defer follower.Close()

	firstCtx, cancelFirst := context.WithTimeout(context.Background(), 3*time.Second)
	first, err := follower.Next(firstCtx)
	cancelFirst()
	if err != nil {
		t.Fatal(err)
	}
	if first.Read == nil || len(first.Read.Entries) != 1 || first.Read.Entries[0].Text != "A first\n" {
		t.Fatalf("first retained output event = %#v, want A first", first)
	}

	startCtx, cancelStart = context.WithTimeout(context.Background(), 3*time.Second)
	_, err = client.Start(startCtx, testStartRequest(root, "B", "fake", "B"))
	cancelStart()
	if err != nil {
		t.Fatal(err)
	}
	waitForDaemonTest(t, 3*time.Second, "process B to exit while process A stays reserved", func() bool {
		b, bErr := server.Supervisor().Get(root, "B")
		_, aErr := server.Supervisor().Get(root, "A")
		return aErr == nil && (errors.Is(bErr, app.ErrProcessNotFound) || bErr == nil && b.State == app.StateExited)
	})

	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelDrain()
	gotSecond := false
	for !gotSecond {
		event, err := follower.Next(drainCtx)
		if err != nil {
			t.Fatalf("draining reserved follower: %v", err)
		}
		if event.Read != nil {
			for _, entry := range event.Read.Entries {
				gotSecond = gotSecond || entry.Text == "A second\n"
			}
		}
	}
	if !gotSecond {
		t.Fatal("follower missed retained output while its completed session was protected")
	}

}

func TestRepeatedCompletedProcessFollow(t *testing.T) {
	server := testServer(t, Config{CompletedLimit: 1})
	root := t.TempDir()
	client, err := Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	shell := testShell(t)

	const repetitions = 8
	for i := range repetitions {
		name := "completed-" + strconv.Itoa(i)
		if _, err := client.Start(context.Background(), testStartRequest(root, name, shell, "-c", "printf 'done\\n'")); err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
		waitForDaemonTest(t, 3*time.Second, name+" to exit", func() bool {
			process, err := client.Get(context.Background(), protocol.NewGetRequest(name, root))
			return err == nil && process.State == app.StateExited
		})

		followCtx, cancelFollow := context.WithTimeout(context.Background(), 3*time.Second)
		follower, err := client.Follow(followCtx, protocol.NewFollowRequest(name, root))
		cancelFollow()
		if err != nil {
			t.Fatalf("follow %s: %v", name, err)
		}
		nextCtx, cancelNext := context.WithTimeout(context.Background(), 3*time.Second)
		for {
			event, err := follower.Next(nextCtx)
			if err != nil {
				cancelNext()
				_ = follower.Close()
				t.Fatalf("follow %s missed retained output: %v", name, err)
			}
			seen := false
			if event.Read != nil {
				for _, entry := range event.Read.Entries {
					seen = seen || strings.Contains(entry.Text, "done")
				}
			}
			if seen {
				break
			}
		}
		cancelNext()
		_ = follower.Close()
	}
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
	t.Run("Restart and Shutdown(false) admission", func(t *testing.T) {
		runtimeDir := shortRuntimeDir(t)
		root := t.TempDir()
		restartEntered := make(chan struct{})
		releaseRestart := make(chan struct{})
		var releaseOnce sync.Once
		firstChild := &daemonTestChild{pid: 424243, done: make(chan struct{})}
		secondChild := &daemonTestChild{pid: 424244, done: make(chan struct{})}
		var starts atomic.Int32
		supervisor, err := app.New(app.Options{
			StopGrace: time.Millisecond,
			StartProcess: func(process.Spec) (app.Child, error) {
				switch starts.Add(1) {
				case 1:
					return firstChild, nil
				case 2:
					close(restartEntered)
					<-releaseRestart
					return secondChild, nil
				default:
					return nil, errors.New("unexpected extra process start")
				}
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		server, err := NewServer(Config{RuntimeDir: runtimeDir, Supervisor: supervisor})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			releaseOnce.Do(func() { close(releaseRestart) })
			_ = server.Close()
		})

		startResponse, _ := server.dispatch(wireRequest{
			Op:   string(protocol.OpStart),
			Name: "race",
			Cwd:  root,
			Argv: []string{"fake", "race"},
		})
		if startResponse.Error != nil || startResponse.Process == nil {
			t.Fatalf("initial process start = %+v", startResponse)
		}

		restartDone := make(chan wireResponse, 1)
		go func() {
			response, _ := server.dispatch(wireRequest{Op: string(protocol.OpRestart), Cwd: root, Name: "race"})
			restartDone <- response
		}()
		select {
		case <-restartEntered:
		case <-time.After(time.Second):
			t.Fatal("restart did not reach the relaunch admission barrier")
		}

		shutdownDone := make(chan wireResponse, 1)
		go func() {
			response, _ := server.dispatch(wireRequest{Op: string(protocol.OpShutdown), Force: false})
			shutdownDone <- response
		}()
		releaseOnce.Do(func() { close(releaseRestart) })

		var restartResponse, shutdownResponse wireResponse
		select {
		case restartResponse = <-restartDone:
		case <-time.After(time.Second):
			t.Fatal("restart remained blocked after relaunch barrier release")
		}
		select {
		case shutdownResponse = <-shutdownDone:
		case <-time.After(time.Second):
			t.Fatal("non-forced shutdown remained blocked after restart admission")
		}
		if restartResponse.Error != nil || restartResponse.Process == nil || restartResponse.Process.RestartCount != 1 {
			t.Fatalf("restart response during concurrent shutdown = %+v", restartResponse)
		}
		if shutdownResponse.Error == nil || shutdownResponse.Error.Code != string(protocol.ErrorActiveProcesses) {
			t.Fatalf("restart admitted without shutdown refusal: shutdown response = %+v", shutdownResponse)
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

func TestRemoveAndShutdown(t *testing.T) {
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

	t.Run("v3 client to v2 daemon rejects wait during hello but permits frozen shutdown", func(t *testing.T) {
		const oldDaemonVersion = 2
		server := testServer(t, Config{Version: strconv.Itoa(oldDaemonVersion)})
		conn, err := net.Dial("unix", server.Paths().Socket)
		if err != nil {
			t.Fatal(err)
		}
		recordingConn := &daemonTestWriteConn{Conn: conn}
		client := NewClient(recordingConn)
		t.Cleanup(func() { _ = client.Close() })

		helloCtx, cancelHello := context.WithTimeout(context.Background(), time.Second)
		err = client.Hello(helloCtx)
		cancelHello()
		var mismatch *VersionMismatchError
		if !errors.As(err, &mismatch) || mismatch == nil {
			t.Fatalf("old-daemon hello error = %v, want version mismatch", err)
		}
		if mismatch.ClientVersion != protocol.Version || mismatch.DaemonVersion != oldDaemonVersion {
			t.Fatalf("old-daemon mismatch versions = client %d daemon %d, want client %d daemon %d", mismatch.ClientVersion, mismatch.DaemonVersion, protocol.Version, oldDaemonVersion)
		}

		helloWrites := recordingConn.writeCalls.Load()
		waitCtx, cancelWait := context.WithTimeout(context.Background(), time.Second)
		_, waitErr := client.Wait(waitCtx, daemonWaitRequest("wait", t.TempDir(), "", time.Second))
		cancelWait()
		var waitMismatch *VersionMismatchError
		if !errors.As(waitErr, &waitMismatch) || waitMismatch == nil {
			t.Fatalf("wait after old-daemon hello mismatch = %v, want version mismatch", waitErr)
		}
		if got := recordingConn.writeCalls.Load(); got != helloWrites {
			t.Fatalf("wait after old-daemon hello mismatch made additional writes: got %d, want %d", got, helloWrites)
		}

		if err := client.Shutdown(context.Background(), protocol.NewShutdownRequest(false)); err != nil {
			t.Fatalf("frozen shutdown after version mismatch: %v", err)
		}
		if got := recordingConn.writeCalls.Load(); got != helloWrites+1 {
			t.Fatalf("shutdown after wait rejection writes = %d, want %d", got, helloWrites+1)
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

func daemonWaitFixture(t *testing.T) (*Server, *Client, string, *output.Store) {
	t.Helper()
	root := t.TempDir()
	var store *output.Store
	child := &daemonTestChild{pid: 9301, done: make(chan struct{})}
	supervisor, err := app.New(app.Options{
		StartProcess: func(spec process.Spec) (app.Child, error) {
			store = spec.Output
			return child, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := testServer(t, Config{Supervisor: supervisor})
	client, err := Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := supervisor.Start(app.StartRequest{Name: "wait", Cwd: root, Argv: []string{"fake"}}); err != nil {
		t.Fatal(err)
	}
	if store == nil {
		t.Fatal("wait fixture did not expose output store")
	}
	return server, client, root, store
}

func daemonWaitRequest(name, cwd, match string, timeout time.Duration) protocol.WaitRequest {
	return protocol.WaitRequest{Op: protocol.OpWait, Name: name, Cwd: cwd, Match: match, TimeoutMS: int64(timeout / time.Millisecond)}
}

func TestWaitRequestConversion(t *testing.T) {
	after := protocol.Cursor(0)
	request := protocol.Request{
		Op: protocol.OpWait,
		Wait: &protocol.WaitRequest{
			Op: protocol.OpWait, Name: "wait", Cwd: "/work/project",
			After: &after, Match: "ready", TimeoutMS: 1234,
		},
	}
	wire, err := wireRequestFromProtocol(request)
	if err != nil {
		t.Fatal(err)
	}
	if wire.Op != string(protocol.OpWait) || wire.Name != "wait" || wire.Cwd != "/work/project" || wire.Match != "ready" || wire.TimeoutMS != 1234 || wire.After == nil || *wire.After != 0 {
		t.Fatalf("wire wait request = %#v", wire)
	}
	var sink strings.Builder
	if err := writeProtocolRequest(protocol.NewEncoder(&sink), wire); err != nil {
		t.Fatal(err)
	}
	decoded, err := protocol.NewDecoder(strings.NewReader(sink.String())).DecodeRequest()
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Wait == nil || decoded.Wait.After == nil || *decoded.Wait.After != 0 || decoded.Wait.TimeoutMS != 1234 || decoded.Wait.Match != "ready" {
		t.Fatalf("decoded wait request = %#v", decoded.Wait)
	}
}

func TestWaitResponseShape(t *testing.T) {
	exitTime := time.Unix(3, 0)
	cases := []app.WaitResult{
		{Outcome: app.WaitMatched, Cursor: 0},
		{Outcome: app.WaitExited, Cursor: 4, Exit: &process.Result{ExitCode: 7, Err: errors.New("boom"), ExitedAt: exitTime}},
		{Outcome: app.WaitTimedOut, Cursor: 9},
	}
	for _, result := range cases {
		t.Run(string(result.Outcome), func(t *testing.T) {
			var sink strings.Builder
			if err := writeProtocolResponse(protocol.NewEncoder(&sink), wireResponseFromWait(result)); err != nil {
				t.Fatalf("encode wait response: %v", err)
			}
			encoded := sink.String()
			if !strings.Contains(encoded, `"op":"wait"`) || !strings.Contains(encoded, `"ok":true`) {
				t.Fatalf("wait response envelope = %s", encoded)
			}
			if !strings.Contains(encoded, `"outcome":"`+string(result.Outcome)+`"`) {
				t.Fatalf("wait response outcome = %s", encoded)
			}
			if !strings.Contains(encoded, `"cursor":`+strconv.FormatUint(uint64(result.Cursor), 10)) {
				t.Fatalf("wait response cursor = %s", encoded)
			}
			var decoded protocol.WaitResponse
			if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
				t.Fatalf("decode wait response: %v", err)
			}
			if decoded.Op != protocol.OpWait || decoded.Outcome != protocol.WaitOutcome(result.Outcome) || decoded.Cursor != protocol.Cursor(result.Cursor) {
				t.Fatalf("decoded wait response = %#v", decoded)
			}
		})
	}
}

func TestWaitDaemonBridge(t *testing.T) {
	t.Run("buffered match", func(t *testing.T) {
		_, client, root, store := daemonWaitFixture(t)
		cursor, err := store.Append(output.Stdout, time.Unix(1, 0), "ready\n")
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.Wait(context.Background(), daemonWaitRequest("wait", root, "ready", time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != app.WaitMatched || result.Cursor != cursor {
			t.Fatalf("buffered wait result = %#v, want matched at %d", result, cursor)
		}
	})

	t.Run("new matching output", func(t *testing.T) {
		_, client, root, store := daemonWaitFixture(t)
		type waitCall struct {
			result app.WaitResult
			err    error
		}
		done := make(chan waitCall, 1)
		go func() {
			result, err := client.Wait(context.Background(), daemonWaitRequest("wait", root, "later", time.Second))
			done <- waitCall{result: result, err: err}
		}()
		time.Sleep(10 * time.Millisecond)
		cursor, err := store.Append(output.Stdout, time.Unix(1, 0), "later\n")
		if err != nil {
			t.Fatal(err)
		}
		select {
		case call := <-done:
			if call.err != nil {
				t.Fatal(call.err)
			}
			if call.result.Outcome != app.WaitMatched || call.result.Cursor != cursor {
				t.Fatalf("new-output wait result = %#v, want matched at %d", call.result, cursor)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("new-output wait did not return")
		}
	})

	t.Run("exit before match", func(t *testing.T) {
		_, client, root, store := daemonWaitFixture(t)
		type waitCall struct {
			result app.WaitResult
			err    error
		}
		done := make(chan waitCall, 1)
		go func() {
			result, err := client.Wait(context.Background(), daemonWaitRequest("wait", root, "ready", time.Second))
			done <- waitCall{result: result, err: err}
		}()
		time.Sleep(10 * time.Millisecond)
		exitTime := time.Unix(2, 0)
		store.NotifyExit(output.Exit{Code: 7, Time: exitTime})
		select {
		case call := <-done:
			if call.err != nil {
				t.Fatal(call.err)
			}
			if call.result.Outcome != app.WaitExited || call.result.Exit == nil || call.result.Exit.ExitCode != 7 {
				t.Fatalf("exit wait result = %#v, want exited code 7", call.result)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("exit wait did not return")
		}
	})

	t.Run("exit without match", func(t *testing.T) {
		_, client, root, store := daemonWaitFixture(t)
		store.NotifyExit(output.Exit{Code: 0, Time: time.Now()})
		result, err := client.Wait(context.Background(), daemonWaitRequest("wait", root, "", time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != app.WaitExited || result.Exit == nil || result.Exit.ExitCode != 0 {
			t.Fatalf("exit-without-match wait result = %#v, want exited code 0", result)
		}
	})

	t.Run("timeout returns consumed cursor", func(t *testing.T) {
		_, client, root, _ := daemonWaitFixture(t)
		result, err := client.Wait(context.Background(), daemonWaitRequest("wait", root, "never", 25*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != app.WaitTimedOut || result.Cursor != 0 {
			t.Fatalf("timeout wait result = %#v, want timed_out at cursor 0", result)
		}
	})

	t.Run("invalid request and server bound", func(t *testing.T) {
		_, client, root, _ := daemonWaitFixture(t)
		tooLarge := protocol.WaitRequest{Op: protocol.OpWait, Name: "wait", Cwd: root, TimeoutMS: maxWaitTimeoutMS + 1}
		cases := []protocol.WaitRequest{
			{Op: protocol.OpWait, Cwd: root, TimeoutMS: 1000},
			daemonWaitRequest("wait", root, "[", time.Second),
			daemonWaitRequest("wait", root, "", 0),
			daemonWaitRequest("wait", root, "", -time.Millisecond),
			tooLarge,
		}
		for i, request := range cases {
			_, err := client.Wait(context.Background(), request)
			var wireErr *protocol.WireError
			if !errors.As(err, &wireErr) || wireErr.Code != protocol.ErrorInvalidRequest {
				t.Errorf("case %d wait error = %v, want invalid_request", i, err)
			}
		}
	})

	t.Run("future cursor is rejected by output", func(t *testing.T) {
		_, client, root, _ := daemonWaitFixture(t)
		after := protocol.Cursor(99)
		request := daemonWaitRequest("wait", root, "", time.Second)
		request.After = &after
		_, err := client.Wait(context.Background(), request)
		var wireErr *protocol.WireError
		if !errors.As(err, &wireErr) || wireErr.Code != protocol.ErrorOutput {
			t.Fatalf("future cursor wait error = %v, want output_error", err)
		}
	})

	t.Run("disconnect releases waiter", func(t *testing.T) {
		_, client, root, store := daemonWaitFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := client.Wait(ctx, daemonWaitRequest("wait", root, "reconnect", 10*time.Second))
			done <- err
		}()
		time.Sleep(20 * time.Millisecond)
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("disconnected wait error = %v, want context canceled", err)
			}
		case <-time.After(time.Second):
			t.Fatal("disconnected wait did not return")
		}
		if _, err := store.Append(output.Stdout, time.Unix(1, 0), "reconnect\n"); err != nil {
			t.Fatal(err)
		}
		result, err := client.Wait(context.Background(), daemonWaitRequest("wait", root, "reconnect", time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != app.WaitMatched {
			t.Fatalf("post-disconnect wait result = %#v, want matched", result)
		}
	})

	t.Run("concurrent waiters", func(t *testing.T) {
		_, client, root, store := daemonWaitFixture(t)
		type waitCall struct {
			result app.WaitResult
			err    error
		}
		done := make(chan waitCall, 2)
		for _, match := range []string{"one", "two"} {
			go func(match string) {
				result, err := client.Wait(context.Background(), daemonWaitRequest("wait", root, match, time.Second))
				done <- waitCall{result: result, err: err}
			}(match)
		}
		time.Sleep(10 * time.Millisecond)
		if _, err := store.Append(output.Stdout, time.Unix(1, 0), "one\n"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Append(output.Stdout, time.Unix(1, 0), "two\n"); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			select {
			case call := <-done:
				if call.err != nil {
					t.Fatal(call.err)
				}
				if call.result.Outcome != app.WaitMatched {
					t.Fatalf("concurrent wait result = %#v, want matched", call.result)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("concurrent waiter did not return")
			}
		}
	})

	t.Run("coexists with follower", func(t *testing.T) {
		_, client, root, store := daemonWaitFixture(t)
		follower, err := client.Follow(context.Background(), protocol.NewFollowRequest("wait", root))
		if err != nil {
			t.Fatal(err)
		}
		defer follower.Close()
		followerDone := make(chan struct {
			event output.Event
			err   error
		}, 1)
		go func() {
			event, err := follower.Next(context.Background())
			followerDone <- struct {
				event output.Event
				err   error
			}{event: event, err: err}
		}()
		waitDone := make(chan struct {
			result app.WaitResult
			err    error
		}, 1)
		go func() {
			result, err := client.Wait(context.Background(), daemonWaitRequest("wait", root, "shared", time.Second))
			waitDone <- struct {
				result app.WaitResult
				err    error
			}{result: result, err: err}
		}()
		time.Sleep(10 * time.Millisecond)
		cursor, err := store.Append(output.Stdout, time.Unix(1, 0), "shared\n")
		if err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-waitDone:
			if got.err != nil || got.result.Outcome != app.WaitMatched || got.result.Cursor != cursor {
				t.Fatalf("wait alongside follower = %#v, err %v", got.result, got.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("wait alongside follower did not return")
		}
		select {
		case got := <-followerDone:
			if got.err != nil || got.event.Read == nil || len(got.event.Read.Entries) != 1 || got.event.Read.Entries[0].Text != "shared\n" {
				t.Fatalf("follower alongside wait = %#v, err %v", got.event, got.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("follower alongside wait did not return")
		}
	})
}
