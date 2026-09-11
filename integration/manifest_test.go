//go:build darwin || linux

package integration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"hum/internal/testutil"
)

const manifestWorkflowTimeout = 30 * time.Second

type manifestTestReady struct {
	Match   string
	Timeout string
}

type manifestTestDefinition struct {
	Name  string
	Argv  []string
	Cwd   string
	Ready *manifestTestReady
	After []string
}

// manifestLaunchResult is deliberately local to this integration test. Start
// and up use one JSON object per declared name, while optional process identity
// fields are useful when present but are not part of every outcome.
type manifestLaunchResult struct {
	Name                string          `json:"name"`
	Outcome             string          `json:"outcome"`
	Source              string          `json:"source"`
	Argv                []string        `json:"argv"`
	State               string          `json:"state,omitempty"`
	PID                 *int            `json:"pid,omitempty"`
	LaunchCursor        *uint64         `json:"launch_cursor,omitempty"`
	Readiness           string          `json:"readiness,omitempty"`
	ReadinessMatch      string          `json:"readiness_match,omitempty"`
	ReadinessMethod     string          `json:"readiness_method,omitempty"`
	ReadinessArgv       []string        `json:"readiness_argv,omitempty"`
	ReadinessInterval   time.Duration   `json:"readiness_interval,omitempty"`
	ReadinessDiagnostic string          `json:"readiness_diagnostic,omitempty"`
	ReadyCursor         *uint64         `json:"ready_cursor,omitempty"`
	BlockedBy           []string        `json:"blocked_by,omitempty"`
	ChangedFields       []string        `json:"changed_fields,omitempty"`
	Guidance            string          `json:"guidance,omitempty"`
	Error               json.RawMessage `json:"error,omitempty"`
}

type manifestProcess struct {
	Name         string   `json:"name"`
	Source       string   `json:"source"`
	PID          int      `json:"pid"`
	Cwd          string   `json:"cwd"`
	Argv         []string `json:"argv"`
	LaunchCursor uint64   `json:"launch_cursor"`
	State        string   `json:"state"`
	Readiness    string   `json:"readiness,omitempty"`
	Relaunches   int      `json:"relaunches,omitempty"`
	ReadyCursor  *uint64  `json:"ready_cursor,omitempty"`
}

type manifestListResponse struct {
	Processes []manifestProcess `json:"processes"`
}

