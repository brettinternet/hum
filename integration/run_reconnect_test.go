//go:build darwin || linux

package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"hum/internal/testutil"
)

const (
	runitWaitTimeout = 5 * time.Second
	runitPollPeriod  = 10 * time.Millisecond
)

type runitScenario struct {
	hum        string
	fixture    string
	runtimeDir string
	cwd        string
	env        []string
}

type runitInspectSnapshot struct {
	Argv []string          `json:"argv"`
	Cwd  string            `json:"cwd"`
	Env  map[string]string `json:"env"`
}

type runitRunSummary struct {
	Name   string `json:"name"`
	PID    int    `json:"pid"`
	Cursor uint64 `json:"cursor"`
}

type runitProcessRecord struct {
	Name         string   `json:"name"`
	PID          int      `json:"pid"`
	Cwd          string   `json:"cwd"`
	Argv         []string `json:"argv"`
	LaunchCursor uint64   `json:"launch_cursor"`
	State        string   `json:"state"`
}

type runitListResponse struct {
	Processes []runitProcessRecord `json:"processes"`
}

var runitHumanRunPattern = regexp.MustCompile(`^started ([A-Za-z0-9._-]+) \(PID ([0-9]+), cursor ([0-9]+)\)$`)

func TestAttachedRun(t *testing.T) {
	t.Run("argv cwd environment and raw streams", func(t *testing.T) {
		scenario := runitNewScenario(t)
		name := "attached-inspect"
		runitCleanup(t, scenario, name)

		fixtureArgs := []string{scenario.fixture, "inspect", "arg with spaces", "arg\twith-tabs", "--literal"}
		runArgs := append([]string{"run", name, "--"}, fixtureArgs...)
		result := testutil.Run(t, scenario.hum, scenario.cwd, scenario.env, runArgs...)
		if result.Code != 23 {
			t.Fatalf("attached run exit code = %d (err %v), want 23; stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}

		snapshot := runitDecodeInspectSnapshot(t, result.Stdout)
		if !reflect.DeepEqual(snapshot.Argv, fixtureArgs) {
			t.Fatalf("fixture argv = %#v, want exact %#v", snapshot.Argv, fixtureArgs)
		}
		if snapshot.Cwd != scenario.cwd {
			t.Fatalf("fixture cwd = %q, want canonical %q", snapshot.Cwd, scenario.cwd)
		}
		if want := runitSelectedEnvironment(scenario.env); !reflect.DeepEqual(snapshot.Env, want) {
			t.Fatalf("fixture selected environment = %#v, want %#v", snapshot.Env, want)
		}

		if !strings.Contains(result.Stdout, "stdout:raw with spaces \r\n") || !strings.Contains(result.Stdout, "stdout:partial") {
			t.Fatalf("stdout lost raw content: %q", result.Stdout)
		}
		if !strings.Contains(result.Stderr, "stderr:raw with spaces \r\n") || !strings.Contains(result.Stderr, "stderr:partial") {
			t.Fatalf("stderr lost raw content: %q", result.Stderr)
		}
		if strings.Contains(result.Stdout, "stderr:raw") || strings.Contains(result.Stderr, "stdout:raw") {
			t.Fatalf("attached streams were not kept separate: stdout=%q stderr=%q", result.Stdout, result.Stderr)
		}
	})

	t.Run("first Ctrl-C forwards and second stops", func(t *testing.T) {
		scenario := runitNewScenario(t)
		name := "attached-signals"
		runitCleanup(t, scenario, name)
		marker := filepath.Join(t.TempDir(), "signals")
		runArgs := []string{"run", name, "--", scenario.fixture, "stream", marker}
		client := testutil.Start(t, scenario.hum, scenario.cwd, scenario.env, runArgs...)

		testutil.WaitForFile(t, marker+".started", runitWaitTimeout)
		runitWaitForOutput(t, client, false, "stdout:live with spaces \r\n")
		runitWaitForOutput(t, client, true, "stderr:live with spaces \r\n")

		if err := client.Signal(os.Interrupt); err != nil {
			t.Fatalf("send first Ctrl-C: %v", err)
		}
		runitWaitForOutput(t, client, false, "fixture:sigint-1\n")
		if client.Exited() {
			t.Fatal("first Ctrl-C detached or terminated the attached client")
		}

		if err := client.Signal(os.Interrupt); err != nil {
			t.Fatalf("send second Ctrl-C: %v", err)
		}
		testutil.WaitForFile(t, marker+".terminated", runitWaitTimeout)
		if err := client.Wait(runitWaitTimeout); err != nil {
			t.Fatalf("attached client after graceful stop: %v; stdout=%q stderr=%q", err, client.Stdout(), client.Stderr())
		}
	})

	t.Run("SIGTERM detaches without terminating", func(t *testing.T) {
		scenario := runitNewScenario(t)
		name := "attached-term-detach"
		runitCleanup(t, scenario, name)
		marker := filepath.Join(t.TempDir(), "term-detach")
		runArgs := []string{"run", name, "--", scenario.fixture, "stream", marker}
		client := testutil.Start(t, scenario.hum, scenario.cwd, scenario.env, runArgs...)

		testutil.WaitForFile(t, marker+".started", runitWaitTimeout)
		runitWaitForOutput(t, client, false, "stdout:live with spaces \r\n")
		runitWaitForOutput(t, client, true, "stderr:live with spaces \r\n")
		managed := runitListProcess(t, scenario, name)
		if !testutil.ProcessAlive(managed.PID) {
			t.Fatalf("managed process %q is not alive before detach (PID %d)", name, managed.PID)
		}

		if err := client.Signal(syscall.SIGTERM); err != nil {
			t.Fatalf("send SIGTERM to attached client: %v", err)
		}
		if err := client.Wait(runitWaitTimeout); err != nil {
			t.Fatalf("SIGTERM-detached client exit: %v; stdout=%q stderr=%q", err, client.Stdout(), client.Stderr())
		}
		remaining := runitListProcess(t, scenario, name)
		if remaining.State != "running" || remaining.PID != managed.PID || !testutil.ProcessAlive(remaining.PID) {
			t.Fatalf("managed process after client SIGTERM = %#v, want same running process", remaining)
		}

		runitStopProcess(t, scenario, name, marker, managed.PID)
	})
}

func TestDetachedRun(t *testing.T) {
	t.Run("human name pid cursor and listed argv", func(t *testing.T) {
		scenario := runitNewScenario(t)
		name := "detached-human"
		runitCleanup(t, scenario, name)
		marker := filepath.Join(t.TempDir(), "human")
		fixtureArgs := []string{scenario.fixture, "stream", marker}
		runArgs := append([]string{"run", name, "--detach", "--"}, fixtureArgs...)
		result := testutil.Run(t, scenario.hum, scenario.cwd, scenario.env, runArgs...)
		if result.Code != 0 {
			t.Fatalf("detached human run exit code = %d (err %v); stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		if result.Stderr != "" {
			t.Fatalf("detached human run wrote stderr: %q", result.Stderr)
		}
		summary, err := runitParseHumanSummary(result.Stdout)
		if err != nil {
			t.Fatalf("parse detached human summary: %v; output=%q", err, result.Stdout)
		}
		if summary.Name != name || summary.PID <= 0 {
			t.Fatalf("detached human summary = %#v, want name %q and positive PID", summary, name)
		}

		testutil.WaitForFile(t, marker+".started", runitWaitTimeout)
		managed := runitListProcess(t, scenario, name)
		runitAssertListedProcess(t, managed, summary, scenario.cwd, fixtureArgs)
		humanList := testutil.Run(t, scenario.hum, scenario.cwd, scenario.env, "list")
		if humanList.Code != 0 || !strings.Contains(humanList.Stdout, name) || !strings.Contains(humanList.Stdout, strconv.Itoa(summary.PID)) || !strings.Contains(humanList.Stdout, scenario.fixture) {
			t.Fatalf("human list = code %d stdout=%q stderr=%q, want name/PID/argv", humanList.Code, humanList.Stdout, humanList.Stderr)
		}
		runitStopProcess(t, scenario, name, marker, managed.PID)
	})

	t.Run("json name pid cursor and listed cwd argv", func(t *testing.T) {
		scenario := runitNewScenario(t)
		name := "detached-json"
		runitCleanup(t, scenario, name)
		marker := filepath.Join(t.TempDir(), "json")
		fixtureArgs := []string{scenario.fixture, "stream", marker}
		runArgs := append([]string{"run", name, "--detach", "--json", "--"}, fixtureArgs...)
		result := testutil.Run(t, scenario.hum, scenario.cwd, scenario.env, runArgs...)
		if result.Code != 0 {
			t.Fatalf("detached JSON run exit code = %d (err %v); stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
		}
		if result.Stderr != "" {
			t.Fatalf("detached JSON run wrote stderr: %q", result.Stderr)
		}
		var summary runitRunSummary
		if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &summary); err != nil {
			t.Fatalf("decode detached JSON summary: %v; output=%q", err, result.Stdout)
		}
		if summary.Name != name || summary.PID <= 0 {
			t.Fatalf("detached JSON summary = %#v, want name %q and positive PID", summary, name)
		}

		testutil.WaitForFile(t, marker+".started", runitWaitTimeout)
		managed := runitListProcess(t, scenario, name)
		runitAssertListedProcess(t, managed, summary, scenario.cwd, fixtureArgs)
		runitStopProcess(t, scenario, name, marker, managed.PID)
	})
}

func TestReconnect(t *testing.T) {
	scenario := runitNewScenario(t)
	name := "reconnect"
	runitCleanup(t, scenario, name)
	marker := filepath.Join(t.TempDir(), "reconnect")
	runArgs := []string{"run", name, "--", scenario.fixture, "stream", marker}
	client := testutil.Start(t, scenario.hum, scenario.cwd, scenario.env, runArgs...)

	testutil.WaitForFile(t, marker+".started", runitWaitTimeout)
	runitWaitForOutput(t, client, false, "stdout:live with spaces \r\n")
	runitWaitForOutput(t, client, true, "stderr:live with spaces \r\n")
	managed := runitListProcess(t, scenario, name)
	if !testutil.ProcessAlive(managed.PID) {
		t.Fatalf("managed process is not alive before attached-client loss (PID %d)", managed.PID)
	}

	if err := client.Kill(); err != nil {
		t.Fatalf("kill original attached client: %v", err)
	}
	_ = client.Wait(runitWaitTimeout)
	remaining := runitListProcess(t, scenario, name)
	if remaining.State != "running" || remaining.PID != managed.PID || !testutil.ProcessAlive(remaining.PID) {
		t.Fatalf("managed process after attached-client loss = %#v, want same running process", remaining)
	}

	observerHum := testutil.BuildHum(t)
	observer := testutil.Start(t, observerHum, scenario.cwd, scenario.env, "logs", name, "--follow")
	runitWaitForOutput(t, observer, false, "stdout:live with spaces \r\n")
	runitWaitForOutput(t, observer, false, "stderr:live with spaces \r\n")
	runitStopProcess(t, scenario, name, marker, managed.PID)
	if err := observer.Wait(runitWaitTimeout); err != nil {
		t.Fatalf("fresh logs --follow observer exit: %v; stdout=%q stderr=%q", err, observer.Stdout(), observer.Stderr())
	}
}

func runitNewScenario(t *testing.T) runitScenario {
	t.Helper()
	runtimeDir := testutil.RuntimeDir(t)
	clientDir := t.TempDir()
	return runitScenario{
		hum:        testutil.BuildHum(t),
		fixture:    testutil.BuildFixture(t),
		runtimeDir: runtimeDir,
		cwd:        runitCanonicalPath(t, clientDir),
		env: testutil.RuntimeEnv(runtimeDir,
			"HUM_TEST_RUN=integration",
			"HUM_TEST_VALUE=value with spaces",
			"HUM_TEST_EMPTY=",
		),
	}
}

func runitCleanup(t *testing.T, scenario runitScenario, names ...string) {
	t.Helper()
	t.Cleanup(func() {
		daemonPID := runitReadPID(filepath.Join(scenario.runtimeDir, "hum.pid"))
		for _, name := range names {
			_ = testutil.Run(t, scenario.hum, scenario.cwd, scenario.env, "stop", name)
		}
		_ = testutil.Run(t, scenario.hum, scenario.cwd, scenario.env, "shutdown", "--stop-processes")
		if daemonPID > 0 && testutil.ProcessAlive(daemonPID) {
			testutil.WaitForProcessGone(t, daemonPID, runitWaitTimeout)
		}
	})
}

func runitStopProcess(t *testing.T, scenario runitScenario, name, marker string, pid int) {
	t.Helper()
	result := testutil.Run(t, scenario.hum, scenario.cwd, scenario.env, "stop", name)
	if result.Code != 0 {
		t.Fatalf("stop %q exit code = %d (err %v); stdout=%q stderr=%q", name, result.Code, result.Err, result.Stdout, result.Stderr)
	}
	testutil.WaitForFile(t, marker+".terminated", runitWaitTimeout)
	if pid > 0 {
		testutil.WaitForProcessGone(t, pid, runitWaitTimeout)
	}
}

func runitListProcess(t *testing.T, scenario runitScenario, name string) runitProcessRecord {
	t.Helper()
	result := testutil.Run(t, scenario.hum, scenario.cwd, scenario.env, "list", "--json")
	if result.Code != 0 {
		t.Fatalf("list --json exit code = %d (err %v); stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
	}
	var response runitListResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &response); err != nil {
		t.Fatalf("decode list --json: %v; output=%q", err, result.Stdout)
	}
	for _, process := range response.Processes {
		if process.Name == name {
			return process
		}
	}
	t.Fatalf("list --json omitted running process %q: %q", name, result.Stdout)
	return runitProcessRecord{}
}

func runitAssertListedProcess(t *testing.T, process runitProcessRecord, summary runitRunSummary, wantCwd string, wantArgv []string) {
	t.Helper()
	if process.Name != summary.Name || process.PID != summary.PID || process.LaunchCursor != summary.Cursor {
		t.Fatalf("listed process identity = %#v, want name=%q PID=%d cursor=%d", process, summary.Name, summary.PID, summary.Cursor)
	}
	if process.State != "running" {
		t.Fatalf("listed process state = %q, want running", process.State)
	}
	if process.Cwd != wantCwd {
		t.Fatalf("listed process cwd = %q, want canonical %q", process.Cwd, wantCwd)
	}
	if !reflect.DeepEqual(process.Argv, wantArgv) {
		t.Fatalf("listed process argv = %#v, want exact %#v", process.Argv, wantArgv)
	}
}

func runitDecodeInspectSnapshot(t *testing.T, stdout string) runitInspectSnapshot {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		if !strings.HasPrefix(line, "SNAPSHOT ") {
			continue
		}
		var snapshot runitInspectSnapshot
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "SNAPSHOT ")), &snapshot); err != nil {
			t.Fatalf("decode fixture snapshot: %v; output=%q", err, stdout)
		}
		return snapshot
	}
	t.Fatalf("attached output omitted SNAPSHOT line: %q", stdout)
	return runitInspectSnapshot{}
}

