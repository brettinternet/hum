package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hum/internal/testutil"
)

func TestMCPManifestEnvironmentLifecycle(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	runtime := lifecycleNewRuntime(t)
	runtime.env = append(runtime.env, "VALUE=baseline", "REMOVED=baseline")
	t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, 0) })

	root, err := filepath.EvalSymlinks(runtime.cwd)
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "print-environment.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s|%s' \"$VALUE\" \"${REMOVED-unset}\" > \"$1\"\nsleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	resultFile := filepath.Join(root, "mcp-environment.txt")
	manifest := "version: 1\nenvironment:\n  files: [.env]\nprocesses:\n  api:\n    argv: [" + yamlQuote(script) + ", " + yamlQuote(resultFile) + "]\n    env:\n      REMOVED: null\n"
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("VALUE=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session := newMCPTestSession(t, hum, root, runtime.env)
	started, isErr := session.call(t, "start", root, map[string]any{"name": "api", "no_wait": true})
	if isErr {
		t.Fatalf("start=%s", started)
	}
	if strings.Contains(string(started), `"env"`) || strings.Contains(string(started), "from-file") || strings.Contains(string(started), "baseline") {
		t.Fatalf("start exposed environment: %s", started)
	}
	manifestWaitForFileContents(t, resultFile, "from-file|unset")

	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("VALUE=reloaded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(resultFile); err != nil {
		t.Fatal(err)
	}
	restarted, isErr := session.call(t, "restart", root, map[string]any{"name": "api", "no_wait": true})
	if isErr {
		t.Fatalf("restart=%s", restarted)
	}
	if strings.Contains(string(restarted), `"env"`) || strings.Contains(string(restarted), "reloaded") || strings.Contains(string(restarted), "baseline") {
		t.Fatalf("restart exposed environment: %s", restarted)
	}
	manifestWaitForFileContents(t, resultFile, "reloaded|unset")

	if err := os.Remove(filepath.Join(root, ".env")); err != nil {
		t.Fatal(err)
	}
	status, isErr := session.call(t, "status", root, map[string]any{"name": "api"})
	if isErr || !strings.Contains(string(status), `"state":"running"`) {
		t.Fatalf("read-only status=%s error=%v", status, isErr)
	}
	failed, isErr := session.call(t, "start", root, map[string]any{"name": "api", "no_wait": true})
	if !isErr || !strings.Contains(string(failed), ".env") {
		t.Fatalf("missing required file start=%s error=%v", failed, isErr)
	}
}

type mcpTestSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr bytes.Buffer
	nextID int
}

type mcpTestResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  struct {
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
		Tools      []struct {
			Name string `json:"name"`
		} `json:"tools"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newMCPTestSession(t *testing.T, hum, cwd string, env []string) *mcpTestSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, hum, "mcp")
	cmd.Dir = cwd
	cmd.Env = append([]string(nil), env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	s := &mcpTestSession{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}
	cmd.Stderr = &s.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})
	resp := s.request(t, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "integration", "version": "1"}})
	if resp.Error != nil {
		t.Fatalf("initialize: %#v stderr=%q", resp.Error, s.stderr.String())
	}
	if _, err := fmt.Fprintln(stdin, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); err != nil {
		t.Fatal(err)
	}
	return s
}
func (s *mcpTestSession) request(t *testing.T, method string, params any) mcpTestResponse {
	t.Helper()
	s.nextID++
	request := map[string]any{"jsonrpc": "2.0", "id": s.nextID, "method": method, "params": params}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintln(s.stdin, string(data)); err != nil {
		t.Fatal(err)
	}
	line, err := s.stdout.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read %s: %v stderr=%q", method, err, s.stderr.String())
	}
	var response mcpTestResponse
	if err = json.Unmarshal(line, &response); err != nil {
		t.Fatalf("decode %s: %v line=%q", method, err, line)
	}
	return response
}
func (s *mcpTestSession) call(t *testing.T, name, root string, arguments map[string]any) (json.RawMessage, bool) {
	t.Helper()
	raw, isErr := s.callStructured(t, name, root, arguments)
	if isErr {
		return raw, true
	}
	key := ""
	switch name {
	case "up", "down":
		key = "results"
	case "list":
		key = "processes"
	}
	if key == "" {
		return raw, false
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode %s structured content: %v", name, err)
	}
	value, ok := envelope[key]
	if !ok {
		t.Fatalf("%s structured content = %s, missing %q", name, raw, key)
	}
	return value, false
}

func (s *mcpTestSession) callStructured(t *testing.T, name, root string, arguments map[string]any) (json.RawMessage, bool) {
	t.Helper()
	if arguments == nil {
		arguments = map[string]any{}
	}
	arguments["project_root"] = root
	resp := s.request(t, "tools/call", map[string]any{"name": name, "arguments": arguments})
	if resp.Error != nil {
		t.Fatalf("call %s rpc error: %#v", name, resp.Error)
	}
	return resp.Result.Structured, resp.Result.IsError
}

func TestMCPResolvedAndAdHocLifecycle(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	fixture := integrationFixture(t)
	runtime := lifecycleNewRuntime(t)
	t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtime, 0) })

	explicit, err := filepath.EvalSymlinks(runtime.cwd)
	if err != nil {
		t.Fatal(err)
	}
	explicitMarker := filepath.Join(t.TempDir(), "explicit")
	manifest := fmt.Sprintf("version: 1\nprocesses:\n  api:\n    argv: [%q, stream, %q]\n    ready:\n      match: %q\n      timeout: 5s\n", fixture, explicitMarker, "stdout:live")
	if err := os.WriteFile(filepath.Join(explicit, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	session := newMCPTestSession(t, hum, explicit, runtime.env)
	listed := session.request(t, "tools/list", map[string]any{})
	if listed.Error != nil || len(listed.Result.Tools) != 12 {
		t.Fatalf("tools/list=%#v", listed)
	}
	startRaw, isErr := session.call(t, "start", explicit, map[string]any{"name": "api"})
	if isErr {
		t.Fatalf("start error=%s", startRaw)
	}
	var started map[string]any
	if err := json.Unmarshal(startRaw, &started); err != nil {
		t.Fatal(err)
	}
	startedProcess, ok := started["process"].(map[string]any)
	if started["name"] != "api" || started["outcome"] != "started" || !ok || startedProcess["source"] != "manifest" {
		t.Fatalf("start=%s", startRaw)
	}
	if _, ok := startedProcess["env"]; ok {
		t.Fatalf("start exposed env: %s", startRaw)
	}
	testutil.WaitForFile(t, explicitMarker+".started", lifecycleTimeout)

	statusRaw, isErr := session.call(t, "status", explicit, map[string]any{"name": "api"})
	if isErr {
		t.Fatalf("status=%s", statusRaw)
	}
	cliStatus := testutil.Run(t, hum, explicit, runtime.env, "status", "api", "--json")
	if cliStatus.Code != 0 {
		t.Fatalf("cli status: %s", cliStatus.Stderr)
	}
	if !strings.Contains(string(statusRaw), `"state":"running"`) || !strings.Contains(cliStatus.Stdout, `"state":"running"`) {
		t.Fatalf("status mismatch mcp=%s cli=%s", statusRaw, cliStatus.Stdout)
	}
	logsRaw, isErr := session.call(t, "logs", explicit, map[string]any{"name": "api", "tail": 2, "max_entries": 2, "max_bytes": 4096})
	if isErr || !strings.Contains(string(logsRaw), "stdout:live") {
		t.Fatalf("logs=%s error=%v", logsRaw, isErr)
	}
	waitRaw, isErr := session.call(t, "wait", explicit, map[string]any{"name": "api", "after": 0, "match": "stdout:live", "timeout_ms": 3000})
	if isErr || !strings.Contains(string(waitRaw), `"outcome":"matched"`) {
		t.Fatalf("wait=%s error=%v", waitRaw, isErr)
	}
	restartRaw, isErr := session.call(t, "restart", explicit, map[string]any{"name": "api"})
	if isErr || !strings.Contains(string(restartRaw), `"restart_count":1`) {
		t.Fatalf("restart=%s error=%v", restartRaw, isErr)
	}

	zeroRoot := t.TempDir()
	zeroMarker := filepath.Join(t.TempDir(), "zero")
	if err := os.Mkdir(filepath.Join(zeroRoot, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nexec %q stream %q\n", fixture, zeroMarker)
	binDev := filepath.Join(zeroRoot, "bin", "dev")
	if err := os.WriteFile(binDev, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	zeroRaw, isErr := session.call(t, "start", zeroRoot, map[string]any{"name": "dev"})
	if isErr || !strings.Contains(string(zeroRaw), `"running_unverified"`) {
		t.Fatalf("zero-config start=%s error=%v", zeroRaw, isErr)
	}
	testutil.WaitForFile(t, zeroMarker+".started", lifecycleTimeout)

	adHocMarker := filepath.Join(t.TempDir(), "adhoc")
	run := testutil.Run(t, hum, explicit, runtime.env, "run", "transient", "--detach", "--", fixture, "stream", adHocMarker)
	if run.Code != 0 {
		t.Fatalf("ad hoc run: %s", run.Stderr)
	}
	testutil.WaitForFile(t, adHocMarker+".started", lifecycleTimeout)
	// The marker is written before the fixture output. Wait for the final
	// stderr fragment so sequential MCP and CLI status snapshots compare a
	// stable next_cursor rather than racing output ingestion.
	outputRaw, outputErr := session.call(t, "wait", explicit, map[string]any{"name": "transient", "after": 0, "match": "stderr:live", "timeout_ms": 3000})
	if outputErr || !strings.Contains(string(outputRaw), `"outcome":"matched"`) {
		t.Fatalf("wait for ad hoc fixture output=%s error=%v", outputRaw, outputErr)
	}
	listRaw, isErr := session.callStructured(t, "list", explicit, nil)
	if isErr {
		t.Fatalf("list=%s error=%v", listRaw, isErr)
	}
	var listEnvelope struct {
		Processes []json.RawMessage `json:"processes"`
	}
	if err := json.Unmarshal(listRaw, &listEnvelope); err != nil {
		t.Fatalf("decode list envelope %q: %v", listRaw, err)
	}
	if len(listEnvelope.Processes) == 0 || !strings.Contains(string(listRaw), `"source":"ad_hoc"`) {
		t.Fatalf("list=%s error=%v", listRaw, isErr)
	}
	adStatusRaw, isErr := session.call(t, "status", explicit, map[string]any{"name": "transient"})
	if isErr {
		t.Fatalf("ad hoc status=%s", adStatusRaw)
	}
	var adStatus map[string]any
	if err := json.Unmarshal(adStatusRaw, &adStatus); err != nil {
		t.Fatal(err)
	}
	cliAdStatus := testutil.Run(t, hum, explicit, runtime.env, "status", "transient", "--json")
	var cliAd map[string]any
	if cliAdStatus.Code != 0 || json.Unmarshal([]byte(cliAdStatus.Stdout), &cliAd) != nil {
		t.Fatalf("CLI ad hoc status: code=%d stdout=%q stderr=%q", cliAdStatus.Code, cliAdStatus.Stdout, cliAdStatus.Stderr)
	}
	for _, field := range []string{"name", "source", "cwd", "state", "next_cursor"} {
		if !reflect.DeepEqual(adStatus[field], cliAd[field]) {
			t.Fatalf("ad hoc status field %s: MCP=%#v CLI=%#v", field, adStatus[field], cliAd[field])
		}
	}
	adLogsRaw, isErr := session.call(t, "logs", explicit, map[string]any{"name": "transient", "tail": 1, "max_entries": 1, "max_bytes": 4096})
	var adLogs struct {
		Entries []json.RawMessage `json:"entries"`
		Next    *uint64           `json:"next"`
		Oldest  *uint64           `json:"oldest"`
		Latest  *uint64           `json:"latest"`
	}
	if isErr || json.Unmarshal(adLogsRaw, &adLogs) != nil || len(adLogs.Entries) > 1 || adLogs.Next == nil || adLogs.Oldest == nil || adLogs.Latest == nil {
		t.Fatalf("bounded ad hoc logs=%s error=%v", adLogsRaw, isErr)
	}
	cliAdLogs := testutil.Run(t, hum, explicit, runtime.env, "logs", "transient", "--json", "--tail", "1", "--limit-bytes", "4096")
	if cliAdLogs.Code != 0 || !strings.Contains(cliAdLogs.Stdout, `"next":`) {
		t.Fatalf("CLI bounded ad hoc logs: code=%d stdout=%q stderr=%q", cliAdLogs.Code, cliAdLogs.Stdout, cliAdLogs.Stderr)
	}
	adWaitRaw, isErr := session.call(t, "wait", explicit, map[string]any{"name": "transient", "after": 0, "match": "stdout:live", "timeout_ms": 3000})
	cliAdWait := testutil.Run(t, hum, explicit, runtime.env, "wait", "transient", "--json", "--after-cursor", "0", "--match", "stdout:live", "--timeout", "3s")
	if isErr || !strings.Contains(string(adWaitRaw), `"outcome":"matched"`) || cliAdWait.Code != 0 || !strings.Contains(cliAdWait.Stdout, `"outcome":"matched"`) {
		t.Fatalf("ad hoc wait mismatch MCP=%s error=%v CLI=%q/%q", adWaitRaw, isErr, cliAdWait.Stdout, cliAdWait.Stderr)
	}
	adRestart, isErr := session.call(t, "restart", explicit, map[string]any{"name": "transient"})
	var restarted map[string]any
	if isErr || json.Unmarshal(adRestart, &restarted) != nil {
		t.Fatalf("ad hoc restart=%s error=%v", adRestart, isErr)
	}
	wantArgv := []any{fixture, "stream", adHocMarker}
	if !reflect.DeepEqual(restarted["argv"], wantArgv) || restarted["cwd"] != explicit {
		t.Fatalf("ad hoc restart changed launch spec: %s", adRestart)
	}
	stopRaw, isErr := session.call(t, "stop", zeroRoot, map[string]any{"name": "dev"})
	if isErr || !strings.Contains(string(stopRaw), `"stopped"`) {
		t.Fatalf("stop=%s error=%v", stopRaw, isErr)
	}
	downRaw, isErr := session.callStructured(t, "down", explicit, nil)
	if isErr {
		t.Fatalf("down=%s error=%v", downRaw, isErr)
	}
	var downEnvelope struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(downRaw, &downEnvelope); err != nil {
		t.Fatalf("decode down envelope %q: %v", downRaw, err)
	}
	if len(downEnvelope.Results) < 2 || !strings.Contains(string(downRaw), `"name":"api"`) || !strings.Contains(string(downRaw), `"name":"transient"`) || strings.Count(string(downRaw), `"state":"stopped"`) < 2 {
		t.Fatalf("down=%s error=%v", downRaw, isErr)
	}

	shutdown := testutil.Run(t, hum, explicit, runtime.env, "shutdown", "--stop-processes")
	if shutdown.Code != 0 {
		t.Fatalf("shutdown: %s", shutdown.Stderr)
	}
	serve := testutil.Run(t, hum, explicit, runtime.env, "serve", "--daemon")
	if serve.Code != 0 {
		t.Fatalf("restart daemon: %s", serve.Stderr)
	}
	lostRaw, lostErr := session.call(t, "restart", explicit, map[string]any{"name": "transient"})
	if !lostErr || !strings.Contains(string(lostRaw), "not_found") {
		t.Fatalf("lost ad hoc restart=%s error=%v", lostRaw, lostErr)
	}
}

// upParitySummary is the subset HUM-035's shared-scheduler contract requires to
// match at both adapters. The projections nest process data differently, so
// each field is read from wherever its own adapter reports it.
type upParitySummary struct {
	Name           string
	Outcome        string
	Readiness      string
	ReadinessMatch string
	ChangedFields  []string
	BlockedBy      []string
	Guidance       string
}

// TestUpAdapterParityAcrossSurfaces drives one project state through both `hum
// up --json` and the MCP up tool and compares the shared fields. The
// per-adapter unit tests exercise each projection over different inputs and
// cannot observe a divergence between the two surfaces.
func TestUpAdapterParityAcrossSurfaces(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	projectRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir, "HUM_STOP_GRACE=1s")
	initial := `version: 1
processes:
  db:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: old}
  api:
    argv: [/bin/sh, -c, "sleep 30"]
    ready: {match: api-ready}
    after: [db]
`
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes")
	})
	started := testutil.Run(t, hum, projectRoot, env, "start", "--json", "--no-wait", "db")
	if started.Code != 0 {
		t.Fatalf("start db = code %d stdout=%q stderr=%q", started.Code, started.Stdout, started.Stderr)
	}
	// Drift db so up reports a drifted prerequisite and a dependent skip: one
	// call covers ordering, outcome, readiness, changed_fields, blocked_by, and
	// guidance without launching anything, so both surfaces observe the same
	// definitions and process snapshots.
	drifted := strings.Replace(initial, "match: old", "match: new", 1)
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(drifted), 0o600); err != nil {
		t.Fatal(err)
	}

	cliRun := testutil.Run(t, hum, projectRoot, env, "up", "--json")
	if cliRun.Code != 1 || cliRun.Stderr != "" {
		t.Fatalf("cli up = code %d stdout=%q stderr=%q", cliRun.Code, cliRun.Stdout, cliRun.Stderr)
	}
	cliObjects := make([]map[string]any, 0, 2)
	for _, line := range strings.Split(strings.TrimSpace(cliRun.Stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var object map[string]any
		if err := json.Unmarshal([]byte(line), &object); err != nil {
			t.Fatalf("decode cli up line %q: %v", line, err)
		}
		cliObjects = append(cliObjects, object)
	}

	session := newMCPTestSession(t, hum, projectRoot, env)
	raw, isErr := session.call(t, "up", projectRoot, nil)
	if isErr {
		t.Fatalf("mcp up error: %s", raw)
	}
	var mcpObjects []map[string]any
	if err := json.Unmarshal(raw, &mcpObjects); err != nil {
		t.Fatalf("decode mcp up results %s: %v", raw, err)
	}

	cli := upParitySummaries(t, cliObjects)
	adapter := upParitySummaries(t, mcpObjects)
	if len(cli) != 2 {
		t.Fatalf("cli up results = %#v, want db drift and api skip", cli)
	}
	if !reflect.DeepEqual(cli, adapter) {
		t.Fatalf("adapter parity mismatch:\n cli=%#v\n mcp=%#v", cli, adapter)
	}
	// Guard against the comparison passing on two empty projections.
	if cli[0].Name != "api" || cli[0].Outcome != "skipped" || !reflect.DeepEqual(cli[0].BlockedBy, []string{"db"}) {
		t.Fatalf("expected api skipped blocked by db, got %#v", cli[0])
	}
	if cli[1].Name != "db" || cli[1].Outcome != "definition_drift" || !reflect.DeepEqual(cli[1].ChangedFields, []string{"readiness_match"}) || cli[1].Guidance != "hum restart db" {
		t.Fatalf("expected db definition drift with restart guidance, got %#v", cli[1])
	}
}

