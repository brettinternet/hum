package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"hum/internal/orchestrate"
	"hum/internal/protocol"
	sharedsignals "hum/internal/signals"
)

const (
	defaultTimeoutMS       = int64(30_000)
	automaticRelaunchLimit = 5
	maxSinceMilliseconds   = int64((1<<63 - 1) / int64(time.Millisecond))
)

func sinceCutoffUnixNano(cutoff time.Time) int64 {
	if cutoff.IsZero() {
		return 0
	}
	return cutoff.UnixNano()
}

func validateSinceMS(input commonInput) error {
	if input.SinceMS < 0 {
		return &ToolError{Code: "invalid_request", Message: "since_ms must be positive"}
	}
	if input.SinceMS > maxSinceMilliseconds {
		return &ToolError{Code: "invalid_request", Message: "since_ms is too large"}
	}
	if _, sinceSet := input.fields["since_ms"]; sinceSet && input.SinceMS == 0 {
		return &ToolError{Code: "invalid_request", Message: "since_ms must be positive"}
	}
	return nil
}

func captureSinceCutoff(input commonInput) (int64, error) {
	if err := validateSinceMS(input); err != nil {
		return 0, err
	}
	if input.SinceMS == 0 {
		return 0, nil
	}
	return sinceCutoffUnixNano(time.Now().Add(-time.Duration(input.SinceMS) * time.Millisecond)), nil
}

// ErrDaemonUnavailable identifies a missing daemon without coupling MCP to the daemon package.
var ErrDaemonUnavailable = errors.New("daemon unavailable")

// Definition is one explicit or discovered project process.
type Definition struct {
	Name    string
	Source  string
	Argv    []string
	Cwd     string
	Ready   *protocol.ReadinessConfig
	After   []string
	TTY     bool
	Restart string
}

// Resolution is the canonical project root and its process definitions.
type Resolution struct {
	Root        string
	Definitions []Definition
}

// InputRequest is the protocol-independent one-shot input seam used by MCP.
// Data is already decoded and is never retained by the adapter.
type InputRequest struct {
	Name string
	Cwd  string
	Root string
	Data []byte
}

// InputResult is the stable result returned by the input tool.
type InputResult struct {
	Name         string          `json:"name"`
	Bytes        int             `json:"bytes"`
	LaunchCursor protocol.Cursor `json:"launch_cursor"`
}

// SessionNotRunningError is a client-facing result derived from the initial
// stopped input_state event; it is not a daemon wire operation.
type SessionNotRunningError struct{ Name string }

func (e *SessionNotRunningError) Error() string {
	if e == nil || e.Name == "" {
		return "session is not running; start it with hum start NAME"
	}
	return fmt.Sprintf("session %q is not running; start it with hum start %s", e.Name, e.Name)
}

// Resolver applies the same nearest-Git-root-or-cwd fallback used by the CLI.
type Resolver interface {
	Resolve(context.Context, string) (Resolution, error)
}

// Client is the protocol-only daemon surface used by MCP. Production wiring wraps the CLI daemon client.
type Client interface {
	Start(context.Context, protocol.StartRequest) (protocol.Process, error)
	List(context.Context, protocol.ListRequest) ([]protocol.Process, error)
	Get(context.Context, protocol.GetRequest) (protocol.Process, error)
	Output(context.Context, protocol.OutputRequest) (protocol.OutputResult, error)
	Wait(context.Context, protocol.WaitRequest) (protocol.WaitResponse, error)
	Input(context.Context, InputRequest) (InputResult, error)
	SignalResult(context.Context, protocol.SignalRequest) (protocol.SignalResult, error)
	Stop(context.Context, protocol.StopRequest) error
	Remove(context.Context, protocol.RemoveRequest) error
	Restart(context.Context, protocol.RestartRequest) (protocol.Process, error)
	Close() error
}

type startupWarningReader interface {
	StartupWarnings() []protocol.StartupWarning
}

func startupWarnings(client Client) []protocol.StartupWarning {
	reader, ok := client.(startupWarningReader)
	if !ok {
		return nil
	}
	return reader.StartupWarnings()
}

// ClientFactory returns the shared CLI daemon client adapter. ensure is true only for start and up.
type ClientFactory func(context.Context, bool) (Client, error)

// Options supplies production adapters without introducing a second supervisor or wire client.
type Options struct {
	Resolver      Resolver
	ClientFactory ClientFactory
	Environment   func() []string
	Version       string
}

// Server is a stdio MCP server.
type Server struct{ opts Options }

// NewServer constructs an MCP server. Dependencies are checked when serving or calling a tool.
func NewServer(opts Options) *Server { return &Server{opts: opts} }

// ToolError is a stable, response-safe tool failure.
type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func (e *ToolError) Error() string {
	if e == nil {
		return "tool error"
	}
	return e.Message
}

func unavailable(err error) bool { return errors.Is(err, ErrDaemonUnavailable) }

func mapError(err error) *ToolError {
	if err == nil {
		return nil
	}
	var classified *orchestrate.Error
	if errors.As(err, &classified) && classified != nil {
		return &ToolError{Code: string(classified.KindValue()), Message: classified.Error()}
	}
	var tool *ToolError
	if errors.As(err, &tool) {
		return tool
	}
	var wire protocol.WireError
	if errors.As(err, &wire) {
		return &ToolError{Code: string(wire.Code), Message: wire.Error(), Details: wire.Details}
	}
	var wirePtr *protocol.WireError
	if errors.As(err, &wirePtr) && wirePtr != nil {
		return &ToolError{Code: string(wirePtr.Code), Message: wirePtr.Error(), Details: wirePtr.Details}
	}
	var notRunning *SessionNotRunningError
	if errors.As(err, &notRunning) {
		return &ToolError{Code: "session_not_running", Message: notRunning.Error()}
	}
	if unavailable(err) {
		return &ToolError{Code: "unavailable", Message: err.Error()}
	}
	return &ToolError{Code: "internal", Message: err.Error()}
}

