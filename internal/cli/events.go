package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/output"
	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
	"golang.org/x/term"
)

func eventsCommand(ctx context.Context, cmd *urfavecli.Command, writer io.Writer) error {
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	tail := cmd.Int("tail")
	if tail < 1 || tail > 2000 {
		return newCLIUsageError(fmt.Errorf("events tail must be between 1 and 2000"))
	}
	var since time.Time
	if value := cmd.String("since"); value != "" {
		duration, parseErr := time.ParseDuration(value)
		if parseErr != nil || duration <= 0 {
			return newCLIUsageError(fmt.Errorf("events --since must be a positive duration"))
		}
		since = time.Now().Add(-duration)
	}
	kinds := make([]protocol.EventKind, 0)
	for _, value := range cmd.StringSlice("kind") {
		kind := protocol.EventKind(value)
		if kind != protocol.EventLifecycle && kind != protocol.EventOperation {
			return newCLIUsageError(fmt.Errorf("events --kind must be lifecycle or operation"))
		}
		kinds = append(kinds, kind)
	}
	var match *regexp.Regexp
	if value := cmd.String("match"); value != "" {
		match, err = regexp.Compile(value)
		if err != nil {
			return newCLIUsageError(fmt.Errorf("events --match: %w", err))
		}
	}
	var after *protocol.Cursor
	if cmd.IsSet("after-cursor") {
		value := protocol.Cursor(cmd.Uint64("after-cursor"))
		after = &value
	}
	names := cmd.Args().Slice()
	if len(names) > 2000 {
		return newCLIUsageError(errors.New("events accepts at most 2000 names"))
	}
	for _, name := range names {
		if err := app.ValidateName(name); err != nil {
			return newCLIUsageError(err)
		}
	}
	maxBytes := cmd.Int("limit-bytes")
	if maxBytes < 0 {
		return newCLIUsageError(errors.New("events --limit-bytes must not be negative"))
	}
	runtimeDir := daemon.ResolveRuntimeDir(cmd.String("runtime-dir"))
	sinceUnixNano := int64(0)
	if !since.IsZero() {
		sinceUnixNano = since.UnixNano()
	}
	var page daemon.HistoryPage
	client, dialErr := daemon.Dial(ctx, daemon.NewRuntimePaths(runtimeDir).Socket)
	if dialErr == nil {
		defer client.Close()
		response, readErr := client.Events(ctx, protocol.EventsRequest{Scope: selection.scope, Root: selection.root, Cwd: selection.cwd, Names: names, SinceUnixNano: sinceUnixNano, Kinds: kinds, Failed: cmd.Bool("failed"), Match: cmd.String("match"), Tail: tail, AfterCursor: after, MaxBytes: maxBytes})
		if readErr == nil {
			page = daemon.HistoryPage{Events: response.Events, NextCursor: response.NextCursor, Truncated: response.Truncated, HasMore: response.HasMore}
		} else if isWireCode(readErr, string(protocol.ErrorInvalidRequest)) {
			return newCLIUsageError(errors.New(readErr.Error()))
		} else if !isWireCode(readErr, string(protocol.ErrorUnknownOperation)) {
			return readErr
		} else {
			dialErr = readErr
		}
	}
	if dialErr != nil {
		if client != nil {
			_ = client.Close()
		}
		history := daemon.NewEventHistory(runtimeDir, selection.scope, selection.root)
		page, err = history.Read(names, since, kinds, cmd.Bool("failed"), match, tail, after, maxBytes)
		if err != nil {
			if errors.Is(err, daemon.ErrHistoryCursorFuture) {
				return newCLIUsageError(err)
			}
			return err
		}
	}
	if cmd.Bool("json") {
		return writeEventsJSON(writer, page.Events, page.NextCursor, page.Truncated, page.HasMore)
	}
	if len(page.Events) == 0 {
		_, err = io.WriteString(writer, "No service events retained.\n")
		return err
	}
	return writeEventsHuman(writer, page.Events, cmd.Bool("full"), colorPolicyForWriter(writer))
}

func writeEventsJSON(writer io.Writer, events []protocol.HistoryEvent, next protocol.Cursor, truncated, more bool) error {
	encoder := json.NewEncoder(writer)
	for _, event := range events {
		value := map[string]any{"schema_version": 1, "type": "event", "cursor": event.Cursor, "time": event.Time, "kind": event.Kind, "name": event.Name, "event": event.Event}
		if event.Detail != "" {
			value["detail"] = event.Detail
		}
		if event.OperationID != "" {
			value["operation_id"] = event.OperationID
		}
		if event.Origin != "" {
			value["origin"] = event.Origin
		}
		if event.Outcome != "" {
			value["outcome"] = event.Outcome
		}
		if event.ExitCode != nil {
			value["exit_code"] = *event.ExitCode
		}
		if event.Signal != "" {
			value["signal"] = event.Signal
		}
		if event.LogCursor != nil {
			value["log_cursor"] = *event.LogCursor
		}
		if err := encoder.Encode(value); err != nil {
			return err
		}
	}
	return encoder.Encode(map[string]any{"schema_version": 1, "type": "metadata", "next_cursor": next, "truncated": truncated, "has_more": more})
}

