//go:build darwin || linux

package integration

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hum/internal/testutil"
)

const downWorkflowTimeout = 15 * time.Second

type downWorkflowProcess struct {
	Name   string   `json:"name"`
	Source string   `json:"source"`
	Root   string   `json:"root"`
	PID    int      `json:"pid"`
	PGID   int      `json:"pgid"`
	State  string   `json:"state"`
	Argv   []string `json:"argv"`
}

type downWorkflowListResponse struct {
	Processes []downWorkflowProcess `json:"processes"`
}

type downWorkflowResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type downWorkflowStatus struct {
	Name        string `json:"name"`
	ProjectRoot string `json:"project_root"`
	PID         int    `json:"pid"`
	State       string `json:"state"`
}

type downWorkflowRun struct {
	Name string `json:"name"`
	PID  int    `json:"pid"`
}

func TestDownWorkflow(t *testing.T) {
	fixture := testutil.BuildFixture(t)
	hum := testutil.BuildHum(t)
	runtimeDir := testutil.RuntimeDir(t)
	firstRoot := stopitCanonicalTempDir(t)
	secondRoot := stopitCanonicalTempDir(t)
	env := testutil.RuntimeEnv(runtimeDir, "HUM_OUTPUT_BYTES=65536", "HUM_STOP_GRACE=1s")

	alphaMarker := filepath.Join(firstRoot, "alpha")
	zetaMarker := filepath.Join(firstRoot, "zeta")
	adHocMarker := filepath.Join(firstRoot, "ad-hoc")
	secondMarker := filepath.Join(secondRoot, "second")
	definitions := []manifestTestDefinition{
		{Name: "zeta", Argv: []string{fixture, "stream", zetaMarker}},
		{Name: "alpha", Argv: []string{fixture, "stream", alphaMarker}},
	}
	writeManifestTestYAML(t, firstRoot, definitions)

	daemon := testutil.Start(t, hum, firstRoot, env, "serve")
	groups := make([]int, 0, len(definitions)+2)
	t.Cleanup(func() {
		// Stop by name first while the daemon is normally available. The forced
		// shutdown then covers any process launched before a failed assertion or
		// any process whose name was not observed by a preceding list call.
		for _, item := range []struct {
			root  string
			names []string
		}{
			{root: firstRoot, names: []string{"alpha", "zeta", "ad-hoc"}},
			{root: secondRoot, names: []string{"second"}},
		} {
			for _, name := range item.names {
				_ = testutil.Run(t, hum, item.root, env, "stop", name)
			}
		}
		_ = testutil.Run(t, hum, firstRoot, env, "shutdown", "--stop-processes")
		stopitCleanupGroups(groups)
	})

	// A real client connection both proves the foreground daemon is ready and
	// creates the readiness artifact before any manifest launch occurs.
	initial := testutil.Run(t, hum, firstRoot, env, "list", "--json")
	if initial.Code != 0 || initial.Err != nil || initial.Stderr != "" {
		t.Fatalf("initial list readiness probe: code=%d err=%v stdout=%q stderr=%q", initial.Code, initial.Err, initial.Stdout, initial.Stderr)
	}

	up := testutil.Run(t, hum, firstRoot, env, "up", "--json")
	if up.Code != 0 || up.Err != nil || up.Stderr != "" {
		t.Fatalf("up --json: code=%d err=%v stdout=%q stderr=%q", up.Code, up.Err, up.Stdout, up.Stderr)
	}
	launches := manifestDecodeLaunchResults(t, up.Stdout)
	if got := manifestLaunchNames(launches); !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("up result names = %#v, want lexical [alpha zeta]", got)
	}
	for _, launch := range launches {
		if launch.Source != "manifest" || launch.Outcome == "error" || launch.PID == nil || *launch.PID <= 0 {
			t.Fatalf("up result = %#v, want successful manifest launch with positive PID", launch)
		}
	}
	testutil.WaitForFile(t, alphaMarker+".started", downWorkflowTimeout)
	testutil.WaitForFile(t, zetaMarker+".started", downWorkflowTimeout)

	firstBefore := downWorkflowList(t, hum, firstRoot, env, false)
	downWorkflowRememberGroups(firstBefore, &groups)
	if len(firstBefore) != len(definitions) {
		t.Fatalf("first project list after up = %#v, want %d manifest processes", firstBefore, len(definitions))
	}
	downWorkflowAssertRunning(t, firstBefore, firstRoot, map[string]string{
		"alpha": "manifest",
		"zeta":  "manifest",
	})

	adHoc := testutil.Run(t, hum, firstRoot, env, "run", "ad-hoc", "--detach", "--json", "--", fixture, "stream", adHocMarker)
	if adHoc.Code != 0 || adHoc.Err != nil || adHoc.Stderr != "" {
		t.Fatalf("ad-hoc detached run: code=%d err=%v stdout=%q stderr=%q", adHoc.Code, adHoc.Err, adHoc.Stdout, adHoc.Stderr)
	}
	var adHocRun downWorkflowRun
	if err := json.Unmarshal([]byte(strings.TrimSpace(adHoc.Stdout)), &adHocRun); err != nil {
		t.Fatalf("decode ad-hoc run JSON: %v; output=%q", err, adHoc.Stdout)
	}
	if adHocRun.Name != "ad-hoc" || adHocRun.PID <= 0 {
		t.Fatalf("ad-hoc run result = %#v, want name ad-hoc and positive PID", adHocRun)
	}
	testutil.WaitForFile(t, adHocMarker+".started", downWorkflowTimeout)
	firstWithAdHoc := downWorkflowList(t, hum, firstRoot, env, false)
	downWorkflowRememberGroups(firstWithAdHoc, &groups)
	if len(firstWithAdHoc) != len(definitions)+1 {
		t.Fatalf("first project list with ad-hoc = %#v, want three processes", firstWithAdHoc)
	}
	downWorkflowAssertRunning(t, firstWithAdHoc, firstRoot, map[string]string{
		"ad-hoc": "ad_hoc",
		"alpha":  "manifest",
		"zeta":   "manifest",
	})

	second := testutil.Run(t, hum, secondRoot, env, "run", "second", "--detach", "--json", "--", fixture, "stream", secondMarker)
	if second.Code != 0 || second.Err != nil || second.Stderr != "" {
		t.Fatalf("second project detached run: code=%d err=%v stdout=%q stderr=%q", second.Code, second.Err, second.Stdout, second.Stderr)
	}
	var secondRun downWorkflowRun
	if err := json.Unmarshal([]byte(strings.TrimSpace(second.Stdout)), &secondRun); err != nil {
		t.Fatalf("decode second project run JSON: %v; output=%q", err, second.Stdout)
	}
	if secondRun.Name != "second" || secondRun.PID <= 0 {
		t.Fatalf("second project run result = %#v, want name second and positive PID", secondRun)
	}
	testutil.WaitForFile(t, secondMarker+".started", downWorkflowTimeout)
	secondBefore := downWorkflowList(t, hum, secondRoot, env, false)
	downWorkflowRememberGroups(secondBefore, &groups)
	if len(secondBefore) != 1 || secondBefore[0].Name != "second" || secondBefore[0].Root != secondRoot || secondBefore[0].State != "running" {
		t.Fatalf("second project list before down = %#v, want one running second record", secondBefore)
	}
	if secondBefore[0].PID != secondRun.PID || !testutil.ProcessAlive(secondBefore[0].PID) {
		t.Fatalf("second project process before down = %#v, want live PID %d", secondBefore[0], secondRun.PID)
	}

	down := testutil.Run(t, hum, firstRoot, env, "down", "--json")
	if down.Code != 0 || down.Err != nil || down.Stderr != "" {
		t.Fatalf("down --json: code=%d err=%v stdout=%q stderr=%q", down.Code, down.Err, down.Stdout, down.Stderr)
	}
	results := downWorkflowDecodeResults(t, down.Stdout)
	wantNames := []string{"ad-hoc", "alpha", "zeta"}
	if got := downWorkflowResultNames(results); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("down result names = %#v, want deterministic lexical %#v", got, wantNames)
	}
	for _, result := range results {
		if result.Status != "stopped" {
			t.Errorf("down result %q status = %q, want stopped", result.Name, result.Status)
		}
		if result.Message != "" {
			t.Errorf("down result %q message = %q, want empty on success", result.Name, result.Message)
		}
	}

	firstAfter := downWorkflowList(t, hum, firstRoot, env, false)
	downWorkflowRememberGroups(firstAfter, &groups)
	downWorkflowAssertManifestStopped(t, firstAfter, firstRoot, []string{"alpha", "zeta"})
	for _, process := range firstBefore {
		testutil.WaitForProcessGone(t, process.PID, downWorkflowTimeout)
	}
	testutil.WaitForProcessGone(t, adHocRun.PID, downWorkflowTimeout)
	adHocStatus := downWorkflowStatusResult(t, hum, firstRoot, env, "ad-hoc")
	if adHocStatus.Name != "ad-hoc" || adHocStatus.ProjectRoot != firstRoot || adHocStatus.State != "exited" {
		t.Fatalf("ad-hoc status after down = %#v, want retained exited record in first project", adHocStatus)
	}
	if testutil.ProcessAlive(adHocStatus.PID) {
		t.Fatalf("ad-hoc status after down reports live PID %d", adHocStatus.PID)
	}

	allAfter := downWorkflowList(t, hum, firstRoot, env, true)
	downWorkflowRememberGroups(allAfter, &groups)
	downWorkflowAssertNoRunningRoot(t, allAfter, firstRoot)
	secondAfter := downWorkflowFind(allAfter, secondRoot, "second")
	if secondAfter.State != "running" || secondAfter.PID != secondRun.PID || !testutil.ProcessAlive(secondAfter.PID) {
		t.Fatalf("second project process after first down = %#v, want same live running process", secondAfter)
	}
	downWorkflowAssertDaemonAlive(t, daemon, runtimeDir)

	secondDown := testutil.Run(t, hum, firstRoot, env, "down", "--json")
	if secondDown.Code != 0 || secondDown.Err != nil || secondDown.Stderr != "" {
		t.Fatalf("second down --json: code=%d err=%v stdout=%q stderr=%q", secondDown.Code, secondDown.Err, secondDown.Stdout, secondDown.Stderr)
	}
	secondResults := downWorkflowDecodeResults(t, secondDown.Stdout)
	if got := downWorkflowResultNames(secondResults); !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("second down result names = %#v, want declared lexical [alpha zeta]", got)
	}
	for _, result := range secondResults {
		if result.Status != "not_running" || result.Message != "" {
			t.Errorf("second down result = %#v, want not_running without message", result)
		}
	}
	secondFinal := downWorkflowList(t, hum, secondRoot, env, false)
	if len(secondFinal) != 1 || secondFinal[0].Name != "second" || secondFinal[0].State != "running" || secondFinal[0].PID != secondRun.PID || !testutil.ProcessAlive(secondFinal[0].PID) {
		t.Fatalf("second project list after no-op down = %#v, want unchanged running process", secondFinal)
	}
	downWorkflowAssertDaemonAlive(t, daemon, runtimeDir)
}

