//go:build !windows

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"hum/internal/daemon"
	"hum/internal/project"
	"hum/internal/protocol"
)

type eventCommandFixture struct {
	root, runtime string
	history       *daemon.EventHistory
}

func newEventCommandFixture(t *testing.T) eventCommandFixture {
	t.Helper()
	root := t.TempDir()
	canonical, err := project.CanonicalPath(root)
	if err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(t.TempDir(), "runtime")
	return eventCommandFixture{root: canonical, runtime: runtime, history: daemon.NewEventHistory(runtime, protocol.ScopeProject, canonical)}
}

func (f eventCommandFixture) append(t *testing.T, events ...protocol.HistoryEvent) {
	t.Helper()
	for _, event := range events {
		if _, err := f.history.Append(event); err != nil {
			t.Fatal(err)
		}
	}
}

func (f eventCommandFixture) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	base := []string{"hum", "--project", f.root, "events", "--runtime-dir", f.runtime}
	err := NewRootCommand("test", "test", &out, &out).Run(context.Background(), append(base, args...))
	return out.String(), err
}

func decodeEventOutput(t *testing.T, output string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	values := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var value map[string]any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		values = append(values, value)
	}
	return values
}

func TestEventsNoArgs(t *testing.T) {
	fixture := newEventCommandFixture(t)
	for i := 0; i < 60; i++ {
		fixture.append(t, protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "launch", Detail: string(rune('a' + i%26))})
	}
	output, err := fixture.run(t, "--json")
	if err != nil {
		t.Fatal(err)
	}
	values := decodeEventOutput(t, output)
	if len(values) != 51 || values[0]["cursor"] != float64(11) || values[49]["cursor"] != float64(60) || values[50]["type"] != "metadata" {
		t.Fatalf("default recent window = %#v", values)
	}
}

func TestEventsEmptyState(t *testing.T) {
	fixture := newEventCommandFixture(t)
	output, err := fixture.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if output != "No service events retained.\n" {
		t.Fatalf("output=%q", output)
	}
}

func TestEventsNames(t *testing.T) {
	fixture := newEventCommandFixture(t)
	fixture.append(t,
		protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "launch"},
		protocol.HistoryEvent{Name: "web", Kind: protocol.EventLifecycle, Event: "launch"},
		protocol.HistoryEvent{Name: "db", Kind: protocol.EventLifecycle, Event: "launch"},
	)
	output, err := fixture.run(t, "api", "web", "unknown", "--json")
	if err != nil {
		t.Fatal(err)
	}
	values := decodeEventOutput(t, output)
	if len(values) != 3 || values[0]["name"] != "api" || values[1]["name"] != "web" {
		t.Fatalf("named events=%#v", values)
	}
}

func TestEventsFilters(t *testing.T) {
	fixture := newEventCommandFixture(t)
	now := time.Now().UTC()
	fixture.append(t,
		protocol.HistoryEvent{Time: now.Add(-2 * time.Hour), Name: "api", Kind: protocol.EventLifecycle, Event: "startup_failure", Detail: "boom old"},
		protocol.HistoryEvent{Time: now, Name: "api", Kind: protocol.EventOperation, Event: "start", Outcome: "failure", Detail: "boom operation"},
		protocol.HistoryEvent{Time: now, Name: "api", Kind: protocol.EventLifecycle, Event: "startup_failure", Detail: "boom current"},
		protocol.HistoryEvent{Time: now, Name: "web", Kind: protocol.EventLifecycle, Event: "startup_failure", Detail: "boom other"},
	)
	output, err := fixture.run(t, "api", "--since", "1h", "--kind", "lifecycle", "--failed", "--match", "current$", "--tail", "1", "--json")
	if err != nil {
		t.Fatal(err)
	}
	values := decodeEventOutput(t, output)
	if len(values) != 2 || values[0]["cursor"] != float64(3) {
		t.Fatalf("composed filters=%#v", values)
	}

	for _, args := range [][]string{{"--since", "0s"}, {"--kind", "other"}, {"--match", "["}, {"--tail", "0"}, {"--tail", "2001"}, {"bad/name"}} {
		runtime := filepath.Join(t.TempDir(), "must-not-contact")
		candidate := eventCommandFixture{root: fixture.root, runtime: runtime}
		if _, err := candidate.run(t, args...); err == nil {
			t.Fatalf("args %v unexpectedly succeeded", args)
		}
		if _, statErr := os.Stat(runtime); !os.IsNotExist(statErr) {
			t.Fatalf("args %v contacted runtime before validation: %v", args, statErr)
		}
	}
}

