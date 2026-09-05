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
	"sync"
	"time"
	"unicode"

	"hum/internal/protocol"
)

const defaultTimeoutMS int64 = 30_000

// ErrDaemonUnavailable identifies a missing daemon without coupling MCP to the daemon package.
var ErrDaemonUnavailable = errors.New("daemon unavailable")

// Definition is one explicit or discovered project process.
type Definition struct {
	Name   string
	Source string
	Argv   []string
	Cwd    string
	Ready  *protocol.ReadinessConfig
	TTY    bool
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
	Stop(context.Context, protocol.StopRequest) error
	Remove(context.Context, protocol.RemoveRequest) error
	Restart(context.Context, protocol.RestartRequest) (protocol.Process, error)
	Close() error
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
	readiness := objectSchema(map[string]any{
		"state":  stringProperty("starting, ready, or running_unverified"),
		"cursor": map[string]any{"type": "integer", "minimum": 0},
		"time":   map[string]any{"type": "string"},
		"match":  map[string]any{"type": "string"},
	}, "state")
	exit := objectSchema(map[string]any{
		"code":  map[string]any{"type": "integer"},
		"time":  map[string]any{"type": "string"},
		"error": map[string]any{"type": "string"},
	}, "code", "time")
	process := objectSchema(map[string]any{
		"name": map[string]any{"type": "string"}, "source": map[string]any{"type": "string"},
		"root": map[string]any{"type": "string"}, "tty": map[string]any{"type": "boolean"}, "pid": map[string]any{"type": "integer"},
		"pgid": map[string]any{"type": "integer"}, "cwd": map[string]any{"type": "string"},
		"argv":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"start": map[string]any{"type": "string"}, "launch_cursor": map[string]any{"type": "integer", "minimum": 0},
		"next_cursor": map[string]any{"type": "integer", "minimum": 0}, "state": map[string]any{"type": "string"},
		"exit": exit, "exit_code": map[string]any{"type": "integer"}, "exited_at": map[string]any{"type": "string"},
		"restart_count": map[string]any{"type": "integer", "minimum": 0},
		"followers":     map[string]any{"type": "integer", "minimum": 0, "description": "Live run and logs --follow clients attached to this supervision session."},
		"readiness":     readiness,
	}, "name", "source", "root", "tty", "cwd", "argv", "state", "launch_cursor", "followers")
	toolError := objectSchema(map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}}, "code", "message")
	launch := objectSchema(map[string]any{"name": map[string]any{"type": "string"}, "outcome": map[string]any{"type": "string"}, "process": process, "error": toolError}, "name", "outcome")
	stop := objectSchema(map[string]any{"name": map[string]any{"type": "string"}, "state": map[string]any{"type": "string"}, "error": toolError}, "name", "state")
	outputEntry := objectSchema(map[string]any{"cursor": map[string]any{"type": "integer", "minimum": 0}, "stream": map[string]any{"type": "string"}, "time": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}}, "cursor", "stream", "time", "text")
	output := objectSchema(map[string]any{"entries": map[string]any{"type": "array", "items": outputEntry}, "next": map[string]any{"type": "integer", "minimum": 0}, "oldest": map[string]any{"type": "integer", "minimum": 0}, "latest": map[string]any{"type": "integer", "minimum": 0}, "evicted_through": map[string]any{"type": "integer", "minimum": 0}, "truncated": map[string]any{"type": "boolean"}, "more": map[string]any{"type": "boolean"}}, "entries")
	wait := objectSchema(map[string]any{"op": map[string]any{"type": "string"}, "ok": map[string]any{"type": "boolean"}, "outcome": map[string]any{"type": "string"}, "cursor": map[string]any{"type": "integer", "minimum": 0}}, "op", "ok", "cursor")
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
	return []toolDefinition{
		{Name: "start", Description: "Start one resolved project definition through the hum daemon; waits for configured readiness by default.", InputSchema: objectSchema(startProps, "project_root", "name"), OutputSchema: launch},
		{Name: "up", Description: "Start every resolved project definition through the hum daemon; waits for configured readiness by default.", InputSchema: objectSchema(waitProps, "project_root"), OutputSchema: map[string]any{"type": "array", "items": launch}},
		{Name: "down", Description: "Stop every running runtime record in the project and return one result per name; does not shut down the daemon.", InputSchema: objectSchema(map[string]any{"project_root": root}, "project_root"), OutputSchema: map[string]any{"type": "array", "items": stop}},
		{Name: "list", Description: "Merge resolved definitions with all daemon runtime records in the project, including ad_hoc records.", InputSchema: objectSchema(map[string]any{"project_root": root}, "project_root"), OutputSchema: map[string]any{"type": "array", "items": process}},
		{Name: "status", Description: "Return one existing declared or ad_hoc runtime record; this tool never creates a daemon.", InputSchema: objectSchema(map[string]any{"project_root": root, "name": nameExisting}, "project_root", "name"), OutputSchema: process},
		{Name: "logs", Description: "Read a bounded cursor-based output window for an existing declared or ad_hoc runtime record. Child output is terminal-control-stripped per entry; system entries, stored bytes, cursors, and limit accounting remain raw.", InputSchema: objectSchema(map[string]any{"project_root": root, "name": nameExisting, "after": map[string]any{"type": "integer", "minimum": 0}, "tail": map[string]any{"type": "integer", "minimum": 0}, "max_entries": map[string]any{"type": "integer", "minimum": 1}, "max_bytes": map[string]any{"type": "integer", "minimum": 1}}, "project_root", "name"), OutputSchema: output},
		{Name: "wait", Description: "Wait for output or exit on an existing declared or ad_hoc runtime record; defaults after to the current launch cursor and timeout to 30000 ms.", InputSchema: objectSchema(map[string]any{"project_root": root, "name": nameExisting, "after": map[string]any{"type": "integer", "minimum": 0}, "match": map[string]any{"type": "string"}, "timeout_ms": map[string]any{"type": "integer", "minimum": 1}}, "project_root", "name"), OutputSchema: wait},
		{Name: "input", Description: "Write one exact, bounded payload to an already-running TTY incarnation at its initial launch cursor with at-most-once behavior; never starts, waits, queues, retries, resends, retains, or explicitly echoes input and fails immediately on ownership conflict.", InputSchema: inputSchema, OutputSchema: inputResult},
		{Name: "restart", Description: "Restart a resolved definition using the current server environment, or an existing retained ad_hoc record using its recorded launch specification.", InputSchema: objectSchema(map[string]any{"project_root": root, "name": nameExisting}, "project_root", "name"), OutputSchema: process},
		{Name: "stop", Description: "Stop one existing declared or ad_hoc runtime record while preserving its supervision session.", InputSchema: objectSchema(map[string]any{"project_root": root, "name": nameExisting}, "project_root", "name"), OutputSchema: stop},
		{Name: "remove", Description: "Stop and discard one runtime supervision session, its retained launch specification, and output.", InputSchema: objectSchema(map[string]any{"project_root": root, "name": nameExisting}, "project_root", "name"), OutputSchema: stop},
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
	ProjectRoot string  `json:"project_root"`
	Name        string  `json:"name,omitempty"`
	NoWait      bool    `json:"no_wait,omitempty"`
	TimeoutMS   int64   `json:"timeout_ms,omitempty"`
	After       *uint64 `json:"after,omitempty"`
	Tail        int     `json:"tail,omitempty"`
	MaxEntries  int     `json:"max_entries,omitempty"`
	MaxBytes    int     `json:"max_bytes,omitempty"`
	Match       string  `json:"match,omitempty"`
	Text        *string `json:"text,omitempty"`
	Base64      *string `json:"base64,omitempty"`

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

func normalizeProcess(process protocol.Process) protocol.Process {
	process.Argv = append([]string(nil), process.Argv...)
	if process.Source == "" {
		process.Source = "ad_hoc"
	}
	return process
}

func stoppedProcess(root string, definition Definition) protocol.Process {
	return protocol.Process{Name: definition.Name, Source: definition.Source, Root: root, TTY: definition.TTY, Cwd: definition.Cwd, Argv: append([]string(nil), definition.Argv...), State: "stopped"}
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
	if override != 0 {
		return positiveTimeout(override)
	}
	if definition.Ready != nil && definition.Ready.Timeout > 0 {
		milliseconds := definition.Ready.Timeout / time.Millisecond
		if milliseconds < 1 {
			milliseconds = 1
		}
		return int64(milliseconds), nil
	}
	return defaultTimeoutMS, nil
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
		return s.restart(ctx, resolution, input.Name)
	case "stop":
		return s.stop(ctx, resolution, input.Name)
	case "remove":
		return s.remove(ctx, resolution, input.Name)
	default:
		panic("unreachable")
	}
}
func (s *Server) ensureDefinition(ctx context.Context, client Client, resolution Resolution, definition Definition) (protocol.Process, bool, error) {
	process, err := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: definition.Name, Cwd: resolution.Root})
	if err == nil && process.State == "running" {
		if definition.TTY && !process.TTY {
			return protocol.Process{}, false, &ToolError{Code: string(protocol.ErrorInputNotTTY), Message: fmt.Sprintf("declared process %q is running without a tty; stop it and rerun with tty: true", definition.Name)}
		}
		if process.Source == definition.Source {
			return process, true, nil
		}
		return protocol.Process{}, false, &ToolError{Code: string(protocol.ErrorNameInUse), Message: fmt.Sprintf("declared process %q is occupied by an ad_hoc launch", definition.Name)}
	}
	if err != nil && mapError(err).Code != string(protocol.ErrorNotFound) {
		return protocol.Process{}, false, mapError(err)
	}
	process, err = client.Start(ctx, protocol.StartRequest{Op: protocol.OpStart, Name: definition.Name, Argv: append([]string(nil), definition.Argv...), Cwd: definition.Cwd, Root: resolution.Root, Env: s.environment(), Source: definition.Source, Ready: definition.Ready, TTY: definition.TTY})
	if err == nil || mapError(err).Code != string(protocol.ErrorNameInUse) {
		return process, false, err
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, getErr := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: definition.Name, Cwd: resolution.Root})
		if getErr == nil && current.State == "running" {
			if definition.TTY && !current.TTY {
				return protocol.Process{}, false, &ToolError{Code: string(protocol.ErrorInputNotTTY), Message: fmt.Sprintf("declared process %q is running without a tty; stop it and rerun with tty: true", definition.Name)}
			}
			if current.Source == definition.Source {
				return current, true, nil
			}
			return protocol.Process{}, false, &ToolError{Code: string(protocol.ErrorNameInUse), Message: fmt.Sprintf("declared process %q is occupied by an ad_hoc launch", definition.Name)}
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return protocol.Process{}, false, ctx.Err()
		case <-timer.C:
		}
	}
	return protocol.Process{}, false, err
}