type toolDefinition struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func (s *Server) toolDefinitions() []toolDefinition {
	root := stringProperty("Absolute path to an existing project directory; hum resolves its nearest Git root or uses the directory itself.")
	nameResolved := stringProperty("Declared or conventionally discovered process name.")
	nameExisting := stringProperty("Name of any existing project runtime record, including an ad_hoc process launched by hum run.")
	waitProps := map[string]any{
		"project_root": root,
		"no_wait":      map[string]any{"type": "boolean", "description": "Return after launch instead of waiting for readiness."},
		"timeout_ms":   map[string]any{"type": "integer", "minimum": 1, "description": "Readiness timeout in milliseconds; defaults to 30000."},
	}
	startProps := cloneProperties(waitProps)
	startProps["name"] = nameResolved
	restartProps := cloneProperties(waitProps)
	restartProps["name"] = nameExisting
	readiness := objectSchema(map[string]any{
		"state":  stringProperty("starting, ready, or running_unverified; recovery records retain starting with their configured matcher"),
		"cursor": map[string]any{"type": "integer", "minimum": 0},
		"time":   map[string]any{"type": "string"},
		"match":  map[string]any{"type": "string"},
	}, "state")
	signalInfo := objectSchema(map[string]any{
		"name":   map[string]any{"type": "string", "description": "Canonical SIG-prefixed signal name."},
		"number": map[string]any{"type": "integer", "minimum": 1, "description": "Signal number on the current Unix OS."},
	}, "name", "number")
	exit := objectSchema(map[string]any{
		"code":   map[string]any{"type": "integer"},
		"time":   map[string]any{"type": "string"},
		"error":  map[string]any{"type": "string"},
		"signal": signalInfo,
	}, "code", "time")
	startupWarning := objectSchema(map[string]any{"project": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}, "outcome": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}}, "project", "name", "outcome", "message")
	startupWarningsSchema := map[string]any{"type": "array", "items": startupWarning}
	process := objectSchema(map[string]any{
		"name": map[string]any{"type": "string"}, "source": map[string]any{"type": "string"},
		"root": map[string]any{"type": "string"}, "tty": map[string]any{"type": "boolean"}, "pid": map[string]any{"type": "integer"},
		"pgid": map[string]any{"type": "integer"}, "cwd": map[string]any{"type": "string"},
		"argv":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"start": map[string]any{"type": "string"}, "launch_cursor": map[string]any{"type": "integer", "minimum": 0},
		"next_cursor": map[string]any{"type": "integer", "minimum": 0}, "state": map[string]any{"type": "string"},
		"exit": exit, "exit_code": map[string]any{"type": "integer"}, "exited_at": map[string]any{"type": "string"},
		"restart_count":  map[string]any{"type": "integer", "minimum": 0},
		"followers":      map[string]any{"type": "integer", "minimum": 0, "description": "Live run and logs --follow clients attached to this supervision session."},
		"restart":        map[string]any{"type": "string", "enum": []string{"never", "on-failure"}},
		"relaunches":     map[string]any{"type": "integer", "minimum": 0, "maximum": 5},
		"next_launch_at": map[string]any{"type": "string"},
		"readiness":      readiness,
		"warnings":       startupWarningsSchema,
	}, "name", "source", "root", "tty", "cwd", "argv", "state", "launch_cursor", "followers", "restart", "relaunches")
	toolError := objectSchema(map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}}, "code", "message")
	launch := objectSchema(map[string]any{"name": map[string]any{"type": "string"}, "outcome": map[string]any{"type": "string"}, "process": process, "error": toolError, "blocked_by": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "existing_state": map[string]any{"type": "string", "enum": []string{"running", "stopped", "exited"}}, "changed_fields": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "guidance": map[string]any{"type": "string"}}, "name", "outcome")
	restart := objectSchema(map[string]any{
		"name":           map[string]any{"type": "string", "description": "The restarted process name."},
		"outcome":        map[string]any{"type": "string", "description": "restarted, running_unverified, exited_before_ready, timed_out, or error."},
		"readiness":      map[string]any{"type": "string", "description": "The replacement readiness state observed by this request."},
		"pid":            map[string]any{"type": "integer", "description": "The replacement process ID, or zero when no running process remains."},
		"launch_cursor":  map[string]any{"type": "integer", "minimum": 0, "description": "The output cursor assigned to the replacement launch."},
		"message":        map[string]any{"type": "string", "description": "Optional detail for a readiness or request failure."},
		"source":         map[string]any{"type": "string"},
		"root":           map[string]any{"type": "string"},
		"tty":            map[string]any{"type": "boolean", "description": "Whether the replacement owns a pseudo-terminal."},
		"pgid":           map[string]any{"type": "integer"},
		"cwd":            map[string]any{"type": "string"},
		"argv":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"start":          map[string]any{"type": "string"},
		"next_cursor":    map[string]any{"type": "integer", "minimum": 0},
		"state":          map[string]any{"type": "string"},
		"exit":           exit,
		"exit_code":      map[string]any{"type": "integer"},
		"exited_at":      map[string]any{"type": "string"},
		"restart_count":  map[string]any{"type": "integer", "minimum": 0},
		"followers":      map[string]any{"type": "integer", "minimum": 0},
		"restart":        map[string]any{"type": "string", "enum": []string{"never", "on-failure"}},
		"relaunches":     map[string]any{"type": "integer", "minimum": 0, "maximum": 5},
		"next_launch_at": map[string]any{"type": "string"},
	}, "name", "outcome", "readiness", "pid", "launch_cursor")
	stop := objectSchema(map[string]any{"name": map[string]any{"type": "string"}, "state": map[string]any{"type": "string"}, "error": toolError}, "name", "state")
	outputEntry := objectSchema(map[string]any{"cursor": map[string]any{"type": "integer", "minimum": 0}, "stream": map[string]any{"type": "string"}, "time": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}}, "cursor", "stream", "time", "text")
	output := objectSchema(map[string]any{"entries": map[string]any{"type": "array", "items": outputEntry}, "next": map[string]any{"type": "integer", "minimum": 0}, "oldest": map[string]any{"type": "integer", "minimum": 0}, "latest": map[string]any{"type": "integer", "minimum": 0}, "evicted_through": map[string]any{"type": "integer", "minimum": 0}, "truncated": map[string]any{"type": "boolean"}, "more": map[string]any{"type": "boolean"}}, "entries")
	wait := objectSchema(map[string]any{"op": map[string]any{"type": "string"}, "ok": map[string]any{"type": "boolean"}, "outcome": map[string]any{"type": "string"}, "cursor": map[string]any{"type": "integer", "minimum": 0}, "exit": exit, "process_observed": map[string]any{"type": "boolean", "description": "On timeout, true when a matching runtime record existed initially or at any point during this wait request; false means no process record was observed."}, "message": map[string]any{"type": "string", "description": "Actionable guidance when a timeout observed no process record."}}, "op", "ok", "cursor")
	inputText := map[string]any{"type": "string", "minLength": 1, "description": "Exact UTF-8 text bytes; no newline is appended."}
	inputBase64 := map[string]any{
		"type": "string", "minLength": 1, "maxLength": 43692,
		"pattern":         "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$",
		"contentEncoding": "base64", "description": "Standard padded base64 without whitespace for 1-32768 decoded bytes.",
	}
	inputSchema := objectSchema(map[string]any{
		"project_root": root, "name": nameExisting, "text": inputText, "base64": inputBase64,
	}, "project_root", "name")
	inputSchema["oneOf"] = []any{
		objectSchema(map[string]any{"project_root": root, "name": nameExisting, "text": inputText}, "project_root", "name", "text"),
		objectSchema(map[string]any{"project_root": root, "name": nameExisting, "base64": inputBase64}, "project_root", "name", "base64"),
	}
	inputResult := objectSchema(map[string]any{
		"name":          map[string]any{"type": "string"},
		"bytes":         map[string]any{"type": "integer", "minimum": 1},
		"launch_cursor": map[string]any{"type": "integer", "minimum": 0},
	}, "name", "bytes", "launch_cursor")
	signalResult := objectSchema(map[string]any{
		"name":   map[string]any{"type": "string"},
		"signal": signalInfo,
		"status": map[string]any{"type": "string", "enum": []string{"sent"}},
	}, "name", "signal", "status")
	signalSchema := objectSchema(map[string]any{
		"project_root": root,
		"name":         nameExisting,
		"signal":       stringProperty("Case-insensitive signal name with an optional SIG prefix, or a positive decimal value present in the supported named signal table."),
	}, "project_root", "name", "signal")
	collectionResults := func(items map[string]any) map[string]any {
		return objectSchema(map[string]any{"results": map[string]any{"type": "array", "items": items}, "warnings": startupWarningsSchema}, "results")
	}
	collectionProcesses := objectSchema(map[string]any{"processes": map[string]any{"type": "array", "items": process}, "warnings": startupWarningsSchema}, "processes")
	return []toolDefinition{
		{Name: "start", Description: "Start one explicitly named resolved project definition through the hum daemon; it never pulls in after prerequisites and waits for that definition's configured readiness by default. A running or recovery-capable manifest record whose argv, cwd, readiness matcher, tty, or restart policy changed returns definition_drift with sorted changed_fields and hum restart NAME guidance; only restart applies a changed definition. Manifest restart: on-failure uses bounded crash relaunches; discovered definitions remain never.", InputSchema: objectSchema(startProps, "project_root", "name"), OutputSchema: launch},
		{Name: "up", Description: "Start every resolved project definition through the hum daemon in declared after dependency order; independent roots launch concurrently and each prerequisite must be observed ready before its dependent launches. Skips report sorted direct blocked_by names plus any retained existing_state and process snapshot without lifecycle mutation. A changed running or recovery-capable manifest record returns definition_drift with sorted changed_fields and hum restart NAME guidance. Manifest-sourced running, pending-recovery, or exhausted records absent from the current declarations are returned as lexical removed_definition warnings with hum stop NAME or hum remove NAME guidance; these warnings do not change aggregate status and never include ad_hoc or discovered records; removed records require an explicit stop or remove. no_wait is rejected before daemon contact when after is declared; readiness timeouts begin per launch. During bounded on-failure recovery, an exited declaration returns recovery_pending or recovery_exhausted without a start request or waiting for an automatic successor. Use targeted start or restart to cancel recovery and launch immediately. Manifest restart: on-failure uses bounded crash relaunches; discovered definitions remain never.", InputSchema: objectSchema(waitProps, "project_root"), OutputSchema: collectionResults(launch)},
		{Name: "down", Description: "Stop every running runtime record in the project and return one result per name; does not shut down the daemon.", InputSchema: objectSchema(map[string]any{"project_root": root}, "project_root"), OutputSchema: collectionResults(stop)},
		{Name: "list", Description: "Merge resolved definitions with all daemon runtime records in the project, including ad_hoc records. Snapshots include restart, relaunches, and pending next_launch_at.", InputSchema: objectSchema(map[string]any{"project_root": root}, "project_root"), OutputSchema: collectionProcesses},
		{Name: "status", Description: "Return one existing declared or ad_hoc runtime record; this tool never creates a daemon. Snapshots include restart, relaunches, and pending next_launch_at.", InputSchema: objectSchema(map[string]any{"project_root": root, "name": nameExisting}, "project_root", "name"), OutputSchema: process},
		{Name: "logs", Description: "Read a bounded cursor-based output window for an existing declared or ad_hoc runtime record. since_ms uses one request-time cutoff and includes entries at or after it; it composes with the cursor, tail, and entry/byte bounds. Child output is terminal-control-stripped per entry; system entries, stored bytes, cursors, and limit accounting remain raw.", InputSchema: objectSchema(map[string]any{"project_root": root, "name": nameExisting, "after": map[string]any{"type": "integer", "minimum": 0, "description": "Exclusive output cursor to read from; omitting it selects the newest default window."}, "since_ms": map[string]any{"type": "integer", "minimum": 1, "maximum": maxSinceMilliseconds, "description": "Positive duration in milliseconds from the request time; entries at or after the computed cutoff are included."}, "tail": map[string]any{"type": "integer", "minimum": 0, "description": "Return at most this many of the most recent entries; omitting it uses the newest default window."}, "max_entries": map[string]any{"type": "integer", "minimum": 1, "description": "Maximum number of entries to return in this window."}, "max_bytes": map[string]any{"type": "integer", "minimum": 1, "description": "Maximum total text bytes to return across this window's entries."}}, "project_root", "name"), OutputSchema: output},
		{Name: "wait", Description: "Wait for output or exit on an existing declared or ad_hoc runtime record; defaults after to the current launch cursor and timeout to 30000 ms. Timeout results include process_observed from the same daemon wait request without an extra round trip; false means no runtime record for NAME was observed and includes actionable guidance.", InputSchema: objectSchema(map[string]any{"project_root": root, "name": nameExisting, "after": map[string]any{"type": "integer", "minimum": 0, "description": "Exclusive output cursor to wait from; omitting it waits from the current launch cursor."}, "match": map[string]any{"type": "string", "description": "Regular expression that resolves the wait early when it matches new output."}, "timeout_ms": map[string]any{"type": "integer", "minimum": 1, "description": "Maximum time to wait in milliseconds; defaults to 30000."}}, "project_root", "name"), OutputSchema: wait},
		{Name: "input", Description: "Write one exact, bounded payload to an already-running TTY incarnation at its initial launch cursor with at-most-once behavior; never starts, waits, queues, retries, resends, retains, or explicitly echoes input and fails immediately on ownership conflict.", InputSchema: inputSchema, OutputSchema: inputResult},
		{Name: "restart", Description: "Restart a resolved definition using the current server environment, or an existing retained ad_hoc record using its recorded launch specification. By default it waits for the replacement incarnation to become ready or running_unverified when no matcher exists; no_wait returns after spawn and timeout_ms is a positive per-name readiness limit.", InputSchema: objectSchema(restartProps, "project_root", "name"), OutputSchema: restart},
		{Name: "stop", Description: "Stop one existing declared or ad_hoc runtime record while preserving its supervision session.", InputSchema: objectSchema(map[string]any{"project_root": root, "name": nameExisting}, "project_root", "name"), OutputSchema: stop},
		{Name: "remove", Description: "Stop and discard one runtime supervision session, its retained launch specification, and output.", InputSchema: objectSchema(map[string]any{"project_root": root, "name": nameExisting}, "project_root", "name"), OutputSchema: stop},
		{Name: "signal", Description: "Send one observational signal to a running declared or ad_hoc process group without changing stop intent or automatic relaunch policy. Signal names are case-insensitive with an optional SIG prefix, and positive decimal values are accepted only when they map to the supported named signal table; the result is canonical and reports sent.", InputSchema: signalSchema, OutputSchema: signalResult},
	}
}

