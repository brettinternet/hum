package daemon

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/process"
	"hum/internal/protocol"
)

// Protocol DTOs are the only socket representation. These conversions are
// limited to the application domain types used by the supervisor and public
// client APIs.
func appReadinessConfigFromProtocol(config *protocol.ReadinessConfig) *app.ReadinessConfig {
	if config == nil {
		return nil
	}
	return &app.ReadinessConfig{Match: config.Match, Timeout: config.Timeout}
}

func writeProtocolError(encoder *protocol.Encoder, op protocol.Operation, err error) error {
	if err == nil {
		return nil
	}
	var wire *protocol.WireError
	if errors.As(err, &wire) && wire != nil {
		return encoder.EncodeResponse(protocol.ErrorResponse{Op: op, OK: false, Error: wire})
	}
	if errors.Is(err, protocol.ErrMalformed) || errors.Is(err, protocol.ErrOversized) || errors.Is(err, protocol.ErrUnknownOperation) || errors.Is(err, protocol.ErrMissingOperation) {
		return encoder.EncodeResponse(protocol.ErrorResponse{Op: op, OK: false, Error: protocol.WireErrorForDecode(err)})
	}
	return encoder.EncodeResponse(protocol.ErrorResponse{Op: op, OK: false, Error: protocolWireError(err)})
}

func protocolWireError(err error) *protocol.WireError {
	if err == nil {
		return nil
	}
	if errors.Is(err, app.ErrInputStopped) {
		return protocol.NewWireError(protocol.ErrorInputClosed, err.Error(), map[string]any{"stopped": true})
	}
	var wire *protocol.WireError
	if errors.As(err, &wire) && wire != nil {
		return wire
	}
	var version *VersionMismatchError
	if errors.As(err, &version) {
		return protocol.NewWireError(protocol.ErrorVersionMismatch, version.Error(), protocol.VersionMismatchDetails{Client: version.ClientVersion, Daemon: version.DaemonVersion})
	}
	var active *ActiveProcessesError
	if errors.As(err, &active) {
		return protocol.NewWireError(protocol.ErrorActiveProcesses, active.Error(), append([]string(nil), active.Names...))
	}
	var notFound *app.NotFoundError
	if errors.As(err, &notFound) && notFound != nil {
		details := map[string]any{"scope": app.ScopeProject}
		if notFound.Root == "" {
			details["scope"] = app.ScopeGlobal
		} else {
			details["project_root"] = notFound.Root
		}
		if len(notFound.OtherScopes) != 0 {
			details["other_scopes"] = notFound.OtherScopes
		}
		return protocol.NewWireError(protocol.ErrorNotFound, notFound.Error(), details)
	}
	return protocol.NewWireError(protocol.ErrorCode(errorCode(err)), err.Error(), nil)
}

func protocolErrorToError(wire *protocol.WireError) error { return wireErrorToError(wire) }

func wireErrorToError(wire *protocol.WireError) error {
	if wire == nil {
		return nil
	}
	if wire.Code == protocol.ErrorVersionMismatch {
		clientVersion, daemonVersion := 0, 0
		if details, ok := wire.Details.(map[string]any); ok {
			if value, ok := details["client"].(float64); ok {
				clientVersion = int(value)
			}
			if value, ok := details["daemon"].(float64); ok {
				daemonVersion = int(value)
			}
		}
		if details, ok := wire.Details.(protocol.VersionMismatchDetails); ok {
			clientVersion, daemonVersion = details.Client, details.Daemon
		}
		return &VersionMismatchError{ClientVersion: clientVersion, DaemonVersion: daemonVersion, Message: wire.Message}
	}
	if wire.Code == protocol.ErrorActiveProcesses {
		var names []string
		if values, ok := wire.Details.([]any); ok {
			for _, value := range values {
				if name, ok := value.(string); ok {
					names = append(names, name)
				}
			}
		}
		if values, ok := wire.Details.([]string); ok {
			names = append(names, values...)
		}
		return &ActiveProcessesError{Names: names}
	}
	return &WireError{Code: wire.Code, Message: wire.Message, Details: wire.Details}
}

func protocolSignalResultFromResponse(response *protocol.SignalResponse, fallbackName string) (protocol.SignalResult, error) {
	if response == nil || response.Signal == nil {
		return protocol.SignalResult{}, errors.New("daemon signal response omitted signal")
	}
	name := response.Name
	if name == "" {
		name = fallbackName
	}
	status := response.Status
	if status == "" {
		status = "sent"
	}
	return protocol.SignalResult{Name: name, Signal: *response.Signal, Status: status}, nil
}

