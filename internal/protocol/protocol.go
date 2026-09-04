// Package protocol defines the private newline-delimited JSON protocol used by
// hum clients and the local daemon. Wire messages are deliberately small,
// stable structs; presentation and process supervision stay outside this
// package.
package protocol

import (
	"encoding/json"
	"time"
)

// Version is the current private protocol version. The hello exchange carries
// this value on every connection.
const Version = 6

// CurrentVersion is an explicit alias for Version for callers that prefer a
// descriptive name.
const CurrentVersion = Version

// ProtocolVersion is an explicit alias for Version.
const ProtocolVersion = Version

// Operation identifies a request or response operation on the wire.
type Operation string

const (
	// OpHello starts the per-connection version handshake.
	OpHello Operation = "hello"
	// OpStart launches one supervised process.
	OpStart Operation = "start"
	// OpList lists supervised processes.
	OpList Operation = "list"
	// OpGet retrieves one supervised process.
	OpGet Operation = "get"
	// OpOutput reads one bounded output window.
	OpOutput Operation = "output"
	// OpFollow follows bounded output and lifecycle events.
	OpFollow Operation = "follow"
	// OpWait waits for matching output or process exit.
	OpWait Operation = "wait"
	// OpSignal forwards a signal to one supervised process group.
	OpSignal Operation = "signal"
	// OpStop stops one supervised process group.
	OpStop Operation = "stop"
	// OpRestart stops and relaunches one supervised process.
	OpRestart Operation = "restart"
	// OpRemove stops and discards one supervision session.
	OpRemove Operation = "remove"
	// OpShutdown retires the daemon.
	OpShutdown Operation = "shutdown"
	// OpEvent marks a streaming event response. It is not a client request.
	OpEvent Operation = "event"

	// OperationHello through OperationEvent are descriptive aliases for the
	// Op-prefixed constants.
	OperationHello    = OpHello
	OperationStart    = OpStart
	OperationList     = OpList
	OperationGet      = OpGet
	OperationOutput   = OpOutput
	OperationFollow   = OpFollow
	OperationWait     = OpWait
	OperationSignal   = OpSignal
	OperationStop     = OpStop
	OperationRestart  = OpRestart
	OperationRemove   = OpRemove
	OperationShutdown = OpShutdown
	OperationEvent    = OpEvent
)

// WaitOutcome identifies the terminal condition returned by a wait request.
type WaitOutcome string

const (
	// WaitMatched reports that a matching output entry was observed.
	WaitMatched WaitOutcome = "matched"
	// WaitExited reports that the process exited before a matching entry.
	WaitExited WaitOutcome = "exited"
	// WaitTimedOut reports that the wait deadline elapsed first.
	WaitTimedOut WaitOutcome = "timed_out"
)

var knownOperations = map[Operation]struct{}{
	OpHello: {}, OpStart: {}, OpList: {}, OpGet: {}, OpOutput: {},
	OpFollow: {}, OpWait: {}, OpSignal: {}, OpStop: {}, OpRestart: {}, OpRemove: {}, OpShutdown: {},
}

// IsKnown reports whether op is one of the protocol operations.
func IsKnown(op Operation) bool {
	_, ok := knownOperations[op]
	return ok
}

// Hello is the frozen per-connection hello wire shape. It intentionally has
// exactly two JSON fields: op and version.
type Hello struct {
	Op      Operation `json:"op"`
	Version int       `json:"version"`
}

// HelloRequest is the client-to-daemon hello shape.
type HelloRequest = Hello

// HelloResponse is the daemon's version-carrying hello response shape.
type HelloResponse = Hello

// NewHello returns a hello carrying the current protocol version.
func NewHello() Hello {
	return Hello{Op: OpHello, Version: Version}
}

// MarshalJSON keeps the hello operation frozen even when Op is zero or an
// incorrect value in a caller-created struct.
func (h Hello) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op      Operation `json:"op"`
		Version int       `json:"version"`
	}{Op: OpHello, Version: h.Version})
}

// UnmarshalJSON decodes the frozen hello shape and rejects a different op.
func (h *Hello) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op      Operation `json:"op"`
		Version int       `json:"version"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != OpHello {
		return &UnknownOperationError{Operation: wire.Op}
	}
	h.Op = OpHello
	h.Version = wire.Version
	return nil
}

// ShutdownRequest is the frozen shutdown request wire shape. Force requests
// that active processes be stopped before the daemon exits.
type ShutdownRequest struct {
	Op    Operation `json:"op"`
	Force bool      `json:"force"`
}

// Shutdown is a descriptive alias for ShutdownRequest.
type Shutdown = ShutdownRequest

// NewShutdownRequest builds a frozen shutdown request.
func NewShutdownRequest(force bool) ShutdownRequest {
	return ShutdownRequest{Op: OpShutdown, Force: force}
}

// MarshalJSON keeps the shutdown operation and fields frozen.
func (r ShutdownRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op    Operation `json:"op"`
		Force bool      `json:"force"`
	}{Op: OpShutdown, Force: r.Force})
}

// UnmarshalJSON decodes the frozen shutdown request and rejects a different
// operation when one is supplied.
func (r *ShutdownRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op    Operation `json:"op"`
		Force bool      `json:"force"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != OpShutdown {
		return &UnknownOperationError{Operation: wire.Op}
	}
	r.Op = OpShutdown
	r.Force = wire.Force
	return nil
}

// ReadinessConfig describes an output expression used to mark a resolved
// process ready. Timeout is represented as a Go duration on the in-process
// API and uses the standard duration JSON number on the wire.
type ReadinessConfig struct {
	Match   string        `json:"match"`
	Timeout time.Duration `json:"timeout"`
}

