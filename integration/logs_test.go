package integration

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"hum/internal/testutil"
)

const (
	logsitWaitTimeout     = 5 * time.Second
	logsitFollowerTimeout = 10 * time.Second
	logsitPollInterval    = 10 * time.Millisecond
	logsitOutputBytes     = "65536"
)

type logsitEntry struct {
	Cursor uint64 `json:"cursor"`
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

type logsitExit struct {
	Code int       `json:"code"`
	Time time.Time `json:"time"`
}

type logsitEvent struct {
	Op             string          `json:"op"`
	OK             bool            `json:"ok"`
	Type           string          `json:"type"`
	Name           string          `json:"name"`
	Entries        []logsitEntry   `json:"entries"`
	Next           *uint64         `json:"next"`
	Oldest         *uint64         `json:"oldest"`
	Latest         *uint64         `json:"latest"`
	EvictedThrough *uint64         `json:"evicted_through"`
	Truncated      bool            `json:"truncated"`
	More           bool            `json:"more"`
	Cursor         *uint64         `json:"cursor"`
	Exit           *logsitExit     `json:"exit"`
	Error          json.RawMessage `json:"error"`
}

type logsitJSONLine struct {
	Event logsitEvent
	Raw   map[string]json.RawMessage
}

type logsitProcess struct {
	Name  string `json:"name"`
	PID   int    `json:"pid"`
	State string `json:"state"`
}

type logsitListResponse struct {
	Processes []logsitProcess `json:"processes"`
}

type logsitHarness struct {
	hum     string
	fixture string
	project string
	env     []string
	gates   []string
}

func TestLogFollowers(t *testing.T) {
	harness := logsitNewHarness(t)

	filterGate := filepath.Join(harness.project, "filter.release")
	harness.gates = append(harness.gates, filterGate)
	logsitStartDetached(t, harness, "filters", harness.fixture, "burst", filterGate, "8")
	initial := logsitWaitOutput(t, harness, "filters", []string{"--json", "--stream", "both"}, func(lines []logsitJSONLine) bool {
		return len(lines) == 1 && len(lines[0].Event.Entries) >= 8
	})
	if len(initial) != 1 || initial[0].Event.Op != "output" || !initial[0].Event.OK {
		t.Fatalf("initial logs response = %#v, want successful output response", initial)
	}
	if len(initial[0].Event.Entries) != 8 {
		t.Fatalf("initial bounded entries = %d, want eight entries before the gate", len(initial[0].Event.Entries))
	}

	tail := logsitRunLogs(t, harness, "filters", "--json", "--tail", "2", "--stream", "stdout", "--match", `stdout:000[23]`)
	tailLines := logsitDecodeJSONLines(t, tail.Stdout)
	if len(tailLines) != 1 {
		t.Fatalf("tail response = %q, decoded %d lines; want one response", tail.Stdout, len(tailLines))
	}
	tailEvent := tailLines[0].Event
	if tailEvent.Op != "output" || !tailEvent.OK {
		t.Fatalf("tail event = %#v, want successful output response", tailEvent)
	}
	if got := logsitEntryTexts(t, tailEvent.Entries); !logsitEqualStrings(got, []string{"stdout:0002\n", "stdout:0003\n"}) {
		t.Fatalf("tail/filter entries = %#v, want final matching stdout lines", got)
	}
	for _, entry := range tailEvent.Entries {
		if entry.Stream != "stdout" {
			t.Fatalf("tail/filter entry = %#v, want stdout only", entry)
		}
	}

	limited := logsitRunLogs(t, harness, "filters", "--json", "--stream", "stdout", "--limit-bytes", "12")
	limitedLines := logsitDecodeJSONLines(t, limited.Stdout)
	if len(limitedLines) != 1 {
		t.Fatalf("byte-limited response = %q, decoded %d lines; want one response", limited.Stdout, len(limitedLines))
	}
	limitedEvent := limitedLines[0].Event
	if len(limitedEvent.Entries) != 1 || limitedEvent.Entries[0].Text != "stdout:0000\n" {
		t.Fatalf("byte-limited entries = %#v, want first complete stdout line", limitedEvent.Entries)
	}
	if limitedEvent.Next == nil || *limitedEvent.Next < limitedEvent.Entries[0].Cursor {
		t.Fatalf("byte-limited next = %v, entry cursor = %d; want a cursor at or after the consumed entry", limitedEvent.Next, limitedEvent.Entries[0].Cursor)
	}
	if !limitedEvent.More {
		t.Fatalf("byte-limited response = %#v, want more=true", limitedEvent)
	}
	if logsitEntryBytes(limitedEvent.Entries) > 12 {
		t.Fatalf("byte-limited entries use %d bytes, want at most 12", logsitEntryBytes(limitedEvent.Entries))
	}

	continued := logsitRunLogs(t, harness, "filters", "--json", "--stream", "stdout", "--after-cursor", strconv.FormatUint(*limitedEvent.Next, 10), "--limit-bytes", "12")
	continuedLines := logsitDecodeJSONLines(t, continued.Stdout)
	if len(continuedLines) != 1 {
		t.Fatalf("cursor continuation response = %q, decoded %d lines; want one response", continued.Stdout, len(continuedLines))
	}
	continuedEvent := continuedLines[0].Event
	if len(continuedEvent.Entries) != 1 || continuedEvent.Entries[0].Text != "stdout:0001\n" {
		t.Fatalf("cursor continuation entries = %#v, want next stdout line", continuedEvent.Entries)
	}
	if continuedEvent.Next == nil || *continuedEvent.Next <= *limitedEvent.Next {
		t.Fatalf("cursor continuation next = %v after %v, want a strictly newer cursor", continuedEvent.Next, *limitedEvent.Next)
	}

	logsitReleaseGate(t, filterGate)
	logsitWaitOutput(t, harness, "filters", []string{"--json", "--stream", "both"}, func(lines []logsitJSONLine) bool {
		if len(lines) != 1 {
			return false
		}
		return logsitHasEntryText(lines[0].Event.Entries, "stdout:0007\n") && logsitHasEntryText(lines[0].Event.Entries, "stderr:0007\n")
	})

	multiGate := filepath.Join(harness.project, "multi.release")
	harness.gates = append(harness.gates, multiGate)
	logsitStartDetached(t, harness, "multi", harness.fixture, "burst", multiGate, "12")
	logsitWaitOutput(t, harness, "multi", []string{"--json", "--stream", "both"}, func(lines []logsitJSONLine) bool {
		return len(lines) == 1 && logsitHasEntryText(lines[0].Event.Entries, "stdout:0005\n")
	})

	followers := []*testutil.Process{
		testutil.Start(t, harness.hum, harness.project, harness.env, "logs", "multi", "--follow", "--json", "--stream", "both", "--limit-bytes", "4096"),
		testutil.Start(t, harness.hum, harness.project, harness.env, "logs", "multi", "--follow", "--json", "--stream", "both", "--limit-bytes", "4096"),
	}
	for i, follower := range followers {
		logsitWaitFollowerText(t, follower, `"type":"output"`)
		if follower.Exited() {
			t.Fatalf("follower %d exited before the release gate", i)
		}
	}
	logsitReleaseGate(t, multiGate)
	for i, follower := range followers {
		logsitWaitFollowerText(t, follower, `"type":"exit"`)
		if err := follower.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		if err := follower.Wait(logsitFollowerTimeout); err != nil {
			t.Fatalf("follower %d wait: %v; stdout=%q stderr=%q", i, err, follower.Stdout(), follower.Stderr())
		}
		lines := logsitDecodeJSONLines(t, follower.Stdout())
		logsitAssertFollowerOutput(t, "multi", lines, []string{"stdout:0000\n", "stderr:0000\n", "stdout:0011\n", "stderr:0011\n"})
	}

	cancelMarker := filepath.Join(t.TempDir(), "stream")
	logsitStartDetached(t, harness, "cancel", harness.fixture, "stream", cancelMarker)
	testutil.WaitForFile(t, cancelMarker+".started", logsitWaitTimeout)
	canceled := testutil.Start(t, harness.hum, harness.project, harness.env, "logs", "cancel", "--follow", "--json")
	logsitWaitFollowerText(t, canceled, `"type":"output"`)
	if err := canceled.Kill(); err != nil {
		t.Fatalf("kill canceled follower: %v", err)
	}
	_ = canceled.Wait(logsitWaitTimeout)
	if !canceled.Exited() {
		t.Fatal("canceled follower did not exit after Kill")
	}

	listed := logsitRunList(t, harness)
	cancelProcess, ok := logsitProcessByName(listed, "cancel")
	if !ok {
		t.Fatalf("list after follower cancellation = %#v, missing managed process", listed)
	}
	if cancelProcess.State != "running" {
		t.Fatalf("managed process state after follower cancellation = %q, want running", cancelProcess.State)
	}
	if cancelProcess.PID <= 0 || !testutil.ProcessAlive(cancelProcess.PID) {
		t.Fatalf("managed process after follower cancellation = %#v, want live PID", cancelProcess)
	}

	stopped := testutil.Run(t, harness.hum, harness.project, harness.env, "stop", "cancel", "--json")
	if stopped.Code != 0 {
		t.Fatalf("stop canceled process: code=%d stdout=%q stderr=%q err=%v", stopped.Code, stopped.Stdout, stopped.Stderr, stopped.Err)
	}
	testutil.WaitForFile(t, cancelMarker+".terminated", logsitWaitTimeout)
}

func TestNDJSONFollow(t *testing.T) {
	harness := logsitNewHarness(t)
	gate := filepath.Join(harness.project, "eviction.release")
	harness.gates = append(harness.gates, gate)

	run := logsitStartDetached(t, harness, "eviction", harness.fixture, "burst", gate, "7000")
	if run.Code != 0 {
		t.Fatalf("detached eviction run: code=%d stdout=%q stderr=%q err=%v", run.Code, run.Stdout, run.Stderr, run.Err)
	}
	logsitWaitOutput(t, harness, "eviction", []string{"--json", "--stream", "both", "--match", "stdout:3499"}, func(lines []logsitJSONLine) bool {
		return len(lines) == 1 && logsitHasEntryText(lines[0].Event.Entries, "stdout:3499\n")
	})
	follower := testutil.Start(t, harness.hum, harness.project, harness.env, "logs", "eviction", "--follow", "--json", "--after-cursor", "0", "--stream", "both", "--limit-bytes", "4096")
	logsitWaitFollowerText(t, follower, `"type":"eviction"`)
	logsitReleaseGate(t, gate)
	logsitWaitOutput(t, harness, "eviction", []string{"--json", "--stream", "stdout", "--match", "stdout:6999"}, func(lines []logsitJSONLine) bool {
		return len(lines) == 1 && logsitHasEntryText(lines[0].Event.Entries, "stdout:6999\n")
	})
	logsitWaitFollowerText(t, follower, `"type":"exit"`)
	if err := follower.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := follower.Wait(logsitFollowerTimeout); err != nil {
		t.Fatalf("eviction follower wait: %v; stdout=%q stderr=%q", err, follower.Stdout(), follower.Stderr())
	}
	lines := logsitDecodeJSONLines(t, follower.Stdout())
	if len(lines) < 2 {
		t.Fatalf("eviction follow lines = %#v, want bounded output plus exit", lines)
	}

	var (
		outputEvents  int
		evictionEvent *logsitJSONLine
		exitEvents    int
		lastCursor    uint64
		hasCursor     bool
		sawMore       bool
		exitIndex     = -1
	)
	for index := range lines {
		line := &lines[index]
		event := line.Event
		if event.Op != "event" || event.Name != "eviction" {
			t.Fatalf("follow line %d = %#v, want event operation for eviction", index, event)
		}
		if raw, ok := line.Raw["op"]; !ok || string(raw) != `"event"` {
			t.Fatalf("follow line %d raw schema = %#v, want op=event", index, line.Raw)
		}
		if _, ok := line.Raw["type"]; !ok {
			t.Fatalf("follow line %d raw schema = %#v, missing type", index, line.Raw)
		}
		if event.Type == "output" && len(event.Entries) != 0 && event.Entries[0].Stream == "system" {
			continue
		}
		switch event.Type {
		case "output", "eviction":
			if exitIndex >= 0 {
				t.Fatalf("process output line %d appeared after exit line %d", index, exitIndex)
			}
			outputEvents++
			if len(event.Entries) == 0 {
				t.Fatalf("follow output line %d = %#v, want at least one entry", index, event)
			}
			if logsitEntryBytes(event.Entries) > 4096 {
				t.Fatalf("follow output line %d uses %d bytes, want at most 4096", index, logsitEntryBytes(event.Entries))
			}
			if event.Next == nil || event.Oldest == nil || event.Latest == nil {
				t.Fatalf("follow output line %d = %#v, want next/oldest/latest cursor metadata", index, event)
			}
			if event.More {
				sawMore = true
			}
			for _, entry := range event.Entries {
				if entry.Stream != "stdout" && entry.Stream != "stderr" {
					t.Fatalf("follow output line %d entry = %#v, want stdout or stderr", index, entry)
				}
				if hasCursor && entry.Cursor <= lastCursor {
					t.Fatalf("follow output cursors regressed at line %d: cursor=%d previous=%d", index, entry.Cursor, lastCursor)
				}
				lastCursor = entry.Cursor
				hasCursor = true
			}
			if *event.Next < lastCursor {
				t.Fatalf("follow output line %d next=%d before last entry cursor=%d", index, *event.Next, lastCursor)
			}
			if event.Type == "eviction" {
				if evictionEvent == nil {
					copyLine := *line
					evictionEvent = &copyLine
				}
				if !event.Truncated || event.EvictedThrough == nil || *event.EvictedThrough == 0 {
					t.Fatalf("eviction event = %#v, want truncated and evicted_through metadata", event)
				}
				if _, ok := line.Raw["evicted_through"]; !ok {
					t.Fatalf("eviction event raw schema = %#v, missing evicted_through", line.Raw)
				}
				if _, ok := line.Raw["truncated"]; !ok {
					t.Fatalf("eviction event raw schema = %#v, missing truncated", line.Raw)
				}
				if *event.EvictedThrough >= event.Entries[0].Cursor {
					t.Fatalf("eviction event = %#v, evicted_through should precede first retained entry", event)
				}
			}
		case "exit":
			if exitIndex >= 0 {
				t.Fatalf("duplicate exit events at lines %d and %d", exitIndex, index)
			}
			exitIndex = index
			exitEvents++
			if event.Exit == nil || event.Exit.Code != 0 || event.Exit.Time.IsZero() {
				t.Fatalf("exit event = %#v, want code 0 and timestamp", event)
			}
			if _, ok := line.Raw["exit"]; !ok {
				t.Fatalf("exit event raw schema = %#v, missing exit payload", line.Raw)
			}
			if _, ok := line.Raw["time"]; !ok {
				t.Fatalf("exit event raw schema = %#v, missing event time", line.Raw)
			}
			if _, ok := line.Raw["entries"]; ok {
				t.Fatalf("exit event raw schema = %#v, must not include entries", line.Raw)
			}
		default:
			t.Fatalf("follow line %d type=%q, want output, eviction, or exit", index, event.Type)
		}
	}
	if outputEvents == 0 {
		t.Fatalf("eviction follow lines = %#v, want output events", lines)
	}
	if evictionEvent == nil {
		t.Fatalf("eviction follow lines = %#v, want eviction metadata event", lines)
	}
	if !sawMore {
		t.Fatalf("eviction follow lines = %#v, want more=true from bounded delivery", lines)
	}
	if exitEvents != 1 || exitIndex < 0 {
		t.Fatalf("eviction follow ordering: exitEvents=%d exitIndex=%d lines=%d, want one exit before waiting boundaries", exitEvents, exitIndex, len(lines))
	}
}

func logsitNewHarness(t *testing.T) *logsitHarness {
	t.Helper()
	harness := &logsitHarness{
		hum:     testutil.BuildHum(t),
		fixture: testutil.BuildFixture(t),
		project: t.TempDir(),
		env:     testutil.RuntimeEnv(testutil.RuntimeDir(t), "HUM_OUTPUT_BYTES="+logsitOutputBytes, "HUM_COMPLETED_RECORDS=20", "HUM_STOP_GRACE=1s"),
	}
	harness.gates = make([]string, 0, 4)
	t.Cleanup(func() {
		for _, gate := range harness.gates {
			logsitReleaseGate(t, gate)
		}
		shutdown := testutil.Start(t, harness.hum, harness.project, harness.env, "shutdown", "--stop-processes", "--json")
		if err := shutdown.Wait(logsitFollowerTimeout); err != nil {
			t.Logf("cleanup daemon shutdown: %v; stdout=%q stderr=%q", err, shutdown.Stdout(), shutdown.Stderr())
		}
	})
	return harness
}

func logsitStartDetached(t *testing.T, harness *logsitHarness, name string, command ...string) testutil.Result {
	t.Helper()
	args := []string{"run", name, "--detach", "--"}
	args = append(args, command...)
	result := testutil.Run(t, harness.hum, harness.project, harness.env, args...)
	if result.Code != 0 {
		t.Fatalf("start %s: code=%d stdout=%q stderr=%q err=%v", name, result.Code, result.Stdout, result.Stderr, result.Err)
	}
	return result
}

func logsitRunLogs(t *testing.T, harness *logsitHarness, name string, args ...string) testutil.Result {
	t.Helper()
	command := []string{"logs", name}
	command = append(command, args...)
	result := testutil.Run(t, harness.hum, harness.project, harness.env, command...)
	if result.Code != 0 {
		t.Fatalf("logs %s %v: code=%d stdout=%q stderr=%q err=%v", name, args, result.Code, result.Stdout, result.Stderr, result.Err)
	}
	return result
}

func logsitRunList(t *testing.T, harness *logsitHarness) logsitListResponse {
	t.Helper()
	result := testutil.Run(t, harness.hum, harness.project, harness.env, "list", "--json")
	if result.Code != 0 {
		t.Fatalf("list: code=%d stdout=%q stderr=%q err=%v", result.Code, result.Stdout, result.Stderr, result.Err)
	}
	var response logsitListResponse
	if err := json.Unmarshal([]byte(result.Stdout), &response); err != nil {
		t.Fatalf("decode list JSON %q: %v", result.Stdout, err)
	}
	return response
}

func logsitWaitOutput(t *testing.T, harness *logsitHarness, name string, args []string, ready func([]logsitJSONLine) bool) []logsitJSONLine {
	t.Helper()
	deadline := time.NewTimer(logsitWaitTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(logsitPollInterval)
	defer ticker.Stop()
	for {
		result := logsitRunLogs(t, harness, name, args...)
		lines := logsitDecodeJSONLines(t, result.Stdout)
		if ready(lines) {
			return lines
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s logs %v; last=%q", name, args, result.Stdout)
		case <-ticker.C:
		}
	}
}

func logsitWaitFollowerText(t *testing.T, process *testutil.Process, text string) {
	t.Helper()
	deadline := time.NewTimer(logsitWaitTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(logsitPollInterval)
	defer ticker.Stop()
	for {
		if strings.Contains(process.Stdout(), text) {
			return
		}
		if process.Exited() {
			t.Fatalf("follower exited before %q: stdout=%q stderr=%q", text, process.Stdout(), process.Stderr())
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for follower text %q: stdout=%q stderr=%q", text, process.Stdout(), process.Stderr())
		case <-ticker.C:
		}
	}
}

func logsitReleaseGate(t *testing.T, gate string) {
	t.Helper()
	if err := os.WriteFile(gate, []byte("release\n"), 0o600); err != nil && !os.IsExist(err) {
		t.Fatalf("release gate %q: %v", gate, err)
	}
}

func logsitDecodeJSONLines(t *testing.T, raw string) []logsitJSONLine {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 1024), 2*1024*1024)
	lines := make([]logsitJSONLine, 0, 8)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			t.Fatalf("blank JSON line in %q", raw)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(line, &object); err != nil {
			t.Fatalf("decode JSON line %q: %v", line, err)
		}
		var event logsitEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode event JSON line %q: %v", line, err)
		}
		lines = append(lines, logsitJSONLine{Event: event, Raw: object})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan JSON lines: %v", err)
	}
	return lines
}