func runitParseHumanSummary(output string) (runitRunSummary, error) {
	match := runitHumanRunPattern.FindStringSubmatch(strings.TrimSpace(output))
	if len(match) != 4 {
		return runitRunSummary{}, fmt.Errorf("want one started NAME (PID N, cursor N) line")
	}
	pid, err := strconv.Atoi(match[2])
	if err != nil {
		return runitRunSummary{}, fmt.Errorf("parse PID: %w", err)
	}
	cursor, err := strconv.ParseUint(match[3], 10, 64)
	if err != nil {
		return runitRunSummary{}, fmt.Errorf("parse cursor: %w", err)
	}
	return runitRunSummary{Name: match[1], PID: pid, Cursor: cursor}, nil
}

func runitSelectedEnvironment(env []string) map[string]string {
	selected := make(map[string]string)
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok && strings.HasPrefix(key, "HUM_TEST_") {
			selected[key] = value
		}
	}
	return selected
}

func runitCanonicalPath(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("canonicalize %q: %v", path, err)
	}
	absolute, err := filepath.Abs(canonical)
	if err != nil {
		t.Fatalf("absolute canonical path %q: %v", canonical, err)
	}
	return filepath.Clean(absolute)
}

func runitReadPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func runitWaitForOutput(t *testing.T, process *testutil.Process, stderr bool, text string) {
	t.Helper()
	read := func() string {
		if stderr {
			return process.Stderr()
		}
		return process.Stdout()
	}
	contains := func() bool { return strings.Contains(read(), text) }
	if contains() {
		return
	}
	timer := time.NewTimer(runitWaitTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(runitPollPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if contains() {
				return
			}
			if process.Exited() {
				stream := "stdout"
				if stderr {
					stream = "stderr"
				}
				t.Fatalf("process exited before %s contained %q: %q", stream, text, read())
			}
		case <-timer.C:
			stream := "stdout"
			if stderr {
				stream = "stderr"
			}
			t.Fatalf("timed out waiting for %s text %q: %q", stream, text, read())
		}
	}
}
