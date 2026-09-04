package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/output"
	"hum/internal/protocol"
)

func TestList(t *testing.T) {
	runtimeDir := hum006ListLogsTempDir(t, "runtime")
	hum006ListLogsStartDaemon(t, runtimeDir, 512)

	project := hum006ListLogsProject(t, "project")
	otherProject := hum006ListLogsProject(t, "other")

	if stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "run", "api", "--detach", "--", "/bin/sh", "-c", "printf 'api-ready\\n'; sleep 30"); err != nil {
		t.Fatalf("start current-project process: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	if stdout, stderr, err := hum006ListLogsRunAt(t, otherProject, context.Background(), "run", "worker", "--detach", "--", "/bin/sh", "-c", "printf 'worker-ready\\n'; sleep 30"); err != nil {
		t.Fatalf("start other-project process: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}

	current, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "list")
	if err != nil {
		t.Fatalf("list current project: %v (stderr=%q)", err, stderr)
	}
	if !strings.Contains(current, "api") {
		t.Fatalf("current-project list = %q, missing api", current)
	}
	if strings.Contains(current, "worker") {
		t.Fatalf("current-project list = %q, unexpectedly contains worker", current)
	}

	all, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "list", "--all")
	if err != nil {
		t.Fatalf("list all projects: %v (stderr=%q)", err, stderr)
	}
	for _, want := range []string{"api", "worker", project, otherProject} {
		if !strings.Contains(all, want) {
			t.Fatalf("all-project list = %q, missing %q", all, want)
		}
	}

	currentJSON, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "list", "--json")
	if err != nil {
		t.Fatalf("list current project as JSON: %v (stderr=%q)", err, stderr)
	}
	currentProcesses := hum006ListLogsProcessObjects(t, currentJSON)
	hum006ListLogsAssertProcessNames(t, currentProcesses, []string{"api"}, []string{"worker"})

	allJSON, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "list", "--all", "--json")
	if err != nil {
		t.Fatalf("list all projects as JSON: %v (stderr=%q)", err, stderr)
	}
	allProcesses := hum006ListLogsProcessObjects(t, allJSON)
	hum006ListLogsAssertProcessNames(t, allProcesses, []string{"api", "worker"}, nil)
	if !strings.Contains(allJSON, project) || !strings.Contains(allJSON, otherProject) {
		t.Fatalf("all-project JSON list = %q, missing project roots", allJSON)
	}
}