func cloneProperties(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src)+1)
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

type commonInput struct {
	ProjectRoot   string  `json:"project_root"`
	Name          string  `json:"name,omitempty"`
	NoWait        bool    `json:"no_wait,omitempty"`
	TimeoutMS     int64   `json:"timeout_ms,omitempty"`
	After         *uint64 `json:"after,omitempty"`
	SinceMS       int64   `json:"since_ms,omitempty"`
	SinceUnixNano int64   `json:"-"`
	Tail          int     `json:"tail,omitempty"`
	MaxEntries    int     `json:"max_entries,omitempty"`
	MaxBytes      int     `json:"max_bytes,omitempty"`
	Match         string  `json:"match,omitempty"`
	Text          *string `json:"text,omitempty"`
	Base64        *string `json:"base64,omitempty"`
	Signal        string  `json:"signal,omitempty"`

	textSet   bool
	base64Set bool
	fields    map[string]json.RawMessage
	Data      []byte
}

func decodeInput(raw json.RawMessage) (commonInput, error) {
	var input commonInput
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		return input, &ToolError{Code: "invalid_request", Message: "invalid tool arguments: " + err.Error()}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return input, &ToolError{Code: "invalid_request", Message: "invalid tool arguments: " + err.Error()}
	}
	input.fields = fields
	_, input.textSet = fields["text"]
	_, input.base64Set = fields["base64"]
	if input.ProjectRoot == "" || !filepath.IsAbs(input.ProjectRoot) {
		return input, &ToolError{Code: "invalid_request", Message: "project_root must be an absolute existing directory"}
	}
	info, err := os.Stat(input.ProjectRoot)
	if err != nil || !info.IsDir() {
		return input, &ToolError{Code: "invalid_request", Message: "project_root must be an absolute existing directory"}
	}
	return input, nil
}

func decodeInputPayload(input commonInput) ([]byte, error) {
	if input.textSet == input.base64Set {
		return nil, &ToolError{Code: "invalid_request", Message: "input requires exactly one of text or base64"}
	}
	if input.textSet {
		if input.Text == nil || *input.Text == "" {
			return nil, &ToolError{Code: "invalid_request", Message: "text must be a non-empty string"}
		}
		data := []byte(*input.Text)
		if len(data) > protocol.MaxInputBytes {
			return nil, &ToolError{Code: string(protocol.ErrorInputTooLarge), Message: fmt.Sprintf("input payload exceeds %d bytes", protocol.MaxInputBytes)}
		}
		return data, nil
	}
	if input.Base64 == nil || *input.Base64 == "" {
		return nil, &ToolError{Code: "invalid_request", Message: "base64 must be a non-empty string"}
	}
	value := *input.Base64
	for _, runeValue := range value {
		if unicode.IsSpace(runeValue) {
			return nil, &ToolError{Code: "invalid_request", Message: "base64 must not contain whitespace"}
		}
	}
	data, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(data) == 0 {
		return nil, &ToolError{Code: "invalid_request", Message: "base64 must be standard padded base64 for at least one byte"}
	}
	if len(data) > protocol.MaxInputBytes {
		return nil, &ToolError{Code: string(protocol.ErrorInputTooLarge), Message: fmt.Sprintf("input payload exceeds %d bytes", protocol.MaxInputBytes)}
	}
	return data, nil
}

func (s *Server) resolve(ctx context.Context, root string) (Resolution, error) {
	if s == nil || s.opts.Resolver == nil {
		return Resolution{}, errors.New("MCP resolver is not configured")
	}
	resolution, err := s.opts.Resolver.Resolve(ctx, root)
	if err != nil {
		return Resolution{}, err
	}
	if resolution.Root == "" {
		return Resolution{}, errors.New("resolver returned an empty project root")
	}
	return resolution, nil
}