func downWorkflowList(t *testing.T, hum, cwd string, env []string, all bool) []downWorkflowProcess {
	t.Helper()
	args := []string{"list"}
	if all {
		args = append(args, "--all")
	}
	args = append(args, "--json")
	result := testutil.Run(t, hum, cwd, env, args...)
	if result.Code != 0 || result.Err != nil || result.Stderr != "" {
		t.Fatalf("list %s: code=%d err=%v stdout=%q stderr=%q", strings.Join(args[1:], " "), result.Code, result.Err, result.Stdout, result.Stderr)
	}
	var response downWorkflowListResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &response); err != nil {
		t.Fatalf("decode list --json: %v; output=%q", err, result.Stdout)
	}
	return response.Processes
}

func downWorkflowRememberGroups(processes []downWorkflowProcess, groups *[]int) {
	for _, process := range processes {
		if process.PGID > 0 {
			*groups = append(*groups, process.PGID)
		}
	}
}

func downWorkflowAssertRunning(t *testing.T, processes []downWorkflowProcess, root string, want map[string]string) {
	t.Helper()
	if len(processes) != len(want) {
		t.Fatalf("running process count = %d, want %d: %#v", len(processes), len(want), processes)
	}
	for _, process := range processes {
		wantSource, ok := want[process.Name]
		if !ok {
			t.Errorf("unexpected process in project list: %#v", process)
			continue
		}
		if process.Root != root || process.Source != wantSource || process.State != "running" || process.PID <= 0 {
			t.Errorf("process %q = %#v, want root=%q source=%q running", process.Name, process, root, wantSource)
		}
		if !testutil.ProcessAlive(process.PID) {
			t.Errorf("process %q PID %d is not alive", process.Name, process.PID)
		}
	}
}

