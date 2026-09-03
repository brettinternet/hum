package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/protocol"
)

type listJSON struct {
	Processes []listProcessJSON `json:"processes"`
}

type listProcessJSON struct {
	Name         string           `json:"name"`
	Source       string           `json:"source"`
	Root         string           `json:"root"`
	PID          int              `json:"pid"`
	PGID         int              `json:"pgid"`
	Cwd          string           `json:"cwd"`
	Argv         []string         `json:"argv"`
	Start        time.Time        `json:"start"`
	LaunchCursor protocol.Cursor  `json:"launch_cursor"`
	NextCursor   *protocol.Cursor `json:"next_cursor,omitempty"`
	State        string           `json:"state"`
	Exit         *protocol.Exit   `json:"exit,omitempty"`
	ExitCode     int              `json:"exit_code,omitempty"`
	ExitedAt     time.Time        `json:"exited_at,omitempty"`
	RestartCount int              `json:"restart_count,omitempty"`
	Readiness    string           `json:"readiness,omitempty"`
	ReadyCursor  *protocol.Cursor `json:"ready_cursor,omitempty"`
}

// statusJSON is the stable, response-safe representation used by status.
// Keep this type separate from protocol.Process so status output does not
// expose protocol-only fields.
type statusJSON struct {
	Name         string           `json:"name"`
	Source       string           `json:"source,omitempty"`
	ProjectRoot  string           `json:"project_root"`
	PID          int              `json:"pid"`
	PGID         int              `json:"pgid"`
	Cwd          string           `json:"cwd"`
	Argv         []string         `json:"argv"`
	StartedAt    string           `json:"started_at"`
	State        string           `json:"state"`
	Readiness    string           `json:"readiness,omitempty"`
	ReadyCursor  *protocol.Cursor `json:"ready_cursor,omitempty"`
	ExitStatus   *int             `json:"exit_status"`
	RestartCount int              `json:"restart_count"`
	NextCursor   protocol.Cursor  `json:"next_cursor"`
}

func statusJSONFor(process app.Process) statusJSON {
	result := statusJSON{
		Name:         process.Name,
		Source:       process.Source,
		ProjectRoot:  process.Root,
		PID:          process.PID,
		PGID:         process.PGID,
		Cwd:          process.Cwd,
		Argv:         append([]string(nil), process.Argv...),
		StartedAt:    process.Start.Format(time.RFC3339Nano),
		State:        string(process.State),
		RestartCount: process.RestartCount,
		NextCursor:   protocol.Cursor(process.NextCursor),
	}
	result.Readiness, result.ReadyCursor = processReadinessFields(process)
	if result.Argv == nil {
		result.Argv = []string{}
	}
	if process.State == app.StateExited || process.Exit != nil {
		exitStatus := process.ExitCode
		result.ExitStatus = &exitStatus
	}
	return result
}

type runResult struct {
	Name        string           `json:"name"`
	Source      string           `json:"source,omitempty"`
	Argv        []string         `json:"argv,omitempty"`
	Outcome     string           `json:"outcome,omitempty"`
	Readiness   string           `json:"readiness,omitempty"`
	ReadyCursor *protocol.Cursor `json:"ready_cursor,omitempty"`
	PID         int              `json:"pid"`
	Cursor      protocol.Cursor  `json:"cursor"`
}

type stopResult struct {
	Name    string            `json:"name"`
	Status  string            `json:"status"`
	Process *protocol.Process `json:"process,omitempty"`
	Message string            `json:"message,omitempty"`
}

type restartResult struct {
	Name         string           `json:"name"`
	Source       string           `json:"source,omitempty"`
	Argv         []string         `json:"argv"`
	PID          int              `json:"pid"`
	Restarts     int              `json:"restarts"`
	LaunchCursor protocol.Cursor  `json:"launch_cursor"`
	Readiness    string           `json:"readiness,omitempty"`
	ReadyCursor  *protocol.Cursor `json:"ready_cursor,omitempty"`
}

type legacyRestartResult struct {
	Name         string          `json:"name"`
	PID          int             `json:"pid"`
	Restarts     int             `json:"restarts"`
	LaunchCursor protocol.Cursor `json:"launch_cursor"`
}

type shutdownResult struct {
	Status string `json:"status"`
}

func waitJSONFor(result app.WaitResult) protocol.WaitResponse {
	response := protocol.WaitResponse{
		Op:      protocol.OpWait,
		OK:      true,
		Outcome: protocol.WaitOutcome(result.Outcome),
		Cursor:  protocol.Cursor(result.Cursor),
	}
	if result.Exit != nil {
		exit := protocol.Exit{
			Code: result.Exit.ExitCode,
			Time: result.Exit.ExitedAt,
		}
		if result.Exit.Err != nil {
			exit.Error = result.Exit.Err.Error()
		}
		response.Exit = &exit
	}
	return response
}

func encodeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
func processJSON(process app.Process) listProcessJSON {
	result := listProcessJSON{
		Name:         process.Name,
		Source:       process.Source,
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
	if process.NextCursor != 0 {
		nextCursor := protocol.Cursor(process.NextCursor)
		result.NextCursor = &nextCursor
	}
	result.Readiness, result.ReadyCursor = processReadinessFields(process)
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
		argv := shellJoin(process.Argv)
		readiness, readyCursor := processReadinessFields(process)
		prefix := fmt.Sprintf("%s\t%s\tPID %d\tsource=%s\targv=%s", process.Name, process.State, process.PID, process.Source, argv)
		if all {
			prefix = fmt.Sprintf("%s: %s\t%s\tPID %d\tsource=%s\targv=%s", process.Root, process.Name, process.State, process.PID, process.Source, argv)
		}
		if readiness != "" {
			prefix += "\treadiness=" + readiness
			if readyCursor != nil {
				prefix += fmt.Sprintf("\tready_cursor=%d", *readyCursor)
			}
		}
		if _, err := fmt.Fprintln(w, prefix); err != nil {
			return err
		}
	}
	return nil
}
func shellJoin(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, shellEscape(arg))
	}
	return strings.Join(parts, " ")
}

func shellEscape(value string) string {
	if value == "" {
		return "''"
	}
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || strings.ContainsRune("_@%+=:,./-", ch) {
			continue
		}
		return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
	}
	return value
}

func renderManifestLaunchHuman(w io.Writer, result manifestLaunchResult) error {
	line := fmt.Sprintf("%s %s", result.Outcome, result.Name)
	if result.Source != "" {
		line += fmt.Sprintf(" (%s: %s)", result.Source, shellJoin(result.Argv))
	} else if len(result.Argv) != 0 {
		line += " argv=" + shellJoin(result.Argv)
	}
	if result.PID != nil {
		line += fmt.Sprintf(" pid=%d", *result.PID)
	}
	if result.LaunchCursor != nil {
		line += fmt.Sprintf(" launch_cursor=%d", *result.LaunchCursor)
	}
	if result.Readiness != "" {
		line += " readiness=" + result.Readiness
	}
	if result.ReadyCursor != nil {
		line += fmt.Sprintf(" ready_cursor=%d", *result.ReadyCursor)
	}
	if result.Error != "" {
		line += " error=" + result.Error
	}
	_, err := fmt.Fprintln(w, line)
	return err
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

func renderDownResults(w io.Writer, results []stopResult, jsonOutput bool) error {
	if len(results) == 0 {
		if jsonOutput {
			return nil
		}
		_, err := fmt.Fprintln(w, "Nothing is running in this project.")
		return err
	}
	for _, result := range results {
		if jsonOutput {
			if err := encodeJSON(w, result); err != nil {
				return err
			}
			continue
		}
		if err := renderStopHuman(w, result); err != nil {
			return err
		}
	}
	return nil
}

func renderRestartHuman(w io.Writer, result restartResult) error {
	line := fmt.Sprintf("%s restarted pid=%d restarts=%d launch_cursor=%d", result.Name, result.PID, result.Restarts, result.LaunchCursor)
	if result.Source != "" {
		line += fmt.Sprintf(" source=%s argv=%s", result.Source, shellJoin(result.Argv))
		if result.Readiness != "" {
			line += " readiness=" + result.Readiness
			if result.ReadyCursor != nil {
				line += fmt.Sprintf(" ready_cursor=%d", *result.ReadyCursor)
			}
		}
	}
	_, err := fmt.Fprintln(w, line)
	return err
}

func renderWaitHuman(w io.Writer, result app.WaitResult) error {
	if _, err := fmt.Fprintf(w, "outcome: %s\ncursor: %d\n", result.Outcome, result.Cursor); err != nil {
		return err
	}
	if result.Exit == nil {
		return nil
	}
	_, err := fmt.Fprintf(w, "exit_code: %d\n", result.Exit.ExitCode)
	return err
}

func renderStatusHuman(w io.Writer, process app.Process) error {
	status := statusJSONFor(process)
	if _, err := fmt.Fprintf(w,
		"name: %s\nsource: %s\nproject_root: %s\npid: %d\npgid: %d\ncwd: %s\nargv: %s\nstarted_at: %s\nstate: %s\n",
		status.Name, status.Source, status.ProjectRoot, status.PID, status.PGID, status.Cwd,
		shellJoin(status.Argv), status.StartedAt, status.State,
	); err != nil {
		return err
	}
	if status.Readiness != "" {
		if _, err := fmt.Fprintf(w, "readiness: %s\n", status.Readiness); err != nil {
			return err
		}
		if status.ReadyCursor != nil {
			if _, err := fmt.Fprintf(w, "ready_cursor: %d\n", *status.ReadyCursor); err != nil {
				return err
			}
		}
	}
	if status.ExitStatus == nil {
		if _, err := fmt.Fprintln(w, "exit_status: null"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(w, "exit_status: %d\n", *status.ExitStatus); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "restart_count: %d\nnext_cursor: %d\n", status.RestartCount, status.NextCursor)
	return err
}