func TestExecutableReadiness(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	fixture := integrationFixture(t)
	projectRoot := t.TempDir()
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir, "HUM_STOP_GRACE=1s")

	probeSource := filepath.Join(t.TempDir(), "readiness-probe.go")
	probe, err := os.Create(probeSource)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probe.WriteString(`package main

import (
	"os"
)

func main() {
	if len(os.Args) != 2 {
		os.Exit(2)
	}
	if _, err := os.Stat(os.Args[1]); err != nil {
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[1]+".probed", []byte("ready"), 0600); err != nil {
		os.Exit(3)
	}
}
`); err != nil {
		_ = probe.Close()
		t.Fatal(err)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	probeBinary := filepath.Join(t.TempDir(), "readiness-probe")
	build := exec.Command("go", "build", "-o", probeBinary, probeSource)
	buildOutput, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build readiness probe: %v\n%s", err, buildOutput)
	}
	missing := testutil.Run(t, probeBinary, projectRoot, env, filepath.Join(projectRoot, "service.started"))
	if missing.Code != 1 || missing.Err == nil {
		t.Fatalf("initial probe = code %d err=%v, want nonzero before marker", missing.Code, missing.Err)
	}

	manifest := fmt.Sprintf(`version: 1
processes:
  service:
    argv: [%s, stream, %s]
    ready:
      exec: [%s, %s]
      interval: 10ms
  dependent:
    argv: [%s, stream, %s]
    after: [service]
    ready:
      exec: [%s, %s]
      interval: 10ms
`, yamlQuote(fixture), yamlQuote(filepath.Join(projectRoot, "service")), yamlQuote(probeBinary), yamlQuote(filepath.Join(projectRoot, "service.started")), yamlQuote(fixture), yamlQuote(filepath.Join(projectRoot, "dependent")), yamlQuote(probeBinary), yamlQuote(filepath.Join(projectRoot, "dependent.started")))
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = testutil.Run(t, hum, projectRoot, env, "stop", "dependent")
		_ = testutil.Run(t, hum, projectRoot, env, "stop", "service")
		_ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes")
	})

	up := testutil.Run(t, hum, projectRoot, env, "up", "--json")
	if up.Code != 0 || up.Err != nil || up.Stderr != "" {
		t.Fatalf("executable-readiness up = code %d err=%v stdout=%q stderr=%q", up.Code, up.Err, up.Stdout, up.Stderr)
	}
	results := manifestDecodeLaunchResults(t, up.Stdout)
	if len(results) != 2 {
		t.Fatalf("executable-readiness results=%#v, want service and dependent", results)
	}
	byName := make(map[string]manifestLaunchResult, len(results))
	for _, result := range results {
		byName[result.Name] = result
	}
	for _, name := range []string{"service", "dependent"} {
		result, ok := byName[name]
		if !ok || result.Outcome != "started" || result.Readiness != "ready" {
			t.Fatalf("%s launch=%#v, want started/ready executable result", name, result)
		}
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "service.started")); err != nil {
		t.Fatalf("service marker: %v", err)
	}
	dependentMarker := filepath.Join(projectRoot, "dependent.started")
	dependentInfo, err := os.Stat(dependentMarker)
	if err != nil {
		t.Fatalf("dependent marker: %v", err)
	}
	serviceProbeInfo, err := os.Stat(filepath.Join(projectRoot, "service.started.probed"))
	if err != nil {
		t.Fatalf("service probe marker: %v", err)
	}
	if !serviceProbeInfo.ModTime().Before(dependentInfo.ModTime()) {
		t.Fatalf("dependent started before service probe completed: probe=%s dependent=%s", serviceProbeInfo.ModTime(), dependentInfo.ModTime())
	}

	// A retained manifest definition must start a fresh executable probe for
	// every automatic relaunch, rather than inheriting a cleared incarnation's
	// tracker. The fixture exits twice before remaining alive on its third
	// launch; its marker is created before each probe runs.
	relaunchMarker := filepath.Join(projectRoot, "relaunch-count")
	relaunchManifest := manifest + fmt.Sprintf(`
  relaunch:
    argv: [%s, relaunch, %s]
    ready:
      exec: [%s, %s]
      interval: 10ms
    restart: on-failure
`, yamlQuote(fixture), yamlQuote(relaunchMarker), yamlQuote(probeBinary), yamlQuote(relaunchMarker))
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(relaunchManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	start := testutil.Run(t, hum, projectRoot, env, "start", "--json", "--no-wait", "relaunch")
	if start.Code != 0 || start.Err != nil || start.Stderr != "" {
		t.Fatalf("automatic relaunch start = code %d err=%v stdout=%q stderr=%q", start.Code, start.Err, start.Stdout, start.Stderr)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		status := testutil.Run(t, hum, projectRoot, env, "status", "--json", "relaunch")
		if status.Code == 0 {
			var process manifestProcess
			if err := json.Unmarshal([]byte(strings.TrimSpace(status.Stdout)), &process); err == nil && process.State == "running" && process.Readiness == "ready" && process.Relaunches >= 2 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	status := testutil.Run(t, hum, projectRoot, env, "status", "--json", "relaunch")
	t.Fatalf("automatic relaunch did not become ready: %q (stderr=%q)", status.Stdout, status.Stderr)
}

func yamlQuote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func TestUpReportsManifestRuntimeDrift(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	projectRoot := t.TempDir()
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir, "HUM_STOP_GRACE=1s")
	marker := filepath.Join(projectRoot, "api-launched")
	initial := `version: 1
processes:
  db:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: old}
  api:
    argv: [/bin/sh, -c, "touch api-launched; sleep 30"]
    ready: {match: api-ready}
    after: [db]
`
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_ = testutil.Run(t, hum, projectRoot, env, "stop", "db")
		_ = testutil.Run(t, hum, projectRoot, env, "stop", "api")
		_ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes")
	}
	t.Cleanup(cleanup)

	started := testutil.Run(t, hum, projectRoot, env, "start", "--json", "--no-wait", "db")
	if started.Code != 0 || started.Err != nil || started.Stderr != "" {
		t.Fatalf("initial start = code %d err=%v stdout=%q stderr=%q", started.Code, started.Err, started.Stdout, started.Stderr)
	}
	initialResults := manifestDecodeLaunchResults(t, started.Stdout)
	if len(initialResults) != 1 || initialResults[0].PID == nil {
		t.Fatalf("initial start results = %#v", initialResults)
	}
	pid := *initialResults[0].PID
	changed := strings.Replace(initial, "match: old", "match: new", 1)
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := testutil.Run(t, hum, projectRoot, env, "up", "--json")
	if stale.Code != 1 || stale.Err == nil || stale.Stderr != "" {
		t.Fatalf("stale gate up = code %d err=%v stdout=%q stderr=%q", stale.Code, stale.Err, stale.Stdout, stale.Stderr)
	}
	staleResults := manifestDecodeLaunchResults(t, stale.Stdout)
	if len(staleResults) != 2 || staleResults[0].Name != "api" || staleResults[0].Outcome != "skipped" || !reflect.DeepEqual(staleResults[0].BlockedBy, []string{"db"}) || staleResults[1].Name != "db" || staleResults[1].Outcome != "definition_drift" || !reflect.DeepEqual(staleResults[1].ChangedFields, []string{"readiness_match"}) || staleResults[1].Guidance != "hum restart db" {
		t.Fatalf("stale gate results = %#v", staleResults)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("stale gate launched api: %v", err)
	}
	status := testutil.Run(t, hum, projectRoot, env, "status", "--json", "db")
	if status.Code != 0 || status.Err != nil {
		t.Fatalf("status after drift = code %d err=%v stdout=%q stderr=%q", status.Code, status.Err, status.Stdout, status.Stderr)
	}
	var unchanged manifestProcess
	if err := json.Unmarshal([]byte(status.Stdout), &unchanged); err != nil {
		t.Fatalf("decode status after drift: %v", err)
	}
	if unchanged.PID != pid || !reflect.DeepEqual(unchanged.Argv, []string{"/bin/sh", "-c", "sleep 30"}) || unchanged.State != "running" {
		t.Fatalf("process mutated by drift: before pid=%d after=%#v", pid, unchanged)
	}

	human := testutil.Run(t, hum, projectRoot, env, "up")
	if human.Code != 1 || human.Err == nil || !strings.Contains(human.Stdout, "db    definition drift") || !strings.Contains(human.Stdout, "api   skipped") || !strings.Contains(human.Stderr, "definition_drift (readiness_match); run hum restart db") || !strings.Contains(human.Stderr, "skipped (blocked by db)") {
		t.Fatalf("human drift up = code %d err=%v stdout=%q stderr=%q", human.Code, human.Err, human.Stdout, human.Stderr)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte("version: 1\nprocesses: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed := testutil.Run(t, hum, projectRoot, env, "up", "--json")
	if removed.Code != 0 || removed.Err != nil || removed.Stderr != "" {
		t.Fatalf("removed up = code %d err=%v stdout=%q stderr=%q", removed.Code, removed.Err, removed.Stdout, removed.Stderr)
	}
	removedResults := manifestDecodeLaunchResults(t, removed.Stdout)
	if len(removedResults) != 1 || removedResults[0].Name != "db" || removedResults[0].Outcome != "removed_definition" || !strings.Contains(removedResults[0].Guidance, "hum stop db") || !strings.Contains(removedResults[0].Guidance, "hum remove db") {
		t.Fatalf("removed results = %#v", removedResults)
	}
	status = testutil.Run(t, hum, projectRoot, env, "status", "--json", "db")
	if status.Code != 0 || status.Err != nil {
		t.Fatalf("status after removal warning = code %d err=%v stdout=%q stderr=%q", status.Code, status.Err, status.Stdout, status.Stderr)
	}
	if err := json.Unmarshal([]byte(status.Stdout), &unchanged); err != nil {
		t.Fatalf("decode status after removal warning: %v", err)
	}
	if unchanged.PID != pid || unchanged.State != "running" {
		t.Fatalf("process mutated by removal warning: before pid=%d after=%#v", pid, unchanged)
	}
}

type manifestOutputEntry struct {
	Cursor uint64 `json:"cursor"`
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

type manifestOutputResponse struct {
	Entries        []manifestOutputEntry `json:"entries"`
	EvictedThrough *uint64               `json:"evicted_through,omitempty"`
	Truncated      bool                  `json:"truncated,omitempty"`
}

func TestUpOrderedStack(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	projectRoot := t.TempDir()
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir, "HUM_STOP_GRACE=1s")
	timeline := filepath.Join(projectRoot, "timeline")
	manifest := fmt.Sprintf(`version: 1
processes:
  db:
    argv: [/bin/sh, -c, %s]
    ready: {match: db-ready}
  api:
    argv: [/bin/sh, -c, %s]
    after: [db]
    ready: {match: api-ready}
  web:
    argv: [/bin/sh, -c, %s]
    after: [api]
    ready: {match: web-ready}
  root:
    argv: [/bin/sh, -c, %s]
    ready: {match: root-ready}
`, strconv.Quote(fmt.Sprintf("printf 'db-launched\\n' >> %q; sleep 0.2; printf 'db-ready\\n' >> %q; printf db-ready; sleep 30", timeline, timeline)), strconv.Quote(fmt.Sprintf("printf 'api-launched\\n' >> %q; printf api-ready; sleep 30", timeline)), strconv.Quote(fmt.Sprintf("printf 'web-launched\\n' >> %q; printf web-ready; sleep 30", timeline)), strconv.Quote(fmt.Sprintf("printf 'root-launched\\n' >> %q; printf root-ready; sleep 30", timeline)))
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, name := range []string{"api", "db", "root", "web"} {
			_ = testutil.Run(t, hum, projectRoot, env, "stop", name)
		}
		_ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes")
	})

	first := testutil.Run(t, hum, projectRoot, env, "up", "--json")
	if first.Code != 0 || first.Err != nil || first.Stderr != "" {
		t.Fatalf("ordered up = code %d err=%v stdout=%q stderr=%q", first.Code, first.Err, first.Stdout, first.Stderr)
	}
	launches := manifestDecodeLaunchResults(t, first.Stdout)
	if got := manifestLaunchNames(launches); !reflect.DeepEqual(got, []string{"api", "db", "root", "web"}) {
		t.Fatalf("ordered up names = %#v", got)
	}
	for _, launch := range launches {
		if launch.Outcome != "started" || launch.Readiness != "ready" {
			t.Fatalf("ordered up launch = %#v", launch)
		}
	}
	contents, err := os.ReadFile(timeline)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	positions := make(map[string]int, len(lines))
	for index, line := range lines {
		positions[line] = index
	}
	if positions["db-launched"] >= positions["db-ready"] || positions["db-ready"] >= positions["api-launched"] || positions["api-launched"] >= positions["web-launched"] {
		t.Fatalf("launch timeline = %v, want db launch < db ready < api < web", lines)
	}
	if positions["root-launched"] >= positions["db-ready"] {
		t.Fatalf("launch timeline = %v, want independent root to overlap db readiness wait", lines)
	}
	second := testutil.Run(t, hum, projectRoot, env, "up", "--json")
	if second.Code != 0 || second.Err != nil || second.Stderr != "" {
		t.Fatalf("idempotent ordered up = code %d err=%v stdout=%q stderr=%q", second.Code, second.Err, second.Stdout, second.Stderr)
	}
	for _, launch := range manifestDecodeLaunchResults(t, second.Stdout) {
		if launch.Outcome != "already_running" || launch.Readiness != "ready" {
			t.Fatalf("idempotent launch = %#v", launch)
		}
	}

	manifestTestUpBlockedStack(t, hum)
	manifestTestUpRecovery(t, hum, integrationFixture(t))
}

