package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"hum/internal/app"
	"hum/internal/output"
)

func unsetRenderTestEnv(t *testing.T, key string) {
	t.Helper()
	old, present := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func renderTestTTY(t *testing.T) *os.File {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}
	t.Cleanup(func() {
		_ = slave.Close()
		_ = master.Close()
	})
	return slave
}

type renderTestTTYWriter struct {
	bytes.Buffer
	fd uintptr
}

func (w *renderTestTTYWriter) Fd() uintptr { return w.fd }

func stripRenderANSI(value string) string {
	var result strings.Builder
	for index := 0; index < len(value); {
		if value[index] != '\x1b' || index+1 >= len(value) || value[index+1] != '[' {
			result.WriteByte(value[index])
			index++
			continue
		}
		index += 2
		for index < len(value) {
			if value[index] >= '@' && value[index] <= '~' {
				index++
				break
			}
			index++
		}
	}
	return result.String()
}

func TestColorPolicy(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	unsetRenderTestEnv(t, "NO_COLOR")

	t.Run("tty enables", func(t *testing.T) {
		if policy := colorPolicyForWriter(renderTestTTY(t)); !policy.enabled {
			t.Fatal("TTY output did not enable color")
		}
	})
	t.Run("pipe disables", func(t *testing.T) {
		var pipe bytes.Buffer
		if policy := colorPolicyForWriter(&pipe); policy.enabled {
			t.Fatal("pipe output enabled color")
		}
	})
	t.Run("any NO_COLOR disables", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		if policy := colorPolicyForWriter(renderTestTTY(t)); policy.enabled {
			t.Fatal("empty NO_COLOR did not disable color")
		}
		t.Setenv("NO_COLOR", "1")
		if policy := colorPolicyForWriter(renderTestTTY(t)); policy.enabled {
			t.Fatal("present NO_COLOR did not disable color")
		}
	})
	t.Run("dumb TERM disables", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		t.Setenv("TERM", "dumb")
		if policy := colorPolicyForWriter(renderTestTTY(t)); policy.enabled {
			t.Fatal("TERM=dumb did not disable color")
		}
	})
	t.Run("JSON remains unstyled", func(t *testing.T) {
		var output bytes.Buffer
		if err := encodeJSON(&output, map[string]string{"state": "running", "message": "child text"}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "\x1b[") {
			t.Fatalf("JSON contains ANSI styling: %q", output.String())
		}
	})
	t.Run("start renderer remains unstyled on TTY", func(t *testing.T) {
		tty := renderTestTTY(t)
		output := &renderTestTTYWriter{fd: tty.Fd()}
		if err := renderManifestLaunchHuman(output, manifestLaunchResult{Name: "service", Outcome: "started"}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "\x1b[") {
			t.Fatalf("start output contains ANSI styling: %q", output.String())
		}
	})
	t.Run("redirected progress writer remains unstyled", func(t *testing.T) {
		tty := renderTestTTY(t)
		stdout := &renderTestTTYWriter{fd: tty.Fd()}
		var progress bytes.Buffer
		if !colorPolicyForWriter(stdout).enabled {
			t.Fatal("TTY stdout did not enable color")
		}
		if colorPolicyForWriter(&progress).enabled {
			t.Fatal("redirected progress writer enabled color")
		}
	})
}

