package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	mcpProtocolVersion = "2025-06-18"
	maxMessageBytes    = 4 << 20

	maxInFlightRequests = 64
	responseQueueSize   = maxInFlightRequests * 2
	serverShutdownWait  = time.Second
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callToolResult struct {
	Content           []textContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

type requestState struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	cancelled bool
}

func (r *requestState) markCancelled() {
	r.mu.Lock()
	r.cancelled = true
	r.mu.Unlock()
	r.cancel()
}

func (r *requestState) stopForShutdown() {
	r.cancel()
}

func (r *requestState) wasCancelled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelled
}

type requestRegistry struct {
	mu       sync.Mutex
	requests map[string]*requestState
}

func newRequestRegistry() *requestRegistry {
	return &requestRegistry{requests: make(map[string]*requestState)}
}

func (r *requestRegistry) reserve(parent context.Context, id string) (*requestState, *rpcError) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.requests[id]; exists {
		return nil, &rpcError{Code: -32600, Message: "request ID is already in flight"}
	}
	if len(r.requests) >= maxInFlightRequests {
		return nil, &rpcError{Code: -32001, Message: "server is busy; too many requests are in flight"}
	}
	requestContext, cancel := context.WithCancel(parent)
	state := &requestState{ctx: requestContext, cancel: cancel}
	r.requests[id] = state
	return state, nil
}

func (r *requestRegistry) finish(id string, state *requestState) bool {
	state.cancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, exists := r.requests[id]; !exists || current != state {
		return false
	}
	cancelled := state.wasCancelled()
	delete(r.requests, id)
	return cancelled
}

func (r *requestRegistry) cancel(id string) {
	r.mu.Lock()
	state := r.requests[id]
	if state != nil {
		state.markCancelled()
	}
	r.mu.Unlock()
}

func (r *requestRegistry) cancelAll() {
	r.mu.Lock()
	for _, state := range r.requests {
		state.stopForShutdown()
	}
	r.mu.Unlock()
}

type handlerTracker struct {
	mu     sync.Mutex
	active int
	done   chan struct{}
}

func newHandlerTracker() *handlerTracker {
	done := make(chan struct{})
	close(done)
	return &handlerTracker{done: done}
}

func (t *handlerTracker) start() {
	t.mu.Lock()
	if t.active == 0 {
		t.done = make(chan struct{})
	}
	t.active++
	t.mu.Unlock()
}

func (t *handlerTracker) finish() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active--
	if t.active == 0 {
		close(t.done)
	}
}

func (t *handlerTracker) doneChannel() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.done
}

type responseMessage struct {
	response    rpcResponse
	done        chan error
	beforeWrite func(*rpcResponse)
	onComplete  func()
}

type responseTransport struct {
	writer    io.Writer
	responses chan responseMessage
	closed    chan struct{}
	done      chan struct{}
	closeOnce sync.Once

	mu  sync.Mutex
	err error
}

var (
	errResponseTransportClosed     = errors.New("MCP response transport is closed")
	errServeInputEnded             = errors.New("MCP input has ended")
	errServeReaderNotInterruptible = errors.New("MCP stdio reader must be closeable or a finite in-memory reader")
	errServeWriterNotInterruptible = errors.New("MCP stdio writer must be closeable or an in-memory writer")
)

func newResponseTransport(writer io.Writer) *responseTransport {
	transport := &responseTransport{
		writer:    writer,
		responses: make(chan responseMessage, responseQueueSize),
		closed:    make(chan struct{}),
		done:      make(chan struct{}),
	}
	go transport.run()
	return transport
}

func (t *responseTransport) sendWithCallbacks(response rpcResponse, beforeWrite func(*rpcResponse), onComplete func()) error {
	return t.sendUntilWithCallbacks(response, nil, nil, beforeWrite, onComplete)
}