func manifestTestUpBlockedStack(t *testing.T, hum string) {
	t.Helper()
	projectRoot := t.TempDir()
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir, "HUM_STOP_GRACE=1s")
	apiMarker := filepath.Join(projectRoot, "api-launched")
	webMarker := filepath.Join(projectRoot, "web-launched")
	manifest := fmt.Sprintf(`version: 1
processes:
  db:
    argv: [/bin/sh, -c, "exit 4"]
    ready: {match: db-ready}
  queue:
    argv: [/bin/sh, -c, "exit 5"]
    ready: {match: queue-ready}
  api:
    argv: [/bin/sh, -c, %s]
    after: [queue, db]
    ready: {match: api-ready}
  web:
    argv: [/bin/sh, -c, %s]
    after: [api]
    ready: {match: web-ready}
`, strconv.Quote(fmt.Sprintf("touch %q; printf api-ready; sleep 30", apiMarker)), strconv.Quote(fmt.Sprintf("touch %q; printf web-ready; sleep 30", webMarker)))
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes") })

	result := testutil.Run(t, hum, projectRoot, env, "up", "--json")
	if result.Code != 3 || result.Err == nil {
		t.Fatalf("blocked up = code %d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
	}
	launches := manifestDecodeLaunchResults(t, result.Stdout)
	if got := manifestLaunchNames(launches); !reflect.DeepEqual(got, []string{"api", "db", "queue", "web"}) {
		t.Fatalf("blocked names = %#v", got)
	}
	if launches[0].Outcome != "skipped" || !reflect.DeepEqual(launches[0].BlockedBy, []string{"db", "queue"}) {
		t.Fatalf("api blockers = %#v", launches[0])
	}
	if launches[3].Outcome != "skipped" || !reflect.DeepEqual(launches[3].BlockedBy, []string{"api"}) {
		t.Fatalf("web blockers = %#v", launches[3])
	}
	for _, marker := range []string{apiMarker, webMarker} {
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("blocked process marker %q exists: %v", marker, err)
		}
	}
}