// StartRequest asks the daemon to launch one direct-argv process. Env is sent
// only in this request and is intentionally absent from every response DTO.
// Root is the explicit manifest project root used for supervisor keying; Cwd
// remains the child working directory.
type StartRequest struct {
	Op     Operation        `json:"op"`
	Name   string           `json:"name"`
	Argv   []string         `json:"argv"`
	Cwd    string           `json:"cwd"`
	Root   string           `json:"root,omitempty"`
	Env    []string         `json:"env"`
	Source string           `json:"source,omitempty"`
	Ready  *ReadinessConfig `json:"ready,omitempty"`
}

// NewStartRequest builds a process start request.
func NewStartRequest(name string, argv []string, cwd string, env []string) StartRequest {
	return StartRequest{Op: OpStart, Name: name, Argv: argv, Cwd: cwd, Env: env}
}

// MarshalJSON writes the stable start request fields in protocol order.
func (r StartRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op     Operation        `json:"op"`
		Name   string           `json:"name"`
		Argv   []string         `json:"argv"`
		Cwd    string           `json:"cwd"`
		Root   string           `json:"root,omitempty"`
		Env    []string         `json:"env"`
		Source string           `json:"source,omitempty"`
		Ready  *ReadinessConfig `json:"ready,omitempty"`
	}{Op: OpStart, Name: r.Name, Argv: r.Argv, Cwd: r.Cwd, Root: r.Root, Env: r.Env, Source: r.Source, Ready: r.Ready})
}

// UnmarshalJSON decodes a start request and validates its operation when
// present.
func (r *StartRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op     Operation        `json:"op"`
		Name   string           `json:"name"`
		Argv   []string         `json:"argv"`
		Cwd    string           `json:"cwd"`
		Root   string           `json:"root"`
		Env    []string         `json:"env"`
		Source string           `json:"source"`
		Ready  *ReadinessConfig `json:"ready"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != OpStart {
		return &UnknownOperationError{Operation: wire.Op}
	}
	r.Op = OpStart
	r.Name, r.Argv, r.Cwd, r.Root, r.Env = wire.Name, wire.Argv, wire.Cwd, wire.Root, wire.Env
	r.Source, r.Ready = wire.Source, wire.Ready
	return nil
}

// ListRequest asks for process snapshots in a project or across all projects.
type ListRequest struct {
	Op               Operation `json:"op"`
	Cwd              string    `json:"cwd"`
	All              bool      `json:"all,omitempty"`
	IncludeCompleted bool      `json:"include_completed,omitempty"`
}

// NewListRequest builds a process list request.
func NewListRequest(cwd string, all, includeCompleted bool) ListRequest {
	return ListRequest{Op: OpList, Cwd: cwd, All: all, IncludeCompleted: includeCompleted}
}

// MarshalJSON writes a list request with its stable operation.
func (r ListRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op               Operation `json:"op"`
		Cwd              string    `json:"cwd"`
		All              bool      `json:"all,omitempty"`
		IncludeCompleted bool      `json:"include_completed,omitempty"`
	}{Op: OpList, Cwd: r.Cwd, All: r.All, IncludeCompleted: r.IncludeCompleted})
}

// UnmarshalJSON decodes a list request.
func (r *ListRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op               Operation `json:"op"`
		Cwd              string    `json:"cwd"`
		All              bool      `json:"all"`
		IncludeCompleted bool      `json:"include_completed"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != OpList {
		return &UnknownOperationError{Operation: wire.Op}
	}
	r.Op, r.Cwd, r.All, r.IncludeCompleted = OpList, wire.Cwd, wire.All, wire.IncludeCompleted
	return nil
}

// GetRequest asks for one process snapshot.
type GetRequest struct {
	Op   Operation `json:"op"`
	Name string    `json:"name"`
	Cwd  string    `json:"cwd"`
}

// NewGetRequest builds a process lookup request.
func NewGetRequest(name, cwd string) GetRequest {
	return GetRequest{Op: OpGet, Name: name, Cwd: cwd}
}

// MarshalJSON writes a get request with its stable operation.
func (r GetRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op   Operation `json:"op"`
		Name string    `json:"name"`
		Cwd  string    `json:"cwd"`
	}{Op: OpGet, Name: r.Name, Cwd: r.Cwd})
}

// UnmarshalJSON decodes a get request.
func (r *GetRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op   Operation `json:"op"`
		Name string    `json:"name"`
		Cwd  string    `json:"cwd"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != OpGet {
		return &UnknownOperationError{Operation: wire.Op}
	}
	r.Op, r.Name, r.Cwd = OpGet, wire.Name, wire.Cwd
	return nil
}

// WaitRequest asks the daemon to wait for matching output or process exit.
// After is a strict-exclusive cursor filter; a nil pointer means the caller
// did not provide an explicit cursor. TimeoutMS is always carried in
// milliseconds and is validated by the daemon against its bounds.
type WaitRequest struct {
	Op        Operation `json:"op"`
	Name      string    `json:"name"`
	Cwd       string    `json:"cwd"`
	After     *Cursor   `json:"after,omitempty"`
	Match     string    `json:"match,omitempty"`
	TimeoutMS int64     `json:"timeout_ms"`
}

// NewWaitRequest builds a wait request with a timeout in milliseconds.
func NewWaitRequest(name, cwd string, timeoutMS int64) WaitRequest {
	return WaitRequest{Op: OpWait, Name: name, Cwd: cwd, TimeoutMS: timeoutMS}
}

// MarshalJSON writes a wait request with its stable operation and field
// ordering.
func (r WaitRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op        Operation `json:"op"`
		Name      string    `json:"name"`
		Cwd       string    `json:"cwd"`
		After     *Cursor   `json:"after,omitempty"`
		Match     string    `json:"match,omitempty"`
		TimeoutMS int64     `json:"timeout_ms"`
	}{Op: OpWait, Name: r.Name, Cwd: r.Cwd, After: r.After, Match: r.Match, TimeoutMS: r.TimeoutMS})
}

// UnmarshalJSON decodes a wait request and validates its operation when
// present.
func (r *WaitRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op        Operation `json:"op"`
		Name      string    `json:"name"`
		Cwd       string    `json:"cwd"`
		After     *Cursor   `json:"after"`
		Match     string    `json:"match"`
		TimeoutMS int64     `json:"timeout_ms"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != OpWait {
		return &UnknownOperationError{Operation: wire.Op}
	}
	r.Op, r.Name, r.Cwd = OpWait, wire.Name, wire.Cwd
	r.After, r.Match, r.TimeoutMS = wire.After, wire.Match, wire.TimeoutMS
	return nil
}

