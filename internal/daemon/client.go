package daemon

import (
	"context"
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
type SignalRequest = protocol.SignalRequest
type StopRequest = protocol.StopRequest
type ShutdownRequest = protocol.ShutdownRequest

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
	response, err := c.roundTrip(ctx, wireRequest{Op: "start", Name: req.Name, Argv: append([]string(nil), req.Argv...), Cwd: req.Cwd, Env: append([]string(nil), req.Env...)})
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
	return &Follower{client: followerClient}, nil
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

func (c *Client) Signal(ctx context.Context, req SignalRequest) error {
	_, err := c.roundTrip(ctx, wireRequest{Op: "signal", Name: req.Name, Cwd: req.Cwd, Signal: req.Signal})
	return err
}

func (c *Client) Stop(ctx context.Context, req StopRequest) error {
	_, err := c.roundTrip(ctx, wireRequest{Op: "stop", Name: req.Name, Cwd: req.Cwd})
	return err
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
		value = protocol.StartRequest{Op: protocol.OpStart, Name: req.Name, Argv: req.Argv, Cwd: req.Cwd, Env: req.Env}
	case "list":
		value = protocol.ListRequest{Op: protocol.OpList, Cwd: req.Cwd, All: req.All, IncludeCompleted: req.IncludeCompleted}
	case "get":
		value = protocol.GetRequest{Op: protocol.OpGet, Name: req.Name, Cwd: req.Cwd}
	case "output":
		value = protocol.OutputRequest{Op: protocol.OpOutput, Name: req.Name, Cwd: req.Cwd, After: protocolCursorFromUint64(req.After), Tail: req.Tail, Stream: protocol.Stream(req.Stream), Match: req.Match, MaxEntries: req.MaxEntries, MaxBytes: req.MaxBytes}
	case "follow":
		value = protocol.FollowRequest{Op: protocol.OpFollow, Name: req.Name, Cwd: req.Cwd, After: protocolCursorFromUint64(req.After), Tail: req.Tail, Stream: protocol.Stream(req.Stream), Match: req.Match, MaxEntries: req.MaxEntries, MaxBytes: req.MaxBytes}
	case "signal":
		value = protocol.SignalRequest{Op: protocol.OpSignal, Name: req.Name, Cwd: req.Cwd, Signal: req.Signal}
	case "stop":
		value = protocol.StopRequest{Op: protocol.OpStop, Name: req.Name, Cwd: req.Cwd}
	case "shutdown":
		value = protocol.ShutdownRequest{Op: protocol.OpShutdown, Force: req.Force}
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
		Name: item.Name, Root: item.Root, PID: item.PID, PGID: item.PGID,
		Cwd: item.Cwd, Argv: append([]string(nil), item.Argv...), Start: item.Start,
		LaunchCursor: output.Cursor(item.LaunchCursor), State: app.State(item.State),
		ExitCode: item.ExitCode, ExitedAt: item.ExitedAt, RestartCount: item.RestartCount,
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