func manifestTestUpRecovery(t *testing.T, hum, fixture string) {
	t.Helper()
	projectRoot := t.TempDir()
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir, "HUM_OUTPUT_BYTES=65536", "HUM_COMPLETED_RECORDS=20", "HUM_STOP_GRACE=1s")
	launchesMarker := filepath.Join(projectRoot, "db-launches")
	apiMarker := filepath.Join(projectRoot, "api-launched")
	manifest := fmt.Sprintf(`version: 1
processes:
  db:
    argv: [%s, relaunch, %s]
    ready: {match: ready, timeout: 5s}
    restart: on-failure
  api:
    argv: [/bin/sh, -c, %s]
    after: [db]
    ready: {match: api-ready}
`, strconv.Quote(fixture), strconv.Quote(launchesMarker), strconv.Quote(fmt.Sprintf("touch %q; printf api-ready; sleep 30", apiMarker)))
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes") })

	first := testutil.Run(t, hum, projectRoot, env, "up", "--json")
	if first.Code != 3 || first.Err == nil {
		t.Fatalf("recovery first up = code %d err=%v stdout=%q stderr=%q", first.Code, first.Err, first.Stdout, first.Stderr)
	}
	firstLaunches := manifestDecodeLaunchResults(t, first.Stdout)
	if firstLaunches[0].Outcome != "skipped" || !reflect.DeepEqual(firstLaunches[0].BlockedBy, []string{"db"}) || firstLaunches[1].Outcome != "exited_before_ready" {
		t.Fatalf("recovery first results = %#v", firstLaunches)
	}
	if _, err := os.Stat(apiMarker); !os.IsNotExist(err) {
		t.Fatalf("same invocation followed successor: api marker exists: %v", err)
	}
	relaunchIntegrationWaitStatus(t, hum, projectRoot, env, "db", func(status relaunchIntegrationStatus) bool {
		return status.State == "running" && status.Readiness == "ready" && status.Relaunches == 2
	})
	if _, err := os.Stat(apiMarker); !os.IsNotExist(err) {
		t.Fatalf("automatic recovery launched dependent: %v", err)
	}
	second := testutil.Run(t, hum, projectRoot, env, "up", "--json")
	if second.Code != 0 || second.Err != nil {
		t.Fatalf("recovery second up = code %d err=%v stdout=%q stderr=%q", second.Code, second.Err, second.Stdout, second.Stderr)
	}
	secondLaunches := manifestDecodeLaunchResults(t, second.Stdout)
	if secondLaunches[0].Outcome != "started" || secondLaunches[0].Readiness != "ready" || secondLaunches[1].Outcome != "already_running" || secondLaunches[1].Readiness != "ready" {
		t.Fatalf("recovery second results = %#v", secondLaunches)
	}
	if _, err := os.Stat(apiMarker); err != nil {
		t.Fatalf("later up did not launch dependent: %v", err)
	}
}

func TestUpStartupProgress(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	projectRoot := t.TempDir()
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir, "HUM_STOP_GRACE=1s")
	manifest := fmt.Sprintf(`version: 1
processes:
  fast:
    argv: [/bin/sh, -c, %s]
    ready: {match: fast-ready, timeout: 2s}
  slow:
    argv: [/bin/sh, -c, %s]
    ready: {match: never-seen, timeout: 2s}
`, strconv.Quote("printf fast-child-output; sleep 0.2; printf fast-ready; sleep 30"), strconv.Quote("printf slow-child-output; sleep 30"))
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, name := range []string{"fast", "slow"} {
			_ = testutil.Run(t, hum, projectRoot, env, "stop", name)
		}
		_ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes")
	})

	up := testutil.Start(t, hum, projectRoot, env, "up", "--timeout", "700ms")
	manifestIntegrationWaitForText(t, up, "hum up: fast: started; waiting for readiness", manifestWorkflowTimeout)
	manifestIntegrationWaitForText(t, up, "hum up: slow: started; waiting for readiness", manifestWorkflowTimeout)
	manifestIntegrationWaitForText(t, up, "hum up: fast: ready", manifestWorkflowTimeout)
	if up.Exited() {
		t.Fatalf("up exited before slow readiness timeout: stdout=%q stderr=%q", up.Stdout(), up.Stderr())
	}
	if strings.Contains(up.Stderr(), "fast-child-output") || strings.Contains(up.Stderr(), "slow-child-output") {
		t.Fatalf("up progress copied child output: %q", up.Stderr())
	}
	if err := up.Wait(manifestWorkflowTimeout); err == nil {
		t.Fatalf("up unexpectedly succeeded: stdout=%q stderr=%q", up.Stdout(), up.Stderr())
	}
	if up.Cmd.ProcessState == nil || up.Cmd.ProcessState.ExitCode() != 2 {
		t.Fatalf("up exit code = %v, want 2; stdout=%q stderr=%q", up.Cmd.ProcessState, up.Stdout(), up.Stderr())
	}
	if !strings.Contains(up.Stderr(), "hum up: slow: readiness timed out; inspect retained logs: hum logs slow") {
		t.Fatalf("timeout progress = %q", up.Stderr())
	}
	lines := strings.Split(strings.TrimSpace(up.Stdout()), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "NAME ") || !strings.HasPrefix(lines[1], "fast ") || !strings.HasPrefix(lines[2], "slow ") {
		t.Fatalf("final summary table = %q, want header then lexical fast and slow rows", up.Stdout())
	}
}

