package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/process"
	"hum/internal/protocol"
)

// Client is one request connection to a daemon. It owns no managed process;
// closing it only closes this transport.
type Client struct {
	conn     net.Conn
	decoder  *protocol.Decoder
	encoder  *protocol.Encoder
	maxLine  int
	socket   string
	mu       sync.Mutex
	stateMu  sync.Mutex
	closed   bool
	helloOK  bool
	helloErr error
}

// StartRequest carries the exact argv, cwd, and environment for a launch.
// Request aliases keep the client surface exactly aligned with the shared
// protocol DTOs while allowing callers to use typed operation methods.
type StartRequest = protocol.StartRequest
type ListRequest = protocol.ListRequest
type GetRequest = protocol.GetRequest
type OutputRequest = protocol.OutputRequest
type FollowRequest = protocol.FollowRequest
type WaitRequest = protocol.WaitRequest
type SignalRequest = protocol.SignalRequest
type StopRequest = protocol.StopRequest
type RestartRequest = protocol.RestartRequest
type RemoveRequest = protocol.RemoveRequest
type ShutdownRequest = protocol.ShutdownRequest
type InputAttachRequest = protocol.InputAttachRequest

// Dial connects to a socket, performs the mandatory hello, and returns the
// connection even for VersionMismatchError so an idle older daemon can still
// receive the frozen shutdown request.
func Dial(ctx context.Context, socket string) (*Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if socket == "" {
		socket = NewRuntimePaths("").Socket
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, err
	}
	client := newClient(conn, socket)
	if err := client.hello(ctx); err != nil {
		var mismatch *VersionMismatchError
		if errors.As(err, &mismatch) {
			return client, err
		}
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// DialRuntime connects using the canonical runtime path set.
func DialRuntime(ctx context.Context, paths RuntimePaths) (*Client, error) {
	return Dial(ctx, paths.normalized().Socket)
}

// DialDefault connects to the current user's canonical runtime socket.
func DialDefault(ctx context.Context) (*Client, error) {
	return Dial(ctx, NewRuntimePaths("").Socket)
}

// NewClient wraps an already-connected Unix connection. The caller must call
// Hello before operations; Dial is preferred when a socket path is available.
func NewClient(conn net.Conn) *Client { return newClient(conn, "") }
func newClient(conn net.Conn, socket string) *Client {
	return &Client{conn: conn, decoder: protocol.NewDecoder(conn, defaultWireMaxLine), encoder: protocol.NewEncoder(conn, defaultWireMaxLine), maxLine: defaultWireMaxLine, socket: socket}
}

// Hello performs (or repeats) the version handshake. Shutdown is the only
// operation allowed by the server after a mismatched hello.
func (c *Client) Hello(ctx context.Context) error {
	return c.hello(ctx)
}

func (c *Client) hello(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		return net.ErrClosed
	}
	if c.helloOK {
		c.stateMu.Unlock()
		return nil
	}
	if c.helloErr != nil {
		err := c.helloErr
		c.stateMu.Unlock()
		return err
	}
	c.stateMu.Unlock()

	response, err := c.roundTripLocked(ctx, wireRequest{Op: "hello", Version: wireVersion})
	if err != nil {
		var mismatch *VersionMismatchError
		if errors.As(err, &mismatch) {
			c.stateMu.Lock()
			if !c.closed {
				c.helloErr = err
			}
			c.stateMu.Unlock()
		}
		return err
	}
	if response.Version == 0 {
		c.invalidate()
		return errors.New("daemon hello response omitted version")
	}
	if response.Version != wireVersion {
		err := &VersionMismatchError{ClientVersion: wireVersion, DaemonVersion: response.Version}
		c.stateMu.Lock()
		if !c.closed {
			c.helloErr = err
		}
		c.stateMu.Unlock()
		return err
	}
	c.stateMu.Lock()
	if !c.closed {
		c.helloOK = true
	}
	c.stateMu.Unlock()
	return nil
}

// Close disconnects this client. It never signals a supervised process.
func (c *Client) Close() error {
	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		return nil
	}
	c.closed = true
	err := c.conn.Close()
	c.stateMu.Unlock()
	return err
}

func (c *Client) SocketPath() string {
	if c == nil {
		return ""
	}
	return c.socket
}