// Cursor is the monotonically increasing output sequence cursor.
type Cursor uint64

// Stream identifies the source selected by an output request or entry.
type Stream string

const (
	// StreamStdout is process standard output.
	StreamStdout Stream = "stdout"
	// StreamStderr is process standard error.
	StreamStderr Stream = "stderr"
	// StreamSystem is daemon-generated output.
	StreamSystem Stream = "system"
	// StreamBoth selects stdout and stderr.
	StreamBoth Stream = "both"
	// Stdout, Stderr, System, and Both are concise aliases.
	Stdout = StreamStdout
	Stderr = StreamStderr
	System = StreamSystem
	Both   = StreamBoth
)

// OutputRequest asks for one bounded retained-output read.
type OutputRequest struct {
	Op         Operation `json:"op"`
	Name       string    `json:"name"`
	Cwd        string    `json:"cwd"`
	After      *Cursor   `json:"after,omitempty"`
	Tail       int       `json:"tail,omitempty"`
	Stream     Stream    `json:"stream,omitempty"`
	Match      string    `json:"match,omitempty"`
	MaxEntries int       `json:"max_entries,omitempty"`
	MaxBytes   int       `json:"max_bytes,omitempty"`
}

// NewOutputRequest builds a bounded output read request.
func NewOutputRequest(name, cwd string) OutputRequest {
	return OutputRequest{Op: OpOutput, Name: name, Cwd: cwd}
}

func marshalOutputRequest(op Operation, r OutputRequest) ([]byte, error) {
	return json.Marshal(struct {
		Op         Operation `json:"op"`
		Name       string    `json:"name"`
		Cwd        string    `json:"cwd"`
		After      *Cursor   `json:"after,omitempty"`
		Tail       int       `json:"tail,omitempty"`
		Stream     Stream    `json:"stream,omitempty"`
		Match      string    `json:"match,omitempty"`
		MaxEntries int       `json:"max_entries,omitempty"`
		MaxBytes   int       `json:"max_bytes,omitempty"`
	}{Op: op, Name: r.Name, Cwd: r.Cwd, After: r.After, Tail: r.Tail, Stream: r.Stream, Match: r.Match, MaxEntries: r.MaxEntries, MaxBytes: r.MaxBytes})
}

// MarshalJSON writes an output request with its stable operation.
func (r OutputRequest) MarshalJSON() ([]byte, error) {
	return marshalOutputRequest(OpOutput, r)
}

func unmarshalOutputRequest(data []byte, r *OutputRequest, want Operation) error {
	var wire struct {
		Op         Operation `json:"op"`
		Name       string    `json:"name"`
		Cwd        string    `json:"cwd"`
		After      *Cursor   `json:"after"`
		Tail       int       `json:"tail"`
		Stream     Stream    `json:"stream"`
		Match      string    `json:"match"`
		MaxEntries int       `json:"max_entries"`
		MaxBytes   int       `json:"max_bytes"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != want {
		return &UnknownOperationError{Operation: wire.Op}
	}
	r.Op, r.Name, r.Cwd = want, wire.Name, wire.Cwd
	r.After, r.Tail, r.Stream, r.Match = wire.After, wire.Tail, wire.Stream, wire.Match
	r.MaxEntries, r.MaxBytes = wire.MaxEntries, wire.MaxBytes
	return nil
}

// UnmarshalJSON decodes an output request.
func (r *OutputRequest) UnmarshalJSON(data []byte) error {
	return unmarshalOutputRequest(data, r, OpOutput)
}

// FollowRequest asks for bounded output followed by independent stream events.
type FollowRequest struct {
	Op         Operation `json:"op"`
	Name       string    `json:"name"`
	Cwd        string    `json:"cwd"`
	After      *Cursor   `json:"after,omitempty"`
	Tail       int       `json:"tail,omitempty"`
	Stream     Stream    `json:"stream,omitempty"`
	Match      string    `json:"match,omitempty"`
	MaxEntries int       `json:"max_entries,omitempty"`
	MaxBytes   int       `json:"max_bytes,omitempty"`
}

// NewFollowRequest builds an output-follow request.
func NewFollowRequest(name, cwd string) FollowRequest {
	return FollowRequest{Op: OpFollow, Name: name, Cwd: cwd}
}

// MarshalJSON writes a follow request with its stable operation.
func (r FollowRequest) MarshalJSON() ([]byte, error) {
	return marshalOutputRequest(OpFollow, OutputRequest{Name: r.Name, Cwd: r.Cwd, After: r.After, Tail: r.Tail, Stream: r.Stream, Match: r.Match, MaxEntries: r.MaxEntries, MaxBytes: r.MaxBytes})
}

// UnmarshalJSON decodes a follow request.
func (r *FollowRequest) UnmarshalJSON(data []byte) error {
	var output OutputRequest
	if err := unmarshalOutputRequest(data, &output, OpFollow); err != nil {
		return err
	}
	r.Op, r.Name, r.Cwd = OpFollow, output.Name, output.Cwd
	r.After, r.Tail, r.Stream, r.Match = output.After, output.Tail, output.Stream, output.Match
	r.MaxEntries, r.MaxBytes = output.MaxEntries, output.MaxBytes
	return nil
}

// SignalRequest asks the daemon to forward Signal to one process group.
type SignalRequest struct {
	Op     Operation `json:"op"`
	Name   string    `json:"name"`
	Cwd    string    `json:"cwd"`
	Signal string    `json:"signal"`
}

// NewSignalRequest builds a signal-forward request. Signal uses names such as
// SIGINT, SIGTERM, and SIGKILL rather than platform-specific integer values.
func NewSignalRequest(name, cwd, signal string) SignalRequest {
	return SignalRequest{Op: OpSignal, Name: name, Cwd: cwd, Signal: signal}
}

// MarshalJSON writes a signal request with its stable operation.
func (r SignalRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op     Operation `json:"op"`
		Name   string    `json:"name"`
		Cwd    string    `json:"cwd"`
		Signal string    `json:"signal"`
	}{Op: OpSignal, Name: r.Name, Cwd: r.Cwd, Signal: r.Signal})
}

// UnmarshalJSON decodes a signal request.
func (r *SignalRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op     Operation `json:"op"`
		Name   string    `json:"name"`
		Cwd    string    `json:"cwd"`
		Signal string    `json:"signal"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != OpSignal {
		return &UnknownOperationError{Operation: wire.Op}
	}
	r.Op, r.Name, r.Cwd, r.Signal = OpSignal, wire.Name, wire.Cwd, wire.Signal
	return nil
}

// StopRequest asks the daemon to stop one process group.
type StopRequest struct {
	Op   Operation `json:"op"`
	Name string    `json:"name"`
	Cwd  string    `json:"cwd"`
}

// NewStopRequest builds a stop request.
func NewStopRequest(name, cwd string) StopRequest {
	return StopRequest{Op: OpStop, Name: name, Cwd: cwd}
}

// MarshalJSON writes a stop request with its stable operation.
func (r StopRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op   Operation `json:"op"`
		Name string    `json:"name"`
		Cwd  string    `json:"cwd"`
	}{Op: OpStop, Name: r.Name, Cwd: r.Cwd})
}