func writeEventsHuman(writer io.Writer, events []protocol.HistoryEvent, full bool, colors colorPolicy) error {
	width := eventOutputWidth(writer)
	if full {
		if _, err := fmt.Fprintln(writer, "TIME NAME EVENT DETAIL"); err != nil {
			return err
		}
	} else {
		timestamp, name, event, detail := fitEventColumns("TIME", "NAME", "EVENT", "DETAIL", width)
		headerParts := make([]string, 0, 3)
		for _, part := range []string{timestamp, name, event} {
			if part != "" {
				headerParts = append(headerParts, part)
			}
		}
		header := strings.Join(headerParts, " ")
		if detail != "" {
			header += " " + detail
		}
		if _, err := fmt.Fprintln(writer, header); err != nil {
			return err
		}
	}
	for _, event := range events {
		timestamp := event.Time.Local().Format("15:04:05")
		name := compactEventText(event.Name)
		kind := compactEventText(event.Event)
		detail := output.StripTerminalControl(event.Detail)
		if full {
			if _, err := fmt.Fprintf(writer, "%s %s %s\n", timestamp, name, colors.apply(eventStyle(event), kind)); err != nil {
				return err
			}
			if event.LogCursor != nil {
				if detail != "" {
					detail += " "
				}
				detail += fmt.Sprintf("log_cursor=%d", *event.LogCursor)
			}
			if detail != "" {
				if _, err := fmt.Fprintln(writer, detail); err != nil {
					return err
				}
			}
			continue
		}
		detail = compactEventText(detail)
		timestamp, name, kind, detail = fitEventColumns(timestamp, name, kind, detail, width)
		parts := make([]string, 0, 3)
		for _, part := range []string{timestamp, name, colors.apply(eventStyle(event), kind)} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		line := strings.Join(parts, " ")
		if detail != "" {
			line += " " + detail
		}
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}
	return nil
}

func eventStyle(event protocol.HistoryEvent) ansiStyle {
	if eventFailedForRender(event) {
		return ansiRed
	}
	return ansiGreen
}
func eventFailedForRender(event protocol.HistoryEvent) bool {
	if event.Outcome != "" && event.Outcome != "success" || event.Event == "startup_failure" || event.Event == "relaunch_failure" || event.Event == "relaunch_exhausted" {
		return true
	}
	return event.Event == "exit" && (event.ExitCode == nil || *event.ExitCode != 0 || event.Signal != "")
}
func eventOutputWidth(writer io.Writer) int {
	if value, ok := writer.(interface{ Fd() uintptr }); ok {
		if width, _, err := term.GetSize(int(value.Fd())); err == nil && width > 0 {
			return width
		}
	}
	return 80
}
func compactEventText(value string) string {
	value = output.StripTerminalControl(value)
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}

func elideEventLine(value string, width int) string {
	if width < 1 {
		return ""
	}
	if utf8.RuneCountInString(value) <= width {
		return value
	}
	runes := []rune(value)
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func elideEventSuffix(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(value)
	return "…" + string(runes[len(runes)-(width-1):])
}

func fitEventColumns(timestamp, name, event, detail string, width int) (string, string, string, string) {
	if width < 1 {
		return "", "", "", ""
	}
	base := utf8.RuneCountInString(timestamp) + utf8.RuneCountInString(name) + utf8.RuneCountInString(event) + 2
	if base > width {
		nameBudget := width - utf8.RuneCountInString(timestamp) - utf8.RuneCountInString(event) - 2
		name = elideEventSuffix(name, nameBudget)
		base = utf8.RuneCountInString(timestamp) + utf8.RuneCountInString(name) + utf8.RuneCountInString(event) + 2
	}
	if base > width {
		event = elideEventLine(event, width-utf8.RuneCountInString(timestamp)-utf8.RuneCountInString(name)-2)
		base = utf8.RuneCountInString(timestamp) + utf8.RuneCountInString(name) + utf8.RuneCountInString(event) + 2
	}
	if base > width {
		return elideEventLine(strings.Join([]string{timestamp, name, event}, " "), width), "", "", ""
	}
	if detail != "" {
		detail = elideEventLine(detail, width-base-1)
		if detail == "" {
			return timestamp, name, event, ""
		}
	}
	return timestamp, name, event, detail
}