func appProcessFromProtocol(item protocol.Process) app.Process {
	scope := item.Scope
	if scope == "" {
		scope = app.ScopeProject
	}
	result := app.Process{
		Name: item.Name, Source: item.Source, Scope: scope, Root: item.Root, TTY: item.TTY, PID: item.PID, PGID: item.PGID,
		Cwd: item.Cwd, Argv: append([]string(nil), item.Argv...), Start: item.Start,
		LaunchCursor: output.Cursor(item.LaunchCursor), State: app.State(item.State), ExitCode: item.ExitCode, ExitedAt: item.ExitedAt,
		RestartCount: item.RestartCount, Followers: item.Followers, Restart: app.RestartPolicy(item.Restart), StopGrace: item.StopGrace, StopGraceInherited: item.StopGraceInherited, Relaunches: item.Relaunches, NextLaunchAt: item.NextLaunchAt,
	}
	if item.Readiness != nil {
		result.Readiness = &app.Readiness{State: item.Readiness.State, Cursor: cursorFromProtocol(item.Readiness.Cursor), Time: item.Readiness.Time, Match: item.Readiness.Match}
	}
	if item.NextCursor != nil {
		result.NextCursor = output.Cursor(*item.NextCursor)
	}
	if item.Exit != nil {
		result.Exit = &processResult{ExitCode: item.Exit.Code, Err: errorFromString(item.Exit.Error), ExitedAt: item.Exit.Time}
		if item.Exit.Signal != nil {
			result.Exit.Signal = &process.SignalInfo{Name: item.Exit.Signal.Name, Number: item.Exit.Signal.Number}
		}
	}
	return result
}

type processResult = process.Result

func protocolProcessFromApp(item app.Process) protocol.Process {
	scope := item.Scope
	if scope == "" {
		scope = app.ScopeProject
	}
	result := protocol.Process{Name: item.Name, Source: item.Source, Scope: scope, Root: item.Root, TTY: item.TTY, PID: item.PID, PGID: item.PGID,
		Cwd: item.Cwd, Argv: append([]string(nil), item.Argv...), Start: item.Start, LaunchCursor: protocol.Cursor(item.LaunchCursor), State: string(item.State),
		ExitCode: item.ExitCode, ExitedAt: item.ExitedAt, RestartCount: item.RestartCount, Followers: item.Followers, Restart: string(item.Restart), StopGrace: item.StopGrace, StopGraceInherited: item.StopGraceInherited, Relaunches: item.Relaunches, NextLaunchAt: item.NextLaunchAt}
	if item.Readiness != nil {
		result.Readiness = &protocol.Readiness{State: item.Readiness.State, Cursor: protocolCursor(item.Readiness.Cursor), Time: item.Readiness.Time, Match: item.Readiness.Match}
	}
	if item.Exit != nil {
		result.Exit = &protocol.Exit{Code: item.Exit.ExitCode, Error: errorString(item.Exit.Err), Time: item.Exit.ExitedAt}
		if item.Exit.Signal != nil {
			result.Exit.Signal = &protocol.SignalInfo{Name: item.Exit.Signal.Name, Number: item.Exit.Signal.Number}
		}
	}
	return result
}

func protocolProcessesFromApp(items []app.Process) []protocol.Process {
	result := make([]protocol.Process, 0, len(items))
	for _, item := range items {
		result = append(result, protocolProcessFromApp(item))
	}
	return result
}

func protocolReadResult(result output.ReadResult) protocol.OutputResult {
	entries := make([]protocol.OutputEntry, 0, len(result.Entries))
	for _, item := range result.Entries {
		entries = append(entries, protocol.OutputEntry{Cursor: protocol.Cursor(item.Cursor), Stream: protocol.Stream(streamName(item.Stream)), Time: item.Time, Text: item.Text})
	}
	return protocol.OutputResult{Entries: entries, Next: protocolCursor(result.Next), Oldest: protocolCursor(result.Oldest), Latest: protocolCursor(result.Latest), EvictedThrough: protocolCursor(result.EvictedThrough), Truncated: result.Truncated, More: result.More}
}

func protocolOutputResponse(op protocol.Operation, result output.ReadResult) protocol.OutputResponse {
	value := protocolReadResult(result)
	return protocol.OutputResponse{Op: op, OK: true, Entries: value.Entries, Next: value.Next, Oldest: value.Oldest, Latest: value.Latest, EvictedThrough: value.EvictedThrough, Truncated: value.Truncated, More: value.More, Result: &value}
}