// UnmarshalJSON decodes a stop request.
func (r *StopRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op   Operation `json:"op"`
		Name string    `json:"name"`
		Cwd  string    `json:"cwd"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != OpStop {
		return &UnknownOperationError{Operation: wire.Op}
	}
	r.Op, r.Name, r.Cwd = OpStop, wire.Name, wire.Cwd
	return nil
}

// RemoveRequest asks the daemon to stop and discard one supervision session.
type RemoveRequest struct {
	Op   Operation `json:"op"`
	Name string    `json:"name"`
	Cwd  string    `json:"cwd"`
}

func NewRemoveRequest(name, cwd string) RemoveRequest {
	return RemoveRequest{Op: OpRemove, Name: name, Cwd: cwd}
}

func (r RemoveRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op   Operation `json:"op"`
		Name string    `json:"name"`
		Cwd  string    `json:"cwd"`
	}{Op: OpRemove, Name: r.Name, Cwd: r.Cwd})
}

func (r *RemoveRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op   Operation `json:"op"`
		Name string    `json:"name"`
		Cwd  string    `json:"cwd"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != OpRemove {
		return &UnknownOperationError{Operation: wire.Op}
	}
	r.Op, r.Name, r.Cwd = OpRemove, wire.Name, wire.Cwd
	return nil
}

// RestartRequest asks the daemon to restart one process. By default it uses
// the process's retained launch specification. Update requests replace that
// specification atomically before relaunching; Env is request-only and is
// never echoed in a response. Root is the explicit manifest root used for
// lookup and retained record keying; Cwd remains the update child directory.
type RestartRequest struct {
	Op     Operation        `json:"op"`
	Name   string           `json:"name"`
	Cwd    string           `json:"cwd"`
	Root   string           `json:"root,omitempty"`
	Update bool             `json:"update,omitempty"`
	Argv   []string         `json:"argv,omitempty"`
	Env    []string         `json:"env,omitempty"`
	Source string           `json:"source,omitempty"`
	Ready  *ReadinessConfig `json:"ready,omitempty"`
}

// NewRestartRequest builds a restart request that preserves the retained
// launch specification.
func NewRestartRequest(name, cwd string) RestartRequest {
	return RestartRequest{Op: OpRestart, Name: name, Cwd: cwd}
}

// MarshalJSON writes the stable restart request fields in protocol order.
func (r RestartRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op     Operation        `json:"op"`
		Name   string           `json:"name"`
		Cwd    string           `json:"cwd"`
		Root   string           `json:"root,omitempty"`
		Update bool             `json:"update,omitempty"`
		Argv   []string         `json:"argv,omitempty"`
		Env    []string         `json:"env,omitempty"`
		Source string           `json:"source,omitempty"`
		Ready  *ReadinessConfig `json:"ready,omitempty"`
	}{Op: OpRestart, Name: r.Name, Cwd: r.Cwd, Root: r.Root, Update: r.Update, Argv: r.Argv, Env: r.Env, Source: r.Source, Ready: r.Ready})
}

