package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/protocol"
)

type listJSON struct {
	Processes []protocol.Process `json:"processes"`
}

type runResult struct {
	Name   string          `json:"name"`
	PID    int             `json:"pid"`
	Cursor protocol.Cursor `json:"cursor"`
}

type stopResult struct {
	Name    string            `json:"name"`
	Status  string            `json:"status"`
	Process *protocol.Process `json:"process,omitempty"`
	Message string            `json:"message,omitempty"`
}

type shutdownResult struct {
	Status string `json:"status"`
}

func encodeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func processJSON(process app.Process) protocol.Process {
	result := protocol.Process{
		Name:         process.Name,
		Root:         process.Root,
		PID:          process.PID,
		PGID:         process.PGID,
		Cwd:          process.Cwd,
		Argv:         append([]string(nil), process.Argv...),
		Start:        process.Start,
		LaunchCursor: protocol.Cursor(process.LaunchCursor),
		State:        string(process.State),
		ExitCode:     process.ExitCode,
		ExitedAt:     process.ExitedAt,
		RestartCount: process.RestartCount,
	}
	if result.Argv == nil {
		result.Argv = []string{}
	}
	if process.Exit != nil {
		exit := protocol.Exit{Code: process.Exit.ExitCode, Time: process.Exit.ExitedAt}
		if process.Exit.Err != nil {
			exit.Error = process.Exit.Err.Error()
		}
		result.Exit = &exit
	}
	return result
}

func outputJSON(result output.ReadResult) protocol.OutputResult {
	entries := make([]protocol.OutputEntry, 0, len(result.Entries))
	for _, entry := range result.Entries {
		entries = append(entries, protocol.OutputEntry{
			Cursor: protocol.Cursor(entry.Cursor),
			Stream: protocol.Stream(streamName(entry.Stream)),
			Time:   entry.Time,
			Text:   entry.Text,
		})
	}
	return protocol.OutputResult{
		Entries:        entries,
		Next:           cursorJSON(result.Next),
		Oldest:         cursorJSON(result.Oldest),
		Latest:         cursorJSON(result.Latest),
		EvictedThrough: cursorJSON(result.EvictedThrough),
		Truncated:      result.Truncated,
		More:           result.More,
	}
}

func cursorJSON(cursor *output.Cursor) *protocol.Cursor {
	if cursor == nil {
		return nil
	}
	value := protocol.Cursor(*cursor)
	return &value
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

func eventJSON(name string, event output.Event) protocol.StreamEvent {
	if event.Exit != nil {
		exit := protocol.Exit{Code: event.Exit.Code, Time: event.Exit.Time}
		return protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventExit, Name: name, Exit: &exit, Time: event.Exit.Time}
	}
	if event.Read == nil {
		return protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventOutput, Name: name, Entries: []protocol.OutputEntry{}}
	}
	result := outputJSON(*event.Read)
	eventType := protocol.EventOutput
	if result.Truncated || result.EvictedThrough != nil {
		eventType = protocol.EventEviction
	} else if len(result.Entries) == 0 && result.Next != nil {
		eventType = protocol.EventCursor
	}
	return protocol.StreamEvent{
		Op:             protocol.OpEvent,
		Type:           eventType,
		Name:           name,
		Entries:        result.Entries,
		Next:           result.Next,
		Oldest:         result.Oldest,
		Latest:         result.Latest,
		EvictedThrough: result.EvictedThrough,
		Truncated:      result.Truncated,
		More:           result.More,
		Result:         &result,
	}
}

func writeRawEntry(w io.Writer, entry output.Entry) error {
	_, err := io.WriteString(w, entry.Text)
	return err
}

func writeAttachedEntry(stdout, stderr io.Writer, entry output.Entry) error {
	switch entry.Stream {
	case output.Stderr, output.System:
		return writeRawEntry(stderr, entry)
	default:
		return writeRawEntry(stdout, entry)
	}
}

func writeLogEntries(w io.Writer, entries []output.Entry) error {
	for _, entry := range entries {
		if err := writeRawEntry(w, entry); err != nil {
			return err
		}
	}
	return nil
}

func writeCursorTrailer(w io.Writer, result output.ReadResult) error {
	next := output.Cursor(0)
	if result.Next != nil {
		next = *result.Next
	}
	trailer := fmt.Sprintf("next cursor: %d", next)
	if result.Truncated || result.EvictedThrough != nil {
		trailer += " (truncated)"
	}
	if result.More {
		trailer += " (more available)"
	}
	_, err := fmt.Fprintln(w, trailer)
	return err
}

func renderListHuman(w io.Writer, processes []app.Process, all bool) error {
	if len(processes) == 0 {
		_, err := fmt.Fprintln(w, stopUnavailableMessage)
		return err
	}
	for _, process := range processes {
		argv := strings.Join(process.Argv, " ")
		if all {
			if _, err := fmt.Fprintf(w, "%s: %s\t%s\tPID %d\t%s\n", process.Root, process.Name, process.State, process.PID, argv); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\tPID %d\t%s\n", process.Name, process.State, process.PID, argv); err != nil {
			return err
		}
	}
	return nil
}

func renderStopHuman(w io.Writer, result stopResult) error {
	switch result.Status {
	case "stopped":
		_, err := fmt.Fprintf(w, "%s stopped\n", result.Name)
		return err
	case "not_running":
		_, err := fmt.Fprintf(w, "%s not running\n", result.Name)
		return err
	default:
		_, err := fmt.Fprintf(w, "%s error: %s\n", result.Name, result.Message)
		return err
	}
}
