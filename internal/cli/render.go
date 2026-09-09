package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
	"unicode"
	"unicode/utf8"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/project"
	"hum/internal/protocol"

	"golang.org/x/term"
)

// colorPolicy enables the small, fixed palette used by human lifecycle
// renderers. It is intentionally computed at the output boundary so all
// renderers apply the same TTY and environment rules.
type colorPolicy struct {
	enabled bool
}

type ansiStyle string

const (
	ansiReset                   = "\x1b[0m"
	ansiBold          ansiStyle = "\x1b[1m"
	ansiGreen         ansiStyle = "\x1b[32m"
	ansiYellow        ansiStyle = "\x1b[33m"
	ansiBlue          ansiStyle = "\x1b[34m"
	ansiMagenta       ansiStyle = "\x1b[35m"
	ansiCyan          ansiStyle = "\x1b[36m"
	ansiBrightYellow  ansiStyle = "\x1b[93m"
	ansiBrightBlue    ansiStyle = "\x1b[94m"
	ansiBrightMagenta ansiStyle = "\x1b[95m"
	ansiBrightCyan    ansiStyle = "\x1b[96m"
	ansiDim           ansiStyle = "\x1b[2m"
	ansiRed           ansiStyle = "\x1b[31m"
)

// terminalWriter reports whether writer ultimately targets a terminal.
// NewRootCommand and attached up wrap stdout, so unwrap those adapters before
// checking the file descriptor.
func terminalWriter(writer io.Writer) bool {
	switch wrapped := writer.(type) {
	case *jsonOutputTracker:
		return wrapped != nil && terminalWriter(wrapped.writer)
	case *synchronizedWriter:
		return wrapped != nil && terminalWriter(wrapped.writer)
	}
	fdWriter, ok := writer.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(fdWriter.Fd()))
}

// colorPolicyForWriter reports whether writer is stdout-like terminal output.
// NO_COLOR is presence-based: even an empty value disables styling.
func colorPolicyForWriter(writer io.Writer) colorPolicy {
	if _, present := os.LookupEnv("NO_COLOR"); present || os.Getenv("TERM") == "dumb" {
		return colorPolicy{}
	}
	return colorPolicy{enabled: terminalWriter(writer)}
}

// synchronizedWriter keeps aggregate followed logs and final up summaries
// from interleaving writes on the shared stdout stream.
type synchronizedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *synchronizedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(data)
}

func withSynchronizedWriter(writer io.Writer, write func(io.Writer) error) error {
	if synchronized, ok := writer.(*synchronizedWriter); ok {
		synchronized.mu.Lock()
		defer synchronized.mu.Unlock()
		return write(synchronized.writer)
	}
	return write(writer)
}

func (p colorPolicy) apply(style ansiStyle, value string) string {
	if !p.enabled || style == "" || value == "" {
		return value
	}
	return string(style) + value + ansiReset
}

func processStateStyle(state app.State, exitCode int) ansiStyle {
	switch state {
	case app.StateRunning:
		return ansiGreen
	case app.StateStopped:
		return ansiCyan
	case app.StateExited:
		if exitCode == 0 {
			return ansiDim
		}
		return ansiRed
	default:
		return ""
	}
}

func readinessStyle(readiness string) ansiStyle {
	switch readiness {
	case app.ReadinessReady, app.ReadinessRunningUnverified:
		return ansiGreen
	case app.ReadinessStarting:
		return ansiYellow
	default:
		return ""
	}
}

func manifestOutcomeStyle(outcome string) ansiStyle {
	switch outcome {
	case "started", "already_running", "running_unverified":
		return ansiGreen
	case "recovery_pending":
		return ansiYellow
	case "error", "exited_before_ready", "timed_out", "definition_drift", "recovery_exhausted", "skipped":
		return ansiRed
	default:
		return ""
	}
}

type listCell struct {
	prefix string
	text   string
	style  ansiStyle
}

func (c listCell) plain() string {
	return c.prefix + c.text
}

func (c listCell) render(policy colorPolicy) string {
	return c.prefix + policy.apply(c.style, c.text)
}

type listRow []listCell

func plainListCell(text string) listCell {
	return listCell{text: text}
}

func styledListCell(text string, style ansiStyle) listCell {
	return listCell{text: text, style: style}
}

func prefixedStyledListCell(prefix, text string, style ansiStyle) listCell {
	return listCell{prefix: prefix, text: text, style: style}
}

type listTable struct {
	header listRow
	rows   []listRow
}

