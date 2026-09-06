package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hum/internal/protocol"
)

type serverTestResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type serverTestSession struct {
	input     *io.PipeWriter
	output    *io.PipeReader
	responses chan serverTestResponse
	readErr   chan error

	serveDone chan struct{}
	serveMu   sync.Mutex
	serveErr  error

	pendingMu sync.Mutex
	pending   map[string]serverTestResponse
}

func newServerTestSession(t *testing.T, server *Server, ctx context.Context) *serverTestSession {
	t.Helper()
	inputReader, input := io.Pipe()
	output, outputWriter := io.Pipe()
	session := &serverTestSession{
		input:     input,
		output:    output,
		responses: make(chan serverTestResponse, 256),
		readErr:   make(chan error, 1),
		serveDone: make(chan struct{}),
		pending:   make(map[string]serverTestResponse),
	}
	go func() {
		decoder := json.NewDecoder(output)
		for {
			var response serverTestResponse
			if err := decoder.Decode(&response); err != nil {
				session.readErr <- err
				return
			}
			session.responses <- response
		}
	}()
	go func() {
		err := server.Serve(ctx, inputReader, outputWriter)
		session.serveMu.Lock()
		session.serveErr = err
		session.serveMu.Unlock()
		close(session.serveDone)
	}()
	t.Cleanup(func() {
		_ = input.Close()
		_ = output.Close()
		select {
		case <-session.serveDone:
		case <-time.After(3 * time.Second):
			t.Errorf("MCP server did not stop during cleanup")
		}
	})
	return session
}

func (s *serverTestSession) send(t *testing.T, id any, method string, params any) {
	t.Helper()
	request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		request["params"] = params
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(s.input, string(encoded)); err != nil {
		t.Fatalf("send %s: %v", method, err)
	}
}

func (s *serverTestSession) sendNotification(t *testing.T, method string, params any) {
	t.Helper()
	request := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		request["params"] = params
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(s.input, string(encoded)); err != nil {
		t.Fatalf("send notification %s: %v", method, err)
	}
}

func (s *serverTestSession) response(t *testing.T, id string, timeout time.Duration) serverTestResponse {
	t.Helper()
	s.pendingMu.Lock()
	if response, ok := s.pending[id]; ok {
		delete(s.pending, id)
		s.pendingMu.Unlock()
		return response
	}
	s.pendingMu.Unlock()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case response := <-s.responses:
			responseID := string(response.ID)
			if responseID == id || responseID == `"`+id+`"` {
				return response
			}
			s.pendingMu.Lock()
			s.pending[responseID] = response
			s.pendingMu.Unlock()
		case err := <-s.readErr:
			t.Fatalf("read response %s: %v", id, err)
		case <-deadline.C:
			t.Fatalf("timed out waiting for response %s", id)
		}
	}
}

func (s *serverTestSession) closeInput() {
	_ = s.input.Close()
}

func (s *serverTestSession) wait(t *testing.T, timeout time.Duration) error {
	t.Helper()
	select {
	case <-s.serveDone:
		s.serveMu.Lock()
		defer s.serveMu.Unlock()
		return s.serveErr
	case <-time.After(timeout):
		t.Fatalf("MCP server did not stop within %s", timeout)
		return nil
	}
}

type serverTestClient struct {
	*fakeClient
	waitStarted chan string
	waitRelease chan struct{}
}

func (c *serverTestClient) Wait(ctx context.Context, request protocol.WaitRequest) (protocol.WaitResponse, error) {
	if c.waitStarted != nil {
		select {
		case c.waitStarted <- request.Name:
		default:
		}
	}
	if c.waitRelease != nil {
		select {
		case <-c.waitRelease:
		case <-ctx.Done():
			return protocol.WaitResponse{}, ctx.Err()
		}
	}
	return protocol.NewWaitResponse(protocol.WaitMatched, 9, nil), nil
}

func newConcurrencyServer(t *testing.T, client *serverTestClient, factory func(context.Context, bool) (Client, error), definitions []Definition) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	if client.fakeClient == nil {
		client.fakeClient = &fakeClient{}
	}
	if client.processes == nil {
		client.processes = map[string]protocol.Process{}
	}
	if client.startErr == nil {
		client.startErr = map[string]error{}
	}
	if client.stopErr == nil {
		client.stopErr = map[string]error{}
	}
	return NewServer(Options{
		Resolver:      fakeResolver{resolution: Resolution{Root: root, Definitions: definitions}},
		ClientFactory: factory,
		Environment:   func() []string { return nil },
		Version:       "test",
	}), root
}

