package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLogsStripTerminalControl(t *testing.T) {
	runtimeDir := hum006ListLogsTempDir(t, "terminal-control-runtime")
	hum006ListLogsStartDaemon(t, runtimeDir, 4096)
	project := hum006ListLogsProject(t, "terminal-control-project")

	script := "printf '\\033[31mred\\033[0m\\r\\n'; printf '\\033]0;title\\a\\033[32mgreen\\033[0m\\r\\n'; sleep 2"
	if stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "run", "bounded", "--detach", "--", "/bin/sh", "-c", script); err != nil {
		t.Fatalf("start bounded process: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	hum006ListLogsWaitForText(t, project, "bounded", "red\n")

	human, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "logs", "bounded")
	if err != nil {
		t.Fatalf("human bounded logs: %v (stderr=%q)", err, stderr)
	}
	if !strings.Contains(human, "red\n") || !strings.Contains(human, "green\n") || strings.Contains(human, "\x1b") || strings.Contains(human, "\r") {
		t.Fatalf("human bounded logs = %q, want stripped child text", human)
	}

	jsonOutput, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "logs", "bounded", "--json")
	if err != nil {
		t.Fatalf("JSON bounded logs: %v (stderr=%q)", err, stderr)
	}
	var response struct {
		Entries []struct {
			Text string `json:"text"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &response); err != nil {
		t.Fatalf("decode JSON bounded logs %q: %v", jsonOutput, err)
	}
	if len(response.Entries) != 2 || response.Entries[0].Text != "red\n" || response.Entries[1].Text != "green\n" {
		t.Fatalf("JSON bounded entries = %#v, want stripped red and green lines", response.Entries)
	}
	for _, entry := range response.Entries {
		if strings.Contains(entry.Text, "\x1b") || strings.Contains(entry.Text, "\r") {
			t.Fatalf("JSON bounded entry retained terminal control: %q", entry.Text)
		}
	}

	tailOutput, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "logs", "bounded", "--json", "--tail", "1")
	if err != nil {
		t.Fatalf("tail bounded logs: %v (stderr=%q)", err, stderr)
	}
	var tail struct {
		Entries []struct {
			Text string `json:"text"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(tailOutput), &tail); err != nil {
		t.Fatalf("decode tail bounded logs %q: %v", tailOutput, err)
	}
	if len(tail.Entries) != 1 || tail.Entries[0].Text != "green\n" {
		t.Fatalf("tail bounded entries = %#v, want stripped final line", tail.Entries)
	}

	followScript := "printf '\\033[31mreplay\\033[0m\\r\\n'; sleep .2; printf '\\033[32mlive\\033[0m\\r\\n'; sleep 1"
	if stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "run", "follow-raw", "--detach", "--", "/bin/sh", "-c", followScript); err != nil {
		t.Fatalf("start follow process: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	hum006ListLogsWaitForText(t, project, "follow-raw", "replay\n")
	followContext, cancelFollow := context.WithTimeout(context.Background(), 700*time.Millisecond)
	followOutput, stderr, err := hum006ListLogsRunAt(t, project, followContext, "logs", "follow-raw", "--follow")
	cancelFollow()
	if err != nil {
		t.Fatalf("raw follow logs: %v (stderr=%q)", err, stderr)
	}
	if !strings.Contains(followOutput, "\x1b[31mreplay\x1b[0m\r\n") || !strings.Contains(followOutput, "\x1b[32mlive\x1b[0m\r\n") {
		t.Fatalf("raw follow output = %q, want raw replay and live entries", followOutput)
	}

	attachedContext, cancelAttached := context.WithTimeout(context.Background(), 300*time.Millisecond)
	attachedOutput, stderr, err := hum006ListLogsRunAt(t, project, attachedContext, "run", "attached-raw", "--", "/bin/sh", "-c", "printf '\\033[36mattached\\033[0m\\r\\n'; sleep 1")
	cancelAttached()
	if err != nil {
		t.Fatalf("raw attached run: %v (stderr=%q)", err, stderr)
	}
	if !strings.Contains(attachedOutput, "\x1b[36mattached\x1b[0m\r\n") {
		t.Fatalf("raw attached output = %q, want terminal controls preserved", attachedOutput)
	}

	var help, helpErr bytes.Buffer
	root := NewRootCommand("test", "test", &help, &helpErr)
	if err := root.Run(context.Background(), []string{"hum", "logs", "--help"}); err != nil {
		t.Fatalf("logs help: %v", err)
	}
	lowerHelp := strings.ToLower(help.String())
	for _, want := range []string{"terminal-control-stripped", "system entries remain raw", "follow --match", "selected entries are emitted raw", "per entry", "no --raw flag"} {
		if !strings.Contains(lowerHelp, want) {
			t.Errorf("logs help missing %q: %q", want, help.String())
		}
	}
	for _, command := range root.Commands {
		for _, flag := range command.Flags {
			for _, name := range flag.Names() {
				if name == "raw" {
					t.Errorf("%s command exposes a raw opt-out flag", command.Name)
				}
			}
		}
	}
	if helpErr.Len() != 0 {
		t.Fatalf("logs help stderr = %q", helpErr.String())
	}
}