// UnmarshalJSON decodes a restart request.
func (r *RestartRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op     Operation        `json:"op"`
		Name   string           `json:"name"`
		Cwd    string           `json:"cwd"`
		Root   string           `json:"root"`
		Update bool             `json:"update"`
		Argv   []string         `json:"argv"`
		Env    []string         `json:"env"`
		Source string           `json:"source"`
		Ready  *ReadinessConfig `json:"ready"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != OpRestart {
		return &UnknownOperationError{Operation: wire.Op}
	}
	r.Op, r.Name, r.Cwd, r.Root = OpRestart, wire.Name, wire.Cwd, wire.Root
	r.Update, r.Argv, r.Env, r.Source, r.Ready = wire.Update, wire.Argv, wire.Env, wire.Source, wire.Ready
	return nil
}

// OutputEntry is one bounded output record carried over the wire.
type OutputEntry struct {
	Cursor Cursor    `json:"cursor"`
	Stream Stream    `json:"stream"`
	Time   time.Time `json:"time"`
	Text   string    `json:"text"`
}

// OutputResult is the bounded output payload shared by output responses and
// output stream events. Cursor metadata is explicit so a lagging follower can
// recover after eviction without unbounded server-side buffering.
type OutputResult struct {
	Entries        []OutputEntry `json:"entries"`
	Next           *Cursor       `json:"next,omitempty"`
	Oldest         *Cursor       `json:"oldest,omitempty"`
	Latest         *Cursor       `json:"latest,omitempty"`
	EvictedThrough *Cursor       `json:"evicted_through,omitempty"`
	Truncated      bool          `json:"truncated,omitempty"`
	More           bool          `json:"more,omitempty"`
}

// Output is a concise alias for OutputResult.
type Output = OutputResult

// ReadResult is a descriptive alias for OutputResult.
type ReadResult = OutputResult

// Exit describes a supervised process's terminal status.
type Exit struct {
	Code  int       `json:"code"`
	Time  time.Time `json:"time"`
	Error string    `json:"error,omitempty"`
}

// Readiness describes process readiness state and, when ready, the matching
// output cursor.
type Readiness struct {
	State  string    `json:"state"`
	Cursor *Cursor   `json:"cursor,omitempty"`
	Time   time.Time `json:"time,omitempty"`
	Match  string    `json:"match,omitempty"`
}

const (
	ReadinessStarting          = "starting"
	ReadinessReady             = "ready"
	ReadinessRunningUnverified = "running_unverified"
)

// Process is the response-safe process snapshot. It deliberately has no Env
// field; the environment supplied by StartRequest is never echoed.
type Process struct {
	Name         string     `json:"name"`
	Source       string     `json:"source,omitempty"`
	Root         string     `json:"root"`
	PID          int        `json:"pid"`
	PGID         int        `json:"pgid"`
	Cwd          string     `json:"cwd"`
	Argv         []string   `json:"argv"`
	Start        time.Time  `json:"start"`
	LaunchCursor Cursor     `json:"launch_cursor"`
	NextCursor   *Cursor    `json:"next_cursor,omitempty"`
	State        string     `json:"state"`
	Exit         *Exit      `json:"exit,omitempty"`
	ExitCode     int        `json:"exit_code,omitempty"`
	ExitedAt     time.Time  `json:"exited_at,omitempty"`
	RestartCount int        `json:"restart_count,omitempty"`
	Readiness    *Readiness `json:"readiness,omitempty"`
}

// ProcessResponse is a descriptive alias for Process.
type ProcessResponse = Process

// StartResponse reports the process created by a start request. It contains no
// environment field by design.
type StartResponse struct {
	Op      Operation  `json:"op"`
	OK      bool       `json:"ok"`
	Process *Process   `json:"process,omitempty"`
	Error   *WireError `json:"error,omitempty"`
}

// NewStartResponse builds a successful start response.
func NewStartResponse(process Process) StartResponse {
	return StartResponse{Op: OpStart, OK: true, Process: &process}
}

// ListResponse reports process snapshots.
type ListResponse struct {
	Op        Operation  `json:"op"`
	OK        bool       `json:"ok"`
	Processes []Process  `json:"processes,omitempty"`
	Error     *WireError `json:"error,omitempty"`
}

// NewListResponse builds a successful list response.
func NewListResponse(processes []Process) ListResponse {
	return ListResponse{Op: OpList, OK: true, Processes: processes}
}

// GetResponse reports one process snapshot.
type GetResponse struct {
	Op      Operation  `json:"op"`
	OK      bool       `json:"ok"`
	Process *Process   `json:"process,omitempty"`
	Error   *WireError `json:"error,omitempty"`
}

// NewGetResponse builds a successful get response.
func NewGetResponse(process Process) GetResponse {
	return GetResponse{Op: OpGet, OK: true, Process: &process}
}

// WaitResponse reports the terminal condition for a wait request. Cursor is
// always serialized so callers can resume from the consumed output watermark.
type WaitResponse struct {
	Op      Operation   `json:"op"`
	OK      bool        `json:"ok"`
	Outcome WaitOutcome `json:"outcome,omitempty"`
	Cursor  Cursor      `json:"cursor"`
	Exit    *Exit       `json:"exit,omitempty"`
	Error   *WireError  `json:"error,omitempty"`
}

// NewWaitResponse builds a successful wait response.
func NewWaitResponse(outcome WaitOutcome, cursor Cursor, exit *Exit) WaitResponse {
	return WaitResponse{Op: OpWait, OK: true, Outcome: outcome, Cursor: cursor, Exit: exit}
}

// MarshalJSON writes a wait response with its stable operation and field
// ordering.
func (r WaitResponse) MarshalJSON() ([]byte, error) {
	var outcome *WaitOutcome
	if r.OK {
		outcome = &r.Outcome
	}
	return json.Marshal(struct {
		Op      Operation    `json:"op"`
		OK      bool         `json:"ok"`
		Outcome *WaitOutcome `json:"outcome,omitempty"`
		Cursor  Cursor       `json:"cursor"`
		Exit    *Exit        `json:"exit,omitempty"`
		Error   *WireError   `json:"error,omitempty"`
	}{Op: OpWait, OK: r.OK, Outcome: outcome, Cursor: r.Cursor, Exit: r.Exit, Error: r.Error})
}

// UnmarshalJSON decodes a wait response and validates its operation when
// present.
func (r *WaitResponse) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op      Operation   `json:"op"`
		OK      bool        `json:"ok"`
		Outcome WaitOutcome `json:"outcome"`
		Cursor  Cursor      `json:"cursor"`
		Exit    *Exit       `json:"exit"`
		Error   *WireError  `json:"error"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != OpWait {
		return &UnknownOperationError{Operation: wire.Op}
	}
	r.Op, r.OK, r.Outcome, r.Cursor = OpWait, wire.OK, wire.Outcome, wire.Cursor
	r.Exit, r.Error = wire.Exit, wire.Error
	return nil
}