func downWorkflowAssertManifestStopped(t *testing.T, processes []downWorkflowProcess, root string, names []string) {
	t.Helper()
	seen := make(map[string]downWorkflowProcess, len(processes))
	for _, process := range processes {
		if process.Root != root {
			t.Fatalf("first-project list returned foreign process %#v", process)
		}
		if process.Name == "ad-hoc" {
			if process.State == "running" {
				t.Fatalf("ad-hoc process remained running in first-project list: %#v", process)
			}
			continue
		}
		seen[process.Name] = process
	}
	if len(seen) != len(names) {
		t.Fatalf("first-project list after down = %#v, want %d declared stopped records", processes, len(names))
	}
	for _, name := range names {
		process, ok := seen[name]
		if !ok {
			t.Fatalf("first-project list omitted declared process %q: %#v", name, processes)
		}
		if process.Source != "manifest" || process.State != "stopped" {
			t.Errorf("declared process %q after down = %#v, want manifest/stopped", name, process)
		}
	}
}

func downWorkflowAssertNoRunningRoot(t *testing.T, processes []downWorkflowProcess, root string) {
	t.Helper()
	for _, process := range processes {
		if process.Root == root && process.State == "running" {
			t.Errorf("process remained running in first project after down: %#v", process)
		}
	}
}

