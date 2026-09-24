package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"hum/internal/daemon"
	"hum/internal/output"
	"hum/internal/protocol"
	"hum/internal/testutil"
)

type doctorIntegrationResult struct {
	SchemaVersion int `json:"schema_version"`
	OK            bool
	Checks        []struct {
		Name    string `json:"name"`
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"checks"`
	Summary struct {
		Pass int `json:"pass"`
		Warn int `json:"warn"`
		Fail int `json:"fail"`
		Info int `json:"info"`
	} `json:"summary"`
}

func TestDoctorJSONContract(t *testing.T) {
	hum := integrationHum(t)
	projectRoot := doctorIntegrationProject(t)
	runtimeDir := filepath.Join(t.TempDir(), "absent-runtime")
	env := append(doctorIntegrationEnv(runtimeDir), "PRIVATE_DOCTOR_VALUE=must-not-leak")
	alternate := filepath.Join(projectRoot, "hum.dev.yaml")
	if err := os.WriteFile(alternate, []byte("version: 1\nprocesses:\n  selected:\n    argv: ["+doctorIntegrationExecutable(t)+"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := testutil.Run(t, hum, projectRoot, env, "doctor", "--file", alternate, "--json")
	if result.Code != 0 || result.Err != nil || result.Stderr != "" {
		t.Fatalf("doctor: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
	}
	if !strings.HasSuffix(result.Stdout, "\n") || strings.Count(strings.TrimSuffix(result.Stdout, "\n"), "\n") != 0 {
		t.Fatalf("doctor JSON framing = %q", result.Stdout)
	}
	var document doctorIntegrationResult
	if err := json.Unmarshal([]byte(result.Stdout), &document); err != nil {
		t.Fatal(err)
	}
	if document.SchemaVersion != 1 || !document.OK || document.Summary.Fail != 0 || len(document.Checks) == 0 {
		t.Fatalf("doctor JSON = %+v", document)
	}
	counts := map[string]int{"PASS": 0, "WARN": 0, "FAIL": 0, "INFO": 0}
	for _, check := range document.Checks {
		if check.Name == "" || check.Message == "" {
			t.Fatalf("doctor check omitted stable fields: %+v", check)
		}
		if _, ok := counts[check.Status]; !ok {
			t.Fatalf("doctor check status = %q", check.Status)
		}
		counts[check.Status]++
	}
	if counts["PASS"] != document.Summary.Pass || counts["WARN"] != document.Summary.Warn || counts["FAIL"] != document.Summary.Fail || counts["INFO"] != document.Summary.Info {
		t.Fatalf("doctor summary does not match checks: counts=%v summary=%+v", counts, document.Summary)
	}
	if strings.Contains(result.Stdout+result.Stderr, "must-not-leak") {
		t.Fatal("doctor exposed an environment value")
	}
}

func TestDoctorDoesNotStartDaemon(t *testing.T) {
	hum := integrationHum(t)
	projectRoot := doctorIntegrationProject(t)
	runtimeDir := filepath.Join(t.TempDir(), "absent-runtime")
	env := doctorIntegrationEnv(runtimeDir)
	result := testutil.Run(t, hum, projectRoot, env, "doctor")
	if result.Code != 0 || result.Err != nil || !strings.Contains(result.Stdout, "INFO daemon") {
		t.Fatalf("doctor: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
	}
	if _, err := os.Stat(runtimeDir); !os.IsNotExist(err) {
		t.Fatalf("doctor created runtime artifacts: %v", err)
	}
}

func TestDoctorExistingDaemon(t *testing.T) {
	lifecycleRequireUnix(t)
	hum := integrationHum(t)
	runtimeState := lifecycleNewRuntime(t)
	manifest := "version: 1\nprocesses:\n  check:\n    argv: [/bin/sh]\n"
	if err := os.WriteFile(filepath.Join(runtimeState.cwd, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	started := testutil.Run(t, hum, runtimeState.cwd, runtimeState.env, "serve", "--daemon")
	if started.Code != 0 || started.Err != nil {
		t.Fatalf("start daemon: code=%d err=%v stdout=%q stderr=%q", started.Code, started.Err, started.Stdout, started.Stderr)
	}
	daemonPID := lifecycleParseListeningPID(t, started.Stderr, runtimeState.paths.Socket)
	t.Cleanup(func() { lifecycleCleanupDaemon(t, hum, runtimeState, daemonPID) })

	result := testutil.Run(t, hum, runtimeState.cwd, runtimeState.env, "doctor", "--json")
	if result.Code != 0 || result.Err != nil {
		t.Fatalf("doctor: code=%d err=%v stdout=%q stderr=%q", result.Code, result.Err, result.Stdout, result.Stderr)
	}
	var document doctorIntegrationResult
	if err := json.Unmarshal([]byte(result.Stdout), &document); err != nil {
		t.Fatal(err)
	}
	compatible := false
	for _, check := range document.Checks {
		if check.Name == "daemon" && check.Status == "PASS" {
			compatible = true
		}
	}
	if !compatible {
		t.Fatalf("doctor did not report compatible daemon: %+v", document.Checks)
	}

	shutdown := testutil.Run(t, hum, runtimeState.cwd, runtimeState.env, "shutdown", "--stop-processes")
	if shutdown.Code != 0 || shutdown.Err != nil {
		t.Fatalf("shutdown daemon: code=%d err=%v", shutdown.Code, shutdown.Err)
	}
	lifecycleWaitPathGone(t, runtimeState.paths.Socket, lifecycleTimeout)
	daemonPID = 0
	address := &net.UnixAddr{Name: runtimeState.paths.Socket, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(runtimeState.paths.Socket) })
	unreachable := testutil.Run(t, hum, runtimeState.cwd, runtimeState.env, "doctor", "--json")
	if unreachable.Code != 1 || unreachable.Err == nil {
		t.Fatalf("unreachable daemon: code=%d err=%v stdout=%q stderr=%q", unreachable.Code, unreachable.Err, unreachable.Stdout, unreachable.Stderr)
	}
	var unreachableDocument doctorIntegrationResult
	if err := json.Unmarshal([]byte(unreachable.Stdout), &unreachableDocument); err != nil {
		t.Fatal(err)
	}
	unreachableFailed := false
	for _, check := range unreachableDocument.Checks {
		if check.Name == "daemon" && check.Status == "FAIL" {
			unreachableFailed = true
		}
	}
	if !unreachableFailed {
		t.Fatalf("doctor did not diagnose unreachable daemon: %+v", unreachableDocument.Checks)
	}
	if err := os.Remove(runtimeState.paths.Socket); err != nil {
		t.Fatal(err)
	}

	server, err := daemon.NewServer(daemon.Config{
		RuntimeDir: runtimeState.dir, WireVersion: protocol.Version + 1,
		CompletedLimit: 20, MaxLineBytes: 64 * 1024,
		OutputLimits: output.Limits{RetainedBytes: 64 * 1024, DefaultReadEntries: 100, DefaultReadBytes: 64 * 1024},
	})
	if err != nil {
		t.Fatal(err)
	}
	serverContext, cancelServer := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(serverContext) }()
	if err := server.WaitReady(serverContext); err != nil {
		cancelServer()
		_ = server.Close()
		t.Fatal(err)
	}
	incompatible := testutil.Run(t, hum, runtimeState.cwd, runtimeState.env, "doctor", "--json")
	cancelServer()
	_ = server.Close()
	<-serveDone
	if incompatible.Code != 1 || incompatible.Err == nil {
		t.Fatalf("incompatible daemon: code=%d err=%v stdout=%q stderr=%q", incompatible.Code, incompatible.Err, incompatible.Stdout, incompatible.Stderr)
	}
	var incompatibleDocument doctorIntegrationResult
	if err := json.Unmarshal([]byte(incompatible.Stdout), &incompatibleDocument); err != nil {
		t.Fatal(err)
	}
	for _, check := range incompatibleDocument.Checks {
		if check.Name == "daemon" && check.Status == "FAIL" && strings.Contains(check.Message, "incompatible") {
			return
		}
	}
	t.Fatalf("doctor did not diagnose incompatible daemon: %+v", incompatibleDocument.Checks)
}

func doctorIntegrationEnv(runtimeDir string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "HUM_RUNTIME_DIR=") {
			env = append(env, entry)
		}
	}
	return append(env, "HUM_RUNTIME_DIR="+runtimeDir)
}

func doctorIntegrationExecutable(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("%q", integrationFixture(t))
	}
	return "/bin/sh"
}

func doctorIntegrationProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	manifest := "version: 1\nprocesses:\n  check:\n    argv: [" + doctorIntegrationExecutable(t) + "]\n"
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}