func launchOutcome(already bool, definition Definition) string {
	if already {
		return "already_running"
	}
	if definition.Ready == nil {
		return protocol.ReadinessRunningUnverified
	}
	return "started"
}

func (s *Server) waitForReadiness(ctx context.Context, client Client, resolution Resolution, definition Definition, process protocol.Process, initial string, timeout int64) (protocol.Process, string, error) {
	recordedMatch := definition.Ready.Match
	if process.Readiness != nil && process.Readiness.Match != "" {
		recordedMatch = process.Readiness.Match
	}
	readCurrent := func() (protocol.Process, string, bool, error) {
		current, getErr := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: process.Name, Cwd: resolution.Root})
		if getErr != nil {
			return protocol.Process{}, "", false, getErr
		}
		if current.State != "running" {
			return current, "exited_before_ready", true, nil
		}
		if current.Readiness == nil || current.Readiness.State == protocol.ReadinessReady || current.Readiness.State == protocol.ReadinessRunningUnverified {
			return current, initial, true, nil
		}
		if current.Readiness.State != protocol.ReadinessStarting || current.Readiness.Match != recordedMatch {
			return current, initial, true, nil
		}
		return current, "", false, nil
	}
	current, outcome, done, err := readCurrent()
	if err != nil {
		return protocol.Process{}, "", err
	}
	if done {
		return current, outcome, nil
	}
	after := process.LaunchCursor
	waited, err := client.Wait(ctx, protocol.WaitRequest{Op: protocol.OpWait, Name: process.Name, Cwd: resolution.Root, After: &after, Match: recordedMatch, TimeoutMS: timeout})
	if err != nil {
		return protocol.Process{}, "", err
	}
	if waited.Outcome == protocol.WaitExited {
		current, _, _, getErr := readCurrent()
		return current, "exited_before_ready", getErr
	}
	current, outcome, done, err = readCurrent()
	if err != nil {
		return protocol.Process{}, "", err
	}
	if done {
		return current, outcome, nil
	}
	if waited.Outcome == protocol.WaitTimedOut {
		return current, "timed_out", nil
	}
	if waited.Outcome != protocol.WaitMatched {
		return protocol.Process{}, "", fmt.Errorf("unknown readiness wait outcome %q", waited.Outcome)
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Millisecond)
	for time.Now().Before(deadline) {
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return protocol.Process{}, "", ctx.Err()
		case <-timer.C:
		}
		current, outcome, done, err = readCurrent()
		if err != nil {
			return protocol.Process{}, "", err
		}
		if done {
			return current, outcome, nil
		}
	}
	return current, "timed_out", nil
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
	process, already, err := s.ensureDefinition(ctx, client, resolution, definition)
	if err != nil {
		return nil, mapError(err)
	}
	outcome := launchOutcome(already, definition)
	if definition.Ready == nil {
		process.Readiness = &protocol.Readiness{State: protocol.ReadinessRunningUnverified}
	} else if !input.NoWait {
		process, outcome, err = s.waitForReadiness(ctx, client, resolution, definition, process, outcome, timeout)
		if err != nil {
			return nil, mapError(err)
		}
	}
	process = normalizeProcess(process)
	return launchResult{Name: definition.Name, Outcome: outcome, Process: &process}, nil
}