func TestLifecycleColorMapping(t *testing.T) {
	colors := colorPolicy{enabled: true}
	processes := []app.Process{
		{
			Name: "running-name", Source: "manifest", Root: "/tmp/project", State: app.StateRunning,
			PID: 11, Argv: []string{"echo", "child text"},
			Readiness: &app.Readiness{State: app.ReadinessReady},
		},
		{
			Name: "starting-name", Source: "manifest", Root: "/tmp/project", State: app.StateRunning,
			PID: 12, Argv: []string{"echo", "child text"},
			Readiness: &app.Readiness{State: app.ReadinessStarting},
		},
		{Name: "stopped-name", Source: "manifest", Root: "/tmp/project", State: app.StateStopped, Argv: []string{"echo", "child text"}},
		{Name: "success-name", Source: "manifest", Root: "/tmp/project", State: app.StateExited, ExitCode: 0, Argv: []string{"echo", "child text"}},
		{Name: "failed-name", Source: "manifest", Root: "/tmp/project", State: app.StateExited, ExitCode: 7, Argv: []string{"echo", "child text"}},
	}
	var list bytes.Buffer
	if err := renderListHumanWithPolicy(&list, processes, false, colors); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		ansiBoldString("NAME"), ansiBoldString("STATE"),
		ansiGreenString("running"), ansiGreenString("ready"),
		ansiYellowString("starting"), ansiCyanString("stopped"),
		ansiDimString("exited"), ansiRedString("exited"),
	} {
		if !strings.Contains(list.String(), want) {
			t.Errorf("colored list missing %q: %q", want, list.String())
		}
	}
	var plainList bytes.Buffer
	if err := renderListHumanWithPolicy(&plainList, processes, false, colorPolicy{}); err != nil {
		t.Fatal(err)
	}
	if got := stripRenderANSI(list.String()); got != plainList.String() {
		t.Fatalf("colored list content changed after stripping ANSI:\ncolored=%q\nplain=%q", got, plainList.String())
	}

	var summary bytes.Buffer
	if err := renderStatusSummaryHumanWithPolicy(&summary, processes, colors); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{ansiBoldString("READINESS"), ansiGreenString("running"), ansiGreenString("ready"), ansiCyanString("stopped"), ansiRedString("exited")} {
		if !strings.Contains(summary.String(), want) {
			t.Errorf("colored status summary missing %q: %q", want, summary.String())
		}
	}
	var plainSummary bytes.Buffer
	if err := renderStatusSummaryHumanWithPolicy(&plainSummary, processes, colorPolicy{}); err != nil {
		t.Fatal(err)
	}
	if got := stripRenderANSI(summary.String()); got != plainSummary.String() {
		t.Fatalf("colored status summary changed after stripping ANSI:\ncolored=%q\nplain=%q", got, plainSummary.String())
	}

	var status bytes.Buffer
	statusProcess := processes[0]
	statusProcess.Cwd = "/tmp/project"
	if err := renderStatusHumanWithPolicy(&status, statusProcess, colors); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"state: " + ansiGreenString("running"), "readiness: " + ansiGreenString("ready")} {
		if !strings.Contains(status.String(), want) {
			t.Errorf("colored status missing %q: %q", want, status.String())
		}
	}
	if strings.Contains(status.String(), ansiGreenString("running-name")) || strings.Contains(status.String(), ansiGreenString("/tmp/project")) || strings.Contains(status.String(), ansiGreenString("child text")) {
		t.Fatalf("colored status styled a name, path, or child value: %q", status.String())
	}

	cases := []struct {
		name   string
		result manifestLaunchResult
		style  ansiStyle
		label  string
	}{
		{name: "started", result: manifestLaunchResult{Name: "service", Outcome: "started"}, style: ansiGreen, label: "started"},
		{name: "already running", result: manifestLaunchResult{Name: "service", Outcome: "already_running"}, style: ansiGreen, label: "already_running"},
		{name: "error", result: manifestLaunchResult{Name: "service", Outcome: "error", Error: "message text"}, style: ansiRed, label: "error"},
		{name: "early exit", result: manifestLaunchResult{Name: "service", Outcome: "exited_before_ready"}, style: ansiRed, label: "exited_before_ready"},
		{name: "timeout", result: manifestLaunchResult{Name: "service", Outcome: "timed_out"}, style: ansiRed, label: "timed_out"},
		{name: "drift", result: manifestLaunchResult{Name: "service", Outcome: "definition_drift", ChangedFields: []string{"argv"}, Guidance: "hum restart service"}, style: ansiRed, label: "definition_drift"},
		{name: "recovery", result: manifestLaunchResult{Name: "service", Outcome: "recovery_exhausted"}, style: ansiRed, label: "recovery_exhausted"},
		{name: "skipped", result: manifestLaunchResult{Name: "service", Outcome: "skipped", BlockedBy: []string{"database"}}, style: ansiRed, label: "skipped"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			result := test.result
			result.Source = "manifest"
			result.Argv = []string{"/tmp/child", "child text"}
			if err := renderManifestLaunchHumanWithPolicy(&output, result, colors); err != nil {
				t.Fatal(err)
			}
			if want := string(test.style) + test.label + ansiReset; !strings.Contains(output.String(), want) {
				t.Fatalf("colored up output missing %q: %q", want, output.String())
			}
			for _, unstyled := range []string{"service", "/tmp/child", "child text", "message text", "hum restart service", "database"} {
				if strings.Contains(output.String(), string(test.style)+unstyled+ansiReset) {
					t.Fatalf("styled non-label %q in %q", unstyled, output.String())
				}
			}
		})
	}
}