func buildListTable(processes []app.Process, all bool) listTable {
	header := listRow{styledListCell("NAME", ansiBold), styledListCell("STATE", ansiBold), styledListCell("PID", ansiBold), styledListCell("SOURCE", ansiBold), styledListCell("ARGV", ansiBold)}
	if all {
		header = listRow{styledListCell("ROOT", ansiBold), styledListCell("NAME", ansiBold), styledListCell("STATE", ansiBold), styledListCell("PID", ansiBold), styledListCell("SOURCE", ansiBold), styledListCell("ARGV", ansiBold)}
	}
	table := listTable{header: header, rows: make([]listRow, 0, len(processes))}
	for _, process := range processes {
		readiness, readyCursor := processReadinessFields(process)
		pid := ""
		if process.PID != 0 {
			pid = fmt.Sprintf("PID %d", process.PID)
		}
		state := styledListCell(string(process.State), processStateStyle(process.State, process.ExitCode))
		row := listRow{
			plainListCell(process.Name), state, plainListCell(pid),
			plainListCell("source=" + process.Source), plainListCell("argv=" + shellJoin(process.Argv)),
		}
		if all {
			row = listRow{
				plainListCell(process.Root), plainListCell(process.Name), state, plainListCell(pid),
				plainListCell("source=" + process.Source), plainListCell("argv=" + shellJoin(process.Argv)),
			}
		}
		if process.Followers > 0 {
			row = append(row, plainListCell(fmt.Sprintf("followers=%d", process.Followers)))
		}
		if process.TTY {
			row = append(row, plainListCell("tty=true"))
		}
		if readiness != "" {
			row = append(row, prefixedStyledListCell("readiness=", readiness, readinessStyle(readiness)))
			if readyCursor != nil {
				row = append(row, plainListCell(fmt.Sprintf("ready_cursor=%d", *readyCursor)))
			}
		}
		if process.Exit != nil && process.Exit.Signal != nil {
			row = append(row, plainListCell("exit: "+signalHumanText(process.Exit.Signal.Name, process.Exit.Signal.Number)))
		}
		if effectiveProcessRestart(process) == app.RestartOnFailure {
			row = append(row, plainListCell("restart=on-failure"))
		}
		table.rows = append(table.rows, row)
	}
	return table
}

