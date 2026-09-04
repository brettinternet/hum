package daemon

import (
	"errors"
	"fmt"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/protocol"
)

func wireRequestFromProtocol(req protocol.Request) (wireRequest, error) {
	wire := wireRequest{Op: string(req.Op)}
	switch req.Op {
	case protocol.OpHello:
		if req.Hello == nil {
			return wireRequest{}, errors.New("hello request is missing version")
		}
		wire.Version = req.Hello.Version
	case protocol.OpStart:
		if req.Start == nil {
			return wireRequest{}, errors.New("start request is missing payload")
		}
		wire.Name, wire.Argv, wire.Cwd, wire.Root, wire.Env = req.Start.Name, req.Start.Argv, req.Start.Cwd, req.Start.Root, req.Start.Env
		wire.Source, wire.Ready = req.Start.Source, wireReadinessConfigFromProtocol(req.Start.Ready)
	case protocol.OpList:
		if req.List == nil {
			return wireRequest{}, errors.New("list request is missing payload")
		}
		wire.Cwd, wire.All, wire.IncludeCompleted = req.List.Cwd, req.List.All, req.List.IncludeCompleted
	case protocol.OpGet:
		if req.Get == nil {
			return wireRequest{}, errors.New("get request is missing payload")
		}
		wire.Name, wire.Cwd = req.Get.Name, req.Get.Cwd
	case protocol.OpOutput:
		if req.Output == nil {
			return wireRequest{}, errors.New("output request is missing payload")
		}
		wire = wireRequestFromProtocolOutput(string(req.Op), req.Output.Name, req.Output.Cwd, req.Output.After, req.Output.Tail, req.Output.Stream, req.Output.Match, req.Output.MaxEntries, req.Output.MaxBytes)
	case protocol.OpFollow:
		if req.Follow == nil {
			return wireRequest{}, errors.New("follow request is missing payload")
		}
		wire = wireRequestFromProtocolOutput(string(req.Op), req.Follow.Name, req.Follow.Cwd, req.Follow.After, req.Follow.Tail, req.Follow.Stream, req.Follow.Match, req.Follow.MaxEntries, req.Follow.MaxBytes)
	case protocol.OpWait:
		if req.Wait == nil {
			return wireRequest{}, errors.New("wait request is missing payload")
		}
		wire = wireRequestFromProtocolWait(req.Wait)
	case protocol.OpSignal:
		if req.Signal == nil {
			return wireRequest{}, errors.New("signal request is missing payload")
		}
		wire.Name, wire.Cwd, wire.Signal = req.Signal.Name, req.Signal.Cwd, req.Signal.Signal
	case protocol.OpStop:
		if req.Stop == nil {
			return wireRequest{}, errors.New("stop request is missing payload")
		}
		wire.Name, wire.Cwd = req.Stop.Name, req.Stop.Cwd
	case protocol.OpRestart:
		if req.Restart == nil {
			return wireRequest{}, errors.New("restart request is missing payload")
		}
		wire.Name, wire.Cwd, wire.Root = req.Restart.Name, req.Restart.Cwd, req.Restart.Root
		wire.Update, wire.Argv, wire.Env = req.Restart.Update, req.Restart.Argv, req.Restart.Env
		wire.Source, wire.Ready = req.Restart.Source, wireReadinessConfigFromProtocol(req.Restart.Ready)
	case protocol.OpRemove:
		if req.Remove == nil {
			return wireRequest{}, errors.New("remove request is missing payload")
		}
		wire.Name, wire.Cwd = req.Remove.Name, req.Remove.Cwd
	case protocol.OpShutdown:
		if req.Shutdown == nil {
			return wireRequest{}, errors.New("shutdown request is missing payload")
		}
		wire.Force = req.Shutdown.Force
	default:
		return wireRequest{}, fmt.Errorf("unsupported operation %q", req.Op)
	}
	return wire, nil
}
func wireReadinessConfigFromProtocol(config *protocol.ReadinessConfig) *wireReadinessConfig {
	if config == nil {
		return nil
	}
	return &wireReadinessConfig{Match: config.Match, Timeout: config.Timeout}
}

func protocolReadinessConfigFromWire(config *wireReadinessConfig) *protocol.ReadinessConfig {
	if config == nil {
		return nil
	}
	return &protocol.ReadinessConfig{Match: config.Match, Timeout: config.Timeout}
}

func appReadinessConfigFromWire(config *wireReadinessConfig) *app.ReadinessConfig {
	if config == nil {
		return nil
	}
	return &app.ReadinessConfig{Match: config.Match, Timeout: config.Timeout}
}

func wireRequestFromProtocolOutput(op, name, cwd string, after *protocol.Cursor, tail int, stream protocol.Stream, match string, maxEntries, maxBytes int) wireRequest {
	wire := wireRequest{Op: op, Name: name, Cwd: cwd, Tail: tail, Stream: string(stream), Match: match, MaxEntries: maxEntries, MaxBytes: maxBytes}
	if after != nil {
		value := uint64(*after)
		wire.After = &value
	}
	return wire
}