func (t *responseTransport) sendUntil(response rpcResponse, ctx context.Context, inputEnded <-chan struct{}) error {
	return t.sendUntilWithCallbacks(response, ctx, inputEnded, nil, nil)
}

func (t *responseTransport) sendUntilWithCallbacks(response rpcResponse, ctx context.Context, inputEnded <-chan struct{}, beforeWrite func(*rpcResponse), onComplete func()) error {
	message, err := t.enqueueUntilWithCallbacks(response, ctx, inputEnded, beforeWrite, onComplete)
	if err != nil {
		return err
	}
	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}
	select {
	case err := <-message.done:
		return err
	case <-t.closed:
		return errResponseTransportClosed
	case <-ctxDone:
		return ctx.Err()
	case <-inputEnded:
		return errServeInputEnded
	}
}

func (t *responseTransport) enqueueUntilWithCallbacks(response rpcResponse, ctx context.Context, inputEnded <-chan struct{}, beforeWrite func(*rpcResponse), onComplete func()) (*responseMessage, error) {
	message := &responseMessage{response: response, done: make(chan error, 1), beforeWrite: beforeWrite, onComplete: onComplete}
	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}
	select {
	case <-t.closed:
		return nil, errResponseTransportClosed
	case <-ctxDone:
		return nil, ctx.Err()
	case <-inputEnded:
		return nil, errServeInputEnded
	case t.responses <- *message:
		return message, nil
	}
}

func (t *responseTransport) run() {
	var runErr error
	writeResponse := func(response rpcResponse) error {
		encoded, err := json.Marshal(response)
		if err != nil {
			return err
		}
		encoded = append(encoded, '\n')
		written, err := t.writer.Write(encoded)
		if err != nil {
			return err
		}
		if written != len(encoded) {
			return io.ErrShortWrite
		}
		return nil
	}
	writeMessage := func(message responseMessage) error {
		if message.beforeWrite != nil {
			message.beforeWrite(&message.response)
		}
		runErr := writeResponse(message.response)
		if message.onComplete != nil {
			message.onComplete()
		}
		message.done <- runErr
		return runErr
	}
	writePending := func() error {
		for {
			select {
			case message := <-t.responses:
				if err := writeMessage(message); err != nil {
					return err
				}
			default:
				return nil
			}
		}
	}

	for {
		select {
		case message := <-t.responses:
			if runErr = writeMessage(message); runErr != nil {
				t.Close()
				goto done
			}
		case <-t.closed:
			runErr = writePending()
			goto done
		}
	}

done:
	if runErr != nil {
		t.mu.Lock()
		t.err = runErr
		t.mu.Unlock()
	}
	close(t.done)
}

func (t *responseTransport) Close() error {
	var closeErr error
	t.closeOnce.Do(func() {
		close(t.closed)
		if closer, ok := t.writer.(io.Closer); ok {
			closeErr = closer.Close()
		}
	})
	return closeErr
}