func writeListTable(w io.Writer, table listTable, policy colorPolicy) error {
	rows := make([]listRow, 0, len(table.rows)+1)
	rows = append(rows, table.header)
	rows = append(rows, table.rows...)
	widths := make([]int, len(table.header))
	for _, row := range rows {
		if len(row) > len(widths) {
			widths = append(widths, make([]int, len(row)-len(widths))...)
		}
		for index, cell := range row {
			if width := utf8.RuneCountInString(cell.plain()); width > widths[index] {
				widths[index] = width
			}
		}
	}
	for _, row := range rows {
		for index, cell := range row {
			if _, err := io.WriteString(w, cell.render(policy)); err != nil {
				return err
			}
			if index+1 < len(row) {
				padding := widths[index] - utf8.RuneCountInString(cell.plain()) + 2
				if _, err := io.WriteString(w, strings.Repeat(" ", padding)); err != nil {
					return err
				}
			}
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	return nil
}

type listJSON struct {
	Processes []listProcessJSON         `json:"processes"`
	Warnings  []protocol.StartupWarning `json:"warnings,omitempty"`
}

type listProcessJSON struct {
	Name         string           `json:"name"`
	Source       string           `json:"source"`
	Scope        string           `json:"scope"`
	Root         string           `json:"root"`
	ProjectRoot  string           `json:"project_root"`
	TTY          bool             `json:"tty"`
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
	Followers    int              `json:"followers"`
	Restart      string           `json:"restart"`
	Relaunches   int              `json:"relaunches"`
	NextLaunchAt *time.Time       `json:"next_launch_at,omitempty"`
	Readiness    string           `json:"readiness,omitempty"`
	ReadyCursor  *protocol.Cursor `json:"ready_cursor,omitempty"`
}

// statusJSON is the stable, response-safe representation used by status.
// Keep this type separate from protocol.Process so status output does not
// expose protocol-only fields.
type statusJSON struct {
	Name         string                    `json:"name"`
	Source       string                    `json:"source,omitempty"`
	Scope        string                    `json:"scope"`
	ProjectRoot  string                    `json:"project_root"`
	TTY          bool                      `json:"tty"`
	PID          int                       `json:"pid"`
	PGID         int                       `json:"pgid"`
	Cwd          string                    `json:"cwd"`
	Argv         []string                  `json:"argv"`
	StartedAt    string                    `json:"started_at"`
	State        string                    `json:"state"`
	Readiness    string                    `json:"readiness,omitempty"`
	ReadyCursor  *protocol.Cursor          `json:"ready_cursor,omitempty"`
	ExitStatus   *int                      `json:"exit_status"`
	Signal       *protocol.SignalInfo      `json:"signal,omitempty"`
	RestartCount int                       `json:"restart_count"`
	Followers    int                       `json:"followers"`
	Restart      string                    `json:"restart"`
	Relaunches   int                       `json:"relaunches"`
	NextLaunchAt *time.Time                `json:"next_launch_at,omitempty"`
	NextCursor   protocol.Cursor           `json:"next_cursor"`
	Warnings     []protocol.StartupWarning `json:"warnings,omitempty"`
}

func statusJSONFor(process app.Process) statusJSON {
	scope := process.Scope
	if scope == "" {
		scope = "project"
	}
	result := statusJSON{
		Name:         process.Name,
		Source:       process.Source,
		Scope:        scope,
		ProjectRoot:  process.Root,
		TTY:          process.TTY,
		PID:          process.PID,
		PGID:         process.PGID,
		Cwd:          process.Cwd,
		Argv:         append([]string(nil), process.Argv...),
		StartedAt:    process.Start.Format(time.RFC3339Nano),
		State:        string(process.State),
		RestartCount: process.RestartCount,
		Followers:    process.Followers,
		Restart:      string(effectiveProcessRestart(process)),
		Relaunches:   process.Relaunches,
		NextLaunchAt: process.NextLaunchAt,
		NextCursor:   protocol.Cursor(process.NextCursor),
	}
	result.Readiness, result.ReadyCursor = processReadinessFields(process)
	if result.Argv == nil {
		result.Argv = []string{}
	}
	if process.State == app.StateExited {
		exitStatus := process.ExitCode
		result.ExitStatus = &exitStatus
		if process.Exit != nil && process.Exit.Signal != nil {
			signal := protocol.SignalInfo{Name: process.Exit.Signal.Name, Number: process.Exit.Signal.Number}
			result.Signal = &signal
		}
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
	Restart      string           `json:"restart"`
	Relaunches   int              `json:"relaunches"`
	NextLaunchAt *time.Time       `json:"next_launch_at,omitempty"`
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
		Op:              protocol.OpWait,
		OK:              true,
		Outcome:         protocol.WaitOutcome(result.Outcome),
		Cursor:          protocol.Cursor(result.Cursor),
		ProcessObserved: result.ProcessObserved,
	}
	if result.Exit != nil {
		exit := protocol.Exit{
			Code: result.Exit.ExitCode,
			Time: result.Exit.ExitedAt,
		}
		if result.Exit.Err != nil {
			exit.Error = result.Exit.Err.Error()
		}
		if result.Exit.Signal != nil {
			exit.Signal = &protocol.SignalInfo{Name: result.Exit.Signal.Name, Number: result.Exit.Signal.Number}
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

type jsonErrorEnvelope struct {
	Error *protocol.WireError `json:"error"`
}

func writeJSONError(w io.Writer, wire *protocol.WireError) error {
	return encodeJSON(w, jsonErrorEnvelope{Error: wire})
}

func writeJSONErrorEvent(w io.Writer, name string, wire *protocol.WireError) error {
	return encodeJSON(w, protocol.StreamEvent{
		Op: protocol.OpEvent, Type: protocol.EventError, Name: name, Error: wire,
	})
}

func writeStartupWarnings(w io.Writer, warnings []protocol.StartupWarning) error {
	if len(warnings) == 0 || w == nil {
		return nil
	}
	parts := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		parts = append(parts, fmt.Sprintf("%s %s/%s: %s", warning.Outcome, warning.Project, warning.Name, warning.Message))
	}
	_, err := fmt.Fprintf(w, "hum warning: startup reconciliation: %s\n", strings.Join(parts, "; "))
	return err
}
func processJSON(process app.Process) listProcessJSON {
	scope := process.Scope
	if scope == "" {
		scope = "project"
	}
	result := listProcessJSON{
		Name:         process.Name,
		Source:       process.Source,
		Scope:        scope,
		Root:         process.Root,
		ProjectRoot:  process.Root,
		TTY:          process.TTY,
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
		Followers:    process.Followers,
		Restart:      string(effectiveProcessRestart(process)),
		Relaunches:   process.Relaunches,
		NextLaunchAt: process.NextLaunchAt,
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
		if process.Exit.Signal != nil {
			exit.Signal = &protocol.SignalInfo{Name: process.Exit.Signal.Name, Number: process.Exit.Signal.Number}
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
		if event.Exit.SignalName != "" {
			exit.Signal = &protocol.SignalInfo{Name: event.Exit.SignalName, Number: event.Exit.SignalNumber}
		}
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

// aggregateEventTime is the event time to report for an aggregate logs JSON
// event: the newest entry's time when entries are present, or the zero value
// (omitted on encoding) when there is none to derive it from.
func aggregateEventTime(event output.Event) time.Time {
	if event.Read == nil || len(event.Read.Entries) == 0 {
		return time.Time{}
	}
	newest := event.Read.Entries[0].Time
	for _, entry := range event.Read.Entries[1:] {
		if entry.Time.After(newest) {
			newest = entry.Time
		}
	}
	return newest
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

type aggregateLogRenderer struct {
	mu        sync.Mutex
	writer    io.Writer
	errWriter io.Writer
	colors    colorPolicy
	errColors colorPolicy
	json      bool
}

func newAggregateLogRenderer(writer, errWriter io.Writer, jsonOutput bool) *aggregateLogRenderer {
	return &aggregateLogRenderer{
		writer: writer, errWriter: errWriter,
		colors: colorPolicyForWriter(writer), errColors: colorPolicyForWriter(errWriter),
		json: jsonOutput,
	}
}

func processLogPrefixStyle(name string) ansiStyle {
	styles := [...]ansiStyle{
		ansiBlue, ansiMagenta, ansiCyan, ansiYellow,
		ansiBrightBlue, ansiBrightMagenta, ansiBrightCyan,
	}
	// FNV-1a keeps a process's color stable across commands and invocations.
	hash := uint32(2166136261)
	for index := 0; index < len(name); index++ {
		hash ^= uint32(name[index])
		hash *= 16777619
	}
	return styles[hash%uint32(len(styles))]
}

func processLogPrefix(colors colorPolicy, name string) string {
	return colors.apply(processLogPrefixStyle(name), "["+name+"]")
}

func (r *aggregateLogRenderer) writeEvent(name string, event output.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.json {
		streamEvent := eventJSON(name, event)
		if streamEvent.Time.IsZero() {
			streamEvent.Time = aggregateEventTime(event)
		}
		return encodeJSON(r.writer, streamEvent)
	}
	if event.Read == nil {
		return nil
	}
	for _, entry := range event.Read.Entries {
		if err := writeAggregateLogEntry(r.writer, r.colors, name, entry); err != nil {
			return err
		}
	}
	return nil
}

func (r *aggregateLogRenderer) writeError(name string, wire *protocol.WireError) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.json {
		return encodeJSON(r.writer, protocol.StreamEvent{
			Op: protocol.OpEvent, Type: protocol.EventError, Name: name, Error: wire,
		})
	}
	message := "aggregate logs failed"
	if wire != nil && wire.Message != "" {
		message = wire.Message
	}
	_, err := fmt.Fprintf(r.writer, "%s error: %s\n", processLogPrefix(r.colors, name), message)
	return err
}

// writeNotLaunched reports a declared name with no daemon record yet (never
// launched, or skipped behind a blocked dependency). JSON output emits no
// event at all for it; human output prints one stdout-only line.
func (r *aggregateLogRenderer) writeNotLaunched(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.json {
		return nil
	}
	_, err := fmt.Fprintf(r.writer, "%s not launched\n", processLogPrefix(r.colors, name))
	return err
}

func (r *aggregateLogRenderer) writeCursor(name string, result output.ReadResult) error {
	if r.json {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
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
	_, err := fmt.Fprintf(r.errWriter, "%s %s\n", processLogPrefix(r.errColors, name), trailer)
	return err
}

func writeAggregateLogEntry(w io.Writer, colors colorPolicy, name string, entry output.Entry) error {
	_, err := fmt.Fprintf(w, "%s %s", processLogPrefix(colors, name), entry.Text)
	return err
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

func renderListHuman(w io.Writer, processes []app.Process, all bool, roots ...string) error {
	return renderListHumanWithPolicy(w, processes, all, colorPolicyForWriter(w), roots...)
}

func renderListHumanWithPolicy(w io.Writer, processes []app.Process, all bool, policy colorPolicy, roots ...string) error {
	if len(processes) == 0 {
		message := stopUnavailableMessage
		if len(roots) != 0 && roots[0] != "" {
			message = fmt.Sprintf("Nothing is running in %s. Use hum list --all to see every scope.", roots[0])
		}
		_, err := fmt.Fprintln(w, message)
		return err
	}
	if all {
		groups := make(map[string][]app.Process)
		for _, process := range processes {
			groups[process.Root] = append(groups[process.Root], process)
		}
		ordered := make([]string, 0, len(groups))
		for root := range groups {
			ordered = append(ordered, root)
		}
		sort.Strings(ordered)
		for index, root := range ordered {
			if index > 0 {
				if _, err := fmt.Fprintln(w); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w, "Project: %s (hum --project %s)\n", root, shellEscape(root)); err != nil {
				return err
			}
			if err := writeLifecycleTable(w, buildListTable(groups[root], false), policy); err != nil {
				return err
			}
		}
		return nil
	}
	return writeLifecycleTable(w, buildListTable(processes, false), policy)
}

func renderStatusSummaryHuman(w io.Writer, processes []app.Process, roots ...string) error {
	return renderStatusSummaryHumanWithPolicy(w, processes, colorPolicyForWriter(w), roots...)
}

func renderStatusSummaryHumanWithPolicy(w io.Writer, processes []app.Process, policy colorPolicy, roots ...string) error {
	if len(processes) == 0 {
		message := stopUnavailableMessage
		if len(roots) != 0 && roots[0] != "" {
			message = fmt.Sprintf("Nothing is running in %s. Use hum list --all to see every scope.", roots[0])
		}
		_, err := fmt.Fprintln(w, message)
		return err
	}
	header := listRow{
		styledListCell("NAME", ansiBold), styledListCell("STATE", ansiBold), styledListCell("PID", ansiBold),
		styledListCell("READINESS", ansiBold), styledListCell("RESTART", ansiBold), styledListCell("FOLLOWERS", ansiBold),
	}
	table := listTable{header: header, rows: make([]listRow, 0, len(processes))}
	for _, process := range processes {
		readiness, _ := processReadinessFields(process)
		if readiness == "" {
			readiness = "-"
		}
		pid := "-"
		if process.PID != 0 {
			pid = strconv.Itoa(process.PID)
		}
		table.rows = append(table.rows, listRow{
			plainListCell(process.Name),
			styledListCell(string(process.State), processStateStyle(process.State, process.ExitCode)),
			plainListCell(pid),
			styledListCell(readiness, readinessStyle(readiness)),
			plainListCell(string(effectiveProcessRestart(process))),
			plainListCell(strconv.Itoa(process.Followers)),
		})
	}
	return writeLifecycleTable(w, table, policy)
}

func writeLifecycleTable(w io.Writer, table listTable, policy colorPolicy) error {
	if policy.enabled {
		return writeListTable(w, table, policy)
	}

	// Keep the tabwriter path byte-for-byte stable when styling is disabled.
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := io.WriteString(tw, strings.Join(listRowText(table.header), "\t")+"\n"); err != nil {
		return err
	}
	for _, row := range table.rows {
		if _, err := fmt.Fprintln(tw, strings.Join(listRowText(row), "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func listRowText(row listRow) []string {
	values := make([]string, len(row))
	for index, cell := range row {
		values[index] = cell.plain()
	}
	return values
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

type manifestProgressRenderer struct {
	lines    chan string
	done     chan struct{}
	err      error
	selector string
	colors   colorPolicy
}

func newManifestProgressRenderer(writer io.Writer, declarationCount int, selectors ...string) *manifestProgressRenderer {
	return newManifestProgressRendererWithPolicy(writer, declarationCount, colorPolicyForWriter(writer), selectors...)
}

func newManifestProgressRendererWithPolicy(writer io.Writer, declarationCount int, colors colorPolicy, selectors ...string) *manifestProgressRenderer {
	selector := ""
	if len(selectors) != 0 {
		selector = selectors[0]
	}
	r := &manifestProgressRenderer{
		lines:    make(chan string, 2*declarationCount),
		done:     make(chan struct{}),
		selector: selector,
		colors:   colors,
	}
	go func() {
		defer close(r.done)
		for line := range r.lines {
			if r.err != nil {
				continue
			}
			text := line + "\n"
			written, err := io.WriteString(writer, text)
			if err == nil && written != len(text) {
				err = io.ErrShortWrite
			}
			r.err = err
		}
	}()
	return r
}

func (r *manifestProgressRenderer) writeInitial(definition project.Definition, result manifestLaunchResult) {
	result = manifestResultWithSelector(result, r.selector)
	if !r.colors.enabled {
		r.writeLine(manifestProgressInitialLine(definition, result))
		return
	}
	r.writeLine(manifestProgressInitialLineWithPolicy(definition, result, r.colors))
}

func (r *manifestProgressRenderer) writeTerminal(result manifestLaunchResult) {
	result = manifestResultWithSelector(result, r.selector)
	if !r.colors.enabled {
		r.writeLine(manifestProgressTerminalLine(result))
		return
	}
	r.writeLine(manifestProgressTerminalLineWithPolicy(result, r.colors))
}

func (r *manifestProgressRenderer) writeLine(line string) {
	if line != "" {
		r.lines <- line
	}
}

func (r *manifestProgressRenderer) Close() error {
	close(r.lines)
	<-r.done
	return r.err
}

func manifestProgressInitialLine(definition project.Definition, result manifestLaunchResult) string {
	return manifestProgressInitialLineWithPolicy(definition, result, colorPolicy{})
}

func manifestProgressInitialLineWithPolicy(definition project.Definition, result manifestLaunchResult, colors colorPolicy) string {
	prefix := "hum up: " + manifestProgressText(result.Name) + ": "
	switch result.Outcome {
	case "skipped":
		return prefix + manifestProgressSkippedTextWithPolicy(result, colors)
	case "error":
		return prefix + colors.apply(ansiRed, "error") + ": " + manifestProgressText(result.Error)
	case "exited_before_ready":
		line := prefix + colors.apply(ansiRed, "exited before readiness")
		if result.Signal != nil {
			line += "; exit: " + signalHumanText(result.Signal.Name, result.Signal.Number)
		}
		return line + "; inspect retained logs: " + manifestProgressText(projectCommand(result.ProjectSelector, "logs "+result.Name))
	case "timed_out":
		return prefix + colors.apply(ansiRed, "readiness timed out") + "; inspect retained logs: " + manifestProgressText(projectCommand(result.ProjectSelector, "logs "+result.Name))
	case "started", "already_running":
		action := colors.apply(manifestOutcomeStyle(result.Outcome), manifestProgressAction(result))
		if manifestProgressWaitsForReadiness(definition, result) {
			return prefix + action + "; " + colors.apply(ansiYellow, "waiting for readiness")
		}
		if result.Readiness == app.ReadinessReady {
			return prefix + action + "; " + colors.apply(ansiGreen, "ready")
		}
		return prefix + action + "; readiness unverified"
	case "running_unverified":
		return prefix + colors.apply(ansiGreen, "started") + "; readiness unverified"
	case "definition_drift":
		return prefix + manifestProgressDriftDetailWithPolicy(result, colors)
	default:
		return prefix + colors.apply(manifestOutcomeStyle(result.Outcome), manifestProgressText(result.Outcome))
	}
}

func manifestProgressTerminalLine(result manifestLaunchResult) string {
	return manifestProgressTerminalLineWithPolicy(result, colorPolicy{})
}

func manifestProgressTerminalLineWithPolicy(result manifestLaunchResult, colors colorPolicy) string {
	prefix := "hum up: " + manifestProgressText(result.Name) + ": "
	switch result.Outcome {
	case "skipped":
		return prefix + manifestProgressSkippedTextWithPolicy(result, colors)
	case "error":
		return prefix + colors.apply(ansiRed, "error") + ": " + manifestProgressText(result.Error)
	case "exited_before_ready":
		line := prefix + colors.apply(ansiRed, "exited before readiness")
		if result.Signal != nil {
			line += "; exit: " + signalHumanText(result.Signal.Name, result.Signal.Number)
		}
		return line + "; inspect retained logs: " + manifestProgressText(projectCommand(result.ProjectSelector, "logs "+result.Name))
	case "timed_out":
		return prefix + colors.apply(ansiRed, "readiness timed out") + "; inspect retained logs: " + manifestProgressText(projectCommand(result.ProjectSelector, "logs "+result.Name))
	case "started", "already_running":
		if result.Readiness == app.ReadinessReady {
			return prefix + colors.apply(ansiGreen, "ready")
		}
		return prefix + colors.apply(manifestOutcomeStyle(result.Outcome), manifestProgressAction(result)) + "; readiness unverified"
	case "running_unverified":
		return prefix + colors.apply(ansiGreen, "started") + "; readiness unverified"
	case "definition_drift":
		return prefix + manifestProgressDriftDetailWithPolicy(result, colors)
	default:
		return prefix + colors.apply(manifestOutcomeStyle(result.Outcome), manifestProgressText(result.Outcome))
	}
}

func manifestProgressSkippedText(result manifestLaunchResult) string {
	line := "skipped (blocked by " + manifestProgressText(strings.Join(result.BlockedBy, ", ")) + ")"
	switch result.ExistingState {
	case "running", "stopped", "exited":
		return line + "; existing process " + result.ExistingState
	default:
		return line + "; not launched"
	}
}

func manifestProgressSkippedTextWithPolicy(result manifestLaunchResult, colors colorPolicy) string {
	if !colors.enabled {
		return manifestProgressSkippedText(result)
	}
	line := colors.apply(ansiRed, "skipped") + " (blocked by " + manifestProgressText(strings.Join(result.BlockedBy, ", ")) + ")"
	switch result.ExistingState {
	case "running", "stopped", "exited":
		return line + "; existing process " + result.ExistingState
	default:
		return line + "; not launched"
	}
}

func manifestProgressDriftDetailWithPolicy(result manifestLaunchResult, colors colorPolicy) string {
	return colors.apply(ansiRed, "definition_drift") + " (" + manifestProgressText(strings.Join(result.ChangedFields, ", ")) + "); run " + manifestProgressText(result.Guidance)
}

func manifestProgressAction(result manifestLaunchResult) string {
	switch result.Outcome {
	case "running_unverified", "started":
		return "started"
	case "already_running":
		return "already running"
	default:
		return result.Outcome
	}
}

func manifestProgressText(value string) string {
	value = output.StripTerminalControl(value)
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}

func buildManifestLaunchTable(results []manifestLaunchResult) listTable {
	table := listTable{
		header: listRow{
			styledListCell("NAME", ansiBold),
			styledListCell("RESULT", ansiBold),
			styledListCell("STATE", ansiBold),
			styledListCell("PID", ansiBold),
		},
		rows: make([]listRow, 0, len(results)),
	}
	for _, result := range results {
		state := result.State
		if state == "" {
			state = "-"
		}
		pid := "-"
		if result.PID != nil {
			pid = fmt.Sprintf("%d", *result.PID)
		}
		exitCode := 0
		if result.ExitCode != nil {
			exitCode = *result.ExitCode
		}
		table.rows = append(table.rows, listRow{
			plainListCell(result.Name),
			styledListCell(manifestOutcomeLabel(result.Outcome), manifestOutcomeStyle(result.Outcome)),
			styledListCell(state, processStateStyle(app.State(result.State), exitCode)),
			plainListCell(pid),
		})
	}
	return table
}

func manifestOutcomeLabel(outcome string) string {
	switch outcome {
	case "already_running":
		return "already running"
	case "running_unverified":
		return "running unverified"
	case "exited_before_ready":
		return "exited before ready"
	case "timed_out":
		return "timed out"
	case "definition_drift":
		return "definition drift"
	case "recovery_pending":
		return "recovery pending"
	case "recovery_exhausted":
		return "recovery exhausted"
	case "removed_definition":
		return "removed definition"
	default:
		return outcome
	}
}

func renderManifestLaunchTableWithPolicy(w io.Writer, results []manifestLaunchResult, colors colorPolicy) error {
	return writeListTable(w, buildManifestLaunchTable(results), colors)
}

func renderManifestLaunchHuman(w io.Writer, result manifestLaunchResult) error {
	return renderManifestLaunchHumanWithPolicy(w, result, colorPolicy{})
}

func renderManifestLaunchHumanWithPolicy(w io.Writer, result manifestLaunchResult, colors colorPolicy) error {
	if result.Outcome == "error" && result.Source == "manifest" && len(result.Argv) == 0 {
		// No definition (and no retained launch spec) resolved for this name:
		// undefinedManifestDefinition leaves Source as the synthetic "manifest"
		// placeholder with an empty Argv. Render plainly instead of the
		// (source: argv) parenthetical, which would otherwise print an empty
		// "(manifest: )". A genuinely declared manifest process always has a
		// non-empty Argv and keeps the general result line below.
		_, err := fmt.Fprintf(w, "%s %s: %s\n", colors.apply(ansiRed, "error"), result.Name, result.Error)
		return err
	}
	if result.Outcome == "skipped" {
		line := fmt.Sprintf("%s: %s (blocked by %s)", result.Name, colors.apply(ansiRed, "skipped"), strings.Join(result.BlockedBy, ", "))
		switch result.ExistingState {
		case "running", "stopped", "exited":
			line += "; existing process " + result.ExistingState
		default:
			line += "; not launched"
		}
		_, err := fmt.Fprintln(w, line)
		return err
	}
	line := colors.apply(manifestOutcomeStyle(result.Outcome), result.Outcome) + " " + result.Name
	if result.Source != "" {
		line += fmt.Sprintf(" (%s: %s)", result.Source, shellJoin(result.Argv))
	} else if len(result.Argv) != 0 {
		line += " argv=" + shellJoin(result.Argv)
	}
	if result.State != "" {
		line += " state=" + colors.apply(processStateStyle(app.State(result.State), valueOrZero(result.ExitCode)), result.State)
	}
	if result.PID != nil {
		line += fmt.Sprintf(" pid=%d", *result.PID)
	}
	if result.LaunchCursor != nil {
		line += fmt.Sprintf(" launch_cursor=%d", *result.LaunchCursor)
	}
	if result.Readiness != "" {
		line += " readiness=" + colors.apply(readinessStyle(result.Readiness), result.Readiness)
	}
	if result.ReadinessConfigured {
		line += " readiness_match=" + result.ReadinessMatch
	}
	if result.ReadyCursor != nil {
		line += fmt.Sprintf(" ready_cursor=%d", *result.ReadyCursor)
	}
	if result.Outcome == "recovery_pending" || result.Outcome == "recovery_exhausted" || result.Outcome == "removed_definition" {
		line += fmt.Sprintf(" restart=%s relaunches=%d", result.Restart, result.Relaunches)
		if result.NextLaunchAt != nil {
			line += " next_launch_at=" + result.NextLaunchAt.Format(time.RFC3339Nano)
		}
	}
	if len(result.ChangedFields) != 0 {
		line += " changed_fields=" + strings.Join(result.ChangedFields, ",")
	}
	if result.Guidance != "" {
		line += " guidance=" + result.Guidance
	}
	if result.Error != "" {
		line += " error=" + result.Error
	}
	if result.Signal != nil {
		line += " exit: " + signalHumanText(result.Signal.Name, result.Signal.Number)
	}
	_, err := fmt.Fprintln(w, line)
	return err
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func signalHumanText(name string, number int) string {
	return fmt.Sprintf("signal %s (%d)", name, number)
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

func renderDownResults(w io.Writer, results []stopResult, jsonOutput bool, roots ...string) error {
	if len(results) == 0 {
		if jsonOutput {
			return nil
		}
		message := "Nothing is running in this project."
		if len(roots) != 0 && roots[0] != "" {
			message = fmt.Sprintf("Nothing is running in %s. Use hum list --all to see every scope.", roots[0])
		}
		_, err := fmt.Fprintln(w, message)
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

func renderWaitHuman(w io.Writer, name string, result app.WaitResult) error {
	if _, err := fmt.Fprintf(w, "outcome: %s\ncursor: %d\n", result.Outcome, result.Cursor); err != nil {
		return err
	}
	if result.Outcome == app.WaitTimedOut && !result.ProcessObserved {
		if _, err := fmt.Fprintf(w, "no process named %q was observed during the wait; check the name or start it first.\n", name); err != nil {
			return err
		}
	}
	if result.Exit == nil {
		return nil
	}
	if result.Exit.Signal != nil {
		_, err := fmt.Fprintf(w, "exit: %s\n", signalHumanText(result.Exit.Signal.Name, result.Exit.Signal.Number))
		return err
	}
	_, err := fmt.Fprintf(w, "exit_code: %d\n", result.Exit.ExitCode)
	return err
}

func renderStatusHuman(w io.Writer, process app.Process) error {
	return renderStatusHumanWithPolicy(w, process, colorPolicyForWriter(w))
}

func renderStatusHumanWithPolicy(w io.Writer, process app.Process, colors colorPolicy) error {
	status := statusJSONFor(process)
	restartLabel := status.Restart
	exhausted := status.Restart == string(app.RestartOnFailure) && status.Relaunches == 5 && status.NextLaunchAt == nil && status.ExitStatus != nil && *status.ExitStatus != 0
	if exhausted {
		restartLabel = "on-failure (" + colors.apply(ansiRed, "gave up after 5 relaunch attempts") + ")"
	}
	if _, err := fmt.Fprintf(w,
		"name: %s\nsource: %s\nproject_root: %s\ntty: %t\n",
		status.Name, status.Source, status.ProjectRoot, status.TTY,
	); err != nil {
		return err
	}
	if status.PID != 0 {
		if _, err := fmt.Fprintf(w, "pid: %d\n", status.PID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w,
		"pgid: %d\ncwd: %s\nargv: %s\nstarted_at: %s\nstate: %s\nrestart: %s\n",
		status.PGID, status.Cwd, shellJoin(status.Argv), status.StartedAt,
		colors.apply(processStateStyle(process.State, process.ExitCode), status.State), restartLabel,
	); err != nil {
		return err
	}
	if status.NextLaunchAt != nil {
		remaining := time.Until(*status.NextLaunchAt)
		seconds := int64(0)
		if remaining > 0 {
			seconds = int64((remaining + time.Second - 1) / time.Second)
		}
		if _, err := fmt.Fprintf(w, "relaunching in %ds (attempt %d/5)\n", seconds, status.Relaunches+1); err != nil {
			return err
		}
	}
	if status.Readiness != "" {
		if _, err := fmt.Fprintf(w, "readiness: %s\n", colors.apply(readinessStyle(status.Readiness), status.Readiness)); err != nil {
			return err
		}
		if status.ReadyCursor != nil {
			if _, err := fmt.Fprintf(w, "ready_cursor: %d\n", *status.ReadyCursor); err != nil {
				return err
			}
		}
	}
	if status.ExitStatus != nil {
		if _, err := fmt.Fprintf(w, "exit_status: %d\n", *status.ExitStatus); err != nil {
			return err
		}
	}
	if status.Signal != nil {
		if _, err := fmt.Fprintf(w, "exit: %s\n", signalHumanText(status.Signal.Name, status.Signal.Number)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "relaunches: %d\nrestart_count: %d\nfollowers: %d\nnext_cursor: %d\n", status.Relaunches, status.RestartCount, status.Followers, status.NextCursor)
	return err
}