// OutputResponse reports one bounded output read.
type OutputResponse struct {
	Op             Operation     `json:"op"`
	OK             bool          `json:"ok"`
	Entries        []OutputEntry `json:"entries"`
	Next           *Cursor       `json:"next,omitempty"`
	Oldest         *Cursor       `json:"oldest,omitempty"`
	Latest         *Cursor       `json:"latest,omitempty"`
	EvictedThrough *Cursor       `json:"evicted_through,omitempty"`
	Truncated      bool          `json:"truncated,omitempty"`
	More           bool          `json:"more,omitempty"`
	Error          *WireError    `json:"error,omitempty"`
	Result         *OutputResult `json:"-"`
}

// NewOutputResponse builds a successful output response from a bounded read.
func NewOutputResponse(result OutputResult) OutputResponse {
	response := OutputResponse{Op: OpOutput, OK: true}
	response.setResult(result)
	return response
}

func (r *OutputResponse) setResult(result OutputResult) {
	r.Entries, r.Next, r.Oldest, r.Latest = result.Entries, result.Next, result.Oldest, result.Latest
	r.EvictedThrough, r.Truncated, r.More = result.EvictedThrough, result.Truncated, result.More
	r.Result = &result
}

// MarshalJSON keeps output response metadata flat and stable while retaining a
// convenient Result view for daemon callers.
func (r OutputResponse) MarshalJSON() ([]byte, error) {
	result := OutputResult{Entries: r.Entries, Next: r.Next, Oldest: r.Oldest, Latest: r.Latest, EvictedThrough: r.EvictedThrough, Truncated: r.Truncated, More: r.More}
	if r.Result != nil {
		result = *r.Result
	}
	return json.Marshal(struct {
		Op             Operation     `json:"op"`
		OK             bool          `json:"ok"`
		Entries        []OutputEntry `json:"entries"`
		Next           *Cursor       `json:"next,omitempty"`
		Oldest         *Cursor       `json:"oldest,omitempty"`
		Latest         *Cursor       `json:"latest,omitempty"`
		EvictedThrough *Cursor       `json:"evicted_through,omitempty"`
		Truncated      bool          `json:"truncated,omitempty"`
		More           bool          `json:"more,omitempty"`
		Error          *WireError    `json:"error,omitempty"`
	}{Op: OpOutput, OK: r.OK, Entries: result.Entries, Next: result.Next, Oldest: result.Oldest, Latest: result.Latest, EvictedThrough: result.EvictedThrough, Truncated: result.Truncated, More: result.More, Error: r.Error})
}