func (t *responseTransport) wait() error {
	<-t.done
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

func serveReaderCanStop(reader io.Reader) bool {
	if _, ok := reader.(io.Closer); ok {
		return true
	}
	switch reader.(type) {
	case *bytes.Buffer, *bytes.Reader, *strings.Reader:
		return true
	default:
		return false
	}
}

func serveWriterCanStop(writer io.Writer) bool {
	if _, ok := writer.(io.Closer); ok {
		return true
	}
	switch writer.(type) {
	case *bytes.Buffer, *strings.Builder:
		return true
	default:
		return false
	}
}

// Serve runs the MCP server over newline-delimited JSON-RPC until EOF or cancellation.
func (s *Server) Serve(ctx context.Context, reader io.Reader, writer io.Writer) error {
	if reader == nil || writer == nil {
		return errors.New("MCP stdio reader and writer are required")
	}
	if !serveReaderCanStop(reader) {
		return errServeReaderNotInterruptible
	}
	if !serveWriterCanStop(writer) {
		return errServeWriterNotInterruptible
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Scanner reads in its own goroutine so parent cancellation can stop a
	// server whose stdin has not produced another line. A closeable reader is
	// closed during shutdown to unblock that read.
	type scanEvent struct {
		line []byte
		err  error
		eof  bool
	}
	scanEvents := make(chan scanEvent)
	inputEnded := make(chan struct{})
	var inputEndOnce sync.Once
	readStop := make(chan struct{})
	var readStopOnce sync.Once
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), maxMessageBytes)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case scanEvents <- scanEvent{line: line}:
			case <-readStop:
				return
			}
		}
		err := scanner.Err()
		inputEndOnce.Do(func() { close(inputEnded) })
		select {
		case scanEvents <- scanEvent{err: err, eof: err == nil}:
		case <-readStop:
		}
	}()

	transport := newResponseTransport(writer)
	registry := newRequestRegistry()
	handlers := newHandlerTracker()
	stopReader := func() {
		readStopOnce.Do(func() {
			close(readStop)
			inputEndOnce.Do(func() { close(inputEnded) })
			if closer, ok := reader.(io.Closer); ok {
				_ = closer.Close()
			}
		})
	}

	var terminalErr error
	transportFailed := false
	for {
		select {
		case <-ctx.Done():
			terminalErr = ctx.Err()
			goto shutdown
		case <-transport.done:
			if err := ctx.Err(); err != nil {
				terminalErr = err
			} else {
				transportFailed = true
				terminalErr = transport.wait()
				if terminalErr == nil {
					terminalErr = errResponseTransportClosed
				}
			}
			goto shutdown
		case event := <-scanEvents:
			if event.eof {
				terminalErr = ctx.Err()
				goto shutdown
			}
			if event.err != nil {
				terminalErr = event.err
				if errors.Is(terminalErr, bufio.ErrTooLong) {
					terminalErr = fmt.Errorf("MCP message exceeds %d bytes", maxMessageBytes)
				}
				goto shutdown
			}
			line := event.line
			var request rpcRequest
			if err := json.Unmarshal(line, &request); err != nil {
				if err := transport.sendUntil(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}}, ctx, inputEnded); err != nil {
					terminalErr = err
					if errors.Is(err, errServeInputEnded) {
						terminalErr = ctx.Err()
					}
					goto shutdown
				}
				continue
			}
			if request.JSONRPC != "2.0" || request.Method == "" {
				if len(request.ID) != 0 {
					if err := transport.sendUntil(rpcResponse{JSONRPC: "2.0", ID: request.ID, Error: &rpcError{Code: -32600, Message: "invalid request"}}, ctx, inputEnded); err != nil {
						terminalErr = err
						if errors.Is(err, errServeInputEnded) {
							terminalErr = ctx.Err()
						}
						goto shutdown
					}
				}
				continue
			}
			if len(request.ID) == 0 {
				if request.Method == "notifications/cancelled" {
					registry.cancel(cancellationID(request.Params))
				}
				// Notifications, including initialized and unknown methods, have no response.
				continue
			}

			id := rpcIDKey(request.ID)
			state, reservationErr := registry.reserve(ctx, id)
			if reservationErr != nil {
				if err := transport.sendUntil(rpcResponse{JSONRPC: "2.0", ID: request.ID, Error: reservationErr}, ctx, inputEnded); err != nil {
					terminalErr = err
					if errors.Is(err, errServeInputEnded) {
						terminalErr = ctx.Err()
					}
					goto shutdown
				}
				continue
			}
			handlers.start()
			go func(request rpcRequest, id string, state *requestState) {
				defer handlers.finish()
				var result any
				var rpcErr *rpcError
				if state.wasCancelled() {
					rpcErr = requestCancelledError()
				} else {
					result, rpcErr = s.handleRequest(state.ctx, request)
				}
				if state.wasCancelled() {
					result = nil
					rpcErr = requestCancelledError()
				}
				finishOnce := sync.Once{}
				finish := func() { finishOnce.Do(func() { registry.finish(id, state) }) }
				applyCancellation := func(response *rpcResponse) {
					if state.wasCancelled() {
						response.Result = nil
						response.Error = requestCancelledError()
					}
				}
				if err := transport.sendWithCallbacks(rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: result, Error: rpcErr}, applyCancellation, finish); err != nil {
					finish()
				}
			}(request, id, state)
		}
	}

