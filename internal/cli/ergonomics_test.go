package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
)

// ergonomicsRunAt runs hum at cwd like hum006ListLogsRunAt, but disables the
// default ExitErrHandler first (mirroring cliServeRunInvoke). Tests that
// expect an exit-coded error (urfavecli.Exit) must use this instead of
// hum006ListLogsRunAt/RunHere: the default handler calls os.Exit and silently
// kills the test binary before any assertion runs.
func TestStartupReconciliationWarnings(t *testing.T) {
	warnings := []protocol.StartupWarning{{Project: "/project", Name: "api", Outcome: "reclaimed", Message: "old group stopped"}}
	var stderr bytes.Buffer
	if err := writeStartupWarnings(&stderr, warnings); err != nil {
		t.Fatal(err)
	}
	if got := stderr.String(); strings.Count(got, "\n") != 1 || !strings.Contains(got, "reclaimed /project/api") {
		t.Fatalf("human warning = %q, want one concise line", got)
	}

	encoded, err := json.Marshal(listJSON{Warnings: warnings})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["warnings"]; !ok {
		t.Fatalf("JSON list has no top-level warnings: %s", encoded)
	}

	event, err := json.Marshal(protocol.StreamEvent{Type: protocol.EventWarning, Warnings: warnings})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(event, []byte(`"type":"warning"`)) || !bytes.Contains(event, []byte(`"warnings"`)) {
		t.Fatalf("NDJSON warning event = %s", event)
	}
	var clean bytes.Buffer
	if err := writeStartupWarnings(&clean, nil); err != nil || clean.Len() != 0 {
		t.Fatalf("clean startup warning output = %q, err=%v", clean.String(), err)
	}
}

func ergonomicsRunAt(t *testing.T, cwd string, ctx context.Context, args ...string) (string, string, error) {
	t.Helper()
	oldwd := hum006ListLogsEnterDir(t, cwd)
	defer hum006ListLogsLeaveDir(t, oldwd)
	var stdout, stderr bytes.Buffer
	command := NewRootCommand("test", "test", &stdout, &stderr)
	command.ExitErrHandler = func(context.Context, *urfavecli.Command, error) {}
	err := command.Run(ctx, append([]string{"hum"}, args...))
	return stdout.String(), stderr.String(), err
}

// TestRemoveNoDaemonMatchesStop covers item 1: hum remove with no daemon must
// report the same user-facing message and exit status as hum stop, instead of
// a raw transport dial error.
func TestRemoveNoDaemonMatchesStop(t *testing.T) {
	runtimeDir := hum006ListLogsTempDir(t, "remove-no-daemon")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	stdout, stderr, err := stopShutdownRun(t, "remove", "ghost")
	if err != nil {
		t.Fatalf("remove with no daemon: %v", err)
	}
	if stdout != stopUnavailableMessage+"\n" || stderr != "" {
		t.Fatalf("remove no-daemon output = stdout %q stderr %q, want %q", stdout, stderr, stopUnavailableMessage+"\n")
	}

	jsonOut, stderr, err := stopShutdownRun(t, "remove", "ghost", "--json")
	if err != nil {
		t.Fatalf("remove --json with no daemon: %v", err)
	}
	if stderr != "" {
		t.Fatalf("remove --json no-daemon stderr = %q", stderr)
	}
	var result stopResult
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("decode remove --json: %v (%q)", err, jsonOut)
	}
	if result.Name != "ghost" || result.Status != "not_running" {
		t.Fatalf("remove --json result = %+v, want name ghost status not_running", result)
	}
}