type launchResult struct {
	Name    string            `json:"name"`
	Outcome string            `json:"outcome"`
	Process *protocol.Process `json:"process,omitempty"`
	Error   *ToolError        `json:"error,omitempty"`
}

func (s *Server) up(ctx context.Context, resolution Resolution, input commonInput) (any, error) {
	if input.TimeoutMS < 0 {
		return nil, &ToolError{Code: "invalid_request", Message: "timeout_ms must be positive"}
	}
	if len(resolution.Definitions) == 0 {
		return []launchResult{}, nil
	}
	client, err := s.client(ctx, true)
	if err != nil {
		return nil, mapError(err)
	}
	defer client.Close()
	definitions := append([]Definition(nil), resolution.Definitions...)
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	results := make([]launchResult, len(definitions))
	for index, definition := range definitions {
		results[index].Name = definition.Name
		process, already, startErr := s.ensureDefinition(ctx, client, resolution, definition)
		if startErr != nil {
			results[index].Outcome = "error"
			results[index].Error = mapError(startErr)
			continue
		}
		results[index].Outcome = launchOutcome(already, definition)
		if definition.Ready == nil {
			process.Readiness = &protocol.Readiness{State: protocol.ReadinessRunningUnverified}
		}
		process = normalizeProcess(process)
		results[index].Process = &process
	}
	if input.NoWait {
		return results, nil
	}
	var waits sync.WaitGroup
	for index, definition := range definitions {
		if definition.Ready == nil || results[index].Process == nil {
			continue
		}
		waits.Add(1)
		go func(index int, definition Definition) {
			defer waits.Done()
			process := *results[index].Process
			timeout, timeoutErr := readinessTimeout(input.TimeoutMS, definition)
			outcome := results[index].Outcome
			if timeoutErr == nil {
				process, outcome, timeoutErr = s.waitForReadiness(ctx, client, resolution, definition, process, outcome, timeout)
			}
			if timeoutErr != nil {
				results[index].Process = nil
				results[index].Outcome = "error"
				results[index].Error = mapError(timeoutErr)
				return
			}
			process = normalizeProcess(process)
			results[index].Outcome = outcome
			results[index].Process = &process
		}(index, definition)
	}
	waits.Wait()
	return results, nil
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
	processes, err := client.List(ctx, protocol.ListRequest{Op: protocol.OpList, Cwd: resolution.Root, IncludeCompleted: true})
	if err != nil {
		return nil, mapError(err)
	}
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
	process, err := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: name, Cwd: resolution.Root})
	if err != nil {
		return nil, mapError(err)
	}
	return normalizeProcess(process), nil
}