func (c *Client) Start(ctx context.Context, req StartRequest) (app.Process, error) {
	request := wireRequest{
		Op: "start", Name: req.Name, Source: req.Source, Root: req.Root,
		Argv: append([]string(nil), req.Argv...), Cwd: req.Cwd,
		Env: append([]string(nil), req.Env...), Ready: wireReadinessConfigFromProtocol(req.Ready), TTY: req.TTY,
	}
	if req.TTYSize != nil {
		request.Columns, request.Rows = req.TTYSize.Columns, req.TTYSize.Rows
	}
	response, err := c.roundTrip(ctx, request)
	if err != nil {
		return app.Process{}, err
	}
	if response.Process == nil {
		return app.Process{}, errors.New("daemon start response omitted process")
	}
	return appProcessFromWire(*response.Process), nil
}

func (c *Client) List(ctx context.Context, req ListRequest) ([]app.Process, error) {
	response, err := c.roundTrip(ctx, wireRequest{Op: "list", Cwd: req.Cwd, All: req.All, IncludeCompleted: req.IncludeCompleted})
	if err != nil {
		return nil, err
	}
	items := make([]app.Process, 0, len(response.Processes))
	for _, item := range response.Processes {
		items = append(items, appProcessFromWire(item))
	}
	return items, nil
}

func (c *Client) Get(ctx context.Context, req GetRequest) (app.Process, error) {
	response, err := c.roundTrip(ctx, wireRequest{Op: "get", Name: req.Name, Cwd: req.Cwd})
	if err != nil {
		return app.Process{}, err
	}
	if response.Process == nil {
		return app.Process{}, errors.New("daemon get response omitted process")
	}
	if response.Process.NextCursor == nil {
		return app.Process{}, errors.New("daemon get response omitted next_cursor")
	}
	return appProcessFromWire(*response.Process), nil
}

func (c *Client) Output(ctx context.Context, req OutputRequest) (output.ReadResult, error) {
	response, err := c.roundTrip(ctx, wireRequestFromProtocolOutputRequest(req))
	if err != nil {
		return output.ReadResult{}, err
	}
	return outputResultFromWire(response), nil
}

// Wait opens a fresh connection when this client was dialed from a socket, so
// independent waits do not block control requests or one another.
func (c *Client) Wait(ctx context.Context, req WaitRequest) (app.WaitResult, error) {
	if c.socket != "" {
		waiter, err := Dial(ctx, c.socket)
		if err != nil {
			if waiter != nil {
				_ = waiter.Close()
			}
			return app.WaitResult{}, err
		}
		defer waiter.Close()
		return waiter.wait(ctx, req)
	}
	return c.wait(ctx, req)
}

func (c *Client) wait(ctx context.Context, req WaitRequest) (app.WaitResult, error) {
	response, err := c.roundTrip(ctx, wireRequestFromProtocolWait(&req))
	if err != nil {
		return app.WaitResult{}, err
	}
	if response.Cursor == nil {
		return app.WaitResult{}, errors.New("daemon wait response omitted cursor")
	}
	outcome := app.WaitOutcome(response.Outcome)
	switch outcome {
	case app.WaitMatched, app.WaitExited, app.WaitTimedOut:
	default:
		return app.WaitResult{}, fmt.Errorf("daemon wait response has unknown outcome %q", response.Outcome)
	}
	result := app.WaitResult{Outcome: outcome, Cursor: output.Cursor(*response.Cursor)}
	if response.Exit != nil {
		result.Exit = &processResult{ExitCode: response.Exit.Code, Err: errorFromString(response.Exit.Error), ExitedAt: response.Exit.Time}
	}
	return result, nil
}

// Follow opens a fresh connection, preserving independent follower cursors
// even when several followers are created from one control client.
func (c *Client) Follow(ctx context.Context, req FollowRequest) (*Follower, error) {
	if c.socket == "" {
		return nil, errors.New("client has no socket path for a follower")
	}
	followerClient, err := Dial(ctx, c.socket)
	if err != nil {
		return nil, err
	}
	if err := followerClient.writeOnly(ctx, wireRequestFromProtocolFollowRequest(req)); err != nil {
		_ = followerClient.Close()
		return nil, err
	}
	follower := &Follower{client: followerClient}
	response, err := followerClient.readResponse(ctx)
	if err != nil {
		_ = follower.Close()
		return nil, err
	}
	if response.Error != nil {
		_ = follower.Close()
		return nil, wireErrorToError(response.Error)
	}
	if response.Type != string(protocol.EventReady) {
		_ = follower.Close()
		return nil, fmt.Errorf("daemon follow response omitted ready event")
	}
	return follower, nil
}