func TestAggregateLogPrefixColor(t *testing.T) {
	colors := colorPolicy{enabled: true}
	var stdout, stderr bytes.Buffer
	renderer := &aggregateLogRenderer{
		writer: &stdout, errWriter: &stderr, colors: colors, errColors: colors,
	}
	childText := "\x1b[31mchild text\x1b[0m\n"
	if err := renderer.writeEvent("alpha", output.Event{Read: &output.ReadResult{Entries: []output.Entry{{Text: childText}}}}); err != nil {
		t.Fatal(err)
	}
	if err := renderer.writeEvent("beta", output.Event{Read: &output.ReadResult{Entries: []output.Entry{{Text: "other\n"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := renderer.writeCursor("alpha", output.ReadResult{}); err != nil {
		t.Fatal(err)
	}

	alphaPrefix := colors.apply(processLogPrefixStyle("alpha"), "[alpha]")
	betaPrefix := colors.apply(processLogPrefixStyle("beta"), "[beta]")
	if processLogPrefixStyle("alpha") == processLogPrefixStyle("beta") {
		t.Fatal("representative process names received the same prefix color")
	}
	if got, want := stdout.String(), alphaPrefix+" "+childText+betaPrefix+" other\n"; got != want {
		t.Fatalf("colored aggregate logs = %q, want %q", got, want)
	}
	if got, want := stderr.String(), alphaPrefix+" next cursor: 0\n"; got != want {
		t.Fatalf("colored cursor trailer = %q, want %q", got, want)
	}
	if got, want := stripRenderANSI(stdout.String()), "[alpha] child text\n[beta] other\n"; got != want {
		t.Fatalf("stripped aggregate logs = %q, want %q", got, want)
	}
	for _, name := range []string{"alpha", "beta", "docker", "api", "web", "worker"} {
		style := processLogPrefixStyle(name)
		if style == ansiRed || style == ansiGreen {
			t.Fatalf("process %q received semantic lifecycle color %q", name, style)
		}
	}
}

func TestManifestLaunchTable(t *testing.T) {
	pid := 42
	launchCursor, readyCursor := uint64(3), uint64(5)
	results := []manifestLaunchResult{
		{
			Name: "api", Outcome: "started", State: string(app.StateRunning), PID: &pid,
			LaunchCursor: &launchCursor, Readiness: app.ReadinessReady,
			ReadinessMatch: "Listening on a very long address", ReadinessConfigured: true, ReadyCursor: &readyCursor,
		},
		{Name: "web", Outcome: "timed_out", State: string(app.StateRunning), PID: &pid},
		{Name: "worker", Outcome: "exited_before_ready", State: string(app.StateExited), PID: &pid},
	}
	var output bytes.Buffer
	if err := renderManifestLaunchTableWithPolicy(&output, results, colorPolicy{}); err != nil {
		t.Fatal(err)
	}
	want := "NAME    RESULT               STATE    PID\n" +
		"api     started              running  42\n" +
		"web     timed out            running  42\n" +
		"worker  exited before ready  exited   42\n"
	if output.String() != want {
		t.Fatalf("manifest launch table = %q, want %q", output.String(), want)
	}
	for _, hidden := range []string{"launch_cursor", "ready_cursor", "readiness_match", "Listening"} {
		if strings.Contains(output.String(), hidden) {
			t.Errorf("manifest launch table contains diagnostic detail %q: %q", hidden, output.String())
		}
	}

	output.Reset()
	if err := renderManifestLaunchTableWithPolicy(&output, results, colorPolicy{enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{ansiBoldString("NAME"), ansiBoldString("RESULT"), ansiGreenString("started"), ansiRedString("timed out"), ansiGreenString("running"), ansiDimString("exited")} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("colored manifest launch table missing %q: %q", want, output.String())
		}
	}
}

func ansiBoldString(value string) string  { return string(ansiBold) + value + ansiReset }
func ansiGreenString(value string) string { return string(ansiGreen) + value + ansiReset }
func ansiYellowString(value string) string {
	return string(ansiYellow) + value + ansiReset
}
func ansiCyanString(value string) string { return string(ansiCyan) + value + ansiReset }
func ansiDimString(value string) string  { return string(ansiDim) + value + ansiReset }
func ansiRedString(value string) string  { return string(ansiRed) + value + ansiReset }

func TestUncoloredOutputUnchanged(t *testing.T) {
	process := app.Process{
		Name: "api", Source: "manifest", Root: "/project", PID: 42, PGID: 42,
		Cwd: "/project", Argv: []string{"echo", "hello world"},
		Start:        time.Date(2026, time.January, 1, 2, 3, 4, 0, time.UTC),
		LaunchCursor: 3, NextCursor: 4, State: app.StateRunning,
		Readiness: &app.Readiness{State: app.ReadinessReady, Cursor: outputCursorPointer(5)},
	}
	var list bytes.Buffer
	if err := renderListHuman(&list, []app.Process{process}, false); err != nil {
		t.Fatal(err)
	}
	wantList := "NAME  STATE    PID     SOURCE           ARGV\napi   running  PID 42  source=manifest  argv=echo 'hello world'  readiness=ready  ready_cursor=5\n"
	if list.String() != wantList {
		t.Fatalf("uncolored list = %q, want %q", list.String(), wantList)
	}

	var summary bytes.Buffer
	if err := renderStatusSummaryHuman(&summary, []app.Process{
		process,
		{Name: "worker", Source: "manifest", State: app.StateStopped, Restart: app.RestartOnFailure, Followers: 2},
	}); err != nil {
		t.Fatal(err)
	}
	wantSummary := "NAME    STATE    PID  READINESS  RESTART     FOLLOWERS\n" +
		"api     running  42   ready      never       0\n" +
		"worker  stopped  -    -          on-failure  2\n"
	if summary.String() != wantSummary {
		t.Fatalf("uncolored status summary = %q, want %q", summary.String(), wantSummary)
	}

	var status bytes.Buffer
	if err := renderStatusHuman(&status, process); err != nil {
		t.Fatal(err)
	}
	wantStatus := "name: api\nsource: manifest\nproject_root: /project\ntty: false\npid: 42\npgid: 42\ncwd: /project\nargv: echo 'hello world'\nstarted_at: 2026-01-01T02:03:04Z\nstate: running\nrestart: never\nreadiness: ready\nready_cursor: 5\nrelaunches: 0\nrestart_count: 0\nfollowers: 0\nnext_cursor: 4\n"
	if status.String() != wantStatus {
		t.Fatalf("uncolored status = %q, want %q", status.String(), wantStatus)
	}

	var up bytes.Buffer
	result := manifestLaunchResult{
		Name: "api", Outcome: "started", Source: "manifest", Argv: process.Argv,
		PID: intPointer(42), LaunchCursor: uint64Pointer(3), Readiness: app.ReadinessReady,
		ReadyCursor: uint64Pointer(5),
	}
	if err := renderManifestLaunchHuman(&up, result); err != nil {
		t.Fatal(err)
	}
	wantUp := "started api (manifest: echo 'hello world') pid=42 launch_cursor=3 readiness=ready ready_cursor=5\n"
	if up.String() != wantUp {
		t.Fatalf("uncolored up = %q, want %q", up.String(), wantUp)
	}

	var listJSONOutput bytes.Buffer
	if err := encodeJSON(&listJSONOutput, listJSON{Processes: []listProcessJSON{processJSON(process)}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listJSONOutput.String(), "\x1b[") {
		t.Fatalf("list JSON contains ANSI: %q", listJSONOutput.String())
	}
	wantListJSON := `{"processes":[{"name":"api","source":"manifest","root":"/project","tty":false,"pid":42,"pgid":42,"cwd":"/project","argv":["echo","hello world"],"start":"2026-01-01T02:03:04Z","launch_cursor":3,"next_cursor":4,"state":"running","exited_at":"0001-01-01T00:00:00Z","followers":0,"restart":"never","relaunches":0,"readiness":"ready","ready_cursor":5}]}` + "\n"
	if listJSONOutput.String() != wantListJSON {
		t.Fatalf("list JSON = %q, want %q", listJSONOutput.String(), wantListJSON)
	}
	var statusJSONOutput bytes.Buffer
	if err := encodeJSON(&statusJSONOutput, statusJSONFor(process)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(statusJSONOutput.String(), "\x1b[") {
		t.Fatalf("status JSON contains ANSI: %q", statusJSONOutput.String())
	}
	wantStatusJSON := `{"name":"api","source":"manifest","project_root":"/project","tty":false,"pid":42,"pgid":42,"cwd":"/project","argv":["echo","hello world"],"started_at":"2026-01-01T02:03:04Z","state":"running","readiness":"ready","ready_cursor":5,"exit_status":null,"restart_count":0,"followers":0,"restart":"never","relaunches":0,"next_cursor":4}` + "\n"
	if statusJSONOutput.String() != wantStatusJSON {
		t.Fatalf("status JSON = %q, want %q", statusJSONOutput.String(), wantStatusJSON)
	}
	var upJSONOutput bytes.Buffer
	if err := encodeJSON(&upJSONOutput, manifestResultJSON(result)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(upJSONOutput.String(), "\x1b[") {
		t.Fatalf("up JSON contains ANSI: %q", upJSONOutput.String())
	}
	wantUpJSON := `{"name":"api","outcome":"started","source":"manifest","argv":["echo","hello world"],"pid":42,"launch_cursor":3,"readiness":"ready","ready_cursor":5,"restart":"","relaunches":0}` + "\n"
	if upJSONOutput.String() != wantUpJSON {
		t.Fatalf("up JSON = %q, want %q", upJSONOutput.String(), wantUpJSON)
	}
	for _, output := range []string{listJSONOutput.String(), statusJSONOutput.String(), upJSONOutput.String()} {
		var value any
		if err := json.Unmarshal([]byte(output), &value); err != nil {
			t.Fatalf("JSON golden is invalid: %v (%q)", err, output)
		}
	}
}

func outputCursorPointer(value uint64) *output.Cursor {
	cursor := output.Cursor(value)
	return &cursor
}

func uint64Pointer(value uint64) *uint64 {
	return &value
}

func TestColorDocs(t *testing.T) {
	design, err := os.ReadFile("../../docs/design.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(design)
	for _, phrase := range []string{
		"stdout is a terminal", "running and ready are green", "starting is yellow",
		"operator-stopped is cyan", "autonomous successful exit is dim",
		"failed\nexits, errors", "exhausted recovery", "dependency-skipped results are red",
		"list headers are bold", "Aggregate log prefixes use a stable color", "Only `[NAME]` is styled",
		"NO_COLOR", "including an empty value", "TERM=dumb",
		"Piped output and JSON never contain ANSI styling",
	} {
		if !strings.Contains(text, phrase) {
			t.Errorf("design docs missing %q", phrase)
		}
	}
}