func TestUpReadinessTimeoutDiagnostics(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	projectRoot := t.TempDir()
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir, "HUM_STOP_GRACE=1s")
	manifestPath := filepath.Join(projectRoot, "hum.yaml")
	write := func(command string) {
		t.Helper()
		manifest := fmt.Sprintf(`version: 1
processes:
  probe:
    argv: [/bin/sh, -c, %s]
    ready: {match: never-seen, timeout: 2s}
`, strconv.Quote(command))
		if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("printf timeout-child-output; sleep 30")
	t.Cleanup(func() {
		_ = testutil.Run(t, hum, projectRoot, env, "stop", "probe")
		_ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes")
	})

	timeout := testutil.Run(t, hum, projectRoot, env, "up", "--timeout", "150ms")
	if timeout.Code != 2 || timeout.Err == nil {
		t.Fatalf("timeout up = code %d err=%v stdout=%q stderr=%q, want exit 2", timeout.Code, timeout.Err, timeout.Stdout, timeout.Stderr)
	}
	if !strings.Contains(timeout.Stderr, "hum up: probe: readiness timed out; inspect retained logs: hum logs probe") {
		t.Fatalf("timeout progress = %q", timeout.Stderr)
	}
	if strings.Contains(timeout.Stderr, "timeout-child-output") || !strings.Contains(timeout.Stdout, "timed out") {
		t.Fatalf("timeout output = stdout %q stderr %q", timeout.Stdout, timeout.Stderr)
	}
	logs := testutil.Run(t, hum, projectRoot, env, "logs", "probe", "--json", "--stream", "stdout")
	if logs.Code != 0 || logs.Err != nil {
		t.Fatalf("timeout retained logs = code %d err=%v stdout=%q stderr=%q", logs.Code, logs.Err, logs.Stdout, logs.Stderr)
	}
	var timeoutLogs manifestOutputResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(logs.Stdout)), &timeoutLogs); err != nil {
		t.Fatalf("decode timeout logs = %q: %v", logs.Stdout, err)
	}
	if !manifestOutputContains(timeoutLogs, "timeout-child-output") {
		t.Fatalf("timeout retained logs = %#v, missing child diagnostic", timeoutLogs)
	}
	if stopped := testutil.Run(t, hum, projectRoot, env, "stop", "probe"); stopped.Code != 0 {
		t.Fatalf("stop timeout probe = code %d stderr=%q", stopped.Code, stopped.Stderr)
	}

	write("printf early-child-output; exit 7")
	early := testutil.Run(t, hum, projectRoot, env, "up", "--timeout", "1s")
	if early.Code != 3 || early.Err == nil {
		t.Fatalf("early-exit up = code %d err=%v stdout=%q stderr=%q, want exit 3", early.Code, early.Err, early.Stdout, early.Stderr)
	}
	if !strings.Contains(early.Stderr, "hum up: probe: exited before readiness; inspect retained logs: hum logs probe") {
		t.Fatalf("early-exit progress = %q", early.Stderr)
	}
	if strings.Contains(early.Stderr, "early-child-output") || !strings.Contains(early.Stdout, "exited before ready") {
		t.Fatalf("early-exit output = stdout %q stderr %q", early.Stdout, early.Stderr)
	}
	earlyLogs := testutil.Run(t, hum, projectRoot, env, "logs", "probe", "--json", "--stream", "stdout")
	if earlyLogs.Code != 0 || earlyLogs.Err != nil {
		t.Fatalf("early retained logs = code %d err=%v stdout=%q stderr=%q", earlyLogs.Code, earlyLogs.Err, earlyLogs.Stdout, earlyLogs.Stderr)
	}
	var earlyOutput manifestOutputResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(earlyLogs.Stdout)), &earlyOutput); err != nil {
		t.Fatalf("decode early logs = %q: %v", earlyLogs.Stdout, err)
	}
	if !manifestOutputContains(earlyOutput, "early-child-output") {
		t.Fatalf("early retained logs = %#v, missing child diagnostic", earlyOutput)
	}
}