func TestLogsFollow(t *testing.T) {
	runtimeDir := hum006ListLogsTempDir(t, "runtime")
	hum006ListLogsStartDaemon(t, runtimeDir, 128)
	project := hum006ListLogsProject(t, "project")

	selectScript := "printf 'stdout-first\\n'; printf 'stderr-first\\n' >&2; printf 'stdout-match\\n'; printf 'stderr-ignore\\n' >&2; printf 'stdout-last\\n'; sleep 5"
	if stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "run", "select", "--detach", "--", "/bin/sh", "-c", selectScript); err != nil {
		t.Fatalf("start selection process: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	hum006ListLogsWaitForText(t, project, "select", "stdout-last\n")
	hum006ListLogsWaitForText(t, project, "select", "stderr-ignore\n")

	allOutput := hum006ListLogsWaitForJSONText(t, project, "select", "stdout-last\n")
	allObjects := hum006ListLogsDecodeJSONLines(t, allOutput)
	if len(allObjects) != 1 {
		t.Fatalf("bounded logs JSON = %q, decoded %d objects; want one", allOutput, len(allObjects))
	}
	allEntries := hum006ListLogsEntries(t, allObjects[0])
	if len(allEntries) < 5 {
		t.Fatalf("initial logs entries = %#v, want all selected stdout/stderr lines", allEntries)
	}
	matchCursor, ok := hum006ListLogsEntryCursor(allEntries, "stdout-match\n")
	if !ok {
		t.Fatalf("initial logs entries = %#v, missing stdout-match line", allEntries)
	}

	tailOutput, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "logs", "select", "--json", "--tail", "2", "--stream", "stdout", "--match", "stdout-(match|last)")
	if err != nil {
		t.Fatalf("tail/stream/match logs: %v (stderr=%q)", err, stderr)
	}
	tailObjects := hum006ListLogsDecodeJSONLines(t, tailOutput)
	if len(tailObjects) != 1 {
		t.Fatalf("tail logs JSON = %q, decoded %d objects; want one", tailOutput, len(tailObjects))
	}
	tailEntries := hum006ListLogsEntries(t, tailObjects[0])
	if got := hum006ListLogsEntryTexts(t, tailEntries); !hum006ListLogsEqualStrings(got, []string{"stdout-match\n", "stdout-last\n"}) {
		t.Fatalf("tail/stream/match entries = %#v, want match and last stdout lines", got)
	}
	for _, entry := range tailEntries {
		if entry["stream"] != "stdout" {
			t.Fatalf("tail/stream entries = %#v, contains non-stdout entry", tailEntries)
		}
	}

	afterOutput, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "logs", "select", "--json", "--after-cursor", strconv.FormatUint(matchCursor, 10), "--stream", "stdout")
	if err != nil {
		t.Fatalf("cursor logs: %v (stderr=%q)", err, stderr)
	}
	afterObjects := hum006ListLogsDecodeJSONLines(t, afterOutput)
	afterEntries := hum006ListLogsEntries(t, afterObjects[0])
	if got := hum006ListLogsEntryTexts(t, afterEntries); !hum006ListLogsEqualStrings(got, []string{"stdout-last\n"}) {
		t.Fatalf("after-cursor entries = %#v, want only stdout-last", got)
	}

	limitedOutput, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "logs", "select", "--json", "--stream", "stdout", "--limit-bytes", "13")
	if err != nil {
		t.Fatalf("byte-limited logs: %v (stderr=%q)", err, stderr)
	}
	limitedObjects := hum006ListLogsDecodeJSONLines(t, limitedOutput)
	limitedEntries := hum006ListLogsEntries(t, limitedObjects[0])
	if len(limitedEntries) == 0 {
		t.Fatalf("byte-limited logs JSON = %q, want at least one entry", limitedOutput)
	}
	var limitedBytes int
	for _, entry := range limitedEntries {
		if entry["stream"] != "stdout" {
			t.Fatalf("byte-limited entries = %#v, contains non-stdout entry", limitedEntries)
		}
		limitedBytes += len(entry["text"].(string))
	}
	if limitedBytes > 13 {
		t.Fatalf("byte-limited entries use %d bytes, want at most 13: %#v", limitedBytes, limitedEntries)
	}
	if !hum006ListLogsBool(limitedObjects[0], "more") {
		t.Fatalf("byte-limited logs JSON = %q, want more=true", limitedOutput)
	}

	overflowScript := hum006ListLogsOverflowScript()
	if stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "run", "overflow", "--detach", "--", "/bin/sh", "-c", overflowScript); err != nil {
		t.Fatalf("start eviction process: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	hum006ListLogsWaitForText(t, project, "overflow", "evict-23\n")

	staleOutput, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "logs", "overflow", "--json", "--after-cursor", "0", "--limit-bytes", "16")
	if err != nil {
		t.Fatalf("evicted bounded logs: %v (stderr=%q)", err, stderr)
	}
	staleObjects := hum006ListLogsDecodeJSONLines(t, staleOutput)
	if len(staleObjects) != 1 {
		t.Fatalf("evicted logs JSON = %q, decoded %d objects; want one", staleOutput, len(staleObjects))
	}
	if !hum006ListLogsBool(staleObjects[0], "truncated") {
		t.Fatalf("evicted logs JSON = %q, want truncated=true", staleOutput)
	}
	if evicted, ok := hum006ListLogsUint(staleObjects[0], "evicted_through"); !ok || evicted == 0 {
		t.Fatalf("evicted logs JSON = %q, want evicted_through > 0", staleOutput)
	}
	human, humanErr, err := hum006ListLogsRunAt(t, project, context.Background(), "logs", "overflow", "--after-cursor", "0")
	if err != nil {
		t.Fatalf("human evicted logs: %v (stderr=%q)", err, humanErr)
	}
	if !strings.Contains(human, "evict-23\n") {
		t.Fatalf("human evicted logs stdout = %q, missing retained newest line", human)
	}
	if !strings.Contains(humanErr, "next cursor:") || !strings.Contains(strings.ToLower(humanErr), "truncat") {
		t.Fatalf("human evicted logs stderr = %q, want next-cursor and truncation trailer", humanErr)
	}

	followContext, cancelFollow := context.WithTimeout(context.Background(), 100*time.Millisecond)
	followOutput, stderr, err := hum006ListLogsRunAt(t, project, followContext, "logs", "overflow", "--follow", "--json", "--after-cursor", "0", "--limit-bytes", "16")
	cancelFollow()
	if err != nil {
		t.Fatalf("bounded eviction follow: %v (stderr=%q)", err, stderr)
	}
	followObjects := hum006ListLogsDecodeJSONLines(t, followOutput)
	var outputEvents, evictionEvents int
	var sawMore bool
	for _, event := range followObjects {
		if event["op"] != "event" {
			t.Errorf("follow NDJSON event = %#v, want op=event", event)
		}
		typ, _ := event["type"].(string)
		if hum006ListLogsBool(event, "more") {
			sawMore = true
		}
		hasEvictionMetadata := hum006ListLogsBool(event, "truncated")
		if _, ok := hum006ListLogsUint(event, "evicted_through"); ok {
			hasEvictionMetadata = true
		}
		if hasEvictionMetadata && typ != "eviction" {
			t.Errorf("stale follow event = %#v, want type=eviction", event)
		}
		if typ == "eviction" {
			evictionEvents++
		}
		entries, hasEntries := hum006ListLogsMaybeEntries(event)
		if hasEntries {
			outputEvents++
			var bytesInEvent int
			for _, entry := range entries {
				bytesInEvent += len(entry["text"].(string))
			}
			if bytesInEvent > 16 {
				t.Errorf("follow output event = %#v, uses %d bytes; want at most 16", event, bytesInEvent)
			}
		}
	}
	if outputEvents == 0 {
		t.Fatalf("follow NDJSON = %q, want output events", followOutput)
	}
	if !sawMore {
		t.Fatalf("follow NDJSON = %q, want more=true for bounded delivery", followOutput)
	}
	if evictionEvents == 0 {
		t.Fatalf("follow NDJSON = %q, want eviction reporting", followOutput)
	}
	if len(followObjects) < 2 {
		t.Fatalf("follow NDJSON = %q, want bounded output plus terminal event", followOutput)
	}
	t.Run("late follow exits after process completion", func(t *testing.T) {
		lateScript := "printf 'late-follow\\n'; exit 29"
		if stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "run", "late", "--detach", "--", "/bin/sh", "-c", lateScript); err != nil {
			t.Fatalf("start late-follow process: %v (stdout=%q stderr=%q)", err, stdout, stderr)
		}
		completed := hum006ListLogsWaitForExit(t, runtimeDir, project, "late")
		if completed.ExitCode != 29 {
			t.Fatalf("late-follow managed exit code = %d, want 29", completed.ExitCode)
		}

		followContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		followOutput, stderr, err := hum006ListLogsRunAt(t, project, followContext, "logs", "late", "--follow", "--json")
		timedOut := followContext.Err() != nil
		cancel()
		if !timedOut {
			t.Fatalf("late logs --follow terminated instead of waiting: stdout=%q stderr=%q err=%v", followOutput, stderr, err)
		}
		if err != nil {
			t.Fatalf("late logs --follow: %v (stderr=%q)", err, stderr)
		}
		events := hum006ListLogsDecodeJSONLines(t, followOutput)
		if !hum006ListLogsContainsString(hum006ListLogsAllEventTexts(t, events), "late-follow\n") {
			t.Fatalf("late logs --follow events = %#v, missing retained output", events)
		}
		humanContext, cancelHuman := context.WithTimeout(context.Background(), 100*time.Millisecond)
		humanOutput, humanErr, err := hum006ListLogsRunAt(t, project, humanContext, "logs", "late", "--follow")
		cancelHuman()
		if err != nil {
			t.Fatalf("late human logs --follow: %v (stderr=%q)", err, humanErr)
		}
		if !strings.Contains(humanOutput, "late-follow\n") {
			t.Fatalf("late human logs --follow stdout = %q, missing retained output", humanOutput)
		}
		if humanErr != "" {
			t.Fatalf("late human logs --follow stderr = %q, want no cursor trailers", humanErr)
		}
	})

	multiScript := "printf 'multi-first\\n'; sleep 1; printf 'multi-second\\n'; sleep 1"
	if stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "run", "multi", "--detach", "--", "/bin/sh", "-c", multiScript); err != nil {
		t.Fatalf("start multiple-follower process: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	oldwd := hum006ListLogsEnterDir(t, project)
	defer hum006ListLogsLeaveDir(t, oldwd)
	multiResults := make(chan hum006ListLogsCommandResult, 2)
	for range 2 {
		go func() {
			followCtx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
			defer cancel()
			stdout, stderr, err := hum006ListLogsRunHere(followCtx, "logs", "multi", "--follow", "--json")
			multiResults <- hum006ListLogsCommandResult{stdout: stdout, stderr: stderr, err: err}
		}()
	}
	for i := range 2 {
		select {
		case result := <-multiResults:
			if result.err != nil {
				t.Fatalf("multiple follower %d: %v (stderr=%q)", i, result.err, result.stderr)
			}
			events := hum006ListLogsDecodeJSONLines(t, result.stdout)
			texts := hum006ListLogsAllEventTexts(t, events)
			if !hum006ListLogsContainsString(texts, "multi-first\n") || !hum006ListLogsContainsString(texts, "multi-second\n") {
				t.Fatalf("follower %d events = %#v, want both output lines", i, events)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for follower %d", i)
		}
	}

	cancelScript := "printf 'cancel-start\\n'; sleep 20"
	if stdout, stderr, err := hum006ListLogsRunHere(context.Background(), "run", "cancel", "--detach", "--", "/bin/sh", "-c", cancelScript); err != nil {
		t.Fatalf("start cancellation process: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	hum006ListLogsLeaveDir(t, oldwd)
	cancelContext, cancel := context.WithCancel(context.Background())
	capture := hum006ListLogsFirstWriteWriter()
	cancelResult := make(chan hum006ListLogsCommandResult, 1)
	oldwd = hum006ListLogsEnterDir(t, project)
	go func() {
		var stderr bytes.Buffer
		err := NewRootCommand("test", "test", capture, &stderr).Run(cancelContext, []string{"hum", "logs", "cancel", "--follow", "--json"})
		cancelResult <- hum006ListLogsCommandResult{stdout: capture.String(), stderr: stderr.String(), err: err}
	}()
	select {
	case <-capture.first:
		cancel()
	case <-time.After(3 * time.Second):
		cancel()
		hum006ListLogsLeaveDir(t, oldwd)
		t.Fatal("timed out waiting for follower initial event")
	}
	select {
	case result := <-cancelResult:
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			t.Fatalf("cancel follower: %v (stdout=%q stderr=%q)", result.err, result.stdout, result.stderr)
		}
	case <-time.After(3 * time.Second):
		hum006ListLogsLeaveDir(t, oldwd)
		t.Fatal("canceled follower did not return")
	}
	hum006ListLogsLeaveDir(t, oldwd)

	stillRunning, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "list", "--json")
	if err != nil {
		t.Fatalf("list after follower cancellation: %v (stderr=%q)", err, stderr)
	}
	processes := hum006ListLogsProcessObjects(t, stillRunning)
	var cancelState string
	for _, process := range processes {
		if process["name"] == "cancel" {
			cancelState, _ = process["state"].(string)
		}
	}
	if cancelState != "running" {
		t.Fatalf("process state after follower cancellation = %q, want running (list=%q)", cancelState, stillRunning)
	}
}

func TestLogsFollowJSONEventTypes(t *testing.T) {
	cursor := output.Cursor(7)
	exitTime := time.Unix(123, 0)
	tests := []struct {
		name  string
		event output.Event
		want  protocol.EventType
	}{
		{
			name:  "eviction metadata",
			event: output.Event{Read: &output.ReadResult{EvictedThrough: &cursor, Truncated: true}},
			want:  protocol.EventEviction,
		},
		{
			name:  "cursor-only progress",
			event: output.Event{Read: &output.ReadResult{Next: &cursor}},
			want:  protocol.EventCursor,
		},
		{
			name:  "exit",
			event: output.Event{Exit: &output.Exit{Code: 29, Time: exitTime}},
			want:  protocol.EventExit,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wire, err := json.Marshal(eventJSON("typed", test.event))
			if err != nil {
				t.Fatalf("marshal %s event: %v", test.name, err)
			}
			var object struct {
				Type protocol.EventType `json:"type"`
			}
			if err := json.Unmarshal(wire, &object); err != nil {
				t.Fatalf("decode %s event: %v; JSON=%s", test.name, err, wire)
			}
			if object.Type != test.want {
				t.Fatalf("%s event type = %q, want %q; JSON=%s", test.name, object.Type, test.want, wire)
			}
		})
	}
}

type hum006ListLogsCommandResult struct {
	stdout string
	stderr string
	err    error
}

func hum006ListLogsTempDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hum006-"+prefix+"-")
	if err != nil {
		t.Fatalf("create temporary directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func hum006ListLogsProject(t *testing.T, prefix string) string {
	t.Helper()
	dir := hum006ListLogsTempDir(t, prefix)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("create project marker: %v", err)
	}
	return dir
}

func hum006ListLogsStartDaemon(t *testing.T, runtimeDir string, retainedBytes int) {
	t.Helper()
	server, err := daemon.NewServer(daemon.Config{
		RuntimeDir: runtimeDir,
		Version:    strconv.Itoa(protocol.Version),
		StopGrace:  20 * time.Millisecond,
		OutputLimits: output.Limits{
			RetainedBytes:      retainedBytes,
			DefaultReadEntries: 100,
			DefaultReadBytes:   64 * 1024,
		},
		MaxLineBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("create temporary daemon: %v", err)
	}
	go func() { _ = server.Serve(context.Background()) }()
	readyContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.WaitReady(readyContext); err != nil {
		_ = server.Close()
		t.Fatalf("wait for temporary daemon: %v", err)
	}
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	t.Cleanup(func() { _ = server.Close() })
}

func hum006ListLogsRunAt(t *testing.T, cwd string, ctx context.Context, args ...string) (string, string, error) {
	t.Helper()
	oldwd := hum006ListLogsEnterDir(t, cwd)
	defer hum006ListLogsLeaveDir(t, oldwd)
	return hum006ListLogsRunHere(ctx, args...)
}

func hum006ListLogsRunHere(ctx context.Context, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	err := NewRootCommand("test", "test", &stdout, &stderr).Run(ctx, append([]string{"hum"}, args...))
	return stdout.String(), stderr.String(), err
}

func hum006ListLogsEnterDir(t *testing.T, dir string) string {
	t.Helper()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("change working directory to %q: %v", dir, err)
	}
	return oldwd
}

func hum006ListLogsWaitForExit(t *testing.T, runtimeDir, cwd, name string) app.Process {
	t.Helper()
	canonicalCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("canonicalize project directory %q: %v", cwd, err)
	}
	client, err := daemon.Dial(context.Background(), daemon.NewRuntimePaths(runtimeDir).Socket)
	if err != nil {
		t.Fatalf("dial daemon to wait for %q: %v", name, err)
	}

	defer client.Close()

	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	var last app.Process
	var lastErr error
	for {
		requestContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		last, lastErr = client.Get(requestContext, daemon.GetRequest{Name: name, Cwd: canonicalCwd})
		cancel()
		if lastErr == nil && last.State == app.StateExited {
			return last
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("process %q did not exit: last=%#v err=%v", name, last, lastErr)
			return last
		}
	}
}

