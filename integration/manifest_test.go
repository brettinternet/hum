//go:build darwin || linux

package integration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"hum/internal/testutil"
)

const manifestWorkflowTimeout = 15 * time.Second

type manifestTestReady struct {
	Match   string
	Timeout string
}

type manifestTestDefinition struct {
	Name  string
	Argv  []string
	Cwd   string
	Ready *manifestTestReady
}

// manifestLaunchResult is deliberately local to this integration test. Start
// and up use one JSON object per declared name, while optional process identity
// fields are useful when present but are not part of every outcome.
type manifestLaunchResult struct {
	Name         string          `json:"name"`
	Outcome      string          `json:"outcome"`
	Source       string          `json:"source"`
	Argv         []string        `json:"argv"`
	PID          *int            `json:"pid,omitempty"`
	LaunchCursor *uint64         `json:"launch_cursor,omitempty"`
	Readiness    string          `json:"readiness,omitempty"`
	ReadyCursor  *uint64         `json:"ready_cursor,omitempty"`
	Error        json.RawMessage `json:"error,omitempty"`
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
	ReadyCursor  *uint64  `json:"ready_cursor,omitempty"`
}

type manifestListResponse struct {
	Processes []manifestProcess `json:"processes"`
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

func TestManifestWorkflow(t *testing.T) {
	fixture := testutil.BuildFixture(t)
	hum := testutil.BuildHum(t)
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
			Name:  "alpha-ready",
			Argv:  []string{fixture, "burst", alphaGate, "8"},
			Ready: &manifestTestReady{Match: `stdout:0006`},
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
