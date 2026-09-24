//go:build windows

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"hum/internal/daemon"
	"hum/internal/testutil"
)

func TestWindowsDoctorReadinessNeverConnects(t *testing.T) {
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var connections atomic.Int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			_ = conn.Close()
		}
	}()
	address := listener.Addr().String()
	for _, tc := range []struct {
		name, readiness string
		valid           bool
	}{
		{"tcp", fmt.Sprintf("tcp: %q", address), true},
		{"http", "http: http://" + address + "/ready", true},
		{"invalid-tcp-port", fmt.Sprintf("tcp: %q", address+"junk"), false},
		{"invalid-http-fragment", "http: http://" + address + "/ready#bad", false},
		{"invalid-http-userinfo", "http: http://user@" + address + "/ready", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			manifest := fmt.Sprintf("version: 1\nprocesses:\n  api:\n    argv: [%q]\n    ready:\n      %s\n", fixture, tc.readiness)
			if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			result := testutil.Run(t, hum, root, testutil.RuntimeEnv(filepath.Join(testutil.RuntimeDir(t), "doctor")), "doctor", "--json")
			var report doctorResult
			if err := json.Unmarshal([]byte(result.Stdout), &report); err != nil {
				t.Fatalf("decode doctor %q: %v", result.Stdout, err)
			}
			if (result.Code == 0) != tc.valid || report.OK != tc.valid {
				t.Fatalf("readiness valid=%v: code=%d result=%+v stderr=%q", tc.valid, result.Code, report, result.Stderr)
			}
			if got := connections.Load(); got != 0 {
				t.Fatalf("doctor opened %d readiness connections", got)
			}
		})
	}
}

func TestWindowsDoctorPrivateManifestAndEnvironment(t *testing.T) {
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)
	root := t.TempDir()
	env := testutil.RuntimeEnv(filepath.Join(testutil.RuntimeDir(t), "doctor"))
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte("version: 1\nprocesses: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("version: 1\nenvironment:\n  inherit: false\n  files: [.env]\nprocesses:\n  api:\n    argv: [%q]\n    ready:\n      exec: [%q]\n", fixture, fixture)
	if err := os.WriteFile(filepath.Join(root, ".hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=private-doctor-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func() (doctorResult, testutil.Result) {
		t.Helper()
		result := testutil.Run(t, hum, root, env, "doctor", "--json")
		var report doctorResult
		if err := json.Unmarshal([]byte(result.Stdout), &report); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(result.Stdout+result.Stderr, "private-doctor-value") {
			t.Fatalf("doctor exposed private environment: %+v", result)
		}
		return report, result
	}
	report, result := run()
	if result.Code != 0 || !report.OK {
		t.Fatalf("private doctor: %+v, %+v", result, report)
	}
	for name, want := range map[string]string{
		"project.manifest": doctorPass, "environment.composition": doctorPass,
		"process.executable": doctorPass, "ready.executable": doctorPass,
	} {
		found := false
		for _, check := range report.Checks {
			if check.Name == name {
				found = true
				if check.Status != want {
					t.Fatalf("%s = %+v, want %s", name, check, want)
				}
				if name == "project.manifest" && (check.Details["manifest"] != ".hum.yaml" || check.Details["shadowed_manifest"] != "hum.yaml") {
					t.Fatalf("private manifest selection: %+v", check)
				}
			}
		}
		if !found {
			t.Fatalf("doctor omitted %s: %+v", name, report.Checks)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=private-malformed-value\nBROKEN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	failed, result := run()
	if result.Code == 0 || failed.OK || strings.Contains(result.Stdout+result.Stderr, "private-malformed-value") {
		t.Fatalf("invalid private environment: %+v, %+v", result, failed)
	}
}

func TestWindowsDoctorHumanStatusColors(t *testing.T) {
	report := newDoctorResult([]doctorCheck{
		{Name: "runtime.path", Status: doctorPass, Message: "path is usable"},
		{Name: "project.manifest", Status: doctorWarn, Message: "warning text"},
		{Name: "process.executable", Status: doctorFail, Message: "missing executable"},
		{Name: "daemon", Status: doctorInfo, Message: "pipe absent"},
	})
	var colored, plain bytes.Buffer
	if err := writeDoctorHumanWithPolicy(&colored, report, colorPolicy{enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := writeDoctorHumanWithPolicy(&plain, report, colorPolicy{}); err != nil {
		t.Fatal(err)
	}
	withoutColor := colored.String()
	for _, entry := range []struct {
		style ansiStyle
		label string
	}{{ansiGreen, doctorPass}, {ansiYellow, doctorWarn}, {ansiRed, doctorFail}, {ansiCyan, doctorInfo}} {
		want := string(entry.style) + entry.label + ansiReset
		if !strings.Contains(colored.String(), want) {
			t.Errorf("missing status color %q: %q", want, colored.String())
		}
		withoutColor = strings.ReplaceAll(withoutColor, string(entry.style), "")
	}
	if got := strings.ReplaceAll(withoutColor, ansiReset, ""); got != plain.String() {
		t.Fatalf("color changed doctor content: colored=%q plain=%q", got, plain.String())
	}
}

func TestWindowsDoctorRuntimeManifestAndTTY(t *testing.T) {
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)
	root := t.TempDir()
	runtimeDir := filepath.Join(testutil.RuntimeDir(t), "doctor")
	env := testutil.RuntimeEnv(runtimeDir)
	writeWindowsFixtureManifest(t, root, "api", []string{fixture, "inspect"})

	runDoctor := func() doctorResult {
		t.Helper()
		result := testutil.Run(t, hum, root, env, "doctor", "--json")
		if result.Code != 0 || result.Stderr != "" {
			t.Fatalf("doctor: code=%d stdout=%q stderr=%q runtime ACL=%v", result.Code, result.Stdout, result.Stderr, checkDoctorRuntimeACL(runtimeDir))
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
	if err := checkDoctorRuntimeACL(runtimeDir); err != nil {
		t.Fatalf("daemon-created directory fails doctor ACL before close: %v", err)
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
	if tty.Code != 0 {
		t.Fatalf("doctor rejected supported TTY declaration: code=%d stderr=%q stdout=%q", tty.Code, tty.Stderr, tty.Stdout)
	}
	var ttyReport doctorResult
	if err := json.Unmarshal([]byte(tty.Stdout), &ttyReport); err != nil {
		t.Fatal(err)
	}
	assertCheck(ttyReport, "project.manifest", doctorPass)
	assertCheck(ttyReport, "process.executable", doctorPass)
}