func hum006ListLogsLeaveDir(t *testing.T, oldwd string) {
	t.Helper()
	if err := os.Chdir(oldwd); err != nil {
		t.Fatalf("restore working directory to %q: %v", oldwd, err)
	}
}

func hum006ListLogsWaitForText(t *testing.T, cwd, name, text string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		stdout, _, err := hum006ListLogsRunAt(t, cwd, context.Background(), "logs", name, "--json")
		if err == nil {
			last = stdout
			objects := hum006ListLogsDecodeJSONLines(t, stdout)
			if hum006ListLogsContainsString(hum006ListLogsAllEventTexts(t, objects), text) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("logs %s never contained %q (last=%q)", name, text, last)
}

func hum006ListLogsWaitForJSONText(t *testing.T, cwd, name, text string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		stdout, _, err := hum006ListLogsRunAt(t, cwd, context.Background(), "logs", name, "--json")
		if err == nil {
			last = stdout
			objects := hum006ListLogsDecodeJSONLines(t, stdout)
			if hum006ListLogsContainsString(hum006ListLogsAllEventTexts(t, objects), text) {
				return stdout
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("JSON logs %s never contained %q (last=%q)", name, text, last)
	return ""
}

func hum006ListLogsOverflowScript() string {
	var script strings.Builder
	for i := range 24 {
		fmt.Fprintf(&script, "printf 'evict-%02d\\n';", i)
	}
	script.WriteString("sleep 2")
	return script.String()
}

func hum006ListLogsDecodeJSONLines(t *testing.T, text string) []map[string]any {
	t.Helper()
	var objects []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var object map[string]any
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&object); err != nil {
			t.Fatalf("decode JSON line %q: %v", line, err)
		}
		objects = append(objects, object)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan JSON lines: %v", err)
	}
	return objects
}

func hum006ListLogsProcessObjects(t *testing.T, text string) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(text)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode list JSON %q: %v", text, err)
	}
	var objects []map[string]any
	var walk func(any)
	walk = func(value any) {
		switch value := value.(type) {
		case []any:
			for _, item := range value {
				walk(item)
			}
		case map[string]any:
			if processes, ok := value["processes"].([]any); ok {
				walk(processes)
				return
			}
			objects = append(objects, value)
		}
	}
	walk(value)
	if len(objects) == 0 {
		t.Fatalf("list JSON %q contains no process objects", text)
	}
	return objects
}