shutdown:
	stopReader()
	registry.cancelAll()
	waitTimer := time.NewTimer(serverShutdownWait)
	select {
	case <-handlers.doneChannel():
		if !waitTimer.Stop() {
			<-waitTimer.C
		}
	case <-waitTimer.C:
	}
	_ = transport.Close()
	writerErr := transport.wait()
	// Closing the response transport releases handlers blocked while enqueueing
	// their terminal response. Context-aware handlers should all join here.
	<-handlers.doneChannel()
	<-readDone
	if transportFailed && terminalErr == nil && writerErr != nil {
		terminalErr = writerErr
	}
	return terminalErr
}

func requestCancelledError() *rpcError {
	return &rpcError{Code: -32800, Message: "request cancelled"}
}

func cancellationID(params json.RawMessage) string {
	var input struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if len(params) == 0 || json.Unmarshal(params, &input) != nil || len(input.RequestID) == 0 {
		return ""
	}
	return rpcIDKey(input.RequestID)
}

func rpcIDKey(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	var value any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return string(trimmed)
	}
	switch typed := value.(type) {
	case string:
		return "string:" + typed
	case json.Number:
		return "number:" + typed.String()
	case nil:
		return "null"
	default:
		encoded, err := json.Marshal(value)
		if err == nil {
			return string(encoded)
		}
		return string(trimmed)
	}
}

func (s *Server) handleRequest(ctx context.Context, request rpcRequest) (any, *rpcError) {
	switch request.Method {
	case "initialize":
		version := "dev"
		if s != nil && s.opts.Version != "" {
			version = s.opts.Version
		}
		return map[string]any{
			"protocolVersion": negotiateProtocolVersion(request.Params),
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]string{"name": "hum", "version": version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.toolDefinitions()}, nil
	case "tools/call":
		var params callToolParams
		if err := json.Unmarshal(request.Params, &params); err != nil || params.Name == "" {
			return nil, &rpcError{Code: -32602, Message: "invalid tools/call params"}
		}
		if len(params.Arguments) == 0 || string(params.Arguments) == "null" {
			params.Arguments = json.RawMessage("{}")
		}
		value, err := s.callTool(ctx, params.Name, params.Arguments)
		if err != nil {
			mapped := mapError(err)
			text, _ := json.Marshal(mapped)
			return callToolResult{Content: []textContent{{Type: "text", Text: string(text)}}, StructuredContent: mapped, IsError: true}, nil
		}
		text, err := json.Marshal(value)
		if err != nil {
			return nil, &rpcError{Code: -32603, Message: "failed to encode tool result"}
		}
		return callToolResult{Content: []textContent{{Type: "text", Text: string(text)}}, StructuredContent: value}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

// supportedProtocolVersions lists the MCP revisions this server can speak.
// The tool surface (tools/list, tools/call, ping) is identical across them;
// newer-only fields such as structuredContent are ignored by older clients.
var supportedProtocolVersions = []string{"2024-11-05", "2025-03-26", mcpProtocolVersion}

// negotiateProtocolVersion echoes the client's requested version when it is
// supported, as the specification requires, and otherwise offers the latest
// version this server implements.
func negotiateProtocolVersion(params json.RawMessage) string {
	var requested struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &requested)
	}
	for _, version := range supportedProtocolVersions {
		if requested.ProtocolVersion == version {
			return version
		}
	}
	return mcpProtocolVersion
}