func wireRequestFromProtocolWait(req *protocol.WaitRequest) wireRequest {
	wire := wireRequest{Op: string(protocol.OpWait), Name: req.Name, Cwd: req.Cwd, Match: req.Match, TimeoutMS: req.TimeoutMS}
	if req.After != nil {
		value := uint64(*req.After)
		wire.After = &value
	}
	return wire
}

func writeProtocolError(encoder *protocol.Encoder, op protocol.Operation, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, protocol.ErrMalformed) || errors.Is(err, protocol.ErrOversized) || errors.Is(err, protocol.ErrUnknownOperation) || errors.Is(err, protocol.ErrMissingOperation) {
		return encoder.EncodeResponse(protocol.ErrorResponse{Op: op, OK: false, Error: protocol.WireErrorForDecode(err)})
	}
	return encoder.EncodeResponse(protocol.ErrorResponse{Op: op, OK: false, Error: wireErrorToProtocol(protocolWireError(err))})
}

func writeProtocolResponse(encoder *protocol.Encoder, response wireResponse) error {
	if response.Error != nil {
		return encoder.EncodeResponse(protocol.ErrorResponse{Op: protocol.Operation(response.Op), OK: false, Error: wireErrorToProtocol(response.Error)})
	}
	switch response.Op {
	case "start":
		return encoder.EncodeResponse(protocol.StartResponse{Op: protocol.OpStart, OK: response.OK, Process: optionalProtocolProcess(response.Process), Error: nil})
	case "list":
		return encoder.EncodeResponse(protocol.ListResponse{Op: protocol.OpList, OK: response.OK, Processes: protocolProcessesFromWire(response.Processes)})
	case "get":
		return encoder.EncodeResponse(protocol.GetResponse{Op: protocol.OpGet, OK: response.OK, Process: optionalProtocolGetProcess(response.Process)})
	case "output":
		return encoder.EncodeResponse(protocol.OutputResponse{Op: protocol.OpOutput, OK: response.OK, Entries: protocolEntriesFromWire(response.Entries), Next: protocolCursorFromUint64(response.Next), Oldest: protocolCursorFromUint64(response.Oldest), Latest: protocolCursorFromUint64(response.Latest), EvictedThrough: protocolCursorFromUint64(response.EvictedThrough), Truncated: response.Truncated, More: response.More})
	case "signal":
		return encoder.EncodeResponse(protocol.SignalResponse{Op: protocol.OpSignal, OK: response.OK})
	case "stop":
		return encoder.EncodeResponse(protocol.StopResponse{Op: protocol.OpStop, OK: response.OK, Process: optionalProtocolProcess(response.Process)})
	case "restart":
		return encoder.EncodeResponse(protocol.RestartResponse{Op: protocol.OpRestart, OK: response.OK, Process: optionalProtocolProcess(response.Process)})
	case "remove":
		return encoder.EncodeResponse(protocol.RemoveResponse{Op: protocol.OpRemove, OK: response.OK})
	case "wait":
		return encoder.EncodeResponse(protocolWaitResponseFromWire(response))
	case "shutdown":
		return encoder.EncodeResponse(protocol.ShutdownResponse{Op: protocol.OpShutdown, OK: response.OK, Processes: protocolProcessesFromWire(response.Processes)})
	case "event":
		return encoder.EncodeResponse(protocolStreamEventFromWire(response))
	default:
		return encoder.EncodeResponse(protocol.Response{Op: protocol.Operation(response.Op), OK: response.OK, Processes: protocolProcessesFromWire(response.Processes), Process: optionalProtocolProcess(response.Process), Entries: protocolEntriesFromWire(response.Entries), Error: nil})
	}
}

func wireErrorToProtocol(wire *wireError) *protocol.WireError {
	if wire == nil {
		return nil
	}
	details := wire.Details
	if wire.Code == "version_mismatch" && details == nil {
		details = protocol.VersionMismatchDetails{Client: 0, Daemon: wire.DaemonVersion}
	}
	if wire.Code == "active_processes" && details == nil {
		details = append([]string(nil), wire.Processes...)
	}
	return protocol.NewWireError(protocol.ErrorCode(wire.Code), wire.Message, details)
}

func protocolProcessFromWire(item wireProcess) protocol.Process {
	result := protocol.Process{
		Name: item.Name, Source: item.Source, Root: item.Root, PID: item.PID, PGID: item.PGID,
		Cwd: item.Cwd, Argv: append([]string(nil), item.Argv...), Start: item.Start,
		LaunchCursor: protocol.Cursor(item.LaunchCursor), State: item.State,
		ExitCode: item.ExitCode, ExitedAt: item.ExitedAt, RestartCount: item.RestartCount,
	}
	if item.Readiness != nil {
		result.Readiness = &protocol.Readiness{
			State: item.Readiness.State, Cursor: protocolCursorFromUint64(item.Readiness.Cursor),
			Time: item.Readiness.Time, Match: item.Readiness.Match,
		}
	}
	if item.Exit != nil {
		exitCode := item.Exit.Code
		if exitCode == 0 && item.Exit.ExitCode != 0 {
			exitCode = item.Exit.ExitCode
		}
		result.Exit = &protocol.Exit{Code: exitCode, Time: item.Exit.Time, Error: item.Exit.Error}
	}
	return result
}