func (s *Server) logs(ctx context.Context, resolution Resolution, input commonInput) (any, error) {
	if input.Tail < 0 || input.MaxEntries < 0 || input.MaxBytes < 0 {
		return nil, &ToolError{Code: "invalid_request", Message: "log bounds cannot be negative"}
	}
	client, err := s.client(ctx, false)
	if err != nil {
		return nil, mapError(err)
	}
	defer client.Close()
	request := protocol.OutputRequest{Op: protocol.OpOutput, Name: input.Name, Cwd: resolution.Root, Tail: input.Tail, MaxEntries: input.MaxEntries, MaxBytes: input.MaxBytes}
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
		if process.State == "running" || process.State == "starting" {
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

func (s *Server) restart(ctx context.Context, resolution Resolution, name string) (any, error) {
	client, err := s.client(ctx, false)
	if err != nil {
		return nil, mapError(err)
	}
	defer client.Close()
	request := protocol.RestartRequest{Op: protocol.OpRestart, Name: name, Cwd: resolution.Root}
	if definition, ok := findDefinition(resolution, name); ok {
		request.Root, request.Cwd, request.Update = resolution.Root, definition.Cwd, true
		request.Argv, request.Env, request.Source, request.Ready, request.TTY = append([]string(nil), definition.Argv...), s.environment(), definition.Source, definition.Ready, definition.TTY
	} else {
		if _, err := client.Get(ctx, protocol.GetRequest{Op: protocol.OpGet, Name: name, Cwd: resolution.Root}); err != nil {
			return nil, mapError(err)
		}
	}
	process, err := client.Restart(ctx, request)
	if err != nil {
		return nil, mapError(err)
	}
	return normalizeProcess(process), nil
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
	if process.State != "running" && process.State != "starting" {
		return map[string]string{"name": name, "state": "not_running"}, nil
	}
	if err := client.Stop(ctx, protocol.StopRequest{Op: protocol.OpStop, Name: name, Cwd: resolution.Root}); err != nil {
		return nil, mapError(err)
	}
	return map[string]string{"name": name, "state": "stopped"}, nil
}

// DefaultTimeout exposes the MCP wait/readiness default for CLI adapters and tests.
func DefaultTimeout() time.Duration { return time.Duration(defaultTimeoutMS) * time.Millisecond }