// Follower represents one bounded follow stream. Next returns retained output,
// cursor/eviction metadata, and finally process exit. Closing it does not stop
// the supervised process.
type Follower struct{ client *Client }

func (f *Follower) Next(ctx context.Context) (output.Event, error) {
	if f == nil || f.client == nil {
		return output.Event{}, errors.New("nil follower")
	}
	response, err := f.client.readResponse(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			_ = f.Close()
		}
		return output.Event{}, err
	}
	if response.Error != nil {
		return output.Event{}, wireErrorToError(response.Error)
	}
	if response.Op != "event" {
		return output.Event{}, fmt.Errorf("unexpected follower response %q", response.Op)
	}
	if response.Type == "exit" && response.Exit != nil {
		return output.Event{Exit: &output.Exit{Code: response.Exit.Code, Time: response.Exit.Time}}, nil
	}
	return output.Event{Read: &output.ReadResult{
		Entries: entriesFromWire(response.Entries), Next: cursorFromUint64(response.Next),
		Oldest: cursorFromUint64(response.Oldest), Latest: cursorFromUint64(response.Latest),
		EvictedThrough: cursorFromUint64(response.EvictedThrough), Truncated: response.Truncated, More: response.More,
	}}, nil
}

func (f *Follower) Close() error {
	if f == nil || f.client == nil {
		return nil
	}
	return f.client.Close()
}

// InputSession is the owner side of a dedicated duplex TTY connection. State
// events are consumed independently from synchronous write/resize acks.
type InputSession struct {
	client  *Client
	mu      sync.Mutex
	writeMu sync.Mutex
	state   string
	cursor  protocol.Cursor

	events      chan protocol.InputStateEvent
	eventMu     sync.Mutex
	eventQueue  []protocol.InputStateEvent
	eventNotify chan struct{}
	eventClosed bool

	acks      chan json.RawMessage
	done      chan struct{}
	closeOnce sync.Once
}

// InputAttach opens an exclusive input lease. A known manifest or retained
// ad-hoc definition may include Argv/Source so the daemon can reserve the
// lease before the first launch.
func (c *Client) InputAttach(ctx context.Context, req InputAttachRequest) (*InputSession, error) {
	if c == nil || c.socket == "" {
		return nil, errors.New("client has no socket path for input")
	}
	inputClient, err := Dial(ctx, c.socket)
	if err != nil {
		return nil, err
	}
	request := wireRequest{Op: "input_attach", Name: req.Name, Cwd: req.Cwd, Root: req.Root, TTY: req.TTY, Argv: append([]string(nil), req.Argv...), Source: req.Source, Ready: wireReadinessConfigFromProtocol(req.Ready), Columns: req.Columns, Rows: req.Rows}
	if err := inputClient.writeOnly(ctx, request); err != nil {
		_ = inputClient.Close()
		return nil, err
	}
	var response protocol.InputAttachResponse
	if err := inputClient.readInputJSON(ctx, &response); err != nil {
		_ = inputClient.Close()
		return nil, err
	}
	if response.Error != nil {
		_ = inputClient.Close()
		return nil, protocolErrorToError(response.Error)
	}
	if !response.OK {
		_ = inputClient.Close()
		return nil, errors.New("daemon input attach failed")
	}
	var event protocol.InputStateEvent
	if err := inputClient.readInputJSON(ctx, &event); err != nil {
		_ = inputClient.Close()
		return nil, err
	}
	if event.Error != nil {
		_ = inputClient.Close()
		return nil, protocolErrorToError(event.Error)
	}
	session := &InputSession{
		client: inputClient, state: event.State, cursor: event.LaunchCursor,
		events: make(chan protocol.InputStateEvent, 16), eventNotify: make(chan struct{}, 1),
		acks: make(chan json.RawMessage, 4), done: make(chan struct{}),
	}
	go session.dispatchInputEvents()
	go session.readInputEvents()
	return session, nil
}