func manifestIntegrationWaitForText(t *testing.T, process *testutil.Process, text string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(process.Stderr(), text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q: stdout=%q stderr=%q", text, process.Stdout(), process.Stderr())
}

func manifestOutputContains(result manifestOutputResponse, text string) bool {
	for _, entry := range result.Entries {
		if strings.Contains(entry.Text, text) {
			return true
		}
	}
	return false
}

func TestManifestWorkflow(t *testing.T) {
	fixture := integrationFixture(t)
	hum := integrationHum(t)
	runtimeDir := testutil.RuntimeDir(t)
	projectRoot := t.TempDir()
	env := testutil.RuntimeEnv(runtimeDir, "HUM_OUTPUT_BYTES=65536", "HUM_STOP_GRACE=1s")
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(projectRoot, "alpha-gate"), []byte("release\n"), 0o600)
		for _, name := range []string{"alpha-ready", "gamma-retained", "zeta-plain", "ad-hoc", "beta-fail"} {
			_ = testutil.Run(t, hum, projectRoot, env, "stop", name)
		}
		_ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes")
	})

	alphaGate := filepath.Join(projectRoot, "alpha-gate")
	gate := filepath.Join(projectRoot, "retained-gate")
	betaMarker := filepath.Join(projectRoot, "beta")
	adHocMarker := filepath.Join(projectRoot, "ad-hoc")
	missingCommand := filepath.Join(projectRoot, "missing-command")

	definitions := []manifestTestDefinition{
		{
			Name: "zeta-plain",
			Argv: []string{fixture, "stream", betaMarker},
		},
		{
			Name: "beta-fail",
			Argv: []string{missingCommand},
		},
		{
			Name: "alpha-ready",
			// Keep the process producing output briefly after the match so exit
			// cannot win the readiness observation under a loaded CI runner.
			Argv:  []string{fixture, "burst", alphaGate, "100"},
			Ready: &manifestTestReady{Match: `stdout:0056`},
		},
		{
			Name: "gamma-retained",
			Argv: []string{fixture, "burst", gate, "8000"},
			// Either stream's first line is cursor zero; pipe readers may race.
			Ready: &manifestTestReady{Match: `(stdout|stderr):0000`},
		},
	}
	writeManifestTestYAML(t, projectRoot, definitions)

	// The first up has no daemon to connect to. It must resolve every
	// declaration lexically, continue after beta-fail, and wait until the
	// delayed alpha-ready expression matches before returning its results.
	upProcess := testutil.Start(t, hum, projectRoot, env, "up", "--json")
	manifestWaitForLogText(t, hum, projectRoot, env, "alpha-ready", "stdout:0003\n")
	if upProcess.Exited() {
		t.Fatalf("up exited before delayed readiness gate: stdout=%q stderr=%q", upProcess.Stdout(), upProcess.Stderr())
	}
	if err := os.WriteFile(alphaGate, []byte("release\n"), 0o600); err != nil {
		t.Fatalf("release delayed readiness gate: %v", err)
	}
	waitErr := upProcess.Wait(manifestWorkflowTimeout)
	upCode := -1
	if upProcess.Cmd.ProcessState != nil {
		upCode = upProcess.Cmd.ProcessState.ExitCode()
	}
	if upCode != 1 {
		t.Fatalf("up process did not finish with aggregate exit 1: code=%d err=%v stdout=%q stderr=%q", upCode, waitErr, upProcess.Stdout(), upProcess.Stderr())
	}
	up := testutil.Result{Stdout: upProcess.Stdout(), Stderr: upProcess.Stderr(), Code: upCode, Err: waitErr}
	launches := manifestDecodeLaunchResults(t, up.Stdout)
	wantNames := []string{"alpha-ready", "beta-fail", "gamma-retained", "zeta-plain"}
	if got := manifestLaunchNames(launches); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("up result names = %#v, want lexical %#v", got, wantNames)
	}
	launchByName := make(map[string]manifestLaunchResult, len(launches))
	for _, launch := range launches {
		launchByName[launch.Name] = launch
		if launch.Source != "manifest" {
			t.Errorf("up result %q source = %q, want manifest", launch.Name, launch.Source)
		}
		if launch.Error == nil && len(launch.Argv) == 0 {
			t.Errorf("up result %q omitted argv", launch.Name)
		}
	}
	manifestAssertLaunch(t, launchByName["alpha-ready"], "started", definitions[2].Argv)
	manifestAssertLaunchReadiness(t, launchByName["alpha-ready"], "ready")
	manifestAssertLaunch(t, launchByName["zeta-plain"], "running_unverified", definitions[0].Argv)
	manifestAssertLaunchReadiness(t, launchByName["zeta-plain"], "running_unverified")
	manifestAssertLaunch(t, launchByName["gamma-retained"], "started", definitions[3].Argv)
	manifestAssertLaunchReadiness(t, launchByName["gamma-retained"], "ready")
	if launchByName["beta-fail"].Outcome != "error" || len(launchByName["beta-fail"].Error) == 0 {
		t.Fatalf("failed declaration result = %#v, want outcome=error with an error", launchByName["beta-fail"])
	}
	if !reflect.DeepEqual(launchByName["beta-fail"].Argv, definitions[1].Argv) {
		t.Fatalf("failed declaration argv = %#v, want %#v", launchByName["beta-fail"].Argv, definitions[1].Argv)
	}

	testutil.WaitForFile(t, betaMarker+".started", manifestWorkflowTimeout)

	// gamma-retained emits the matching line in its first burst, then keeps
	// running behind gate. Its small output budget forces that early line out
	// of the retained ring while the process remains alive.
	retainedOutput := manifestWaitForEviction(t, hum, projectRoot, env, "gamma-retained")
	if retainedOutput.EvictedThrough == nil || !retainedOutput.Truncated {
		t.Fatalf("retained output = %#v, want truncation metadata", retainedOutput)
	}
	if launchByName["gamma-retained"].ReadyCursor != nil && *retainedOutput.EvictedThrough < *launchByName["gamma-retained"].ReadyCursor {
		t.Fatalf("retained output evicted through cursor %d, before launch result ready cursor %d", *retainedOutput.EvictedThrough, *launchByName["gamma-retained"].ReadyCursor)
	}

	listed := manifestList(t, hum, projectRoot, env)
	for _, name := range []string{"gamma-retained", "zeta-plain"} {
		process, ok := listed[name]
		if !ok {
			t.Fatalf("list omitted successful manifest process %q: %#v", name, listed)
		}
		if process.Source != "manifest" || process.State != "running" || process.PID <= 0 {
			t.Fatalf("manifest process %q = %#v, want running source=manifest", name, process)
		}
		if !testutil.ProcessAlive(process.PID) {
			t.Fatalf("successful manifest process %q (PID %d) did not survive failed entry", name, process.PID)
		}
		wantArgv := manifestDefinitionByName(definitions, name).Argv
		if !reflect.DeepEqual(process.Argv, wantArgv) {
			t.Fatalf("manifest process %q argv = %#v, want %#v", name, process.Argv, wantArgv)
		}
	}
	if listed["zeta-plain"].Readiness != "running_unverified" {
		t.Fatalf("no-ready process list readiness = %q, want running_unverified", listed["zeta-plain"].Readiness)
	}

	// Ensure-running an already-running manifest process must not replace its
	// incarnation. gamma also proves that readiness remains satisfied after
	// its matching output was evicted.
	beforeGamma := listed["gamma-retained"]
	gammaAgain := testutil.Run(t, hum, projectRoot, env, "start", "gamma-retained", "--json")
	if gammaAgain.Code != 0 || gammaAgain.Err != nil || gammaAgain.Stderr != "" {
		t.Fatalf("idempotent start gamma-retained: code=%d err=%v stdout=%q stderr=%q", gammaAgain.Code, gammaAgain.Err, gammaAgain.Stdout, gammaAgain.Stderr)
	}
	gammaLaunch := manifestDecodeLaunchResults(t, gammaAgain.Stdout)
	if len(gammaLaunch) != 1 {
		t.Fatalf("idempotent start result = %#v, want one object", gammaLaunch)
	}
	manifestAssertLaunch(t, gammaLaunch[0], "already_running", beforeGamma.Argv)
	manifestAssertLaunchReadiness(t, gammaLaunch[0], "ready")
	afterGamma := manifestList(t, hum, projectRoot, env)["gamma-retained"]
	if afterGamma.PID != beforeGamma.PID || afterGamma.LaunchCursor != beforeGamma.LaunchCursor {
		t.Fatalf("idempotent start replaced gamma-retained: before=%#v after=%#v", beforeGamma, afterGamma)
	}

	status := testutil.Run(t, hum, projectRoot, env, "status", "gamma-retained", "--json")
	if status.Code != 0 || status.Err != nil || status.Stderr != "" {
		t.Fatalf("status gamma-retained: code=%d err=%v stdout=%q stderr=%q", status.Code, status.Err, status.Stdout, status.Stderr)
	}
	var statusProcess manifestProcess
	if err := json.Unmarshal([]byte(strings.TrimSpace(status.Stdout)), &statusProcess); err != nil {
		t.Fatalf("decode status --json: %v; output=%q", err, status.Stdout)
	}
	if statusProcess.Name != "gamma-retained" || statusProcess.Source != "manifest" || statusProcess.Readiness != "ready" {
		t.Fatalf("status process = %#v, want manifest gamma-retained ready", statusProcess)
	}
	if statusProcess.ReadyCursor == nil {
		t.Fatalf("status process = %#v, want ready_cursor", statusProcess)
	}
	if statusProcess.State != "running" || !testutil.ProcessAlive(statusProcess.PID) {
		t.Fatalf("status process = %#v, want live running process", statusProcess)
	}

	collisionMarker := filepath.Join(projectRoot, "collision")
	collision := testutil.Run(t, hum, projectRoot, env, "run", "alpha-ready", "--detach", "--", fixture, "stream", collisionMarker)
	if collision.Code == 0 || collision.Err == nil {
		t.Fatalf("raw run for declared name: code=%d err=%v stdout=%q stderr=%q", collision.Code, collision.Err, collision.Stdout, collision.Stderr)
	}
	if !strings.Contains(strings.ToLower(collision.Stderr), "declared") && !strings.Contains(strings.ToLower(collision.Stderr), "manifest") {
		t.Fatalf("raw run collision stderr = %q, want declaration guidance", collision.Stderr)
	}
	if _, err := os.Stat(collisionMarker + ".started"); !os.IsNotExist(err) {
		t.Fatalf("raw run collision created child marker: err=%v", err)
	}

	adHocArgs := []string{fixture, "stream", adHocMarker}
	adHoc := testutil.Run(t, hum, projectRoot, env, "run", "ad-hoc", "--detach", "--", adHocArgs[0], adHocArgs[1], adHocArgs[2])
	if adHoc.Code != 0 || adHoc.Err != nil || adHoc.Stderr != "" {
		t.Fatalf("ad-hoc run: code=%d err=%v stdout=%q stderr=%q", adHoc.Code, adHoc.Err, adHoc.Stdout, adHoc.Stderr)
	}
	testutil.WaitForFile(t, adHocMarker+".started", manifestWorkflowTimeout)
	merged := manifestList(t, hum, projectRoot, env)
	adHocProcess, ok := merged["ad-hoc"]
	if !ok {
		t.Fatalf("merged list omitted ad-hoc process: %#v", merged)
	}
	if adHocProcess.Source != "ad_hoc" {
		t.Fatalf("ad-hoc source = %q, want ad_hoc", adHocProcess.Source)
	}
	if !reflect.DeepEqual(adHocProcess.Argv, adHocArgs) {
		t.Fatalf("ad-hoc argv = %#v, want %#v", adHocProcess.Argv, adHocArgs)
	}
	for _, name := range []string{"alpha-ready", "gamma-retained", "zeta-plain"} {
		if merged[name].Source != "manifest" {
			t.Fatalf("merged list %q source = %q, want manifest", name, merged[name].Source)
		}
	}

}