// UnmarshalJSON decodes a flat output response and populates Result.
func (r *OutputResponse) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op             Operation     `json:"op"`
		OK             bool          `json:"ok"`
		Entries        []OutputEntry `json:"entries"`
		Next           *Cursor       `json:"next"`
		Oldest         *Cursor       `json:"oldest"`
		Latest         *Cursor       `json:"latest"`
		EvictedThrough *Cursor       `json:"evicted_through"`
		Truncated      bool          `json:"truncated"`
		More           bool          `json:"more"`
		Error          *WireError    `json:"error"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.Op, r.OK, r.Error = OpOutput, wire.OK, wire.Error
	r.Entries, r.Next, r.Oldest, r.Latest = wire.Entries, wire.Next, wire.Oldest, wire.Latest
	r.EvictedThrough, r.Truncated, r.More = wire.EvictedThrough, wire.Truncated, wire.More
	r.Result = &OutputResult{Entries: r.Entries, Next: r.Next, Oldest: r.Oldest, Latest: r.Latest, EvictedThrough: r.EvictedThrough, Truncated: r.Truncated, More: r.More}
	return nil
}

// SignalResponse reports a signal-forward operation.
type SignalResponse struct {
	Op    Operation  `json:"op"`
	OK    bool       `json:"ok"`
	Error *WireError `json:"error,omitempty"`
}

// NewSignalResponse builds a successful signal response.
func NewSignalResponse() SignalResponse { return SignalResponse{Op: OpSignal, OK: true} }

// StopResponse reports a stop operation and its resulting process snapshot.
type StopResponse struct {
	Op      Operation  `json:"op"`
	OK      bool       `json:"ok"`
	Process *Process   `json:"process,omitempty"`
	Error   *WireError `json:"error,omitempty"`
}

// NewStopResponse builds a successful stop response.
func NewStopResponse(process *Process) StopResponse {
	return StopResponse{Op: OpStop, OK: true, Process: process}
}

// RestartResponse reports a successful relaunch and its new process snapshot.
type RestartResponse struct {
	Op      Operation  `json:"op"`
	OK      bool       `json:"ok"`
	Process *Process   `json:"process,omitempty"`
	Error   *WireError `json:"error,omitempty"`
}

// RemoveResponse reports completed runtime-session removal.
type RemoveResponse struct {
	Op    Operation  `json:"op"`
	OK    bool       `json:"ok"`
	Error *WireError `json:"error,omitempty"`
}

// NewRestartResponse builds a successful restart response.
func NewRestartResponse(process *Process) RestartResponse {
	return RestartResponse{Op: OpRestart, OK: true, Process: process}
}

// ShutdownResponse reports a successful daemon shutdown or its refusal.
type ShutdownResponse struct {
	Op        Operation  `json:"op"`
	OK        bool       `json:"ok"`
	Processes []Process  `json:"processes,omitempty"`
	Error     *WireError `json:"error,omitempty"`
}

// NewShutdownResponse builds a successful shutdown response.
func NewShutdownResponse(processes []Process) ShutdownResponse {
	return ShutdownResponse{Op: OpShutdown, OK: true, Processes: processes}
}

// EventType identifies one streaming event kind.
type EventType string

const (
	// EventOutput carries a bounded output result and cursor metadata.
	EventOutput EventType = "output"
	// EventCursor carries a cursor progress notification.
	EventCursor EventType = "cursor"
	// EventEviction carries explicit output eviction metadata.
	EventEviction EventType = "eviction"
	// EventExit carries process exit status after its output watermark drains.
	EventExit EventType = "exit"
	// EventReady carries process readiness.
	EventReady EventType = "ready"
	// EventError is a terminal stream error.
	EventError EventType = "error"

	// EventRead and EventTruncated are descriptive aliases used by callers for
	// output and eviction notifications.
	EventRead      = EventOutput
	EventTruncated = EventEviction
)

// StreamEvent is one bounded follow-stream notification. Output and eviction
// metadata remain cursor based; exit, readiness, and terminal errors each have
// a dedicated field. The Op field is always encoded as event.
type StreamEvent struct {
	Op             Operation     `json:"op"`
	Type           EventType     `json:"type"`
	Name           string        `json:"name,omitempty"`
	Entries        []OutputEntry `json:"entries,omitempty"`
	Next           *Cursor       `json:"next,omitempty"`
	Oldest         *Cursor       `json:"oldest,omitempty"`
	Latest         *Cursor       `json:"latest,omitempty"`
	EvictedThrough *Cursor       `json:"evicted_through,omitempty"`
	Truncated      bool          `json:"truncated,omitempty"`
	More           bool          `json:"more,omitempty"`
	Cursor         *Cursor       `json:"cursor,omitempty"`
	Ready          bool          `json:"ready,omitempty"`
	Time           time.Time     `json:"time,omitempty"`
	Exit           *Exit         `json:"exit,omitempty"`
	Error          *WireError    `json:"error,omitempty"`
	Result         *OutputResult `json:"-"`
}

const eventOperation = OpEvent

type Event = StreamEvent

// NewOutputEvent builds an output streaming event.
func NewOutputEvent(result OutputResult) StreamEvent {
	event := StreamEvent{Op: eventOperation, Type: EventOutput}
	event.setResult(result)
	return event
}

func (e *StreamEvent) setResult(result OutputResult) {
	e.Entries, e.Next, e.Oldest, e.Latest = result.Entries, result.Next, result.Oldest, result.Latest
	e.EvictedThrough, e.Truncated, e.More = result.EvictedThrough, result.Truncated, result.More
	e.Result = &result
}

// NewCursorEvent builds a cursor progress event.
func NewCursorEvent(cursor Cursor) StreamEvent {
	return StreamEvent{Op: eventOperation, Type: EventCursor, Cursor: &cursor}
}

// NewEvictionEvent builds an explicit eviction event.
func NewEvictionEvent(cursor Cursor) StreamEvent {
	return StreamEvent{Op: eventOperation, Type: EventEviction, EvictedThrough: &cursor}
}

// NewExitEvent builds a process exit event.
func NewExitEvent(exit Exit) StreamEvent {
	return StreamEvent{Op: eventOperation, Type: EventExit, Exit: &exit, Time: exit.Time}
}

// NewReadyEvent builds a readiness event. Cursor is optional when readiness is
// known without a matching output record.
func NewReadyEvent(cursor *Cursor) StreamEvent {
	return StreamEvent{Op: eventOperation, Type: EventReady, Ready: true, Cursor: cursor, Time: time.Now()}
}

// NewErrorEvent builds a terminal stream error event.
func NewErrorEvent(err *WireError) StreamEvent {
	return StreamEvent{Op: eventOperation, Type: EventError, Error: err}
}

// MarshalJSON keeps event output flat and fixes the event operation marker.
func (e StreamEvent) MarshalJSON() ([]byte, error) {
	result := OutputResult{Entries: e.Entries, Next: e.Next, Oldest: e.Oldest, Latest: e.Latest, EvictedThrough: e.EvictedThrough, Truncated: e.Truncated, More: e.More}
	if e.Result != nil {
		result = *e.Result
	}
	return json.Marshal(struct {
		Op             Operation     `json:"op"`
		Type           EventType     `json:"type"`
		Name           string        `json:"name,omitempty"`
		Entries        []OutputEntry `json:"entries,omitempty"`
		Next           *Cursor       `json:"next,omitempty"`
		Oldest         *Cursor       `json:"oldest,omitempty"`
		Latest         *Cursor       `json:"latest,omitempty"`
		EvictedThrough *Cursor       `json:"evicted_through,omitempty"`
		Truncated      bool          `json:"truncated,omitempty"`
		More           bool          `json:"more,omitempty"`
		Cursor         *Cursor       `json:"cursor,omitempty"`
		Ready          bool          `json:"ready,omitempty"`
		Time           time.Time     `json:"time,omitempty"`
		Exit           *Exit         `json:"exit,omitempty"`
		Error          *WireError    `json:"error,omitempty"`
	}{Op: eventOperation, Type: e.Type, Name: e.Name, Entries: result.Entries, Next: result.Next, Oldest: result.Oldest, Latest: result.Latest, EvictedThrough: result.EvictedThrough, Truncated: result.Truncated, More: result.More, Cursor: e.Cursor, Ready: e.Ready, Time: e.Time, Exit: e.Exit, Error: e.Error})
}

// UnmarshalJSON decodes a streaming event and populates Result for output
// events.
func (e *StreamEvent) UnmarshalJSON(data []byte) error {
	var wire struct {
		Op             Operation     `json:"op"`
		Type           EventType     `json:"type"`
		Name           string        `json:"name"`
		Entries        []OutputEntry `json:"entries"`
		Next           *Cursor       `json:"next"`
		Oldest         *Cursor       `json:"oldest"`
		Latest         *Cursor       `json:"latest"`
		EvictedThrough *Cursor       `json:"evicted_through"`
		Truncated      bool          `json:"truncated"`
		More           bool          `json:"more"`
		Cursor         *Cursor       `json:"cursor"`
		Ready          bool          `json:"ready"`
		Time           time.Time     `json:"time"`
		Exit           *Exit         `json:"exit"`
		Error          *WireError    `json:"error"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Op != "" && wire.Op != eventOperation {
		return &UnknownOperationError{Operation: wire.Op}
	}
	e.Op, e.Type, e.Name = eventOperation, wire.Type, wire.Name
	e.Entries, e.Next, e.Oldest, e.Latest = wire.Entries, wire.Next, wire.Oldest, wire.Latest
	e.EvictedThrough, e.Truncated, e.More = wire.EvictedThrough, wire.Truncated, wire.More
	e.Cursor, e.Ready, e.Time, e.Exit, e.Error = wire.Cursor, wire.Ready, wire.Time, wire.Exit, wire.Error
	e.Result = &OutputResult{Entries: e.Entries, Next: e.Next, Oldest: e.Oldest, Latest: e.Latest, EvictedThrough: e.EvictedThrough, Truncated: e.Truncated, More: e.More}
	return nil
}