func TestEventsPaging(t *testing.T) {
	fixture := newEventCommandFixture(t)
	for i := 0; i < 5; i++ {
		fixture.append(t, protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "launch", Detail: strings.Repeat("x", 80)})
	}
	first, err := fixture.run(t, "--after-cursor", "0", "--tail", "2", "--json")
	if err != nil {
		t.Fatal(err)
	}
	page1 := decodeEventOutput(t, first)
	if len(page1) != 3 || page1[0]["cursor"] != float64(1) || page1[1]["cursor"] != float64(2) || page1[2]["next_cursor"] != float64(2) || page1[2]["has_more"] != true {
		t.Fatalf("first page=%#v", page1)
	}
	second, err := fixture.run(t, "--after-cursor", "2", "--tail", "2", "--json")
	if err != nil {
		t.Fatal(err)
	}
	page2 := decodeEventOutput(t, second)
	if page2[0]["cursor"] != float64(3) || page2[1]["cursor"] != float64(4) {
		t.Fatalf("second page=%#v", page2)
	}
	empty, err := fixture.run(t, "--after-cursor", "0", "--match", "never", "--json")
	if err != nil {
		t.Fatal(err)
	}
	emptyPage := decodeEventOutput(t, empty)
	// A daemonless reader reports the newest durable event, not the writer's
	// unused cursor reservation.
	if len(emptyPage) != 1 || emptyPage[0]["next_cursor"] != float64(5) || emptyPage[0]["has_more"] != false {
		t.Fatalf("empty filtered page=%#v", emptyPage)
	}
	if _, err := fixture.run(t, "--after-cursor", "65"); err == nil {
		t.Fatal("future cursor unexpectedly succeeded")
	}
	limited, err := fixture.run(t, "--after-cursor", "0", "--limit-bytes", "250", "--json")
	if err != nil {
		t.Fatal(err)
	}
	limitedPage := decodeEventOutput(t, limited)
	metadata := limitedPage[len(limitedPage)-1]
	if len(limitedPage) >= 6 || metadata["has_more"] != true {
		t.Fatalf("byte-limited page=%#v", limitedPage)
	}
}

func TestEventsCompletion(t *testing.T) {
	stdout, stderr, err := runCompletionForTest(t, "ev", "--generate-shell-completion")
	if err != nil || !strings.Contains(stdout, "events") || stderr != "" {
		t.Fatalf("command completion: stdout=%q stderr=%q err=%v", stdout, stderr, err)
	}
	for prefix, want := range map[string]string{"--k": "--kind", "--f": "--failed", "--after-c": "--after-cursor"} {
		stdout, stderr, err = runCompletionForTest(t, "events", prefix, "--generate-shell-completion")
		if err != nil || !strings.Contains(stdout, want) || stderr != "" {
			t.Fatalf("completion %s: stdout=%q stderr=%q err=%v", want, stdout, stderr, err)
		}
	}
}