func (s *Server) client(ctx context.Context, ensure bool) (Client, error) {
	if s == nil || s.opts.ClientFactory == nil {
		return nil, errors.New("MCP daemon client is not configured")
	}
	return s.opts.ClientFactory(ctx, ensure)
}

func findDefinition(resolution Resolution, name string) (Definition, bool) {
	for _, definition := range resolution.Definitions {
		if definition.Name == name {
			return definition, true
		}
	}
	return Definition{}, false
}

func effectiveRestart(policy string) string {
	return orchestrate.EffectiveRestart(policy)
}

func normalizeProcess(process protocol.Process) protocol.Process {
	return protocolProcess(orchestrate.NormalizeProcess(orchestrateProcess(process)))
}

func mcpDefinitionDriftResult(resolution Resolution, definition Definition, process protocol.Process) launchResult {
	return mcpLaunchResult(definition, orchestrate.DefinitionDriftResult(resolution.Root, mcpDefinition(definition), orchestrateProcess(process)))
}

func mcpRemovedDefinitionResults(resolution Resolution, processes []protocol.Process) []launchResult {
	definitions := make([]orchestrate.Definition, 0, len(resolution.Definitions))
	for _, definition := range resolution.Definitions {
		definitions = append(definitions, mcpDefinition(definition))
	}
	sharedProcesses := make([]orchestrate.Process, 0, len(processes))
	for _, process := range processes {
		sharedProcesses = append(sharedProcesses, orchestrateProcess(process))
	}
	sharedResults := orchestrate.RemovedDefinitionResults(resolution.Root, definitions, sharedProcesses)
	results := make([]launchResult, 0, len(sharedResults))
	for _, shared := range sharedResults {
		definition := Definition{Name: shared.Name, Source: "manifest"}
		results = append(results, mcpLaunchResult(definition, shared))
	}
	return results
}

func mcpDefinition(definition Definition) orchestrate.Definition {
	shared := orchestrate.Definition{
		Name: definition.Name, Source: definition.Source, Argv: append([]string(nil), definition.Argv...),
		Cwd: definition.Cwd, After: append([]string(nil), definition.After...), TTY: definition.TTY,
		Restart: effectiveRestart(definition.Restart),
	}
	if definition.Ready != nil {
		shared.Ready = &orchestrate.ReadinessConfig{Match: definition.Ready.Match, Timeout: definition.Ready.Timeout}
	}
	return shared
}

func orchestrateProcess(process protocol.Process) orchestrate.Process {
	shared := orchestrate.Process{
		Name: process.Name, Source: process.Source, Root: process.Root, TTY: process.TTY,
		PID: process.PID, PGID: process.PGID, Cwd: process.Cwd, Argv: append([]string(nil), process.Argv...),
		Start: process.Start, LaunchCursor: uint64(process.LaunchCursor), State: process.State,
		ExitCode: process.ExitCode, ExitedAt: process.ExitedAt, RestartCount: process.RestartCount,
		Followers: process.Followers, Restart: process.Restart, Relaunches: process.Relaunches,
		NextLaunchAt: process.NextLaunchAt,
	}
	if process.NextCursor != nil {
		cursor := uint64(*process.NextCursor)
		shared.NextCursor = &cursor
	}
	if process.Exit != nil {
		shared.Exit = &orchestrate.Exit{Code: process.Exit.Code, Time: process.Exit.Time, Error: process.Exit.Error}
		if process.Exit.Signal != nil {
			shared.Exit.Signal = &orchestrate.SignalInfo{Name: process.Exit.Signal.Name, Number: process.Exit.Signal.Number}
		}
	}
	if process.Readiness != nil {
		readiness := &orchestrate.Readiness{State: process.Readiness.State, Time: process.Readiness.Time, Match: process.Readiness.Match}
		if process.Readiness.Cursor != nil {
			cursor := uint64(*process.Readiness.Cursor)
			readiness.Cursor = &cursor
		}
		shared.Readiness = readiness
	}
	return shared
}

func protocolProcess(process orchestrate.Process) protocol.Process {
	process = orchestrate.NormalizeProcess(process)
	result := protocol.Process{
		Name: process.Name, Source: process.Source, Root: process.Root, TTY: process.TTY,
		PID: process.PID, PGID: process.PGID, Cwd: process.Cwd, Argv: append([]string(nil), process.Argv...),
		Start: process.Start, LaunchCursor: protocol.Cursor(process.LaunchCursor), State: process.State,
		ExitCode: process.ExitCode, ExitedAt: process.ExitedAt, RestartCount: process.RestartCount,
		Followers: process.Followers, Restart: process.Restart, Relaunches: process.Relaunches,
		NextLaunchAt: process.NextLaunchAt,
	}
	if process.NextCursor != nil {
		cursor := protocol.Cursor(*process.NextCursor)
		result.NextCursor = &cursor
	}
	if process.Exit != nil {
		result.Exit = &protocol.Exit{Code: process.Exit.Code, Time: process.Exit.Time, Error: process.Exit.Error}
		if process.Exit.Signal != nil {
			result.Exit.Signal = &protocol.SignalInfo{Name: process.Exit.Signal.Name, Number: process.Exit.Signal.Number}
		}
	}
	if process.Readiness != nil {
		readiness := &protocol.Readiness{State: process.Readiness.State, Time: process.Readiness.Time, Match: process.Readiness.Match}
		if process.Readiness.Cursor != nil {
			cursor := protocol.Cursor(*process.Readiness.Cursor)
			readiness.Cursor = &cursor
		}
		result.Readiness = readiness
	}
	return result
}

func mcpLaunchResult(definition Definition, shared orchestrate.Result) launchResult {
	result := launchResult{Name: shared.Name, Outcome: shared.Outcome, BlockedBy: append([]string(nil), shared.BlockedBy...), ExistingState: shared.ExistingState, ChangedFields: append([]string(nil), shared.ChangedFields...), Guidance: shared.Guidance}
	if result.Name == "" {
		result.Name = definition.Name
	}
	if shared.Process != nil {
		process := protocolProcess(*shared.Process)
		result.Process = &process
	}
	if shared.Error != nil {
		result.Error = mapError(shared.Error)
	}
	return result
}

func stoppedProcess(root string, definition Definition) protocol.Process {
	return protocol.Process{Name: definition.Name, Source: definition.Source, Root: root, TTY: definition.TTY, Cwd: definition.Cwd, Argv: append([]string(nil), definition.Argv...), State: "stopped", Restart: effectiveRestart(definition.Restart)}
}

func (s *Server) environment() []string {
	if s.opts.Environment != nil {
		return append([]string(nil), s.opts.Environment()...)
	}
	return append([]string(nil), os.Environ()...)
}

func positiveTimeout(value int64) (int64, error) {
	if value == 0 {
		return defaultTimeoutMS, nil
	}
	if value < 1 {
		return 0, &ToolError{Code: "invalid_request", Message: "timeout_ms must be positive"}
	}
	return value, nil
}
func readinessTimeout(override int64, definition Definition) (int64, error) {
	if override < 0 {
		return 0, &ToolError{Code: "invalid_request", Message: "timeout_ms must be positive"}
	}
	var duration time.Duration
	if override != 0 {
		if override > int64((1<<63-1)/int64(time.Millisecond)) {
			return 0, &ToolError{Code: "invalid_request", Message: "timeout_ms is too large"}
		}
		duration = time.Duration(override) * time.Millisecond
	}
	resolved, err := orchestrate.ReadinessTimeout(duration, mcpDefinition(definition))
	if err != nil {
		return 0, &ToolError{Code: "invalid_request", Message: err.Error()}
	}
	return mcpTimeoutMilliseconds(resolved)
}