func hum006ListLogsAssertProcessNames(t *testing.T, processes []map[string]any, required, forbidden []string) {
	t.Helper()
	for _, name := range required {
		found := false
		for _, process := range processes {
			if process["name"] == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("process list = %#v, missing %q", processes, name)
		}
	}
	for _, name := range forbidden {
		for _, process := range processes {
			if process["name"] == name {
				t.Errorf("process list = %#v, unexpectedly contains %q", processes, name)
			}
		}
	}
}

func hum006ListLogsEntries(t *testing.T, object map[string]any) []map[string]any {
	t.Helper()
	entries, ok := hum006ListLogsMaybeEntries(object)
	if !ok {
		t.Fatalf("JSON object = %#v, missing entries", object)
	}
	return entries
}

func hum006ListLogsMaybeEntries(object map[string]any) ([]map[string]any, bool) {
	raw, ok := object["entries"]
	if !ok {
		return nil, false
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	entries := make([]map[string]any, 0, len(values))
	for _, value := range values {
		entry, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		entries = append(entries, entry)
	}
	return entries, true
}

func hum006ListLogsEntryTexts(t *testing.T, entries []map[string]any) []string {
	t.Helper()
	texts := make([]string, 0, len(entries))
	for _, entry := range entries {
		text, ok := entry["text"].(string)
		if !ok {
			t.Fatalf("entry = %#v, missing text", entry)
		}
		texts = append(texts, text)
	}
	return texts
}

func hum006ListLogsEntryCursor(entries []map[string]any, text string) (uint64, bool) {
	for _, entry := range entries {
		if entry["text"] != text {
			continue
		}
		cursor, ok := entry["cursor"].(json.Number)
		if !ok {
			return 0, false
		}
		value, err := strconv.ParseUint(string(cursor), 10, 64)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	return 0, false
}

func hum006ListLogsBool(object map[string]any, key string) bool {
	value, _ := object[key].(bool)
	return value
}

func hum006ListLogsUint(object map[string]any, key string) (uint64, bool) {
	value, ok := object[key].(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseUint(string(value), 10, 64)
	return parsed, err == nil
}

func hum006ListLogsEqualStrings(got, want []string) bool {
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

func hum006ListLogsContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hum006ListLogsAllEventTexts(t *testing.T, events []map[string]any) []string {
	t.Helper()
	var texts []string
	for _, event := range events {
		entries, ok := hum006ListLogsMaybeEntries(event)
		if !ok {
			continue
		}
		texts = append(texts, hum006ListLogsEntryTexts(t, entries)...)
	}
	return texts
}

type hum006ListLogsFirstWrite struct {
	mu    sync.Mutex
	data  bytes.Buffer
	first chan struct{}
	once  sync.Once
}

func hum006ListLogsFirstWriteWriter() *hum006ListLogsFirstWrite {
	return &hum006ListLogsFirstWrite{first: make(chan struct{})}
}

func (w *hum006ListLogsFirstWrite) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.data.Write(p)
	w.once.Do(func() { close(w.first) })
	return len(p), nil
}

func (w *hum006ListLogsFirstWrite) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.data.String()
}

var _ io.Writer = (*hum006ListLogsFirstWrite)(nil)