// Response is a generic response envelope for daemon callers that dispatch
// operations dynamically. Operation-specific response DTOs remain preferable
// when the operation is known statically.
type Response struct {
	Op        Operation     `json:"op"`
	OK        bool          `json:"ok"`
	ID        string        `json:"id,omitempty"`
	Process   *Process      `json:"process,omitempty"`
	Processes []Process     `json:"processes,omitempty"`
	Entries   []OutputEntry `json:"entries,omitempty"`
	Result    *OutputResult `json:"result,omitempty"`
	Event     *StreamEvent  `json:"event,omitempty"`
	Error     *WireError    `json:"error,omitempty"`
}

// ErrorCode identifies a stable wire error class.
type ErrorCode string

const (
	// ErrorMalformed reports malformed JSON or an invalid line.
	ErrorMalformed ErrorCode = "malformed"
	// ErrorOversized reports a line beyond the configured bound.
	ErrorOversized ErrorCode = "oversized"
	// ErrorUnknownOperation reports an unrecognized op.
	ErrorUnknownOperation ErrorCode = "unknown_operation"
	// ErrorVersionMismatch reports an incompatible hello version.
	ErrorVersionMismatch ErrorCode = "version_mismatch"
	// ErrorInvalidRequest reports a rejected operation payload.
	ErrorInvalidRequest ErrorCode = "invalid_request"
	// ErrorNotFound reports a missing supervised process.
	ErrorNotFound ErrorCode = "not_found"
	// ErrorNameInUse reports a duplicate running process name.
	ErrorNameInUse ErrorCode = "name_in_use"
	// ErrorInvalidSignal reports a signal that cannot be forwarded.
	ErrorInvalidSignal ErrorCode = "invalid_signal"
	// ErrorSupervisorClosed reports a daemon that is shutting down.
	ErrorSupervisorClosed ErrorCode = "supervisor_closed"
	// ErrorOutput reports a bounded-output failure.
	ErrorOutput ErrorCode = "output_error"
	// ErrorInternal reports an unexpected daemon failure.
	ErrorInternal ErrorCode = "internal"
	// ErrorActiveProcesses reports shutdown refusal while processes run.
	ErrorActiveProcesses ErrorCode = "active_processes"
	// ErrorCodeMalformed through ErrorCodeActiveProcesses are aliases with an
	// explicit type-oriented prefix.
	ErrorCodeMalformed        = ErrorMalformed
	ErrorCodeOversized        = ErrorOversized
	ErrorCodeUnknownOperation = ErrorUnknownOperation
	ErrorCodeVersionMismatch  = ErrorVersionMismatch
	ErrorCodeInvalidRequest   = ErrorInvalidRequest
	ErrorCodeNotFound         = ErrorNotFound
	ErrorCodeNameInUse        = ErrorNameInUse
	ErrorCodeInvalidSignal    = ErrorInvalidSignal
	ErrorCodeSupervisorClosed = ErrorSupervisorClosed
	ErrorCodeOutput           = ErrorOutput
	ErrorCodeInternal         = ErrorInternal
	ErrorCodeActiveProcesses  = ErrorActiveProcesses
)

// WireError is the stable typed error representation sent in responses and
// terminal stream events.
type WireError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Details any       `json:"details,omitempty"`
}

// Error implements error using the stable wire message.
func (e WireError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

// NewWireError builds a typed wire error. Details should contain only
// response-safe data; in particular, never place a process environment there.
func NewWireError(code ErrorCode, message string, details any) *WireError {
	return &WireError{Code: code, Message: message, Details: details}
}

// ErrorResponse is a generic failed response envelope.
type ErrorResponse struct {
	Op    Operation  `json:"op"`
	OK    bool       `json:"ok"`
	Error *WireError `json:"error"`
}

// NewErrorResponse builds a failed response for op.
func NewErrorResponse(op Operation, err *WireError) ErrorResponse {
	return ErrorResponse{Op: op, Error: err}
}

// VersionMismatchDetails names both sides of a failed hello negotiation.
type VersionMismatchDetails struct {
	Client int `json:"client"`
	Daemon int `json:"daemon"`
}