func (s *Server) callTool(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	known := false
	for _, definition := range s.toolDefinitions() {
		known = known || definition.Name == name
	}
	if !known {
		return nil, &ToolError{Code: "not_found", Message: fmt.Sprintf("unknown tool %q", name)}
	}
	input, err := decodeInput(raw)
	if err != nil {
		return nil, err
	}
	if name == "logs" {
		input.SinceUnixNano, err = captureSinceCutoff(input)
		if err != nil {
			return nil, err
		}
	}
	if name == "input" {
		for field := range input.fields {
			switch field {
			case "project_root", "name", "text", "base64":
			default:
				return nil, &ToolError{Code: "invalid_request", Message: fmt.Sprintf("unknown input field %q", field)}
			}
		}
		input.Data, err = decodeInputPayload(input)
		if err != nil {
			return nil, err
		}
	} else if input.textSet || input.base64Set {
		return nil, &ToolError{Code: "invalid_request", Message: "text and base64 are only valid for the input tool"}
	}
	if name != "signal" {
		if _, present := input.fields["signal"]; present {
			return nil, &ToolError{Code: "invalid_request", Message: "signal is only valid for the signal tool"}
		}
	}
	if name == "input" && strings.TrimSpace(input.Name) == "" {
		return nil, &ToolError{Code: "invalid_request", Message: "name is required"}
	}
	resolution, err := s.resolve(ctx, input.ProjectRoot)
	if err != nil {
		return nil, mapError(err)
	}
	if name != "up" && name != "down" && name != "list" && strings.TrimSpace(input.Name) == "" {
		return nil, &ToolError{Code: "invalid_request", Message: "name is required"}
	}
	switch name {
	case "start":
		return s.start(ctx, resolution, input)
	case "up":
		return s.up(ctx, resolution, input)
	case "down":
		return s.down(ctx, resolution)
	case "list":
		return s.list(ctx, resolution)
	case "status":
		return s.status(ctx, resolution, input.Name)
	case "logs":
		return s.logs(ctx, resolution, input)
	case "wait":
		return s.wait(ctx, resolution, input)
	case "input":
		return s.input(ctx, resolution, input)
	case "restart":
		return s.restart(ctx, resolution, input)
	case "stop":
		return s.stop(ctx, resolution, input.Name)
	case "remove":
		return s.remove(ctx, resolution, input.Name)
	case "signal":
		return s.signal(ctx, resolution, input)
	default:
		panic("unreachable")
	}
}
func (s *Server) ensureDefinition(ctx context.Context, client Client, resolution Resolution, definition Definition, preserveRecovery bool) (protocol.Process, bool, string, error) {
	shared := orchestrate.Ensure(ctx, resolution.Root, mcpDefinition(definition), s.environment(), preserveRecovery, orchestrate.EnsureOperations{
		Get: func(ctx context.Context, name, root string) (orchestrate.Process, error) {
			current, err := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: name, Cwd: root})
			return orchestrateProcess(current), err
		},
		Start: func(ctx context.Context, request orchestrate.StartRequest) (orchestrate.Process, error) {
			var ready *protocol.ReadinessConfig
			if request.Ready != nil {
				ready = &protocol.ReadinessConfig{Match: request.Ready.Match, Timeout: request.Ready.Timeout}
			}
			current, err := client.Start(ctx, protocol.StartRequest{Op: protocol.OpStart, Name: request.Name, Argv: append([]string(nil), request.Argv...), Cwd: request.Cwd, Root: request.Root, Env: append([]string(nil), request.Env...), Source: request.Source, Ready: ready, TTY: request.TTY, Restart: request.Restart})
			return orchestrateProcess(current), err
		},
		IsNotFound:  func(err error) bool { return mapError(err).Code == string(protocol.ErrorNotFound) },
		IsNameInUse: func(err error) bool { return mapError(err).Code == string(protocol.ErrorNameInUse) },
	})
	process := protocol.Process{}
	if shared.Result.Process != nil {
		process = protocolProcess(*shared.Result.Process)
	} else if shared.Process.Name != "" || shared.Process.State != "" {
		process = protocolProcess(shared.Process)
	}
	if shared.Result.Error != nil {
		return process, false, "", shared.Result.Error
	}
	classification := ""
	switch shared.Result.Outcome {
	case "definition_drift", "recovery_pending", "recovery_exhausted":
		classification = shared.Result.Outcome
	}
	return process, shared.Already, classification, nil
}

func (s *Server) mcpWaitForReadiness(ctx context.Context, client Client, resolution Resolution, definition Definition, process protocol.Process, initial string, timeout int64) (protocol.Process, string, error) {
	shared, err := orchestrate.WaitForReadiness(ctx, resolution.Root, mcpDefinition(definition), orchestrateProcess(process), initial, time.Duration(timeout)*time.Millisecond, orchestrate.ReadinessOperations{
		Get: func(ctx context.Context, name, root string) (orchestrate.Process, error) {
			current, err := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: name, Cwd: root})
			return orchestrateProcess(current), err
		},
		Wait: func(ctx context.Context, request orchestrate.WaitRequest) (orchestrate.WaitResult, error) {
			milliseconds, err := mcpTimeoutMilliseconds(request.Timeout)
			if err != nil {
				return orchestrate.WaitResult{}, err
			}
			waited, err := client.Wait(ctx, protocol.WaitRequest{Op: protocol.OpWait, Name: request.Name, Cwd: request.Cwd, Match: request.Match, TimeoutMS: milliseconds})
			result := orchestrate.WaitResult{Outcome: string(waited.Outcome), Cursor: uint64(waited.Cursor)}
			if waited.Exit != nil {
				result.Exit = &orchestrate.Exit{Code: waited.Exit.Code, Time: waited.Exit.Time, Error: waited.Exit.Error}
				if waited.Exit.Signal != nil {
					result.Exit.Signal = &orchestrate.SignalInfo{Name: waited.Exit.Signal.Name, Number: waited.Exit.Signal.Number}
				}
			}
			return result, err
		},
		IsNotFound: func(err error) bool { return mapError(err).Code == string(protocol.ErrorNotFound) },
	})
	if err != nil {
		return protocol.Process{}, "", err
	}
	if shared.Process == nil {
		return protocol.Process{}, shared.Outcome, nil
	}
	return protocolProcess(*shared.Process), shared.Outcome, nil
}

func mcpTimeoutMilliseconds(timeout time.Duration) (int64, error) {
	milliseconds := timeout / time.Millisecond
	if milliseconds <= 0 {
		milliseconds = 1
	}
	if milliseconds > (1<<63 - 1) {
		return 0, errors.New("timeout is too large")
	}
	return int64(milliseconds), nil
}

func (s *Server) start(ctx context.Context, resolution Resolution, input commonInput) (any, error) {
	definition, ok := findDefinition(resolution, input.Name)
	if !ok {
		client, err := s.client(ctx, true)
		if err != nil {
			return nil, mapError(err)
		}
		defer client.Close()
		process, err := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: input.Name, Cwd: resolution.Root})
		if err != nil {
			return nil, &ToolError{Code: "not_found", Message: fmt.Sprintf("process definition or retained session %q not found", input.Name)}
		}
		outcome := "already_running"
		if process.State != "running" {
			process, err = client.Start(ctx, protocol.StartRequest{Op: protocol.OpStart, Name: input.Name, Cwd: resolution.Root, Root: resolution.Root, TTY: process.TTY})
			if err != nil {
				return nil, mapError(err)
			}
			outcome = "started"
		}
		process = normalizeProcess(process)
		return launchResult{Name: input.Name, Outcome: outcome, Process: &process}, nil
	}
	timeout, err := readinessTimeout(input.TimeoutMS, definition)
	if err != nil {
		return nil, err
	}
	client, err := s.client(ctx, true)
	if err != nil {
		return nil, mapError(err)
	}
	defer client.Close()
	process, already, classification, err := s.ensureDefinition(ctx, client, resolution, definition, false)
	if err != nil {
		return nil, mapError(err)
	}
	if classification == "definition_drift" {
		return mcpDefinitionDriftResult(resolution, definition, process), nil
	}
	outcome := orchestrate.LaunchOutcome(already, mcpDefinition(definition))
	if definition.Ready == nil {
		process.Readiness = &protocol.Readiness{State: protocol.ReadinessRunningUnverified}
	} else if !input.NoWait {
		process, outcome, err = s.mcpWaitForReadiness(ctx, client, resolution, definition, process, outcome, timeout)
		if err != nil {
			return nil, mapError(err)
		}
	}
	process = normalizeProcess(process)
	return launchResult{Name: definition.Name, Outcome: outcome, Process: &process}, nil
}