func toolsCallParams(root, name string, fields map[string]any) map[string]any {
	arguments := map[string]any{"project_root": root}
	for key, value := range fields {
		arguments[key] = value
	}
	return map[string]any{"name": name, "arguments": arguments}
}

func waitForName(t *testing.T, started <-chan string, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case got := <-started:
			if got == want {
				return
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s to start", want)
		}
	}
}

func TestConcurrentRequests(t *testing.T) {
	client := &serverTestClient{
		fakeClient: &fakeClient{processes: map[string]protocol.Process{
			"slow":   {Name: "slow", State: "running", Root: "/unused", Cwd: "/unused"},
			"status": {Name: "status", State: "running", Root: "/unused", Cwd: "/unused"},
		}},
		waitStarted: make(chan string, 8),
		waitRelease: make(chan struct{}),
	}
	upStarted := make(chan struct{})
	upRelease := make(chan struct{})
	var upOnce sync.Once
	server, root := newConcurrencyServer(t, client, func(ctx context.Context, ensure bool) (Client, error) {
		if ensure {
			upOnce.Do(func() { close(upStarted) })
			select {
			case <-upRelease:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return client, nil
	}, []Definition{{Name: "blocked", Source: "manifest", Argv: []string{"blocked"}, Ready: &protocol.ReadinessConfig{Match: "ready"}}})
	session := newServerTestSession(t, server, context.Background())
	session.send(t, "slow", "tools/call", toolsCallParams(root, "wait", map[string]any{"name": "slow"}))
	waitForName(t, client.waitStarted, "slow", time.Second)
	session.send(t, "up", "tools/call", toolsCallParams(root, "up", nil))
	select {
	case <-upStarted:
	case <-time.After(time.Second):
		t.Fatal("up did not enter its blocked operation")
	}

	started := time.Now()
	session.send(t, "ping", "ping", nil)
	session.send(t, "logs", "tools/call", toolsCallParams(root, "logs", map[string]any{"name": "slow"}))
	session.send(t, "status", "tools/call", toolsCallParams(root, "status", map[string]any{"name": "status"}))
	session.send(t, "list", "tools/call", toolsCallParams(root, "list", nil))
	for _, id := range []string{`"ping"`, `"logs"`, `"status"`, `"list"`} {
		session.response(t, id, time.Second)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("liveness requests took %s while wait/up were blocked", elapsed)
	}

	close(client.waitRelease)
	close(upRelease)
	session.closeInput()
	if err := session.wait(t, 2*time.Second); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

type cancellationClient struct {
	*fakeClient
	started map[string]chan struct{}
	release map[string]chan struct{}
	once    map[string]*sync.Once
}

func (c *cancellationClient) Wait(ctx context.Context, request protocol.WaitRequest) (protocol.WaitResponse, error) {
	if once := c.once[request.Name]; once != nil {
		once.Do(func() { close(c.started[request.Name]) })
	}
	select {
	case <-c.release[request.Name]:
		return protocol.NewWaitResponse(protocol.WaitMatched, 9, nil), nil
	case <-ctx.Done():
		return protocol.WaitResponse{}, ctx.Err()
	}
}

type queuedCancellationClient struct {
	*fakeClient
	completed chan struct{}
	once      sync.Once
}

func (c *queuedCancellationClient) Wait(context.Context, protocol.WaitRequest) (protocol.WaitResponse, error) {
	c.once.Do(func() { close(c.completed) })
	return protocol.NewWaitResponse(protocol.WaitMatched, 9, nil), nil
}

func TestRequestCancellation(t *testing.T) {
	client := &cancellationClient{
		fakeClient: &fakeClient{processes: map[string]protocol.Process{
			"cancel": {Name: "cancel", State: "running"},
			"keep":   {Name: "keep", State: "running"},
		}},
		started: map[string]chan struct{}{"cancel": make(chan struct{}), "keep": make(chan struct{})},
		release: map[string]chan struct{}{"cancel": make(chan struct{}), "keep": make(chan struct{})},
		once:    map[string]*sync.Once{"cancel": {}, "keep": {}},
	}
	server, root := newConcurrencyServer(t, &serverTestClient{fakeClient: client.fakeClient}, func(context.Context, bool) (Client, error) {
		return client, nil
	}, nil)
	session := newServerTestSession(t, server, context.Background())
	session.send(t, "cancel", "tools/call", toolsCallParams(root, "wait", map[string]any{"name": "cancel"}))
	session.send(t, "keep", "tools/call", toolsCallParams(root, "wait", map[string]any{"name": "keep"}))
	for _, name := range []string{"cancel", "keep"} {
		select {
		case <-client.started[name]:
		case <-time.After(time.Second):
			t.Fatalf("wait %s did not start", name)
		}
	}
	session.sendNotification(t, "notifications/cancelled", map[string]any{"requestId": "unknown"})
	session.sendNotification(t, "notifications/cancelled", map[string]any{"requestId": "cancel"})
	cancelled := session.response(t, `"cancel"`, time.Second)
	if cancelled.Error == nil || cancelled.Error.Code != -32800 {
		t.Fatalf("cancel response = %#v, want JSON-RPC cancellation", cancelled)
	}
	select {
	case response := <-session.responses:
		t.Fatalf("unrelated request completed after cancellation: %#v", response)
	case <-time.After(100 * time.Millisecond):
	}
	close(client.release["keep"])
	kept := session.response(t, `"keep"`, time.Second)
	if kept.Error != nil {
		t.Fatalf("unrelated request error = %#v", kept.Error)
	}
	session.closeInput()
	if err := session.wait(t, 2*time.Second); err != nil {
		t.Fatalf("serve: %v", err)
	}

	t.Run("queued response observes cancellation", func(t *testing.T) {
		queuedClient := &queuedCancellationClient{
			fakeClient: &fakeClient{processes: map[string]protocol.Process{"queued": {Name: "queued", State: "running"}}},
			completed:  make(chan struct{}),
		}
		queuedServer, queuedRoot := newConcurrencyServer(t, &serverTestClient{fakeClient: queuedClient.fakeClient}, func(context.Context, bool) (Client, error) {
			return queuedClient, nil
		}, nil)
		inputReader, input := io.Pipe()
		writer := newGatedResponseWriter()
		done := make(chan error, 1)
		go func() { done <- queuedServer.Serve(context.Background(), inputReader, writer) }()
		t.Cleanup(func() {
			_ = input.Close()
			_ = writer.Close()
		})

		send := func(request map[string]any) {
			t.Helper()
			encoded, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fmt.Fprintln(input, string(encoded)); err != nil {
				t.Fatal(err)
			}
		}
		send(map[string]any{"jsonrpc": "2.0", "id": "blocker", "method": "ping"})
		select {
		case <-writer.started:
		case <-time.After(time.Second):
			t.Fatal("blocker response did not reach writer")
		}
		send(map[string]any{
			"jsonrpc": "2.0",
			"id":      "queued",
			"method":  "tools/call",
			"params":  toolsCallParams(queuedRoot, "wait", map[string]any{"name": "queued"}),
		})
		select {
		case <-queuedClient.completed:
		case <-time.After(time.Second):
			t.Fatal("queued request did not complete its handler")
		}
		time.Sleep(10 * time.Millisecond)
		send(map[string]any{
			"jsonrpc": "2.0",
			"method":  "notifications/cancelled",
			"params":  map[string]any{"requestId": "queued"},
		})
		time.Sleep(10 * time.Millisecond)
		close(writer.release)

		var queued serverTestResponse
		for range 2 {
			select {
			case line := <-writer.lines:
				var response serverTestResponse
				if err := json.Unmarshal(line, &response); err != nil {
					t.Fatal(err)
				}
				if string(response.ID) == `"queued"` {
					queued = response
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for queued response")
			}
		}
		if queued.Error == nil || queued.Error.Code != -32800 {
			t.Fatalf("queued cancellation response = %#v, want -32800", queued)
		}
		_ = input.Close()
		if err := <-done; err != nil {
			t.Fatalf("serve: %v", err)
		}
	})
}

type capacityClient struct {
	*fakeClient
	started atomic.Int64
	release chan struct{}
	hold    atomic.Bool
	holdNow chan struct{}
}

func (c *capacityClient) Wait(ctx context.Context, request protocol.WaitRequest) (protocol.WaitResponse, error) {
	c.started.Add(1)
	release := c.release
	if c.hold.Load() {
		release = c.holdNow
	}
	select {
	case <-release:
		return protocol.NewWaitResponse(protocol.WaitMatched, 9, nil), nil
	case <-ctx.Done():
		return protocol.WaitResponse{}, ctx.Err()
	}
}

type gatedResponseWriter struct {
	started   chan struct{}
	startOnce sync.Once
	release   chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	lines     chan []byte
}

func newGatedResponseWriter() *gatedResponseWriter {
	return &gatedResponseWriter{
		started: make(chan struct{}),
		release: make(chan struct{}),
		closed:  make(chan struct{}),
		lines:   make(chan []byte, 256),
	}
}

func (w *gatedResponseWriter) Write(data []byte) (int, error) {
	w.startOnce.Do(func() { close(w.started) })
	select {
	case <-w.release:
		w.lines <- append([]byte(nil), data...)
		return len(data), nil
	case <-w.closed:
		return 0, errors.New("response writer closed")
	}
}

func (w *gatedResponseWriter) Close() error {
	w.closeOnce.Do(func() { close(w.closed) })
	return nil
}

func TestRequestCapacityAndIDs(t *testing.T) {
	runGated := func(t *testing.T, requestIDs []string) []serverTestResponse {
		t.Helper()
		server := NewServer(Options{})
		inputReader, input := io.Pipe()
		writer := newGatedResponseWriter()
		done := make(chan error, 1)
		go func() { done <- server.Serve(context.Background(), inputReader, writer) }()
		t.Cleanup(func() {
			_ = input.Close()
			_ = writer.Close()
		})

		writeRequest := func(id string) {
			t.Helper()
			if _, err := fmt.Fprintf(input, `{"jsonrpc":"2.0","id":%q,"method":"ping"}`+"\n", id); err != nil {
				t.Fatalf("send %s: %v", id, err)
			}
		}
		writeRequest(requestIDs[0])
		select {
		case <-writer.started:
		case <-time.After(time.Second):
			t.Fatal("first response did not reach writer")
		}
		for _, id := range requestIDs[1:] {
			writeRequest(id)
		}
		// Let Serve admit every request and block on the terminal admission
		// response before allowing any completed request to release its slot.
		time.Sleep(50 * time.Millisecond)
		close(writer.release)

		responses := make([]serverTestResponse, 0, len(requestIDs))
		for range requestIDs {
			select {
			case line := <-writer.lines:
				var response serverTestResponse
				if err := json.Unmarshal(line, &response); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				responses = append(responses, response)
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for gated responses")
			}
		}
		_ = input.Close()
		if err := <-done; err != nil {
			t.Fatalf("serve: %v", err)
		}
		return responses
	}

	t.Run("ID remains reserved until response is written", func(t *testing.T) {
		responses := runGated(t, []string{"held", "held"})
		var duplicateCode int
		for _, response := range responses {
			if response.Error != nil {
				duplicateCode = response.Error.Code
			}
		}
		if duplicateCode != -32600 {
			t.Fatalf("duplicate code = %d, want -32600", duplicateCode)
		}
	})

	t.Run("slot remains reserved until response is written", func(t *testing.T) {
		requestIDs := make([]string, 0, maxInFlightRequests+1)
		for index := 0; index < maxInFlightRequests; index++ {
			requestIDs = append(requestIDs, fmt.Sprintf("slot-%d", index))
		}
		requestIDs = append(requestIDs, "overflow")
		responses := runGated(t, requestIDs)
		var overloadCode int
		for _, response := range responses {
			if string(response.ID) == `"overflow"` && response.Error != nil {
				overloadCode = response.Error.Code
			}
		}
		if overloadCode != -32001 {
			t.Fatalf("overload code = %d, want -32001", overloadCode)
		}
	})

	client := &capacityClient{
		fakeClient: &fakeClient{processes: map[string]protocol.Process{}},
		release:    make(chan struct{}),
		holdNow:    make(chan struct{}),
	}
	server, root := newConcurrencyServer(t, &serverTestClient{fakeClient: client.fakeClient}, func(context.Context, bool) (Client, error) {
		return client, nil
	}, nil)
	session := newServerTestSession(t, server, context.Background())
	params := toolsCallParams(root, "wait", map[string]any{"name": "held"})
	for index := 0; index < maxInFlightRequests; index++ {
		session.send(t, fmt.Sprintf("request-%d", index), "tools/call", params)
	}
	deadline := time.NewTimer(time.Second)
	for client.started.Load() < maxInFlightRequests {
		select {
		case <-deadline.C:
			t.Fatal("64 requests did not become in flight")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	deadline.Stop()

	session.send(t, "request-0", "ping", nil)
	duplicate := session.response(t, `"request-0"`, time.Second)
	if duplicate.Error == nil || duplicate.Error.Code != -32600 {
		t.Fatalf("duplicate response = %#v, want -32600", duplicate)
	}
	session.send(t, "request-65", "ping", nil)
	overloaded := session.response(t, `"request-65"`, time.Second)
	if overloaded.Error == nil || overloaded.Error.Code != -32001 {
		t.Fatalf("overload response = %#v, want -32001", overloaded)
	}
	session.sendNotification(t, "notifications/initialized", nil)
	if _, err := fmt.Fprintln(session.input, `{"jsonrpc":"2.0","id":"incoming-response","result":{}}`); err != nil {
		t.Fatal(err)
	}
	incoming := session.response(t, `"incoming-response"`, time.Second)
	if incoming.Error == nil || incoming.Error.Code != -32600 {
		t.Fatalf("incoming response handling = %#v, want invalid request without a slot", incoming)
	}

	close(client.release)
	for index := 0; index < maxInFlightRequests; index++ {
		session.response(t, fmt.Sprintf(`"request-%d"`, index), time.Second)
	}
	client.hold.Store(true)
	session.send(t, "reusable", "tools/call", params)
	for client.started.Load() < maxInFlightRequests+1 {
		time.Sleep(time.Millisecond)
	}
	session.sendNotification(t, "notifications/cancelled", map[string]any{"requestId": "reusable"})
	cancelled := session.response(t, `"reusable"`, time.Second)
	if cancelled.Error == nil || cancelled.Error.Code != -32800 {
		t.Fatalf("cancelled reusable response = %#v", cancelled)
	}
	client.hold.Store(false)
	// The output reader may observe bytes just before io.Pipe.Write returns and
	// the response callback releases the registry entry.
	time.Sleep(10 * time.Millisecond)
	session.send(t, "reusable", "ping", nil)
	reused := session.response(t, `"reusable"`, time.Second)
	if reused.Error != nil {
		t.Fatalf("released ID could not be reused: %#v", reused.Error)
	}
	session.closeInput()
	if err := session.wait(t, 2*time.Second); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

type shutdownWriter struct {
	closed    chan struct{}
	closeOnce sync.Once
	started   chan struct{}
	startOnce sync.Once
}

func newShutdownWriter() *shutdownWriter {
	return &shutdownWriter{closed: make(chan struct{}), started: make(chan struct{})}
}

func (w *shutdownWriter) Write([]byte) (int, error) {
	w.startOnce.Do(func() { close(w.started) })
	<-w.closed
	return 0, errors.New("response writer closed")
}

func (w *shutdownWriter) Close() error {
	w.closeOnce.Do(func() { close(w.closed) })
	return nil
}

type frameShutdownWriter struct {
	closed    chan struct{}
	closeOnce sync.Once
	started   chan struct{}
	startOnce sync.Once

	mu     sync.Mutex
	writes int
	output bytes.Buffer
}

func newFrameShutdownWriter() *frameShutdownWriter {
	return &frameShutdownWriter{closed: make(chan struct{}), started: make(chan struct{})}
}

func (w *frameShutdownWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	w.writes++
	if w.writes == 1 {
		written, err := w.output.Write(data)
		w.mu.Unlock()
		return written, err
	}
	w.mu.Unlock()
	w.startOnce.Do(func() { close(w.started) })
	<-w.closed
	return 0, errors.New("response writer closed")
}

func (w *frameShutdownWriter) Close() error {
	w.closeOnce.Do(func() { close(w.closed) })
	return nil
}

func (w *frameShutdownWriter) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.output.Bytes()...)
}

type shutdownClient struct {
	*fakeClient
	started chan struct{}
	once    sync.Once
}

func (c *shutdownClient) Wait(ctx context.Context, _ protocol.WaitRequest) (protocol.WaitResponse, error) {
	c.once.Do(func() { close(c.started) })
	<-ctx.Done()
	return protocol.WaitResponse{}, ctx.Err()
}

func TestConcurrentServerShutdown(t *testing.T) {
	t.Run("EOF interrupts blocked admission response", func(t *testing.T) {
		writer := newShutdownWriter()
		server := NewServer(Options{})
		inputReader, input := io.Pipe()
		done := make(chan error, 1)
		go func() { done <- server.Serve(context.Background(), inputReader, writer) }()
		if _, err := io.WriteString(input, "{\n"); err != nil {
			t.Fatal(err)
		}
		select {
		case <-writer.started:
		case <-time.After(time.Second):
			t.Fatal("parse-error response writer was not blocked")
		}
		_ = input.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Serve on EOF = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Serve did not interrupt a blocked admission response")
		}
	})

	t.Run("EOF closes blocked writer", func(t *testing.T) {
		writer := newShutdownWriter()
		server := NewServer(Options{})
		input := strings.NewReader(`{"jsonrpc":"2.0","id":"ping","method":"ping"}` + "\n")
		done := make(chan error, 1)
		go func() { done <- server.Serve(context.Background(), input, writer) }()
		select {
		case <-writer.started:
		case <-time.After(time.Second):
			t.Fatal("response writer was not blocked")
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Serve on EOF = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Serve did not close a blocked response writer")
		}
	})

	t.Run("shutdown leaves only complete response frames", func(t *testing.T) {
		writer := newFrameShutdownWriter()
		server := NewServer(Options{})
		inputReader, input := io.Pipe()
		done := make(chan error, 1)
		go func() { done <- server.Serve(context.Background(), inputReader, writer) }()
		for _, id := range []string{"first", "second"} {
			if _, err := fmt.Fprintf(input, `{"jsonrpc":"2.0","id":%q,"method":"ping"}`+"\n", id); err != nil {
				t.Fatal(err)
			}
		}
		select {
		case <-writer.started:
		case <-time.After(time.Second):
			t.Fatal("second response did not block")
		}
		_ = input.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Serve on EOF = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Serve did not close the blocked frame writer")
		}

		output := writer.bytes()
		if !bytes.HasSuffix(output, []byte{'\n'}) {
			t.Fatalf("response output ends with a partial frame: %q", output)
		}
		decoder := json.NewDecoder(bytes.NewReader(output))
		var response serverTestResponse
		if err := decoder.Decode(&response); err != nil {
			t.Fatalf("decode complete response: %v", err)
		}
		if err := decoder.Decode(&serverTestResponse{}); !errors.Is(err, io.EOF) {
			t.Fatalf("response output contains a partial or extra frame: %v", err)
		}
	})

	t.Run("parent cancellation joins handlers and writer", func(t *testing.T) {
		client := &shutdownClient{fakeClient: &fakeClient{processes: map[string]protocol.Process{"slow": {Name: "slow", State: "running"}}}, started: make(chan struct{})}
		root := t.TempDir()
		server := NewServer(Options{
			Resolver:      fakeResolver{resolution: Resolution{Root: root}},
			ClientFactory: func(context.Context, bool) (Client, error) { return client, nil },
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		inputReader, input := io.Pipe()
		writer := newShutdownWriter()
		done := make(chan error, 1)
		go func() { done <- server.Serve(ctx, inputReader, writer) }()
		request := `{"jsonrpc":"2.0","id":"slow","method":"tools/call","params":{"name":"wait","arguments":{"project_root":"` + root + `","name":"slow"}}}` + "\n"
		if _, err := io.WriteString(input, request); err != nil {
			t.Fatal(err)
		}
		select {
		case <-client.started:
		case <-time.After(time.Second):
			t.Fatal("slow handler did not start")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Serve parent cancellation = %v, want context canceled", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Serve did not join after parent cancellation")
		}
		_ = input.Close()
	})
}

func TestToolOutputSchemasAreObjects(t *testing.T) {
	for _, definition := range NewServer(Options{}).toolDefinitions() {
		if got := definition.OutputSchema["type"]; got != "object" {
			t.Errorf("%s output schema type = %#v, want object", definition.Name, got)
		}
	}

	for _, version := range supportedProtocolVersions {
		t.Run(version, func(t *testing.T) {
			client := &fakeClient{processes: map[string]protocol.Process{
				"existing": {Name: "existing", State: "running", LaunchCursor: 3},
				"tty":      {Name: "tty", State: "running", TTY: true, LaunchCursor: 4},
			}}
			server, root, _ := newTestServer(t, []Definition{{Name: "declared", Source: "manifest", Argv: []string{"declared"}, Cwd: "."}}, client)
			for name, process := range client.processes {
				process.Root, process.Cwd = root, root
				client.processes[name] = process
			}

			initializeParams, err := json.Marshal(map[string]any{"protocolVersion": version})
			if err != nil {
				t.Fatal(err)
			}
			initialized, rpcErr := server.handleRequest(context.Background(), rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`"initialize"`), Method: "initialize", Params: initializeParams})
			if rpcErr != nil {
				t.Fatalf("initialize: %#v", rpcErr)
			}
			initializeResult, ok := initialized.(map[string]any)
			if !ok || initializeResult["protocolVersion"] != version {
				t.Fatalf("initialize result = %#v, want version %s", initialized, version)
			}

			toolsValue, rpcErr := server.handleRequest(context.Background(), rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`"tools"`), Method: "tools/list"})
			if rpcErr != nil {
				t.Fatalf("tools/list: %#v", rpcErr)
			}
			toolsJSON, err := json.Marshal(toolsValue)
			if err != nil {
				t.Fatal(err)
			}
			var listing struct {
				Tools []struct {
					Name         string                     `json:"name"`
					OutputSchema map[string]json.RawMessage `json:"outputSchema"`
				} `json:"tools"`
			}
			if err := json.Unmarshal(toolsJSON, &listing); err != nil {
				t.Fatalf("decode tools/list: %v", err)
			}
			if len(listing.Tools) != len(server.toolDefinitions()) {
				t.Fatalf("tools/list returned %d tools, want %d", len(listing.Tools), len(server.toolDefinitions()))
			}
			for _, tool := range listing.Tools {
				var schemaType string
				if err := json.Unmarshal(tool.OutputSchema["type"], &schemaType); err != nil || schemaType != "object" {
					t.Errorf("%s advertised output schema type = %q, want object", tool.Name, schemaType)
				}
			}

			calls := []struct {
				name   string
				fields map[string]any
				key    string
			}{
				{name: "start", fields: map[string]any{"name": "declared", "no_wait": true}},
				{name: "up", fields: map[string]any{"no_wait": true}, key: "results"},
				{name: "down", key: "results"},
				{name: "list", key: "processes"},
				{name: "status", fields: map[string]any{"name": "existing"}},
				{name: "logs", fields: map[string]any{"name": "existing"}},
				{name: "wait", fields: map[string]any{"name": "existing"}},
				{name: "input", fields: map[string]any{"name": "tty", "text": "x"}},
				{name: "restart", fields: map[string]any{"name": "existing"}},
				{name: "stop", fields: map[string]any{"name": "existing"}},
				{name: "remove", fields: map[string]any{"name": "existing"}},
			}
			for _, call := range calls {
				params, err := json.Marshal(toolsCallParams(root, call.name, call.fields))
				if err != nil {
					t.Fatal(err)
				}
				value, rpcErr := server.handleRequest(context.Background(), rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`"call"`), Method: "tools/call", Params: params})
				if rpcErr != nil {
					t.Fatalf("%s RPC error: %#v", call.name, rpcErr)
				}
				result, ok := value.(callToolResult)
				if !ok {
					t.Fatalf("%s result type = %T, want callToolResult", call.name, value)
				}
				if result.IsError {
					t.Fatalf("%s returned tool error: %#v", call.name, result)
				}
				structured, err := json.Marshal(result.StructuredContent)
				if err != nil {
					t.Fatalf("%s structuredContent: %v", call.name, err)
				}
				var object map[string]json.RawMessage
				if err := json.Unmarshal(structured, &object); err != nil || object == nil {
					t.Fatalf("%s structuredContent = %s, want object", call.name, structured)
				}
				if call.key != "" {
					if _, ok := object[call.key]; !ok {
						t.Fatalf("%s structuredContent = %s, missing %q", call.name, structured, call.key)
					}
				}
			}
		})
	}
}