func writeManifestTestYAML(t *testing.T, root string, definitions []manifestTestDefinition) {
	t.Helper()
	var document strings.Builder
	document.WriteString("version: 1\nprocesses:\n")
	for _, definition := range definitions {
		fmt.Fprintf(&document, "  %s:\n    argv:\n", definition.Name)
		for _, arg := range definition.Argv {
			fmt.Fprintf(&document, "      - %s\n", strconv.Quote(arg))
		}
		if definition.Cwd != "" {
			fmt.Fprintf(&document, "    cwd: %s\n", strconv.Quote(definition.Cwd))
		}
		if len(definition.After) != 0 {
			fmt.Fprintf(&document, "    after: %s\n", formatManifestStringSequence(definition.After))
		}
		if definition.Ready != nil {
			document.WriteString("    ready:\n")
			fmt.Fprintf(&document, "      match: %s\n", strconv.Quote(definition.Ready.Match))
			if definition.Ready.Timeout != "" {
				fmt.Fprintf(&document, "      timeout: %s\n", strconv.Quote(definition.Ready.Timeout))
			}
		}
	}
	manifestPath := filepath.Join(root, "hum.yaml")
	if err := os.WriteFile(manifestPath, []byte(document.String()), 0o600); err != nil {
		t.Fatalf("write %s: %v", manifestPath, err)
	}
}