func logsitAssertFollowerOutput(t *testing.T, name string, lines []logsitJSONLine, required []string) {
	t.Helper()
	if len(lines) < 2 {
		t.Fatalf("%s follower lines = %#v, want output and exit", name, lines)
	}
	seen := make(map[string]bool)
	exitIndex := -1
	for index, line := range lines {
		if line.Event.Op != "event" || line.Event.Name != name {
			t.Fatalf("%s follower line %d = %#v, want named event", name, index, line.Event)
		}
		if line.Event.Type == "exit" {
			if exitIndex >= 0 {
				t.Fatalf("%s follower duplicate exit at line %d in %#v", name, index, lines)
			}
			exitIndex = index
			if line.Event.Exit == nil || line.Event.Exit.Code != 0 {
				t.Fatalf("%s follower exit = %#v, want successful exit", name, line.Event)
			}
			continue
		}
		if line.Event.Type != "output" && line.Event.Type != "eviction" {
			t.Fatalf("%s follower line %d type=%q, want output or eviction", name, index, line.Event.Type)
		}
		for _, entry := range line.Event.Entries {
			seen[entry.Text] = true
		}
	}
	if exitIndex < 0 {
		t.Fatalf("%s follower lines = %#v, missing exit event", name, lines)
	}
	for _, text := range required {
		if !seen[text] {
			t.Fatalf("%s follower lines = %#v, missing %q", name, lines, text)
		}
	}
}

func logsitEntryTexts(t *testing.T, entries []logsitEntry) []string {
	t.Helper()
	texts := make([]string, 0, len(entries))
	for _, entry := range entries {
		texts = append(texts, entry.Text)
	}
	return texts
}

func logsitEqualStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func logsitHasEntryText(entries []logsitEntry, want string) bool {
	for _, entry := range entries {
		if entry.Text == want {
			return true
		}
	}
	return false
}

func logsitEntryBytes(entries []logsitEntry) int {
	total := 0
	for _, entry := range entries {
		total += len(entry.Text)
	}
	return total
}

func logsitProcessByName(response logsitListResponse, name string) (logsitProcess, bool) {
	for _, process := range response.Processes {
		if process.Name == name {
			return process, true
		}
	}
	return logsitProcess{}, false
}