func protocolGetProcessFromWire(item wireProcess) protocol.Process {
	result := protocolProcessFromWire(item)
	if item.NextCursor != nil {
		nextCursor := protocol.Cursor(*item.NextCursor)
		result.NextCursor = &nextCursor
	}
	return result
}

func optionalProtocolGetProcess(item *wireProcess) *protocol.Process {
	if item == nil {
		return nil
	}
	result := protocolGetProcessFromWire(*item)
	return &result
}

func optionalProtocolProcess(item *wireProcess) *protocol.Process {
	if item == nil {
		return nil
	}
	result := protocolProcessFromWire(*item)
	return &result
}

func protocolProcessesFromWire(items []wireProcess) []protocol.Process {
	result := make([]protocol.Process, 0, len(items))
	for _, item := range items {
		result = append(result, protocolProcessFromWire(item))
	}
	return result
}

func protocolEntriesFromWire(items []wireEntry) []protocol.OutputEntry {
	result := make([]protocol.OutputEntry, 0, len(items))
	for _, item := range items {
		result = append(result, protocol.OutputEntry{Cursor: protocol.Cursor(item.Cursor), Stream: protocol.Stream(item.Stream), Time: item.Time, Text: item.Text})
	}
	return result
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

func protocolCursorFromUint64(value *uint64) *protocol.Cursor {
	if value == nil {
		return nil
	}
	cursor := protocol.Cursor(*value)
	return &cursor
}

func protocolStreamEventFromWire(response wireResponse) protocol.StreamEvent {
	event := protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventType(response.Type), Name: response.Name, Entries: protocolEntriesFromWire(response.Entries), Next: protocolCursorFromUint64(response.Next), Oldest: protocolCursorFromUint64(response.Oldest), Latest: protocolCursorFromUint64(response.Latest), EvictedThrough: protocolCursorFromUint64(response.EvictedThrough), Truncated: response.Truncated, More: response.More, Cursor: protocolCursorFromUint64(response.Cursor), Ready: response.Ready}
	if response.Exit != nil {
		event.Exit = &protocol.Exit{Code: response.Exit.Code, Time: response.Exit.Time, Error: response.Exit.Error}
		event.Time = response.Exit.Time
	}
	if response.Error != nil {
		event.Error = wireErrorToProtocol(response.Error)
	}
	return event
}

func protocolWaitResponseFromWire(response wireResponse) protocol.WaitResponse {
	var cursor protocol.Cursor
	if response.Cursor != nil {
		cursor = protocol.Cursor(*response.Cursor)
	}
	var exit *protocol.Exit
	if response.Exit != nil {
		exit = &protocol.Exit{Code: response.Exit.Code, Time: response.Exit.Time, Error: response.Exit.Error}
	}
	return protocol.WaitResponse{Op: protocol.OpWait, OK: response.OK, Outcome: protocol.WaitOutcome(response.Outcome), Cursor: cursor, Exit: exit, Error: wireErrorToProtocol(response.Error)}
}

func wireResponseFromWait(result app.WaitResult) wireResponse {
	cursor := uint64(result.Cursor)
	response := wireResponse{Op: string(protocol.OpWait), OK: true, Outcome: string(result.Outcome), Cursor: &cursor}
	if result.Exit != nil {
		response.Exit = &wireExit{Code: result.Exit.ExitCode, Error: errorString(result.Exit.Err), Time: result.Exit.ExitedAt}
	}
	return response
}

func wireResponseFromRead(op string, result output.ReadResult) wireResponse {
	wire := wireReadResultFromOutput(result)
	return wireResponse{Op: op, OK: true, Entries: wire.Entries, Next: wire.Next, Oldest: wire.Oldest, Latest: wire.Latest, EvictedThrough: wire.EvictedThrough, Truncated: wire.Truncated, More: wire.More}
}
func protocolStreamEventFromOutput(name string, event output.Event) protocol.StreamEvent {
	if event.Read != nil {
		result := wireReadResultFromOutput(*event.Read)
		wire := wireResponseFromRead("event", *event.Read)
		wire.Name = name
		wire.Type = eventTypeForRead(*event.Read)
		wire.Entries = result.Entries
		wire.Next, wire.Oldest, wire.Latest, wire.EvictedThrough = result.Next, result.Oldest, result.Latest, result.EvictedThrough
		wire.Truncated, wire.More = result.Truncated, result.More
		return protocolStreamEventFromWire(wire)
	}
	if event.Exit != nil {
		return protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventExit, Name: name, Exit: &protocol.Exit{Code: event.Exit.Code, Time: event.Exit.Time}, Time: event.Exit.Time}
	}
	return protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventOutput, Name: name}
}