type launchResult struct {
	Name          string            `json:"name"`
	Outcome       string            `json:"outcome"`
	Process       *protocol.Process `json:"process,omitempty"`
	Error         *ToolError        `json:"error,omitempty"`
	BlockedBy     []string          `json:"blocked_by,omitempty"`
	ExistingState string            `json:"existing_state,omitempty"`
	ChangedFields []string          `json:"changed_fields,omitempty"`
	Guidance      string            `json:"guidance,omitempty"`
}

func (s *Server) up(ctx context.Context, resolution Resolution, input commonInput) (any, error) {
	if input.TimeoutMS < 0 {
		return nil, &ToolError{Code: "invalid_request", Message: "timeout_ms must be positive"}
	}
	definitions := append([]Definition(nil), resolution.Definitions...)
	if input.NoWait && definitionsHaveAfter(definitions) {
		return nil, &ToolError{Code: "invalid_request", Message: "up no_wait is not allowed when definitions declare after dependencies"}
	}
	if len(definitions) == 0 {
		client, err := s.client(ctx, false)
		if err != nil {
			if unavailable(err) {
				return []launchResult{}, nil
			}
			return nil, mapError(err)
		}
		defer client.Close()
		recordStartupWarnings(ctx, startupWarnings(client))
		processes, err := client.List(ctx, protocol.ListRequest{Op: protocol.OpList, Cwd: resolution.Root, IncludeCompleted: true})
		if err != nil {
			return nil, mapError(err)
		}
		recordStartupWarnings(ctx, startupWarnings(client))
		return mcpRemovedDefinitionResults(resolution, processes), nil
	}
	client, err := s.client(ctx, true)
	if err != nil {
		return nil, mapError(err)
	}
	defer client.Close()
	recordStartupWarnings(ctx, startupWarnings(client))

	sharedDefinitions := make([]orchestrate.Definition, 0, len(definitions))
	for _, definition := range definitions {
		sharedDefinitions = append(sharedDefinitions, mcpDefinition(definition))
	}
	sharedResults, err := orchestrate.OrchestrateUp(ctx, orchestrate.UpOptions{
		Root: resolution.Root, Definitions: sharedDefinitions, NoWait: input.NoWait,
		TimeoutFor: func(definition orchestrate.Definition) (time.Duration, error) {
			return mcpTimeoutDuration(input.TimeoutMS, definition)
		},
	}, orchestrate.UpOperations{
		Start: func(ctx context.Context, definition orchestrate.Definition) (orchestrate.StartResult, error) {
			projectDefinition, ok := findDefinition(resolution, definition.Name)
			if !ok {
				return orchestrate.StartResult{Result: orchestrate.ErrorResult(definition, errors.New("definition not found"))}, nil
			}
			observedAt := time.Now()
			process, already, classification, startErr := s.ensureDefinition(ctx, client, resolution, projectDefinition, true)
			if startErr != nil {
				return orchestrate.StartResult{Result: orchestrate.ErrorResult(definition, startErr), Process: orchestrateProcess(process), ObservedAt: observedAt}, nil
			}
			if !already && !process.Start.IsZero() {
				observedAt = process.Start
			} else if already {
				observedAt = time.Now()
			}
			var result orchestrate.Result
			if classification == "definition_drift" {
				result = orchestrate.DefinitionDriftResult(resolution.Root, definition, orchestrateProcess(process))
			} else {
				outcome := classification
				if outcome == "" {
					outcome = orchestrate.LaunchOutcome(already, definition)
				}
				result = orchestrate.ResultForProcess(definition, orchestrateProcess(process), outcome)
			}
			return orchestrate.StartResult{Result: result, Process: orchestrateProcess(process), ObservedAt: observedAt, Already: already}, nil
		},
		Readiness: func(ctx context.Context, definition orchestrate.Definition, process orchestrate.Process, outcome string, timeout time.Duration) (orchestrate.Result, error) {
			projectDefinition, ok := findDefinition(resolution, definition.Name)
			if !ok {
				return orchestrate.ErrorResult(definition, errors.New("definition not found")), nil
			}
			current, currentOutcome, waitErr := s.mcpWaitForReadiness(ctx, client, resolution, projectDefinition, protocolProcess(process), outcome, durationToMCPTimeout(timeout))
			if waitErr != nil {
				return orchestrate.Result{}, waitErr
			}
			return orchestrate.ResultForProcess(definition, orchestrateProcess(current), currentOutcome), nil
		},
		Skipped: func(ctx context.Context, definition orchestrate.Definition, blocked []string) orchestrate.Result {
			return orchestrate.SkippedResult(ctx, resolution.Root, definition, blocked, func(ctx context.Context, name, root string) (orchestrate.Process, error) {
				current, err := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: name, Cwd: root})
				return orchestrateProcess(current), err
			})
		},
		List: func(ctx context.Context) ([]orchestrate.Process, error) {
			processes, err := client.List(ctx, protocol.ListRequest{Op: protocol.OpList, Cwd: resolution.Root, IncludeCompleted: true})
			if err != nil {
				return nil, err
			}
			shared := make([]orchestrate.Process, 0, len(processes))
			for _, process := range processes {
				shared = append(shared, orchestrateProcess(process))
			}
			return shared, nil
		},
	})
	if err != nil {
		return nil, mapError(err)
	}
	recordStartupWarnings(ctx, startupWarnings(client))
	results := make([]launchResult, 0, len(sharedResults))
	for _, shared := range sharedResults {
		definition, ok := findDefinition(resolution, shared.Name)
		if !ok {
			definition = Definition{Name: shared.Name, Source: "manifest"}
		}
		results = append(results, mcpLaunchResult(definition, shared))
	}
	return results, nil
}

func mcpTimeoutDuration(override int64, definition orchestrate.Definition) (time.Duration, error) {
	if override < 0 {
		return 0, &ToolError{Code: "invalid_request", Message: "timeout_ms must be positive"}
	}
	var duration time.Duration
	if override != 0 {
		if override > int64((1<<63-1)/int64(time.Millisecond)) {
			return 0, &ToolError{Code: "invalid_request", Message: "timeout_ms is too large"}
		}
		duration = time.Duration(override) * time.Millisecond
	}
	return orchestrate.ReadinessTimeout(duration, definition)
}

func durationToMCPTimeout(timeout time.Duration) int64 {
	if timeout <= 0 {
		return 0
	}
	milliseconds := timeout / time.Millisecond
	if timeout%time.Millisecond != 0 {
		milliseconds++
	}
	return int64(milliseconds)
}

func mcpRemainingTimeout(timeoutMS int64, launchedAt time.Time) int64 {
	if timeoutMS <= 0 {
		return timeoutMS
	}
	remaining := time.Duration(timeoutMS)*time.Millisecond - time.Since(launchedAt)
	return durationToMCPTimeout(remaining)
}

func definitionsHaveAfter(definitions []Definition) bool {
	shared := make([]orchestrate.Definition, 0, len(definitions))
	for _, definition := range definitions {
		shared = append(shared, mcpDefinition(definition))
	}
	return orchestrate.DefinitionsHaveAfter(shared)
}

func (s *Server) list(ctx context.Context, resolution Resolution) (any, error) {
	byName := make(map[string]protocol.Process, len(resolution.Definitions))
	for _, definition := range resolution.Definitions {
		byName[definition.Name] = stoppedProcess(resolution.Root, definition)
	}
	client, err := s.client(ctx, false)
	if err != nil {
		if unavailable(err) {
			return sortedProcesses(byName), nil
		}
		return nil, mapError(err)
	}
	defer client.Close()
	recordStartupWarnings(ctx, startupWarnings(client))
	processes, err := client.List(ctx, protocol.ListRequest{Op: protocol.OpList, Cwd: resolution.Root, IncludeCompleted: true})
	if err != nil {
		return nil, mapError(err)
	}
	recordStartupWarnings(ctx, startupWarnings(client))
	for _, process := range processes {
		byName[process.Name] = normalizeProcess(process)
	}
	return sortedProcesses(byName), nil
}