func upParitySummaries(t *testing.T, objects []map[string]any) []upParitySummary {
	t.Helper()
	summaries := make([]upParitySummary, 0, len(objects))
	for _, object := range objects {
		readiness, match := upParityReadiness(object)
		summaries = append(summaries, upParitySummary{
			Name:           upParityString(object, "name"),
			Outcome:        upParityString(object, "outcome"),
			Readiness:      readiness,
			ReadinessMatch: match,
			ChangedFields:  upParityStrings(t, object, "changed_fields"),
			BlockedBy:      upParityStrings(t, object, "blocked_by"),
			Guidance:       upParityString(object, "guidance"),
		})
	}
	return summaries
}

// upParityReadiness reads readiness from the flat CLI shape or the nested MCP
// process snapshot, whichever the adapter used.
func upParityReadiness(object map[string]any) (string, string) {
	state, match := upParityString(object, "readiness"), upParityString(object, "readiness_match")
	process, ok := object["process"].(map[string]any)
	if !ok {
		return state, match
	}
	nested, ok := process["readiness"].(map[string]any)
	if !ok {
		return state, match
	}
	if state == "" {
		state = upParityString(nested, "state")
	}
	if match == "" {
		match = upParityString(nested, "match")
	}
	return state, match
}

func upParityString(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func upParityStrings(t *testing.T, object map[string]any, key string) []string {
	t.Helper()
	raw, ok := object[key]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("%s = %#v, want a string list", key, raw)
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("%s contains %#v, want a string", key, item)
		}
		values = append(values, text)
	}
	return values
}