func formatManifestStringSequence(values []string) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = strconv.Quote(value)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func manifestDecodeLaunchResults(t *testing.T, output string) []manifestLaunchResult {
	t.Helper()
	var results []manifestLaunchResult
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var result manifestLaunchResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatalf("decode launch JSON line: %v; line=%q output=%q", err, line, output)
		}
		results = append(results, result)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan launch JSON: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("launch JSON contained no result objects: %q", output)
	}
	return results
}

func manifestLaunchNames(results []manifestLaunchResult) []string {
	names := make([]string, 0, len(results))
	for _, result := range results {
		names = append(names, result.Name)
	}
	return names
}

func manifestAssertLaunch(t *testing.T, result manifestLaunchResult, outcome string, wantArgv []string) {
	t.Helper()
	if result.Name == "" || result.Source != "manifest" || result.Outcome != outcome {
		t.Fatalf("launch result = %#v, want source=manifest outcome=%q", result, outcome)
	}
	if !reflect.DeepEqual(result.Argv, wantArgv) {
		t.Fatalf("launch %q argv = %#v, want %#v", result.Name, result.Argv, wantArgv)
	}
}

func manifestAssertLaunchReadiness(t *testing.T, result manifestLaunchResult, want string) {
	t.Helper()
	if result.Readiness != want && result.Outcome != want {
		t.Fatalf("launch %q outcome/readiness = %q/%q, want %q", result.Name, result.Outcome, result.Readiness, want)
	}
	if want == "ready" && result.Readiness != "" && result.ReadyCursor == nil {
		t.Fatalf("ready launch %q = %#v, want ready_cursor when readiness is reported", result.Name, result)
	}
}

func manifestWaitForLogText(t *testing.T, hum, cwd string, env []string, name, want string) {
	t.Helper()
	deadline := time.Now().Add(manifestWorkflowTimeout)
	for time.Now().Before(deadline) {
		result := testutil.Run(t, hum, cwd, env, "logs", name, "--json", "--stream", "stdout")
		if result.Code == 0 && result.Err == nil {
			var output manifestOutputResponse
			if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &output); err == nil {
				for _, entry := range output.Entries {
					if entry.Text == want {
						return
					}
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s output %q", name, want)
}

func manifestWaitForEviction(t *testing.T, hum, cwd string, env []string, name string) manifestOutputResponse {
	t.Helper()
	deadline := time.Now().Add(manifestWorkflowTimeout)
	var last manifestOutputResponse
	for time.Now().Before(deadline) {
		result := testutil.Run(t, hum, cwd, env, "logs", name, "--json", "--stream", "stdout", "--after-cursor", "0", "--limit-bytes", "65536")
		if result.Code == 0 && result.Err == nil && result.Stderr == "" {
			var output manifestOutputResponse
			if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &output); err == nil {
				last = output
				if output.Truncated && output.EvictedThrough != nil {
					return output
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for retained output eviction: %#v", last)
	return last
}

func manifestList(t *testing.T, hum, cwd string, env []string) map[string]manifestProcess {
	t.Helper()
	result := testutil.Run(t, hum, cwd, env, "list", "--json")
	if result.Code != 0 || result.Err != nil || result.Stderr != "" {
		t.Fatalf("list --json: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
	}
	var response manifestListResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &response); err != nil {
		t.Fatalf("decode list --json: %v; output=%q", err, result.Stdout)
	}
	processes := make(map[string]manifestProcess, len(response.Processes))
	for _, process := range response.Processes {
		processes[process.Name] = process
	}
	return processes
}

func manifestDefinitionByName(definitions []manifestTestDefinition, name string) manifestTestDefinition {
	for _, definition := range definitions {
		if definition.Name == name {
			return definition
		}
	}
	return manifestTestDefinition{}
}