func sortedProcesses(byName map[string]protocol.Process) []protocol.Process {
	result := make([]protocol.Process, 0, len(byName))
	for _, process := range byName {
		result = append(result, process)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (s *Server) status(ctx context.Context, resolution Resolution, name string) (any, error) {
	client, err := s.client(ctx, false)
	if err != nil {
		return nil, mapError(err)
	}
	defer client.Close()
	recordStartupWarnings(ctx, startupWarnings(client))
	process, err := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: name, Cwd: resolution.Root})
	if err != nil {
		return nil, mapError(err)
	}
	recordStartupWarnings(ctx, startupWarnings(client))
	return normalizeProcess(process), nil
}

func (s *Server) logs(ctx context.Context, resolution Resolution, input commonInput) (any, error) {
	if input.Tail < 0 || input.MaxEntries < 0 || input.MaxBytes < 0 {
		return nil, &ToolError{Code: "invalid_request", Message: "log bounds cannot be negative"}
	}
	if err := validateSinceMS(input); err != nil {
		return nil, err
	}
	sinceUnixNano := input.SinceUnixNano
	if sinceUnixNano == 0 && input.SinceMS > 0 {
		sinceUnixNano = sinceCutoffUnixNano(time.Now().Add(-time.Duration(input.SinceMS) * time.Millisecond))
	}
	client, err := s.client(ctx, false)
	if err != nil {
		return nil, mapError(err)
	}
	defer client.Close()
	tail := input.Tail
	if input.After == nil && tail == 0 {
		_, explicitTail := input.fields["tail"]
		if !explicitTail {
			tail = protocol.DefaultReadEntries
		}
	}
	maxEntries := input.MaxEntries
	if tail > 0 && maxEntries == 0 {
		// The daemon caps a read at its own entry limit regardless of tail;
		// without this, a requested tail larger than that cap silently returns
		// the oldest portion of the window instead of the most recent entries.
		maxEntries = tail
	}
	request := protocol.OutputRequest{Op: protocol.OpOutput, Name: input.Name, Cwd: resolution.Root, SinceUnixNano: sinceUnixNano, Tail: tail, MaxEntries: maxEntries, MaxBytes: input.MaxBytes}
	if input.After != nil {
		cursor := protocol.Cursor(*input.After)
		request.After = &cursor
	}
	result, err := client.Output(ctx, request)
	if err != nil {
		return nil, mapError(err)
	}
	return result, nil
}

func (s *Server) wait(ctx context.Context, resolution Resolution, input commonInput) (any, error) {
	timeout, err := positiveTimeout(input.TimeoutMS)
	if err != nil {
		return nil, err
	}
	if input.Match != "" {
		if _, err := regexp.Compile(input.Match); err != nil {
			return nil, &ToolError{Code: "invalid_request", Message: "match must be a valid regular expression"}
		}
	}
	client, err := s.client(ctx, false)
	if err != nil {
		return nil, mapError(err)
	}
	defer client.Close()
	request := protocol.WaitRequest{Op: protocol.OpWait, Name: input.Name, Cwd: resolution.Root, Match: input.Match, TimeoutMS: timeout}
	if input.After != nil {
		cursor := protocol.Cursor(*input.After)
		request.After = &cursor
	}
	result, err := client.Wait(ctx, request)
	if err != nil {
		return nil, mapError(err)
	}
	if result.Outcome == protocol.WaitTimedOut && !result.ProcessObserved {
		result.Message = fmt.Sprintf("no process named %q was observed during the wait; check the name or start it first.", input.Name)
	}
	return result, nil
}

func (s *Server) input(ctx context.Context, resolution Resolution, input commonInput) (any, error) {
	client, err := s.client(ctx, false)
	if err != nil {
		return nil, mapError(err)
	}
	defer client.Close()

	definition, declared := findDefinition(resolution, input.Name)
	process, err := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: input.Name, Cwd: resolution.Root})
	if err != nil {
		mapped := mapError(err)
		if mapped.Code != string(protocol.ErrorNotFound) {
			return nil, mapped
		}
		if declared && !definition.TTY {
			return nil, mcpInputNotTTYError(input.Name, false)
		}
		if declared {
			return nil, mcpInputSessionNotRunningError(input.Name)
		}
		return nil, &ToolError{Code: string(protocol.ErrorNotFound), Message: fmt.Sprintf("process %q was not found; use hum start %s for a resolved name or hum run %s -- COMMAND", input.Name, input.Name, input.Name)}
	}
	if declared && !definition.TTY {
		return nil, mcpInputNotTTYError(input.Name, false)
	}
	if !process.TTY {
		return nil, mcpInputNotTTYError(input.Name, declared && definition.TTY)
	}

	root := process.Root
	if root == "" {
		root = resolution.Root
	}
	cwd := process.Cwd
	if cwd == "" {
		cwd = resolution.Root
	}
	result, err := client.Input(ctx, InputRequest{Name: input.Name, Cwd: cwd, Root: root, Data: append([]byte(nil), input.Data...)})
	if err != nil {
		return nil, mapError(err)
	}
	return InputResult{Name: input.Name, Bytes: result.Bytes, LaunchCursor: result.LaunchCursor}, nil
}

func mcpInputNotTTYError(name string, declaredTTY bool) *ToolError {
	message := fmt.Sprintf("process %q is not a tty; set tty: true in hum.yaml or launch it with hum run %s --tty -- COMMAND", name, name)
	if declaredTTY {
		message = fmt.Sprintf("process %q is running without a tty; stop it and rerun with tty: true or --tty", name)
	}
	return &ToolError{Code: string(protocol.ErrorInputNotTTY), Message: message}
}

func mcpInputSessionNotRunningError(name string) *ToolError {
	return &ToolError{Code: "session_not_running", Message: fmt.Sprintf("session %q is not running; start it with hum start %s", name, name)}
}

func processNeedsRestartControl(process protocol.Process) bool {
	return process.NextLaunchAt != nil || process.Restart == protocol.RestartOnFailure && process.Relaunches > 0
}

type stopResult struct {
	Name  string     `json:"name"`
	State string     `json:"state"`
	Error *ToolError `json:"error,omitempty"`
}