// State reports the latest durable state event and cursor.
func (s *InputSession) State() (string, protocol.Cursor) {
	if s == nil {
		return "", 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.cursor
}
func (s *InputSession) Events() <-chan protocol.InputStateEvent {
	if s == nil {
		return nil
	}
	return s.events
}
func (s *InputSession) Next(ctx context.Context) (protocol.InputStateEvent, error) {
	if s == nil {
		return protocol.InputStateEvent{}, net.ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-s.done:
		return protocol.InputStateEvent{}, net.ErrClosed
	default:
	}
	select {
	case event, ok := <-s.events:
		if !ok {
			return protocol.InputStateEvent{}, net.ErrClosed
		}
		return event, nil
	case <-s.done:
		return protocol.InputStateEvent{}, net.ErrClosed
	case <-ctx.Done():
		return protocol.InputStateEvent{}, ctx.Err()
	}
}
func (s *InputSession) readInputEvents() {
	defer close(s.done)
	defer s.closeInputEvents()
	for {
		var raw json.RawMessage
		if err := s.client.readInputJSONUnlocked(context.Background(), &raw); err != nil {
			return
		}
		var header struct {
			Op protocol.Operation `json:"op"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			return
		}
		if header.Op != protocol.OpInputState {
			select {
			case s.acks <- raw:
			case <-s.done:
				return
			}
			continue
		}
		var event protocol.InputStateEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return
		}
		s.mu.Lock()
		s.state, s.cursor = event.State, event.LaunchCursor
		s.mu.Unlock()
		s.enqueueInputEvent(event)
	}
}

func (s *InputSession) enqueueInputEvent(event protocol.InputStateEvent) {
	if s == nil {
		return
	}
	s.eventMu.Lock()
	if s.eventClosed {
		s.eventMu.Unlock()
		return
	}
	s.eventQueue = append(s.eventQueue, event)
	select {
	case s.eventNotify <- struct{}{}:
	default:
	}
	s.eventMu.Unlock()
}

func (s *InputSession) closeInputEvents() {
	if s == nil {
		return
	}
	s.eventMu.Lock()
	s.eventClosed = true
	select {
	case s.eventNotify <- struct{}{}:
	default:
	}
	s.eventMu.Unlock()
}

func (s *InputSession) dispatchInputEvents() {
	defer close(s.events)
	for {
		s.eventMu.Lock()
		if len(s.eventQueue) != 0 {
			event := s.eventQueue[0]
			s.eventQueue = s.eventQueue[1:]
			if len(s.eventQueue) == 0 {
				s.eventQueue = nil
			}
			s.eventMu.Unlock()
			select {
			case s.events <- event:
			case <-s.done:
				return
			}
			continue
		}
		closed := s.eventClosed
		s.eventMu.Unlock()
		if closed {
			return
		}
		select {
		case <-s.eventNotify:
		case <-s.done:
			return
		}
	}
}
func (s *InputSession) writeRequest(ctx context.Context, req wireRequest, want protocol.Operation) error {
	if s == nil || s.client == nil {
		return net.ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.client.writeOnly(ctx, req); err != nil {
		return err
	}
	select {
	case raw := <-s.acks:
		var ack protocol.InputAckResponse
		if err := json.Unmarshal(raw, &ack); err != nil {
			return err
		}
		if ack.Op != want {
			return fmt.Errorf("daemon input response operation %q does not match %q", ack.Op, want)
		}
		if ack.Error != nil {
			return protocolErrorToError(ack.Error)
		}
		if !ack.OK {
			return errors.New("daemon input operation failed")
		}
		return nil
	case <-s.done:
		return &protocol.WireError{Code: protocol.ErrorInputClosed, Message: "tty input connection is closed"}
	case <-ctx.Done():
		_ = s.client.Close()
		return ctx.Err()
	}
}
func (s *InputSession) Write(ctx context.Context, data []byte) error {
	state, cursor := s.State()
	if state != "running" {
		return &protocol.WireError{Code: protocol.ErrorInputClosed, Message: "tty input is not running"}
	}
	return s.WriteAt(ctx, cursor, data)
}
func (s *InputSession) WriteAt(ctx context.Context, cursor protocol.Cursor, data []byte) error {
	if len(data) > protocol.MaxInputBytes {
		return &protocol.WireError{Code: protocol.ErrorInputTooLarge, Message: "input payload exceeds 32768 bytes"}
	}
	return s.writeRequest(ctx, wireRequest{Op: "input_write", LaunchCursor: uint64(cursor), Data: base64.StdEncoding.EncodeToString(data)}, protocol.OpInputWrite)
}
func (s *InputSession) Resize(ctx context.Context, columns, rows uint16) error {
	_, cursor := s.State()
	return s.ResizeAt(ctx, cursor, columns, rows)
}
func (s *InputSession) ResizeAt(ctx context.Context, cursor protocol.Cursor, columns, rows uint16) error {
	if columns == 0 || rows == 0 {
		return &protocol.WireError{Code: protocol.ErrorInvalidRequest, Message: "tty dimensions must be non-zero"}
	}
	return s.writeRequest(ctx, wireRequest{Op: "input_resize", LaunchCursor: uint64(cursor), Columns: columns, Rows: rows}, protocol.OpInputResize)
}
func (s *InputSession) Release() error {
	if s == nil {
		return nil
	}
	// Closing the transport is the release operation. It is deliberately done
	// without taking writeMu: a pending write may be blocked in the daemon, and
	// the close is what cancels that operation so the server can drain it before
	// releasing the lease. The daemon treats transport loss exactly like an
	// explicit input_release.
	s.closeOnce.Do(func() { _ = s.client.Close() })
	<-s.done
	return nil
}
func (s *InputSession) Close() error { return s.Release() }

func (c *Client) Signal(ctx context.Context, req SignalRequest) error {
	_, err := c.roundTrip(ctx, wireRequest{Op: "signal", Name: req.Name, Cwd: req.Cwd, Signal: req.Signal})
	return err
}

func (c *Client) Stop(ctx context.Context, req StopRequest) error {
	_, err := c.roundTrip(ctx, wireRequest{Op: "stop", Name: req.Name, Cwd: req.Cwd})
	return err
}

func (c *Client) Remove(ctx context.Context, req RemoveRequest) error {
	_, err := c.roundTrip(ctx, wireRequest{Op: "remove", Name: req.Name, Cwd: req.Cwd})
	return err
}

func (c *Client) Restart(ctx context.Context, req RestartRequest) (app.Process, error) {
	request := wireRequest{
		Op: "restart", Name: req.Name, Cwd: req.Cwd, Root: req.Root, Update: req.Update,
		Argv: append([]string(nil), req.Argv...), Env: append([]string(nil), req.Env...),
		Source: req.Source, Ready: wireReadinessConfigFromProtocol(req.Ready), TTY: req.TTY,
	}
	if req.TTYSize != nil {
		request.Columns, request.Rows = req.TTYSize.Columns, req.TTYSize.Rows
	}
	response, err := c.roundTrip(ctx, request)
	if err != nil {
		return app.Process{}, err
	}
	if response.Process == nil {
		return app.Process{}, errors.New("daemon restart response omitted process")
	}
	return appProcessFromWire(*response.Process), nil
}

// Shutdown remains legal on a Client returned alongside VersionMismatchError.
func (c *Client) Shutdown(ctx context.Context, req ShutdownRequest) error {
	_, err := c.roundTrip(ctx, wireRequest{Op: "shutdown", Force: req.Force})
	return err
}

func (c *Client) roundTrip(ctx context.Context, req wireRequest) (wireResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.roundTripLocked(ctx, req)
}

func (c *Client) roundTripLocked(ctx context.Context, req wireRequest) (wireResponse, error) {
	if err := c.requestAllowed(req.Op); err != nil {
		return wireResponse{}, err
	}
	cleanup, err := setConnContextWithCancel(c.conn, ctx)
	if err != nil {
		return wireResponse{}, err
	}
	defer cleanup()
	if err := writeProtocolRequest(c.encoder, req); err != nil {
		if invalidateAfterWriteError(err) {
			c.invalidate()
		}
		return wireResponse{}, contextError(ctx, err)
	}
	response, err := readProtocolResponse(c.decoder)
	if err != nil {
		if response.Error != nil {
			if response.Op == "" {
				c.invalidate()
				return response, err
			}
			if response.Op != req.Op {
				c.invalidate()
				return response, unexpectedResponseOp(req.Op, response.Op)
			}
			return response, err
		}
		c.invalidate()
		return response, contextError(ctx, err)
	}
	if response.Op != req.Op {
		c.invalidate()
		return response, unexpectedResponseOp(req.Op, response.Op)
	}
	return response, nil
}

func (c *Client) readResponse(ctx context.Context) (wireResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requestAllowed("event"); err != nil {
		return wireResponse{}, err
	}
	cleanup, err := setConnContextWithCancel(c.conn, ctx)
	if err != nil {
		return wireResponse{}, err
	}
	defer cleanup()
	response, err := readProtocolResponse(c.decoder)
	if err != nil {
		if response.Error == nil {
			c.invalidate()
			return response, contextError(ctx, err)
		}
		if response.Op == "" {
			c.invalidate()
		}
		return response, err
	}
	if response.Op != "event" {
		c.invalidate()
		return response, unexpectedResponseOp("event", response.Op)
	}
	return response, nil
}

func (c *Client) writeOnly(ctx context.Context, req wireRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requestAllowed(req.Op); err != nil {
		return err
	}
	cleanup, err := setConnContextWithCancel(c.conn, ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := writeProtocolRequest(c.encoder, req); err != nil {
		if invalidateAfterWriteError(err) {
			c.invalidate()
		}
		return contextError(ctx, err)
	}
	return nil
}

func (c *Client) readInputJSON(ctx context.Context, value any) error {
	if c == nil {
		return net.ErrClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readInputJSONUnlocked(ctx, value)
}

func (c *Client) readInputJSONUnlocked(ctx context.Context, value any) error {
	if c == nil {
		return net.ErrClosed
	}
	if err := c.requestAllowed("input"); err != nil {
		return err
	}
	cleanup, err := setConnContextWithCancel(c.conn, ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := c.decoder.Decode(value); err != nil {
		return contextError(ctx, err)
	}
	return nil
}

func (c *Client) requestAllowed(op string) error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	if c.helloErr != nil && op != "shutdown" {
		return c.helloErr
	}
	return nil
}

func (c *Client) invalidate() {
	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		return
	}
	c.closed = true
	_ = c.conn.Close()
	c.stateMu.Unlock()
}

func contextError(ctx context.Context, err error) error {
	if ctx == nil {
		return err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
	}
	return err
}

func invalidateAfterWriteError(err error) bool {
	return !errors.Is(err, protocol.ErrMalformed) && !errors.Is(err, protocol.ErrOversized)
}

func unexpectedResponseOp(expected, actual string) error {
	return fmt.Errorf("daemon response operation %q does not match request operation %q", actual, expected)
}

func writeProtocolRequest(encoder *protocol.Encoder, req wireRequest) error {
	var value any
	switch req.Op {
	case "hello":
		value = protocol.Hello{Op: protocol.OpHello, Version: req.Version}
	case "start":
		value = protocol.StartRequest{
			Op: protocol.OpStart, Name: req.Name, Argv: req.Argv, Cwd: req.Cwd, Root: req.Root, Env: req.Env,
			Source: req.Source, Ready: protocolReadinessConfigFromWire(req.Ready), TTY: req.TTY,
		}
		if req.Columns != 0 || req.Rows != 0 {
			start := value.(protocol.StartRequest)
			start.TTYSize = &protocol.TTYSize{Columns: req.Columns, Rows: req.Rows}
			value = start
		}
	case "list":
		value = protocol.ListRequest{Op: protocol.OpList, Cwd: req.Cwd, All: req.All, IncludeCompleted: req.IncludeCompleted}
	case "get":
		value = protocol.GetRequest{Op: protocol.OpGet, Name: req.Name, Cwd: req.Cwd}
	case "output":
		value = protocol.OutputRequest{Op: protocol.OpOutput, Name: req.Name, Cwd: req.Cwd, After: protocolCursorFromUint64(req.After), Tail: req.Tail, Stream: protocol.Stream(req.Stream), Match: req.Match, MaxEntries: req.MaxEntries, MaxBytes: req.MaxBytes}
	case "follow":
		value = protocol.FollowRequest{Op: protocol.OpFollow, Name: req.Name, Cwd: req.Cwd, After: protocolCursorFromUint64(req.After), Tail: req.Tail, Stream: protocol.Stream(req.Stream), Match: req.Match, MaxEntries: req.MaxEntries, MaxBytes: req.MaxBytes}
	case "wait":
		value = protocol.WaitRequest{Op: protocol.OpWait, Name: req.Name, Cwd: req.Cwd, After: protocolCursorFromUint64(req.After), Match: req.Match, TimeoutMS: req.TimeoutMS}
	case "signal":
		value = protocol.SignalRequest{Op: protocol.OpSignal, Name: req.Name, Cwd: req.Cwd, Signal: req.Signal}
	case "stop":
		value = protocol.StopRequest{Op: protocol.OpStop, Name: req.Name, Cwd: req.Cwd}
	case "remove":
		value = protocol.RemoveRequest{Op: protocol.OpRemove, Name: req.Name, Cwd: req.Cwd}
	case "restart":
		value = protocol.RestartRequest{
			Op: protocol.OpRestart, Name: req.Name, Cwd: req.Cwd, Root: req.Root, Update: req.Update,
			Argv: req.Argv, Env: req.Env, Source: req.Source,
			Ready: protocolReadinessConfigFromWire(req.Ready), TTY: req.TTY,
		}
		if req.Columns != 0 || req.Rows != 0 {
			restart := value.(protocol.RestartRequest)
			restart.TTYSize = &protocol.TTYSize{Columns: req.Columns, Rows: req.Rows}
			value = restart
		}
	case "shutdown":
		value = protocol.ShutdownRequest{Op: protocol.OpShutdown, Force: req.Force}
	case "input_attach":
		value = protocol.InputAttachRequest{Op: protocol.OpInputAttach, Name: req.Name, Cwd: req.Cwd, Root: req.Root, TTY: req.TTY, Argv: req.Argv, Source: req.Source, Ready: protocolReadinessConfigFromWire(req.Ready), Columns: req.Columns, Rows: req.Rows}
	case "input_release":
		value = protocol.InputReleaseRequest{Op: protocol.OpInputRelease}
	case "input_write":
		value = protocol.InputWriteRequest{Op: protocol.OpInputWrite, LaunchCursor: protocol.Cursor(req.LaunchCursor), Data: req.Data}
	case "input_resize":
		value = protocol.InputResizeRequest{Op: protocol.OpInputResize, LaunchCursor: protocol.Cursor(req.LaunchCursor), Columns: req.Columns, Rows: req.Rows}
	default:
		value = protocol.Request{Op: protocol.Operation(req.Op)}
	}
	return encoder.EncodeRequest(value)
}

func readProtocolResponse(decoder *protocol.Decoder) (wireResponse, error) {
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return wireResponse{}, err
	}
	var response wireResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return wireResponse{}, &MalformedRequestError{Err: err}
	}
	if response.Error != nil {
		return response, wireErrorToError(response.Error)
	}
	return response, nil
}

func setConnContext(conn net.Conn, ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		return conn.SetDeadline(deadline)
	}
	return conn.SetDeadline(time.Time{})
}
func setConnContextWithCancel(conn net.Conn, ctx context.Context) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := setConnContext(conn, ctx); err != nil {
		return nil, err
	}
	done := ctx.Done()
	if done == nil {
		return func() { clearConnDeadline(conn) }, nil
	}
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-done:
			_ = conn.SetDeadline(time.Now())
		case <-stop:
		}
	}()
	return func() {
		close(stop)
		<-stopped
		clearConnDeadline(conn)
	}, nil
}

func clearConnDeadline(conn net.Conn) { _ = conn.SetDeadline(time.Time{}) }

func wireRequestFromProtocolOutputRequest(req protocol.OutputRequest) wireRequest {
	wire := wireRequest{Op: string(protocol.OpOutput), Name: req.Name, Cwd: req.Cwd, Tail: req.Tail, Stream: string(req.Stream), Match: req.Match, MaxEntries: req.MaxEntries, MaxBytes: req.MaxBytes}
	if req.After != nil {
		value := uint64(*req.After)
		wire.After = &value
	}
	return wire
}

func wireRequestFromProtocolFollowRequest(req protocol.FollowRequest) wireRequest {
	wire := wireRequest{Op: string(protocol.OpFollow), Name: req.Name, Cwd: req.Cwd, Tail: req.Tail, Stream: string(req.Stream), Match: req.Match, MaxEntries: req.MaxEntries, MaxBytes: req.MaxBytes}
	if req.After != nil {
		value := uint64(*req.After)
		wire.After = &value
	}
	return wire
}

func protocolErrorToError(wire *protocol.WireError) error {
	if wire == nil {
		return nil
	}
	return &protocol.WireError{Code: wire.Code, Message: wire.Message, Details: wire.Details}
}

func wireErrorToError(wire *wireError) error {
	if wire == nil {
		return nil
	}
	if wire.Code == string(protocol.ErrorVersionMismatch) || wire.Code == "version_mismatch" {
		clientVersion, daemonVersion := 0, wire.DaemonVersion
		if details, ok := wire.Details.(map[string]any); ok {
			if value, ok := details["client"].(float64); ok {
				clientVersion = int(value)
			}
			if value, ok := details["daemon"].(float64); ok {
				daemonVersion = int(value)
			}
		}
		return &VersionMismatchError{ClientVersion: clientVersion, DaemonVersion: daemonVersion, Message: wire.Message}
	}
	if wire.Code == string(protocol.ErrorActiveProcesses) || wire.Code == "active_processes" {
		names := append([]string(nil), wire.Processes...)
		if len(names) == 0 {
			if values, ok := wire.Details.([]any); ok {
				names = make([]string, 0, len(values))
				for _, value := range values {
					if name, ok := value.(string); ok {
						names = append(names, name)
					}
				}
			}
		}
		return &ActiveProcessesError{Names: names}
	}
	return &WireError{Code: protocol.ErrorCode(wire.Code), Message: wire.Message, Details: wire.Details}
}

func appProcessFromWire(item wireProcess) app.Process {
	result := app.Process{
		Name: item.Name, Source: item.Source, Root: item.Root, TTY: item.TTY, PID: item.PID, PGID: item.PGID,
		Cwd: item.Cwd, Argv: append([]string(nil), item.Argv...), Start: item.Start,
		LaunchCursor: output.Cursor(item.LaunchCursor), State: app.State(item.State),
		ExitCode: item.ExitCode, ExitedAt: item.ExitedAt, RestartCount: item.RestartCount,
		Followers: item.Followers,
	}
	if item.Readiness != nil {
		result.Readiness = &app.Readiness{
			State: item.Readiness.State, Cursor: cursorFromUint64(item.Readiness.Cursor),
			Time: item.Readiness.Time, Match: item.Readiness.Match,
		}
	}
	if item.NextCursor != nil {
		result.NextCursor = output.Cursor(*item.NextCursor)
	}
	if item.Exit != nil {
		exitCode := item.Exit.Code
		if exitCode == 0 && item.Exit.ExitCode != 0 {
			exitCode = item.Exit.ExitCode
		}
		result.Exit = &processResult{ExitCode: exitCode, Err: errorFromString(item.Exit.Error), ExitedAt: item.Exit.Time}
	}
	return result
}

// processResult is assigned through the app.Process.Exit field below. Keeping
// conversion in one place makes response DTOs incapable of carrying Env.
type processResult = process.Result

func errorFromString(value string) error {
	if value == "" {
		return nil
	}
	return errors.New(value)
}

func outputResultFromWire(response wireResponse) output.ReadResult {
	return output.ReadResult{Entries: entriesFromWire(response.Entries), Next: cursorFromUint64(response.Next), Oldest: cursorFromUint64(response.Oldest), Latest: cursorFromUint64(response.Latest), EvictedThrough: cursorFromUint64(response.EvictedThrough), Truncated: response.Truncated, More: response.More}
}

func entriesFromWire(items []wireEntry) []output.Entry {
	entries := make([]output.Entry, 0, len(items))
	for _, item := range items {
		entries = append(entries, output.Entry{Cursor: output.Cursor(item.Cursor), Stream: outputStreamFromName(item.Stream), Time: item.Time, Text: item.Text})
	}
	return entries
}

func cursorFromUint64(value *uint64) *output.Cursor {
	if value == nil {
		return nil
	}
	cursor := output.Cursor(*value)
	return &cursor
}