func TestMCPAlternateManifest(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := testutil.RuntimeEnv(testutil.RuntimeDir(t), "HUM_STOP_GRACE=1s")
	t.Cleanup(func() { _ = testutil.Run(t, hum, root, env, "shutdown", "--stop-processes") })
	defaultManifest := "version: 1\nprocesses:\n  api:\n    argv: [/bin/sh, -c, 'sleep 30']\n"
	alternateManifest := "version: 1\nprocesses:\n  api:\n    argv: [/bin/sh, -c, 'sleep 30']\n  worker:\n    argv: [/bin/sh, -c, 'sleep 30']\n"
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(defaultManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hum.dev.yaml"), []byte(alternateManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	session := newMCPTestSession(t, hum, root, env)
	started, isErr := session.call(t, "start", root, map[string]any{"name": "api", "no_wait": true})
	if isErr || (!strings.Contains(string(started), `"outcome":"started"`) && !strings.Contains(string(started), `"outcome":"running_unverified"`)) {
		t.Fatalf("default start=%s error=%v", started, isErr)
	}
	listed, isErr := session.call(t, "list", root, map[string]any{"manifest": "hum.dev.yaml"})
	if isErr {
		t.Fatalf("alternate list=%s error=%v", listed, isErr)
	}
	var listedProcesses []map[string]any
	if err := json.Unmarshal(listed, &listedProcesses); err != nil {
		t.Fatalf("decode alternate list=%s: %v", listed, err)
	}
	if len(listedProcesses) != 2 {
		t.Fatalf("alternate list=%s, want retained api plus worker", listed)
	}
	for _, process := range listedProcesses {
		name, _ := process["name"].(string)
		source, _ := process["source"].(string)
		if name == "api" && source != "manifest" {
			t.Fatalf("retained api source=%q, want manifest: %s", source, listed)
		}
		if name == "worker" && source != "manifest:hum.dev.yaml" {
			t.Fatalf("selected worker source=%q, want manifest:hum.dev.yaml: %s", source, listed)
		}
	}
	invalid, isErr := session.callStructured(t, "status", root, map[string]any{"name": "api", "manifest": "hum.dev.yaml"})
	if !isErr || !strings.Contains(string(invalid), "invalid_request") {
		t.Fatalf("runtime manifest rejection=%s error=%v", invalid, isErr)
	}
	global := session.request(t, "tools/call", map[string]any{"name": "list", "arguments": map[string]any{"scope": "global", "manifest": "hum.dev.yaml"}})
	if !global.Result.IsError || !strings.Contains(string(global.Result.Structured), "invalid_request") {
		t.Fatalf("global manifest rejection=%#v", global)
	}
}
