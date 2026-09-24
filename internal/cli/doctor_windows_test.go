//go:build windows

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"hum/internal/daemon"
	"hum/internal/testutil"
)

func TestWindowsDoctorRuntimeManifestAndUnsupportedTTY(t *testing.T) {
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)
	root := t.TempDir()
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir)
	writeWindowsFixtureManifest(t, root, "api", []string{fixture, "inspect"})

	runDoctor := func() doctorResult {
		t.Helper()
		result := testutil.Run(t, hum, root, env, "doctor", "--json")
		if result.Code != 0 || result.Stderr != "" {
			t.Fatalf("doctor: code=%d stdout=%q stderr=%q", result.Code, result.Stdout, result.Stderr)
		}
		var report doctorResult
		if err := json.Unmarshal([]byte(result.Stdout), &report); err != nil {
			t.Fatal(err)
		}
		return report
	}
	assertCheck := func(report doctorResult, name, status string) {
		t.Helper()
		for _, check := range report.Checks {
			if check.Name == name {
				if check.Status != status {
					t.Fatalf("doctor %s = %s: %s, want %s", name, check.Status, check.Message, status)
				}
				return
			}
		}
		t.Fatalf("doctor omitted %s: %+v", name, report.Checks)
	}

	report := runDoctor()
	assertCheck(report, "platform", doctorPass)
	assertCheck(report, "runtime.path", doctorInfo)
	assertCheck(report, "project.manifest", doctorPass)
	assertCheck(report, "process.executable", doctorPass)
	assertCheck(report, "daemon", doctorInfo)
	if _, err := os.Stat(daemon.NewRuntimePaths(runtimeDir).PID); !os.IsNotExist(err) {
		t.Fatalf("doctor started a daemon: %v", err)
	}

	server, err := daemon.NewServer(daemon.Config{RuntimeDir: runtimeDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	assertCheck(runDoctor(), "runtime.path", doctorPass)

	insecureDir := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(insecureDir, 0o700); err != nil {
		t.Fatal(err)
	}
	bad := testutil.Run(t, hum, root, testutil.RuntimeEnv(insecureDir), "doctor", "--json")
	if bad.Code == 0 {
		t.Fatalf("doctor accepted inherited ACL: %q", bad.Stdout)
	}
	var badReport doctorResult
	if err := json.Unmarshal([]byte(bad.Stdout), &badReport); err != nil {
		t.Fatal(err)
	}
	assertCheck(badReport, "runtime.path", doctorFail)

	argv, err := json.Marshal([]string{fixture, "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("version: 1\nprocesses:\n  api:\n    argv: %s\n    tty: true\n", argv)
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	tty := testutil.Run(t, hum, root, env, "doctor", "--json")
	if tty.Code == 0 {
		t.Fatalf("doctor accepted TTY declaration: %q", tty.Stdout)
	}
	var ttyReport doctorResult
	if err := json.Unmarshal([]byte(tty.Stdout), &ttyReport); err != nil {
		t.Fatal(err)
	}
	assertCheck(ttyReport, "process.tty", doctorFail)
}