// TestOnUsageErrorSingleLine covers item 2: an invalid flag value must
// produce exactly one line naming the full command path, no help dump on
// stdout, and the default exit code (not a distinct ExitCoder).
func TestOnUsageErrorSingleLine(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	err := root.Run(context.Background(), []string{"hum", "logs", "x", "--after-cursor", "-1"})
	if err == nil {
		t.Fatalf("expected a usage error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("usage error wrote to stdout: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("OnUsageError must not itself write; harness prints the returned error once: %q", stderr.String())
	}
	if !strings.HasPrefix(err.Error(), `hum logs: invalid value "-1" for flag`) {
		t.Fatalf("usage error = %q, want hum logs: invalid value ... prefix", err.Error())
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("usage error must be a single line: %q", err.Error())
	}
	var exitErr urfavecli.ExitCoder
	if errors.As(err, &exitErr) {
		t.Fatalf("usage error should default to exit code 1, not carry a distinct ExitCoder: %v", exitErr)
	}
}

// TestRenderManifestLaunchHumanErrorOutcome covers item 3: a result with no
// definition renders as "error NAME: MESSAGE" with no empty "(manifest: )"
// parenthetical and no duplicated "error" word.
func TestRenderManifestLaunchHumanErrorOutcome(t *testing.T) {
	var buf bytes.Buffer
	definition := undefinedManifestDefinition("ghost")
	result := manifestLaunchError(definition, errors.New(`no process definition or retained launch specification for "ghost"`))
	if err := renderManifestLaunchHuman(&buf, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "error ghost: no process definition or retained launch specification for \"ghost\"\n"
	if buf.String() != want {
		t.Fatalf("render = %q, want %q", buf.String(), want)
	}
}

// TestStartUndefinedNameHumanError exercises the same fix end to end through
// hum start.
func TestStartUndefinedNameHumanError(t *testing.T) {
	runtimeDir := hum006ListLogsTempDir(t, "start-undefined-runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	project := hum006ListLogsProject(t, "start-undefined-project")
	manifest := "version: 1\nprocesses:\n  other:\n    argv: [sh, -c, \"sleep 30\"]\n"
	if err := os.WriteFile(filepath.Join(project, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write hum.yaml: %v", err)
	}

	stdout, _, err := ergonomicsRunAt(t, project, context.Background(), "start", "definitely-not-a-thing")
	if err == nil {
		t.Fatalf("expected hum start to fail for an undefined name")
	}
	want := `error definitely-not-a-thing: no process definition or retained launch specification for "definitely-not-a-thing"` + "\n"
	if stdout != want {
		t.Fatalf("start undefined name stdout = %q, want %q", stdout, want)
	}
	if strings.Contains(stdout, "(manifest: )") {
		t.Fatalf("start undefined name output retains the empty parenthetical: %q", stdout)
	}
}

// TestShutdownActiveProcessesHumanMessage covers item 4: shutdown refusal
// must render a human-readable "name (root), ..." list with guidance,
// instead of a raw Go slice.
func TestShutdownActiveProcessesHumanMessage(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	stopShutdownStartProcess(t, server, projectRoot, "keep", []string{"/bin/sh", "-c", "sleep 30"})
	t.Cleanup(func() { _, _, _ = stopShutdownRun(t, "stop", "keep") })

	_, _, err := stopShutdownRun(t, "shutdown")
	if err == nil {
		t.Fatalf("expected shutdown to be refused while a process is active")
	}
	if strings.ContainsAny(err.Error(), "[]") {
		t.Fatalf("shutdown error still renders a Go slice: %q", err.Error())
	}
	want := "keep (" + projectRoot + ")"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("shutdown error = %q, want it to contain %q", err.Error(), want)
	}
	if !strings.Contains(err.Error(), "hum shutdown --stop-processes") {
		t.Fatalf("shutdown error missing guidance: %q", err.Error())
	}
}

// TestLogsAndWaitFlagsHideDefault covers item 7: --tail, --after-cursor, and
// --limit-bytes must not advertise a misleading "(default: 0)", and their
// usage text must say what omitting them means.
func TestLogsAndWaitFlagsHideDefault(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	if err := root.Run(context.Background(), []string{"hum", "logs", "--help"}); err != nil {
		t.Fatalf("logs help: %v", err)
	}
	logsHelp := stdout.String()
	if strings.Contains(logsHelp, "(default: 0)") {
		t.Fatalf("logs help still advertises a misleading default: %s", logsHelp)
	}
	for _, want := range []string{
		"omit for default",
		"omit for the newest default window",
	} {
		if !strings.Contains(logsHelp, want) {
			t.Fatalf("logs help missing %q:\n%s", want, logsHelp)
		}
	}

	stdout.Reset()
	stderr.Reset()
	root = NewRootCommand("test", "test", &stdout, &stderr)
	if err := root.Run(context.Background(), []string{"hum", "wait", "--help"}); err != nil {
		t.Fatalf("wait help: %v", err)
	}
	waitHelp := stdout.String()
	for _, line := range strings.Split(waitHelp, "\n") {
		if strings.Contains(line, "after-cursor") && strings.Contains(line, "(default:") {
			t.Fatalf("wait help still advertises a default for after-cursor: %q", line)
		}
	}
	if !strings.Contains(waitHelp, "omit for current launch") {
		t.Fatalf("wait help missing omitted-cursor wording:\n%s", waitHelp)
	}
}

// TestUpDescriptionNoDuplicateClause covers item 9: the duplicated
// "continues after failures" clause must not appear in hum up --help.
func TestUpDescriptionNoDuplicateClause(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	if err := root.Run(context.Background(), []string{"hum", "up", "--help"}); err != nil {
		t.Fatalf("up help: %v", err)
	}
	if strings.Contains(stdout.String(), "continues after launch failures, continues after failures") {
		t.Fatalf("up help retains the duplicated clause: %s", stdout.String())
	}
}

// TestRenderListHumanOmitsZeroPID and TestRenderStatusHumanOmitsZeroPID cover
// item 10: human output must omit the PID field for a zero PID (stopped or
// never-launched) rather than printing PID 0 / pid: 0.
func TestRenderListHumanOmitsZeroPID(t *testing.T) {
	var buf bytes.Buffer
	stopped := []app.Process{{Name: "dev", State: app.State("stopped"), Source: "hum.yaml", Argv: []string{"sh"}}}
	if err := renderListHuman(&buf, stopped, false); err != nil {
		t.Fatalf("render stopped: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("render stopped lines = %v, want header + one row", lines)
	}
	if !strings.HasPrefix(lines[0], "NAME") || !strings.Contains(lines[0], "PID") {
		t.Fatalf("header = %q, want NAME ... PID ...", lines[0])
	}
	if strings.Contains(lines[1], "0") {
		t.Fatalf("stopped row retains a PID 0 value: %q", lines[1])
	}

	buf.Reset()
	running := []app.Process{{Name: "dev", State: app.StateRunning, PID: 42, Source: "hum.yaml", Argv: []string{"sh"}}}
	if err := renderListHuman(&buf, running, false); err != nil {
		t.Fatalf("render running: %v", err)
	}
	if !strings.Contains(buf.String(), "42") {
		t.Fatalf("running row missing its pid: %q", buf.String())
	}
}

func TestRenderStatusHumanOmitsZeroPID(t *testing.T) {
	var buf bytes.Buffer
	stopped := app.Process{Name: "dev", State: app.State("stopped"), Argv: []string{"sh"}}
	if err := renderStatusHuman(&buf, stopped); err != nil {
		t.Fatalf("render stopped: %v", err)
	}
	if strings.Contains(buf.String(), "pid:") {
		t.Fatalf("stopped status output retains a pid line: %q", buf.String())
	}

	buf.Reset()
	running := app.Process{Name: "dev", State: app.StateRunning, PID: 99, Argv: []string{"sh"}}
	if err := renderStatusHuman(&buf, running); err != nil {
		t.Fatalf("render running: %v", err)
	}
	if !strings.Contains(buf.String(), "pid: 99\n") {
		t.Fatalf("running status output missing pid line: %q", buf.String())
	}
}

// TestRenderListHumanHeaderAndAlignment covers item 19: list human output
// gets an aligned header row via text/tabwriter, and the followers=N suffix
// rule is preserved.
func TestRenderListHumanHeaderAndAlignment(t *testing.T) {
	var buf bytes.Buffer
	processes := []app.Process{
		{Name: "a", State: app.StateRunning, PID: 1, Source: "hum.yaml", Argv: []string{"sh"}},
		{Name: "much-longer-name", State: app.StateExited, PID: 12345, Source: "ad_hoc", Argv: []string{"sh", "-c", "true"}},
	}
	if err := renderListHuman(&buf, processes, false); err != nil {
		t.Fatalf("render: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %#v, want header + two rows", lines)
	}
	if !strings.HasPrefix(lines[0], "NAME") {
		t.Fatalf("missing header row: %q", lines[0])
	}
	sourceCol1 := strings.Index(lines[1], "hum.yaml")
	sourceCol2 := strings.Index(lines[2], "ad_hoc")
	if sourceCol1 == -1 || sourceCol2 == -1 || sourceCol1 != sourceCol2 {
		t.Fatalf("SOURCE column not aligned: row1 %d row2 %d\n%q\n%q", sourceCol1, sourceCol2, lines[1], lines[2])
	}

	buf.Reset()
	followed := []app.Process{{Name: "a", State: app.StateRunning, PID: 1, Source: "hum.yaml", Argv: []string{"sh"}, Followers: 2}}
	if err := renderListHuman(&buf, followed, false); err != nil {
		t.Fatalf("render followed: %v", err)
	}
	if !strings.Contains(buf.String(), "followers=2") {
		t.Fatalf("followed row missing followers=2 suffix: %q", buf.String())
	}
}

// TestListIncludesExitedAdHoc covers item 16: list and list --all must show
// exited ad-hoc records exactly like exited manifest records.
func TestListIncludesExitedAdHoc(t *testing.T) {
	runtimeDir := hum006ListLogsTempDir(t, "list-exited-adhoc-runtime")
	hum006ListLogsStartDaemon(t, runtimeDir, 4096)
	project := hum006ListLogsProject(t, "list-exited-adhoc-project")

	if stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "run", "once", "--detach", "--", "/bin/sh", "-c", "echo done; exit 3"); err != nil {
		t.Fatalf("start once: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	hum006ListLogsWaitForExit(t, runtimeDir, project, "once")

	stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "list")
	if err != nil {
		t.Fatalf("list: %v (stderr=%q)", err, stderr)
	}
	if !strings.Contains(stdout, "once") || !strings.Contains(stdout, "exited") {
		t.Fatalf("list omitted the exited ad-hoc record: %q", stdout)
	}

	allOut, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "list", "--all")
	if err != nil {
		t.Fatalf("list --all: %v (stderr=%q)", err, stderr)
	}
	if !strings.Contains(allOut, "once") || !strings.Contains(allOut, "exited") {
		t.Fatalf("list --all omitted the exited ad-hoc record: %q", allOut)
	}
}

// TestAggregateLogsJSONEventTime covers item 12: aggregate hum logs --json
// with several names must not emit the Go zero-value time.
func TestAggregateLogsJSONEventTime(t *testing.T) {
	runtimeDir := hum006ListLogsTempDir(t, "aggregate-time-runtime")
	hum006ListLogsStartDaemon(t, runtimeDir, 4096)
	project := hum006ListLogsProject(t, "aggregate-time-project")

	for _, name := range []string{"one", "two"} {
		if stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "run", name, "--detach", "--", "/bin/sh", "-c", "printf hello"); err != nil {
			t.Fatalf("start %s: %v (stdout=%q stderr=%q)", name, err, stdout, stderr)
		}
		hum006ListLogsWaitForExit(t, runtimeDir, project, name)
	}

	stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "logs", "one", "two", "--json")
	if err != nil {
		t.Fatalf("aggregate logs --json: %v (stderr=%q)", err, stderr)
	}
	events := hum006ListLogsDecodeJSONLines(t, stdout)
	if len(events) != 2 {
		t.Fatalf("aggregate logs events = %d, want 2: %q", len(events), stdout)
	}
	for _, event := range events {
		timeValue, ok := event["time"].(string)
		if !ok || timeValue == "" {
			t.Fatalf("event missing a time field: %#v", event)
		}
		if strings.HasPrefix(timeValue, "0001-01-01") {
			t.Fatalf("event time is the Go zero value: %q", timeValue)
		}
	}
}

// TestAggregateLogsSkippedDependentNotLaunched covers item 14: after hum up
// skips a dependent behind a failed prerequisite, bounded aggregate hum logs
// must report it softly instead of a duplicated not_found error.
func TestAggregateLogsSkippedDependentNotLaunched(t *testing.T) {
	project := hum006ListLogsProject(t, "aggregate-skip-project")
	oldwd := hum006ListLogsEnterDir(t, project)
	defer hum006ListLogsLeaveDir(t, oldwd)

	manifest := "version: 1\n" +
		"processes:\n" +
		"  root:\n" +
		"    argv: [sh, -c, \"exit 1\"]\n" +
		"    ready:\n" +
		"      match: ready\n" +
		"  dependent:\n" +
		"    argv: [sh, -c, \"sleep 30\"]\n" +
		"    after: [root]\n"
	if err := os.WriteFile(filepath.Join(project, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write hum.yaml: %v", err)
	}
	runtimeDir := hum006ListLogsTempDir(t, "aggregate-skip-runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	// root exits before declaring ready, so dependent is reported skipped and
	// never launched; hum up itself exits non-zero for that reason, which is
	// not what this test is exercising. Use stopShutdownRun (cliServeRunInvoke),
	// not hum006ListLogsRunHere: it disables the default ExitErrHandler, which
	// otherwise calls os.Exit on a urfavecli.Exit-coded error and kills the
	// test binary.
	_, _, _ = stopShutdownRun(t, "up")

	stdout, stderr, err := stopShutdownRun(t, "logs")
	if err != nil {
		t.Fatalf("aggregate logs after a skipped dependent: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "[dependent] not launched\n") {
		t.Fatalf("aggregate logs stdout = %q, want [dependent] not launched", stdout)
	}
	if strings.Contains(stderr, "dependent") {
		t.Fatalf("aggregate logs stderr unexpectedly mentions the skipped dependent: %q", stderr)
	}

	jsonOut, stderr, err := stopShutdownRun(t, "logs", "--json")
	if err != nil {
		t.Fatalf("aggregate logs --json after a skipped dependent: %v (stderr=%q)", err, stderr)
	}
	for _, event := range hum006ListLogsDecodeJSONLines(t, jsonOut) {
		if event["name"] == "dependent" {
			t.Fatalf("aggregate logs --json emitted an event for the skipped dependent: %#v", event)
		}
	}
}

// TestWaitPrelaunchMessage covers item 15: waiting on a name with no
// definition and no runtime record must print one stderr line before waiting.
// TestEncodeJSONDoesNotEscapeHTML documents that the shared JSON-writing
// helper (item 5) does not HTML-escape output; hum up --json still does,
// because manifestLaunchResult.MarshalJSON (internal/cli/manifest.go) calls
// json.Marshal directly, independent of any encoder's SetEscapeHTML setting.
// That file is owned by a different concurrent editor; see the task report.
func TestEncodeJSONDoesNotEscapeHTML(t *testing.T) {
	var buf bytes.Buffer
	if err := encodeJSON(&buf, map[string]string{"argv": "echo a >&2"}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(buf.String(), `\u003e`) || strings.Contains(buf.String(), `\u0026`) {
		t.Fatalf("encodeJSON escaped HTML characters: %q", buf.String())
	}
}