func TestEventsWidth(t *testing.T) {
	events := []protocol.HistoryEvent{{Time: time.Now(), Name: strings.Repeat("service", 20) + "-suffix", Kind: protocol.EventLifecycle, Event: "startup_failure", Detail: strings.Repeat("detail", 40)}}
	var compact bytes.Buffer
	if err := writeEventsHuman(&compact, events, false, colorPolicy{}); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(compact.String(), "\n"), "\n") {
		if utf8.RuneCountInString(line) > 80 {
			t.Fatalf("fallback line width=%d: %q", utf8.RuneCountInString(line), line)
		}
	}
	for _, width := range []int{1, 5, 12, 24} {
		timestamp, name, event, detail := fitEventColumns("12:34:56", events[0].Name, events[0].Event, events[0].Detail, width)
		line := strings.TrimSpace(strings.Join([]string{timestamp, name, event, detail}, " "))
		if utf8.RuneCountInString(line) > width {
			t.Fatalf("narrow width %d produced %d: %q", width, utf8.RuneCountInString(line), line)
		}
	}
	var full bytes.Buffer
	if err := writeEventsHuman(&full, events, true, colorPolicy{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(full.String(), events[0].Detail) {
		t.Fatal("--full-equivalent renderer elided detail")
	}
}

func TestEventsColor(t *testing.T) {
	detail := "user\x1b[31m-controlled"
	events := []protocol.HistoryEvent{
		{Name: "green-name", Kind: protocol.EventLifecycle, Event: "launch", Detail: detail},
		{Name: "red-name", Kind: protocol.EventLifecycle, Event: "startup_failure", Detail: detail},
	}
	var colored bytes.Buffer
	if err := writeEventsHuman(&colored, events, false, colorPolicy{enabled: true}); err != nil {
		t.Fatal(err)
	}
	value := colored.String()
	if !strings.Contains(value, ansiGreenString("launch")) || !strings.Contains(value, ansiRedString("startup_failure")) {
		t.Fatalf("semantic colors missing: %q", value)
	}
	for _, uncontrolled := range []string{ansiGreenString("green-name"), ansiRedString("red-name"), "\x1b[31m-controlled"} {
		if strings.Contains(value, uncontrolled) {
			t.Fatalf("colored user-controlled text %q in %q", uncontrolled, value)
		}
	}
	var plain bytes.Buffer
	if err := writeEventsHuman(&plain, events, false, colorPolicy{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.String(), "\x1b") {
		t.Fatalf("plain output contains escape: %q", plain.String())
	}
}

func TestEventsJSON(t *testing.T) {
	exitCode := 7
	events := []protocol.HistoryEvent{
		{Cursor: 1, Time: time.Unix(1, 0).UTC(), Kind: protocol.EventOperation, Name: "api", Event: "start", Detail: strings.Repeat("x", 200), OperationID: "op-1", Origin: "cli", Outcome: "success"},
		{Cursor: 2, Time: time.Unix(2, 0).UTC(), Kind: protocol.EventLifecycle, Name: "api", Event: "exit", OperationID: "op-1", ExitCode: &exitCode, Signal: "TERM"},
	}
	var out bytes.Buffer
	if err := writeEventsJSON(&out, events, 2, true, false); err != nil {
		t.Fatal(err)
	}
	values := decodeEventOutput(t, out.String())
	if len(values) != 3 || values[0]["schema_version"] != float64(1) || values[0]["type"] != "event" || values[0]["cursor"] != float64(1) || values[0]["operation_id"] != "op-1" || values[0]["origin"] != "cli" || values[0]["outcome"] != "success" || values[1]["cursor"] != float64(2) || values[1]["exit_code"] != float64(7) || values[1]["signal"] != "TERM" {
		t.Fatalf("event JSON=%#v", values)
	}
	metadata := values[2]
	if metadata["schema_version"] != float64(1) || metadata["type"] != "metadata" || metadata["next_cursor"] != float64(2) || metadata["truncated"] != true || metadata["has_more"] != false {
		t.Fatalf("metadata=%#v", metadata)
	}
	if values[0]["detail"] != strings.Repeat("x", 200) {
		t.Fatal("JSON detail was width-truncated")
	}

	fixture := newEventCommandFixture(t)
	fixture.append(t, protocol.HistoryEvent{Name: "api", Kind: protocol.EventLifecycle, Event: "launch"})
	errorOutput, err := fixture.run(t, "--after-cursor", "65", "--json")
	if err == nil {
		t.Fatal("future cursor JSON request unexpectedly succeeded")
	}
	if errorOutput != "" && (!strings.Contains(errorOutput, `"schema_version":1`) || !strings.Contains(errorOutput, `"error"`)) {
		t.Fatalf("JSON error output=%q", errorOutput)
	}
}