func (s *Server) down(ctx context.Context, resolution Resolution) (any, error) {
	byName := make(map[string]protocol.Process, len(resolution.Definitions))
	for _, definition := range resolution.Definitions {
		byName[definition.Name] = stoppedProcess(resolution.Root, definition)
	}
	client, err := s.client(ctx, false)
	if err != nil {
		if unavailable(err) {
			results := make([]stopResult, 0, len(byName))
			for _, process := range sortedProcesses(byName) {
				results = append(results, stopResult{Name: process.Name, State: "not_running"})
			}
			return results, nil
		}
		return nil, mapError(err)
	}
	defer client.Close()
	processes, err := client.List(ctx, protocol.ListRequest{Op: protocol.OpList, Cwd: resolution.Root})
	if err != nil {
		return nil, mapError(err)
	}
	for _, process := range processes {
		byName[process.Name] = process
	}
	results := make([]stopResult, 0, len(byName))
	for _, process := range sortedProcesses(byName) {
		result := stopResult{Name: process.Name, State: "not_running"}
		if process.State == "running" || process.State == "starting" || processNeedsRestartControl(process) {
			if stopErr := client.Stop(ctx, protocol.StopRequest{Op: protocol.OpStop, Name: process.Name, Cwd: resolution.Root}); stopErr != nil {
				result.State = "error"
				result.Error = mapError(stopErr)
			} else {
				result.State = "stopped"
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *Server) remove(ctx context.Context, resolution Resolution, name string) (any, error) {
	client, err := s.client(ctx, false)
	if err != nil {
		return nil, mapError(err)
	}
	defer client.Close()
	if err := client.Remove(ctx, protocol.RemoveRequest{Op: protocol.OpRemove, Name: name, Cwd: resolution.Root}); err != nil {
		return nil, mapError(err)
	}
	return stopResult{Name: name, State: "removed"}, nil
}

func (s *Server) signal(ctx context.Context, resolution Resolution, input commonInput) (any, error) {
	parsed, err := sharedsignals.Parse(input.Signal)
	if err != nil {
		return nil, &ToolError{Code: string(protocol.ErrorInvalidSignal), Message: err.Error()}
	}
	client, err := s.client(ctx, false)
	if err != nil {
		return nil, mapError(err)
	}
	defer client.Close()
	process, err := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: input.Name, Cwd: resolution.Root})
	if err != nil {
		mapped := mapError(err)
		if mapped.Code == string(protocol.ErrorNotFound) {
			return nil, &ToolError{Code: string(protocol.ErrorNotFound), Message: fmt.Sprintf("process %q was not found; use hum start %s for a resolved name or hum run %s -- COMMAND", input.Name, input.Name, input.Name)}
		}
		return nil, mapped
	}
	if process.State != protocol.StateRunning {
		return nil, &ToolError{Code: string(protocol.ErrorNotRunning), Message: fmt.Sprintf("process %q is not running; start it with hum start %s", input.Name, input.Name)}
	}
	request := protocol.NewSignalRequest(input.Name, resolution.Root, parsed.Name)
	result, signalErr := client.SignalResult(ctx, request)
	if signalErr != nil {
		return nil, mapError(signalErr)
	}
	if result.Name == "" {
		result.Name = input.Name
	}
	if result.Signal.Name == "" {
		result.Signal = protocol.SignalInfo{Name: parsed.Name, Number: parsed.Number}
	}
	if result.Status == "" {
		result.Status = "sent"
	}
	return result, nil
}

type restartResult struct {
	Name         string           `json:"name"`
	Outcome      string           `json:"outcome"`
	Readiness    string           `json:"readiness"`
	PID          int              `json:"pid"`
	LaunchCursor protocol.Cursor  `json:"launch_cursor"`
	Message      string           `json:"message,omitempty"`
	Source       string           `json:"source,omitempty"`
	Root         string           `json:"root"`
	TTY          bool             `json:"tty"`
	PGID         int              `json:"pgid"`
	Cwd          string           `json:"cwd"`
	Argv         []string         `json:"argv"`
	Start        time.Time        `json:"start"`
	NextCursor   *protocol.Cursor `json:"next_cursor,omitempty"`
	State        string           `json:"state"`
	Exit         *protocol.Exit   `json:"exit,omitempty"`
	ExitCode     int              `json:"exit_code,omitempty"`
	ExitedAt     time.Time        `json:"exited_at,omitempty"`
	RestartCount int              `json:"restart_count,omitempty"`
	Followers    int              `json:"followers"`
	Restart      string           `json:"restart"`
	Relaunches   int              `json:"relaunches"`
	NextLaunchAt *time.Time       `json:"next_launch_at,omitempty"`
}

func restartResultForProcess(process protocol.Process, name, outcome, message string) restartResult {
	process = normalizeProcess(process)
	result := restartResult{
		Name:         process.Name,
		Outcome:      outcome,
		Readiness:    protocol.ReadinessRunningUnverified,
		PID:          process.PID,
		LaunchCursor: process.LaunchCursor,
		Message:      message,
		Source:       process.Source,
		Root:         process.Root,
		TTY:          process.TTY,
		PGID:         process.PGID,
		Cwd:          process.Cwd,
		Argv:         append([]string(nil), process.Argv...),
		Start:        process.Start,
		NextCursor:   process.NextCursor,
		State:        process.State,
		Exit:         process.Exit,
		ExitCode:     process.ExitCode,
		ExitedAt:     process.ExitedAt,
		RestartCount: process.RestartCount,
		Followers:    process.Followers,
		Restart:      process.Restart,
		Relaunches:   process.Relaunches,
		NextLaunchAt: process.NextLaunchAt,
	}
	if result.Argv == nil {
		result.Argv = []string{}
	}
	if result.Name == "" {
		result.Name = name
	}
	if process.Readiness != nil {
		result.Readiness = process.Readiness.State
	}
	if process.State != "running" && process.Readiness == nil {
		result.Readiness = ""
	}
	return result
}

func (s *Server) restart(ctx context.Context, resolution Resolution, input commonInput) (any, error) {
	if _, set := input.fields["timeout_ms"]; set && input.TimeoutMS <= 0 {
		return nil, &ToolError{Code: "invalid_request", Message: "timeout_ms must be positive"}
	}
	if input.TimeoutMS > int64((1<<63-1)/int64(time.Millisecond)) {
		return nil, &ToolError{Code: "invalid_request", Message: "timeout_ms is too large"}
	}
	client, err := s.client(ctx, false)
	if err != nil {
		return nil, mapError(err)
	}
	defer client.Close()

	name := input.Name
	definition, declared := findDefinition(resolution, name)
	if !declared {
		if process, getErr := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: name, Cwd: resolution.Root}); getErr != nil {
			return nil, mapError(getErr)
		} else {
			definition = Definition{Name: name, Source: process.Source, Cwd: process.Cwd, Argv: append([]string(nil), process.Argv...)}
		}
	}
	timeout, err := readinessTimeout(input.TimeoutMS, definition)
	if err != nil {
		return nil, err
	}

	request := protocol.RestartRequest{Op: protocol.OpRestart, Name: name, Cwd: resolution.Root}
	if declared {
		request.Root, request.Cwd, request.Update = resolution.Root, definition.Cwd, true
		request.Argv, request.Env, request.Source, request.Ready, request.TTY = append([]string(nil), definition.Argv...), s.environment(), definition.Source, definition.Ready, definition.TTY
		request.Restart = effectiveRestart(definition.Restart)
	}
	launchedAt := time.Now()
	process, err := client.Restart(ctx, request)
	if err != nil {
		return nil, mapError(err)
	}
	if process.Name == "" {
		process.Name = name
	}
	if definition.Ready == nil && process.Readiness != nil && (process.Readiness.State == protocol.ReadinessStarting || process.Readiness.State == protocol.ReadinessReady) {
		definition.Ready = &protocol.ReadinessConfig{Match: process.Readiness.Match}
	}
	if definition.Ready != nil && process.Readiness == nil && process.State == "running" {
		process.Readiness = &protocol.Readiness{State: protocol.ReadinessStarting, Match: definition.Ready.Match}
	}

	outcome := "restarted"
	if definition.Ready == nil {
		outcome = protocol.ReadinessRunningUnverified
	}
	if input.NoWait || definition.Ready == nil {
		return restartResultForProcess(process, name, outcome, ""), nil
	}
	if !process.Start.IsZero() {
		launchedAt = process.Start
	}
	timeout = mcpRemainingTimeout(timeout, launchedAt)

	process, outcome, err = s.mcpWaitForReadiness(ctx, client, resolution, definition, process, outcome, timeout)
	if err != nil {
		return nil, mapError(err)
	}
	message := ""
	switch outcome {
	case "exited_before_ready":
		message = "process exited before readiness"
	case "timed_out":
		message = "readiness timed out"
	}
	return restartResultForProcess(process, name, outcome, message), nil
}

func (s *Server) stop(ctx context.Context, resolution Resolution, name string) (any, error) {
	client, err := s.client(ctx, false)
	if err != nil {
		if unavailable(err) {
			return map[string]string{"name": name, "state": "not_running"}, nil
		}
		return nil, mapError(err)
	}
	defer client.Close()
	process, err := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: name, Cwd: resolution.Root})
	if err != nil {
		mapped := mapError(err)
		if mapped.Code == string(protocol.ErrorNotFound) {
			return map[string]string{"name": name, "state": "not_running"}, nil
		}
		return nil, mapped
	}
	if process.State != "running" && process.State != "starting" && !processNeedsRestartControl(process) {
		return map[string]string{"name": name, "state": "not_running"}, nil
	}
	if err := client.Stop(ctx, protocol.StopRequest{Op: protocol.OpStop, Name: name, Cwd: resolution.Root}); err != nil {
		return nil, mapError(err)
	}
	return map[string]string{"name": name, "state": "stopped"}, nil
}