func downWorkflowFind(processes []downWorkflowProcess, root, name string) downWorkflowProcess {
	for _, process := range processes {
		if process.Root == root && process.Name == name {
			return process
		}
	}
	return downWorkflowProcess{Name: name, Root: root, State: "missing"}
}

func downWorkflowStatusResult(t *testing.T, hum, cwd string, env []string, name string) downWorkflowStatus {
	t.Helper()
	result := testutil.Run(t, hum, cwd, env, "status", name, "--json")
	if result.Code != 0 || result.Err != nil || result.Stderr != "" {
		t.Fatalf("status %q --json: code=%d err=%v stdout=%q stderr=%q", name, result.Code, result.Err, result.Stdout, result.Stderr)
	}
	var status downWorkflowStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &status); err != nil {
		t.Fatalf("decode status %q --json: %v; output=%q", name, err, result.Stdout)
	}
	return status
}

func downWorkflowDecodeResults(t *testing.T, output string) []downWorkflowResult {
	t.Helper()
	var results []downWorkflowResult
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var result downWorkflowResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatalf("decode down JSON line: %v; line=%q output=%q", err, line, output)
		}
		results = append(results, result)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan down JSON: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("down JSON contained no result objects: %q", output)
	}
	return results
}

func downWorkflowResultNames(results []downWorkflowResult) []string {
	names := make([]string, 0, len(results))
	for _, result := range results {
		names = append(names, result.Name)
	}
	return names
}

func downWorkflowAssertDaemonAlive(t *testing.T, daemon *testutil.Process, runtimeDir string) {
	t.Helper()
	if daemon == nil || daemon.Cmd == nil || daemon.Cmd.Process == nil || !testutil.ProcessAlive(daemon.Cmd.Process.Pid) {
		t.Fatalf("daemon is not alive after down")
	}
	for _, name := range []string{"hum.sock", "hum.pid", "hum.ready"} {
		path := filepath.Join(runtimeDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("runtime artifact %q after down: %v", path, err)
		}
	}
}
