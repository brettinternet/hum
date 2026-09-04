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
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)
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
	if listed.Error != nil || len(listed.Result.Tools) != 10 {
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
	listRaw, isErr := session.call(t, "list", explicit, nil)
	if isErr || !strings.Contains(string(listRaw), `"source":"ad_hoc"`) {
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
	downRaw, isErr := session.call(t, "down", explicit, nil)
	if isErr || !strings.Contains(string(downRaw), `"name":"api"`) || !strings.Contains(string(downRaw), `"name":"transient"`) || strings.Count(string(downRaw), `"state":"stopped"`) < 2 {
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