func protocolStreamEventFromOutput(name string, event output.Event) protocol.StreamEvent {
	if event.Read != nil {
		result := protocolReadResult(*event.Read)
		kind := protocol.EventOutput
		if result.EvictedThrough != nil {
			kind = protocol.EventEviction
		} else if result.Next != nil && len(result.Entries) == 0 {
			kind = protocol.EventCursor
		}
		return protocol.StreamEvent{Op: protocol.OpEvent, Type: kind, Name: name, Entries: result.Entries, Next: result.Next, Oldest: result.Oldest, Latest: result.Latest, EvictedThrough: result.EvictedThrough, Truncated: result.Truncated, More: result.More, Result: &result}
	}
	if event.Exit != nil {
		exit := &protocol.Exit{Code: event.Exit.Code, Time: event.Exit.Time}
		if event.Exit.SignalName != "" {
			exit.Signal = &protocol.SignalInfo{Name: event.Exit.SignalName, Number: event.Exit.SignalNumber}
		}
		return protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventExit, Name: name, Exit: exit, Time: event.Exit.Time}
	}
	return protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventOutput, Name: name}
}

func protocolWaitResponse(result app.WaitResult) protocol.WaitResponse {
	response := protocol.WaitResponse{Op: protocol.OpWait, OK: true, Outcome: protocol.WaitOutcome(result.Outcome), Cursor: protocol.Cursor(result.Cursor)}
	if result.Outcome == app.WaitTimedOut {
		response.ProcessObserved = result.ProcessObserved
	}
	if result.Exit != nil {
		response.Exit = &protocol.Exit{Code: result.Exit.ExitCode, Error: errorString(result.Exit.Err), Time: result.Exit.ExitedAt}
		if result.Exit.Signal != nil {
			response.Exit.Signal = &protocol.SignalInfo{Name: result.Exit.Signal.Name, Number: result.Exit.Signal.Number}
		}
	}
	return response
}

func outputResultFromProtocol(response *protocol.OutputResponse) output.ReadResult {
	if response == nil {
		return output.ReadResult{}
	}
	return output.ReadResult{Entries: entriesFromProtocol(response.Entries), Next: cursorFromProtocol(response.Next), Oldest: cursorFromProtocol(response.Oldest), Latest: cursorFromProtocol(response.Latest), EvictedThrough: cursorFromProtocol(response.EvictedThrough), Truncated: response.Truncated, More: response.More}
}

func entriesFromProtocol(items []protocol.OutputEntry) []output.Entry {
	entries := make([]output.Entry, 0, len(items))
	for _, item := range items {
		entries = append(entries, output.Entry{Cursor: output.Cursor(item.Cursor), Stream: outputStreamFromName(string(item.Stream)), Time: item.Time, Text: item.Text})
	}
	return entries
}

func cursorFromProtocol(value *protocol.Cursor) *output.Cursor {
	if value == nil {
		return nil
	}
	cursor := output.Cursor(*value)
	return &cursor
}
func protocolCursor(value *output.Cursor) *protocol.Cursor {
	if value == nil {
		return nil
	}
	cursor := protocol.Cursor(*value)
	return &cursor
}
func errorFromString(value string) error {
	if value == "" {
		return nil
	}
	return errors.New(value)
}
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func outputStreamFromName(name string) output.Stream {
	switch name {
	case string(protocol.StreamStdout):
		return output.Stdout
	case string(protocol.StreamStderr):
		return output.Stderr
	case string(protocol.StreamSystem):
		return output.System
	default:
		return 0
	}
}
func streamName(stream output.Stream) string {
	switch stream {
	case output.Stdout:
		return string(protocol.StreamStdout)
	case output.Stderr:
		return string(protocol.StreamStderr)
	case output.System:
		return string(protocol.StreamSystem)
	default:
		return ""
	}
}

func readOptionsFromProtocol(req protocol.OutputRequest) (output.ReadOptions, error) {
	return readOptionsFromValues(req.After, req.SinceMS, req.SinceUnixNano, req.Tail, req.Stream, req.Match, req.Context, req.MaxEntries, req.MaxBytes)
}

func readOptionsFromFollow(req protocol.FollowRequest) (output.ReadOptions, error) {
	return readOptionsFromValues(req.After, req.SinceMS, req.SinceUnixNano, req.Tail, req.Stream, req.Match, 0, req.MaxEntries, req.MaxBytes)
}

