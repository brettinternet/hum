package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
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
	conn       net.Conn
	decoder    *protocol.Decoder
	encoder    *protocol.Encoder
	maxLine    int
	socket     string
	mu         sync.Mutex
	stateMu    sync.Mutex
	closed     bool
	warningsMu sync.Mutex
	warnings   []protocol.StartupWarning
	helloOK    bool
	helloErr   error
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

// InputRequest describes one bounded, cursor-scoped one-shot write. The
// daemon client resolves the initial input state, writes exactly once at its
// initial launch cursor, and releases the input lease before returning.
type InputRequest struct {
	Name  string
	Scope string
	Cwd   string
	Root  string
	Data  []byte
}

// InputResult reports the bytes acknowledged by the daemon and the launch
// cursor selected from the initial running state.
type InputResult struct {
	Bytes        int
	LaunchCursor protocol.Cursor
}

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

// StartupBudget returns the maximum time a new daemon may need before it can
// accept connections. Startup reclaims recorded groups sequentially and may
// wait once after TERM and once after KILL for each group; dialSlack covers the
// remaining setup and readiness handshake. Missing state has no recovery work.
func StartupBudget(paths RuntimePaths, stopGrace, dialSlack time.Duration) (time.Duration, error) {
	if stopGrace < 0 || dialSlack < 0 {
		return 0, errors.New("daemon startup budget durations must not be negative")
	}
	paths = paths.normalized()
	state, exists, err := readRuntimeState(paths.State)
	if err != nil {
		return 0, err
	}
	if !exists || len(state.Groups) == 0 {
		return dialSlack, nil
	}
	const maxDuration = time.Duration(1<<63 - 1)
	if stopGrace > maxDuration/2 {
		return maxDuration, nil
	}
	perGroup := 2 * stopGrace
	groupBudget := time.Duration(0)
	for range state.Groups {
		if groupBudget > maxDuration-perGroup {
			return maxDuration, nil
		}
		groupBudget += perGroup
	}
	if groupBudget > maxDuration-dialSlack {
		return maxDuration, nil
	}
	return groupBudget + dialSlack, nil
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

	response, err := c.roundTripLocked(ctx, protocol.Hello{Op: protocol.OpHello, Version: wireVersion}, protocol.OpHello)
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

// StartupWarnings returns the latest daemon-lifetime reconciliation summary
// received on this client connection.
func (c *Client) StartupWarnings() []protocol.StartupWarning {
	if c == nil {
		return nil
	}
	c.warningsMu.Lock()
	defer c.warningsMu.Unlock()
	return append([]protocol.StartupWarning(nil), c.warnings...)
}

func (c *Client) recordWarnings(response protocol.ResponseEnvelope) {
	if c == nil || response.Warnings == nil {
		return
	}
	c.warningsMu.Lock()
	c.warnings = append([]protocol.StartupWarning(nil), response.Warnings...)
	c.warningsMu.Unlock()
}

func (c *Client) Start(ctx context.Context, req StartRequest) (app.Process, error) {
	req.Op = protocol.OpStart
	response, err := c.roundTrip(ctx, req, protocol.OpStart)
	if err != nil {
		return app.Process{}, err
	}
	if response.Start == nil || response.Start.Process == nil {
		return app.Process{}, errors.New("daemon start response omitted process")
	}
	return appProcessFromProtocol(*response.Start.Process), nil
}
func (c *Client) List(ctx context.Context, req ListRequest) ([]app.Process, error) {
	req.Op = protocol.OpList
	response, err := c.roundTrip(ctx, req, protocol.OpList)
	if err != nil {
		return nil, err
	}
	if response.List == nil {
		return nil, errors.New("daemon list response omitted payload")
	}
	items := make([]app.Process, 0, len(response.List.Processes))
	for _, item := range response.List.Processes {
		items = append(items, appProcessFromProtocol(item))
	}
	return items, nil
}
func (c *Client) Get(ctx context.Context, req GetRequest) (app.Process, error) {
	req.Op = protocol.OpGet
	response, err := c.roundTrip(ctx, req, protocol.OpGet)
	if err != nil {
		return app.Process{}, err
	}
	if response.Get == nil || response.Get.Process == nil {
		return app.Process{}, errors.New("daemon get response omitted process")
	}
	if response.Get.Process.NextCursor == nil {
		return app.Process{}, errors.New("daemon get response omitted next_cursor")
	}
	return appProcessFromProtocol(*response.Get.Process), nil
}
func (c *Client) Output(ctx context.Context, req OutputRequest) (output.ReadResult, error) {
	req.Op = protocol.OpOutput
	response, err := c.roundTrip(ctx, req, protocol.OpOutput)
	if err != nil {
		return output.ReadResult{}, err
	}
	return outputResultFromProtocol(response.Output), nil
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
	req.Op = protocol.OpWait
	response, err := c.roundTrip(ctx, req, protocol.OpWait)
	if err != nil {
		return app.WaitResult{}, err
	}
	if response.Wait == nil {
		return app.WaitResult{}, errors.New("daemon wait response omitted payload")
	}
	value := response.Wait
	outcome := app.WaitOutcome(value.Outcome)
	switch outcome {
	case app.WaitMatched, app.WaitExited, app.WaitTimedOut:
	default:
		return app.WaitResult{}, fmt.Errorf("daemon wait response has unknown outcome %q", value.Outcome)
	}
	processObserved := value.ProcessObserved
	result := app.WaitResult{Outcome: outcome, Cursor: output.Cursor(value.Cursor), ProcessObserved: processObserved}
	if value.Exit != nil {
		result.Exit = &processResult{ExitCode: value.Exit.Code, Err: errorFromString(value.Exit.Error), ExitedAt: value.Exit.Time}
		if value.Exit.Signal != nil {
			result.Exit.Signal = &process.SignalInfo{Name: value.Exit.Signal.Name, Number: value.Exit.Signal.Number}
		}
	}
	return result, nil
}

// Follow opens a fresh connection, preserving independent follower cursors.
func (c *Client) Follow(ctx context.Context, req FollowRequest) (*Follower, error) {
	if c.socket == "" {
		return nil, errors.New("client has no socket path for a follower")
	}
	followerClient, err := Dial(ctx, c.socket)
	if err != nil {
		if followerClient != nil {
			_ = followerClient.Close()
		}
		return nil, err
	}
	req.Op = protocol.OpFollow
	if err := followerClient.writeOnly(ctx, req, protocol.OpFollow); err != nil {
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
	if response.Event == nil || response.Event.Type != protocol.EventReady {
		_ = follower.Close()
		return nil, fmt.Errorf("daemon follow response omitted ready event")
	}
	return follower, nil
}

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
	if response.Event == nil {
		return output.Event{}, fmt.Errorf("unexpected follower response %q", response.Op)
	}
	event := response.Event
	if event.Type == protocol.EventExit && event.Exit != nil {
		exit := &output.Exit{Code: event.Exit.Code, Time: event.Exit.Time}
		if event.Exit.Signal != nil {
			exit.SignalName = event.Exit.Signal.Name
			exit.SignalNumber = event.Exit.Signal.Number
		}
		return output.Event{Exit: exit}, nil
	}
	return output.Event{Read: &output.ReadResult{Entries: entriesFromProtocol(event.Entries), Next: cursorFromProtocol(event.Next), Oldest: cursorFromProtocol(event.Oldest), Latest: cursorFromProtocol(event.Latest), EvictedThrough: cursorFromProtocol(event.EvictedThrough), Truncated: event.Truncated, More: event.More}}, nil
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

	// initialState and initialCursor never change after attach. One-shot input
	// uses these values rather than the mutable latest state so a successor
	// event cannot move a write across an incarnation boundary.
	initialState  string
	initialCursor protocol.Cursor

	events      chan protocol.InputStateEvent
	eventMu     sync.Mutex
	eventQueue  []protocol.InputStateEvent
	eventNotify chan struct{}
	eventClosed bool

	acks      chan json.RawMessage
	done      chan struct{}
	closeOnce sync.Once

	releaseOnce sync.Once
	releaseDone chan struct{}
	releaseErr  error
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
		if inputClient != nil {
			_ = inputClient.Close()
		}
		return nil, err
	}
	req.Op = protocol.OpInputAttach
	if err := inputClient.writeOnly(ctx, req, protocol.OpInputAttach); err != nil {
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
	if response.Op != protocol.OpInputAttach {
		_ = inputClient.Close()
		return nil, unexpectedResponseOp(string(protocol.OpInputAttach), string(response.Op))
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
	if event.Op != protocol.OpInputState {
		_ = inputClient.Close()
		return nil, unexpectedResponseOp(string(protocol.OpInputState), string(event.Op))
	}
	session := &InputSession{
		client: inputClient, state: event.State, cursor: event.LaunchCursor,
		initialState: event.State, initialCursor: event.LaunchCursor,
		events: make(chan protocol.InputStateEvent, 16), eventNotify: make(chan struct{}, 1),
		acks: make(chan json.RawMessage, 4), done: make(chan struct{}), releaseDone: make(chan struct{}),
	}
	go session.dispatchInputEvents()
	go session.readInputEvents()
	return session, nil
}

// Input performs one bounded, at-most-once write against an already-running
// TTY incarnation. It never starts a daemon or process, never waits for a
// launch, and releases the exclusive input lease on every terminal path.
func (c *Client) Input(ctx context.Context, req InputRequest) (InputResult, error) {
	if err := validateInputRequest(req); err != nil {
		return InputResult{}, err
	}
	session, err := c.InputAttach(ctx, InputAttachRequest{
		Op: protocol.OpInputAttach, Scope: req.Scope, Name: req.Name, Cwd: req.Cwd, Root: req.Root, TTY: true,
	})
	if err != nil {
		return InputResult{}, err
	}
	defer session.releaseOneShot()

	state, cursor := session.InitialState()
	if state != "running" {
		if err := session.releaseOneShot(); err != nil {
			return InputResult{}, err
		}
		return InputResult{}, &SessionNotRunningError{Name: req.Name}
	}
	if err := session.WriteAt(ctx, cursor, req.Data); err != nil {
		return InputResult{}, err
	}
	if err := session.releaseOneShot(); err != nil {
		return InputResult{}, err
	}
	return InputResult{Bytes: len(req.Data), LaunchCursor: cursor}, nil
}

func validateInputRequest(req InputRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return protocol.NewWireError(protocol.ErrorInvalidRequest, "input name is required", nil)
	}
	if strings.TrimSpace(req.Cwd) == "" {
		return protocol.NewWireError(protocol.ErrorInvalidRequest, "input cwd is required", nil)
	}
	if len(req.Data) == 0 {
		return protocol.NewWireError(protocol.ErrorInvalidRequest, "input payload must not be empty", nil)
	}
	if len(req.Data) > protocol.MaxInputBytes {
		return protocol.NewWireError(protocol.ErrorInputTooLarge, "input payload exceeds 32768 bytes", nil)
	}
	return nil
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

// InitialState reports the state event delivered as part of attach. It is
// immutable even when a later launch or exit event has already been received.
func (s *InputSession) InitialState() (string, protocol.Cursor) {
	if s == nil {
		return "", 0
	}
	return s.initialState, s.initialCursor
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
func (s *InputSession) writeRequest(ctx context.Context, req any, op protocol.Operation) error {
	if s == nil || s.client == nil {
		return net.ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writeRequestLocked(ctx, req, op)
}

func (s *InputSession) writeRequestLocked(ctx context.Context, req any, op protocol.Operation) error {
	if err := s.client.writeOnly(ctx, req, op); err != nil {
		return err
	}
	return s.waitForAck(ctx, op)
}

func (s *InputSession) waitForAck(ctx context.Context, want protocol.Operation) error {
	handle := func(raw json.RawMessage) error {
		var ack protocol.InputAckResponse
		if err := json.Unmarshal(raw, &ack); err != nil {
			return err
		}
		if ack.Error != nil {
			if ack.Op == "" {
				s.client.invalidate()
				return protocolErrorToError(ack.Error)
			}
		}
		if ack.Op != want {
			s.client.invalidate()
			return fmt.Errorf("daemon input response operation %q does not match %q", ack.Op, want)
		}
		if ack.Error != nil {
			return protocolErrorToError(ack.Error)
		}
		if !ack.OK {
			return errors.New("daemon input operation failed")
		}
		return nil
	}
	select {
	case raw := <-s.acks:
		return handle(raw)
	case <-s.done:
		// The reader queues a final acknowledgement before it can observe EOF
		// and close done. Prefer that acknowledgement when both are ready.
		select {
		case raw := <-s.acks:
			return handle(raw)
		default:
			return &protocol.WireError{Code: protocol.ErrorInputClosed, Message: "tty input connection is closed"}
		}
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
	return s.writeRequest(ctx, protocol.InputWriteRequest{Op: protocol.OpInputWrite, LaunchCursor: cursor, Data: base64.StdEncoding.EncodeToString(data)}, protocol.OpInputWrite)
}
func (s *InputSession) Resize(ctx context.Context, columns, rows uint16) error {
	_, cursor := s.State()
	return s.ResizeAt(ctx, cursor, columns, rows)
}
func (s *InputSession) ResizeAt(ctx context.Context, cursor protocol.Cursor, columns, rows uint16) error {
	if columns == 0 || rows == 0 {
		return &protocol.WireError{Code: protocol.ErrorInvalidRequest, Message: "tty dimensions must be non-zero"}
	}
	return s.writeRequest(ctx, protocol.InputResizeRequest{Op: protocol.OpInputResize, LaunchCursor: cursor, Columns: columns, Rows: rows}, protocol.OpInputResize)
}

// releaseOneShot sends the explicit input_release operation and waits for its
// acknowledgement. The daemon sends that acknowledgement only after it has
// cleared the durable lease, so a one-shot caller can attach the next owner
// immediately. If the transport is already closed (including cancellation or
// a lost write acknowledgement), closing it is the best available release
// signal and the existing server disconnect path performs the cleanup.
func (s *InputSession) releaseOneShot() error {
	if s == nil {
		return nil
	}
	s.releaseOnce.Do(func() {
		var err error
		if s.writeMu.TryLock() {
			err = s.writeRequestLocked(context.Background(), protocol.InputReleaseRequest{Op: protocol.OpInputRelease}, protocol.OpInputRelease)
			s.writeMu.Unlock()
		}
		// A failed release acknowledgement cannot establish that the server
		// received the request. Close the transport so its disconnect handler
		// releases the lease, without ever retrying input. Transport failures
		// are intentionally not returned: the server's disconnect path is the
		// release signal available on that terminal path.
		s.closeOnce.Do(func() { _ = s.client.Close() })
		<-s.done
		var wire *protocol.WireError
		if err != nil && errors.As(err, &wire) {
			s.releaseErr = err
		}
		close(s.releaseDone)
	})
	<-s.releaseDone
	return s.releaseErr
}

// Release uses an acknowledged detach when idle, so the caller may acquire a
// successor owner immediately. A concurrent operation is canceled by closing
// the transport instead of waiting behind its write lock.
func (s *InputSession) Release() error {
	if s == nil {
		return nil
	}
	_ = s.releaseOneShot()
	return nil
}
func (s *InputSession) Close() error { return s.Release() }

// ControlSignal forwards operator intent for an attached run. It is kept
// separate from Signal so public observational signal callers cannot suppress
// automatic relaunch policy accidentally.
func (c *Client) ControlSignal(ctx context.Context, req SignalRequest) error {
	req.Control = true
	_, err := c.SignalResult(ctx, req)
	return err
}
func (c *Client) Signal(ctx context.Context, req SignalRequest) error {
	_, err := c.SignalResult(ctx, req)
	return err
}
func (c *Client) SignalResult(ctx context.Context, req SignalRequest) (protocol.SignalResult, error) {
	req.Op = protocol.OpSignal
	response, err := c.roundTrip(ctx, req, protocol.OpSignal)
	if err != nil {
		return protocol.SignalResult{}, err
	}
	return protocolSignalResultFromResponse(response.Signal, req.Name)
}
func (c *Client) Stop(ctx context.Context, req StopRequest) error {
	req.Op = protocol.OpStop
	_, err := c.roundTrip(ctx, req, protocol.OpStop)
	return err
}
func (c *Client) Remove(ctx context.Context, req RemoveRequest) error {
	req.Op = protocol.OpRemove
	_, err := c.roundTrip(ctx, req, protocol.OpRemove)
	return err
}
func (c *Client) Restart(ctx context.Context, req RestartRequest) (app.Process, error) {
	req.Op = protocol.OpRestart
	response, err := c.roundTrip(ctx, req, protocol.OpRestart)
	if err != nil {
		return app.Process{}, err
	}
	if response.Restart == nil || response.Restart.Process == nil {
		return app.Process{}, errors.New("daemon restart response omitted process")
	}
	return appProcessFromProtocol(*response.Restart.Process), nil
}
func (c *Client) Shutdown(ctx context.Context, req ShutdownRequest) error {
	req.Op = protocol.OpShutdown
	_, err := c.roundTrip(ctx, req, protocol.OpShutdown)
	return err
}

func (c *Client) roundTrip(ctx context.Context, req any, op protocol.Operation) (protocol.ResponseEnvelope, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.roundTripLocked(ctx, req, op)
}
func (c *Client) roundTripLocked(ctx context.Context, req any, op protocol.Operation) (protocol.ResponseEnvelope, error) {
	var empty protocol.ResponseEnvelope
	if err := c.requestAllowed(string(op)); err != nil {
		return empty, err
	}
	cleanup, err := setConnContextWithCancel(c.conn, ctx)
	if err != nil {
		return empty, err
	}
	defer cleanup()
	if err := c.encoder.EncodeRequest(req); err != nil {
		if invalidateAfterWriteError(err) {
			c.invalidate()
		}
		return empty, contextError(ctx, err)
	}
	response, err := c.decoder.DecodeResponse()
	c.recordWarnings(response)
	if response.Error != nil {
		if response.Op == "" {
			c.invalidate()
			return response, wireErrorToError(response.Error)
		}
		if response.Op != op {
			c.invalidate()
			return response, unexpectedResponseOp(string(op), string(response.Op))
		}
		return response, wireErrorToError(response.Error)
	}
	if err != nil {
		c.invalidate()
		return response, contextError(ctx, err)
	}
	if response.Op != op {
		c.invalidate()
		return response, unexpectedResponseOp(string(op), string(response.Op))
	}
	return response, nil
}
func (c *Client) readResponse(ctx context.Context) (protocol.ResponseEnvelope, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var empty protocol.ResponseEnvelope
	if err := c.requestAllowed("event"); err != nil {
		return empty, err
	}
	cleanup, err := setConnContextWithCancel(c.conn, ctx)
	if err != nil {
		return empty, err
	}
	defer cleanup()
	response, err := c.decoder.DecodeResponse()
	c.recordWarnings(response)
	if response.Error != nil {
		if response.Op == "" {
			c.invalidate()
		}
		return response, wireErrorToError(response.Error)
	}
	if err != nil {
		c.invalidate()
		return response, contextError(ctx, err)
	}
	if response.Op != protocol.OpEvent {
		c.invalidate()
		return response, unexpectedResponseOp("event", string(response.Op))
	}
	return response, nil
}
func (c *Client) writeOnly(ctx context.Context, req any, op protocol.Operation) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requestAllowed(string(op)); err != nil {
		return err
	}
	cleanup, err := setConnContextWithCancel(c.conn, ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := c.encoder.EncodeRequest(req); err != nil {
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
	if ctx.Done() == nil {
		return func() { clearConnDeadline(conn) }, nil
	}
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			_ = conn.SetDeadline(time.Now())
		case <-stop:
		}
	}()
	return func() { close(stop); <-stopped; clearConnDeadline(conn) }, nil
}
func clearConnDeadline(conn net.Conn) { _ = conn.SetDeadline(time.Time{}) }

func unexpectedResponseOp(expected, actual string) error {
	return fmt.Errorf("daemon response operation %q does not match request operation %q", actual, expected)
}
