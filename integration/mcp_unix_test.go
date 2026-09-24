//go:build darwin || linux

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