func TestCollectionToolTextUnchanged(t *testing.T) {
	for _, version := range supportedProtocolVersions {
		t.Run(version, func(t *testing.T) {
			for _, test := range []struct {
				name        string
				definitions []Definition
				key         string
			}{
				{name: "up", key: "results"},
				{name: "down", definitions: []Definition{{Name: "declared", Source: "manifest", Cwd: ".", Argv: []string{"declared"}}}, key: "results"},
				{name: "list", definitions: []Definition{{Name: "declared", Source: "manifest", Cwd: ".", Argv: []string{"declared"}}}, key: "processes"},
			} {
				t.Run(test.name, func(t *testing.T) {
					root := t.TempDir()
					server := NewServer(Options{
						Resolver: fakeResolver{resolution: Resolution{Root: root, Definitions: test.definitions}},
						ClientFactory: func(context.Context, bool) (Client, error) {
							return nil, ErrDaemonUnavailable
						},
					})
					initializeParams, err := json.Marshal(map[string]any{"protocolVersion": version})
					if err != nil {
						t.Fatal(err)
					}
					if _, rpcErr := server.handleRequest(context.Background(), rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`"initialize"`), Method: "initialize", Params: initializeParams}); rpcErr != nil {
						t.Fatalf("initialize: %#v", rpcErr)
					}

					value, err := server.callTool(context.Background(), test.name, args(root))
					if err != nil {
						t.Fatalf("baseline %s: %v", test.name, err)
					}
					wantText, err := json.Marshal(value)
					if err != nil {
						t.Fatalf("baseline %s JSON: %v", test.name, err)
					}
					params, err := json.Marshal(toolsCallParams(root, test.name, nil))
					if err != nil {
						t.Fatal(err)
					}
					responseValue, rpcErr := server.handleRequest(context.Background(), rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`"call"`), Method: "tools/call", Params: params})
					if rpcErr != nil {
						t.Fatalf("%s RPC error: %#v", test.name, rpcErr)
					}
					response, ok := responseValue.(callToolResult)
					if !ok || response.IsError || len(response.Content) != 1 {
						t.Fatalf("%s response = %#v", test.name, responseValue)
					}
					if got := response.Content[0].Text; got != string(wantText) {
						t.Fatalf("%s text = %q, want unchanged %q", test.name, got, wantText)
					}
					var legacyArray []json.RawMessage
					if err := json.Unmarshal([]byte(response.Content[0].Text), &legacyArray); err != nil {
						t.Fatalf("%s text = %q, want legacy array: %v", test.name, response.Content[0].Text, err)
					}
					structured, err := json.Marshal(response.StructuredContent)
					if err != nil {
						t.Fatal(err)
					}
					var envelope map[string]json.RawMessage
					if err := json.Unmarshal(structured, &envelope); err != nil {
						t.Fatalf("%s structuredContent = %s: %v", test.name, structured, err)
					}
					if _, ok := envelope[test.key]; !ok {
						t.Fatalf("%s structuredContent = %s, missing %q", test.name, structured, test.key)
					}
				})
			}
		})
	}
}

func TestMCPConcurrencyDocs(t *testing.T) {
	paths := []string{"../../docs/design.md", "../../docs/coding-agents.md"}
	var content strings.Builder
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		content.Write(data)
		content.WriteByte('\n')
	}
	all := strings.ToLower(content.String())
	for _, phrase := range []string{"64", "-32001", "-32600", "-32800", "notifications/cancelled", "response transport", "two seconds", "parent cancellation", "eof"} {
		if !strings.Contains(all, phrase) {
			t.Errorf("MCP concurrency docs missing %q", phrase)
		}
	}
}

var (
	_ io.WriteCloser = (*frameShutdownWriter)(nil)
	_ io.WriteCloser = (*gatedResponseWriter)(nil)
	_ io.WriteCloser = (*shutdownWriter)(nil)
)