func readOptionsFromValues(after *protocol.Cursor, sinceMS, sinceUnixNano int64, tail int, stream protocol.Stream, matchExpression string, contextEntries, maxEntries, maxBytes int) (output.ReadOptions, error) {
	options := output.ReadOptions{Tail: tail, Context: contextEntries, MaxEntries: maxEntries, MaxBytes: maxBytes}
	if contextEntries < 0 {
		return output.ReadOptions{}, fmt.Errorf("%w: context must not be negative", app.ErrInvalidRequest)
	}
	if contextEntries > 0 && matchExpression == "" {
		return output.ReadOptions{}, fmt.Errorf("%w: context requires match", app.ErrInvalidRequest)
	}
	if sinceMS < 0 {
		return output.ReadOptions{}, fmt.Errorf("%w: since_ms must be positive", app.ErrInvalidRequest)
	}
	if sinceMS > maxSinceMilliseconds {
		return output.ReadOptions{}, fmt.Errorf("%w: since_ms is too large", app.ErrInvalidRequest)
	}
	if sinceMS != 0 && sinceUnixNano != 0 {
		return output.ReadOptions{}, fmt.Errorf("%w: since_ms and since_unix_nano cannot both be set", app.ErrInvalidRequest)
	}
	if sinceUnixNano != 0 {
		options.Since = time.Unix(0, sinceUnixNano)
	} else if sinceMS != 0 {
		options.Since = time.Now().Add(-time.Duration(sinceMS) * time.Millisecond)
	}
	if after != nil {
		cursor := output.Cursor(*after)
		options.After = &cursor
	}
	if matchExpression != "" {
		match, err := regexp.Compile(matchExpression)
		if err != nil {
			return output.ReadOptions{}, fmt.Errorf("%w: invalid match expression: %v", app.ErrInvalidRequest, err)
		}
		options.Match = match
	}
	options.Streams = streamMask(string(stream))
	if options.MaxBytes > maxBoundedReadBytes {
		options.MaxBytes = maxBoundedReadBytes
	}
	return options, nil
}

func waitOptionsFromProtocol(req protocol.WaitRequest) (app.WaitOptions, time.Duration, error) {
	if req.Name == "" {
		return app.WaitOptions{}, 0, fmt.Errorf("%w: wait name is required", app.ErrInvalidRequest)
	}
	if req.TimeoutMS <= 0 {
		return app.WaitOptions{}, 0, fmt.Errorf("%w: wait timeout must be positive", app.ErrInvalidRequest)
	}
	if req.TimeoutMS > maxWaitTimeoutMS {
		return app.WaitOptions{}, 0, fmt.Errorf("%w: wait timeout exceeds server maximum", app.ErrInvalidRequest)
	}
	options := app.WaitOptions{}
	if req.After != nil {
		cursor := output.Cursor(*req.After)
		options.After = &cursor
	}
	if req.Match != "" {
		match, err := regexp.Compile(req.Match)
		if err != nil {
			return app.WaitOptions{}, 0, fmt.Errorf("%w: invalid match expression: %v", app.ErrInvalidRequest, err)
		}
		options.Match = match
	}
	return options, time.Duration(req.TimeoutMS) * time.Millisecond, nil
}

func normalizeProtocolScope(req *protocol.Request) error {
	set := func(scope *string, root *string, all *bool) error {
		if *scope == "" {
			*scope = app.ScopeProject
		}
		if *scope != app.ScopeProject && *scope != app.ScopeGlobal {
			return protocol.NewWireError(protocol.ErrorInvalidRequest, "scope must be project or global", nil)
		}
		if root != nil && *scope == app.ScopeGlobal && *root != "" {
			return protocol.NewWireError(protocol.ErrorInvalidRequest, "global scope cannot include a project root", nil)
		}
		if all != nil && *scope == app.ScopeGlobal && *all {
			return protocol.NewWireError(protocol.ErrorInvalidRequest, "global scope conflicts with all; all already spans every scope", nil)
		}
		return nil
	}
	switch req.Op {
	case protocol.OpStart:
		return set(&req.Start.Scope, &req.Start.Root, nil)
	case protocol.OpList:
		return set(&req.List.Scope, nil, &req.List.All)
	case protocol.OpGet:
		return set(&req.Get.Scope, nil, nil)
	case protocol.OpOutput:
		return set(&req.Output.Scope, nil, nil)
	case protocol.OpFollow:
		return set(&req.Follow.Scope, nil, nil)
	case protocol.OpWait:
		return set(&req.Wait.Scope, nil, nil)
	case protocol.OpSignal:
		return set(&req.Signal.Scope, nil, nil)
	case protocol.OpStop:
		return set(&req.Stop.Scope, nil, nil)
	case protocol.OpRestart:
		return set(&req.Restart.Scope, &req.Restart.Root, nil)
	case protocol.OpRemove:
		return set(&req.Remove.Scope, nil, nil)
	case protocol.OpInputAttach:
		return set(&req.InputAttach.Scope, &req.InputAttach.Root, nil)
	}
	return nil
}

func streamMask(stream string) output.StreamMask {
	switch strings.ToLower(stream) {
	case "stdout":
		return output.StdoutMask
	case "stderr":
		return output.StderrMask
	case "system":
		return output.SystemMask
	case "both", "stdout+stderr", "stdout,stderr":
		return output.AllStreams
	default:
		return output.AllStreams
	}
}
