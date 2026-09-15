package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"hum/internal/project"

	urfavecli "github.com/urfave/cli/v3"
	"golang.org/x/sys/unix"
)

func TestDoctorReadinessSyntax(t *testing.T) {
	for _, test := range []struct {
		name, ready string
		wantFail    bool
	}{
		{"http-ipv4", "http: http://127.0.0.1:0/readyz", false},
		{"http-localhost", "http: https://localhost:0/", false},
		{"tcp-ipv4", "tcp: \"127.0.0.1:0\"", false},
		{"tcp-ipv6", "tcp: \"[::1]:0\"", false},
		{"http-hostname", "http: http://example.invalid/", true},
		{"tcp-missing-port", "tcp: \"127.0.0.1\"", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ready := test.ready
			var listener net.Listener
			var connections atomic.Int32
			if !test.wantFail {
				network, address := "tcp", "127.0.0.1:0"
				if test.name == "tcp-ipv6" {
					network, address = "tcp6", "[::1]:0"
				}
				var err error
				listener, err = net.Listen(network, address)
				if err != nil {
					t.Fatal(err)
				}
				go func() {
					for {
						conn, err := listener.Accept()
						if err != nil {
							return
						}
						connections.Add(1)
						conn.Close()
					}
				}()
				port := listener.Addr().(*net.TCPAddr).Port
				if strings.HasPrefix(test.name, "http") {
					host, scheme := "127.0.0.1", "http"
					if test.name == "http-localhost" {
						host, scheme = "localhost", "https"
					}
					ready = fmt.Sprintf("http: %s://%s:%d/", scheme, host, port)
				} else if test.name == "tcp-ipv6" {
					ready = fmt.Sprintf("tcp: \"[::1]:%d\"", port)
				} else {
					ready = fmt.Sprintf("tcp: \"127.0.0.1:%d\"", port)
				}
			}
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte("version: 1\nprocesses:\n  app:\n    argv: [/bin/sh]\n    ready:\n      "+ready+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			result, _, _, runErr := runDoctorTest(t, context.Background(), root, "--json")
			if listener != nil {
				listener.Close()
				if connections.Load() != 0 {
					t.Fatalf("doctor opened %d readiness connections", connections.Load())
				}
			}
			if test.wantFail {
				if runErr == nil || result.OK {
					t.Fatalf("invalid readiness accepted: err=%v result=%+v", runErr, result)
				}
			} else if runErr != nil || !result.OK {
				t.Fatalf("valid readiness failed: err=%v result=%+v", runErr, result)
			}
		})
	}
}

func TestDoctorRejectedReadinessMakesNoConnections(t *testing.T) {
	for _, test := range []struct{ name, method, invalid string }{
		{"ipv4-fragment", "http", "fragment"}, {"ipv4-userinfo", "http", "userinfo"}, {"ipv4-port", "tcp", "port"},
		{"ipv6-fragment", "http", "v6-fragment"}, {"ipv6-userinfo", "http", "v6-userinfo"}, {"localhost-port", "http", "localhost-port"},
	} {
		t.Run(test.name, func(t *testing.T) {
			network, address := "tcp", "127.0.0.1:0"
			if strings.HasPrefix(test.name, "ipv6") {
				network, address = "tcp6", "[::1]:0"
			}
			listener, err := net.Listen(network, address)
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
					conn.Close()
				}
			}()
			port := listener.Addr().(*net.TCPAddr).Port
			host := "127.0.0.1"
			if strings.HasPrefix(test.name, "ipv6") {
				host = "[::1]"
			}
			ready := "tcp: \"" + host + ":" + strconv.Itoa(port) + "junk\""
			if test.method == "http" {
				ready = "http: http://" + host + ":" + strconv.Itoa(port) + "/"
				switch test.invalid {
				case "fragment", "v6-fragment":
					ready += "#bad"
				case "userinfo", "v6-userinfo":
					ready = "http://user@" + host + ":" + strconv.Itoa(port) + "/"
				case "localhost-port":
					ready = "http: http://localhost:" + strconv.Itoa(port) + ":"
				}
			}
			root := t.TempDir()
			manifest := "version: 1\nprocesses:\n  app:\n    argv: [/bin/sh]\n    ready:\n      " + ready + "\n"
			if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			result, _, _, runErr := runDoctorTest(t, context.Background(), root, "--json")
			listener.Close()
			if runErr == nil || result.OK || connections.Load() != 0 {
				t.Fatalf("rejected readiness: err=%v result=%+v connections=%d", runErr, result, connections.Load())
			}
		})
	}
}

func TestDoctorHumanColorsStatusLabels(t *testing.T) {
	result := newDoctorResult([]doctorCheck{
		{Name: "runtime.path", Status: doctorPass, Message: "path /private/runtime is usable"},
		{Name: "project.discovery", Status: doctorWarn, Message: "warning text"},
		{Name: "process.executable", Status: doctorFail, Message: "missing /private/bin"},
		{Name: "daemon", Status: doctorInfo, Message: "socket absent"},
	})

	var colored bytes.Buffer
	if err := writeDoctorHumanWithPolicy(&colored, result, colorPolicy{enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		ansiGreenString(doctorPass),
		ansiYellowString(doctorWarn),
		ansiRedString(doctorFail),
		ansiCyanString(doctorInfo),
	} {
		if !strings.Contains(colored.String(), want) {
			t.Errorf("colored doctor output missing %q: %q", want, colored.String())
		}
	}
	for _, value := range []string{"runtime.path", "/private/runtime", "project.discovery", "warning text", "/private/bin", "socket absent", "Summary:"} {
		if strings.Contains(colored.String(), string(ansiGreen)+value+ansiReset) ||
			strings.Contains(colored.String(), string(ansiYellow)+value+ansiReset) ||
			strings.Contains(colored.String(), string(ansiRed)+value+ansiReset) ||
			strings.Contains(colored.String(), string(ansiCyan)+value+ansiReset) {
			t.Errorf("doctor styled non-status value %q: %q", value, colored.String())
		}
	}

	var plain bytes.Buffer
	if err := writeDoctorHumanWithPolicy(&plain, result, colorPolicy{}); err != nil {
		t.Fatal(err)
	}
	if got := stripRenderANSI(colored.String()); got != plain.String() {
		t.Fatalf("color changed doctor output content:\ncolored=%q\nplain=%q", got, plain.String())
	}
}

func TestDoctorConfigurationAndRuntimeContract(t *testing.T) {
	projectRoot := t.TempDir()
	writeDoctorTestFile(t, filepath.Join(projectRoot, "hum.yaml"), "version: 1\nprocesses:\n  ok:\n    argv: [/bin/sh]\n")

	t.Run("defaults and human ordering", func(t *testing.T) {
		for _, name := range []string{"HUM_RUNTIME_DIR", "XDG_RUNTIME_DIR", "HUM_STOP_GRACE", "HUM_OUTPUT_BYTES", "HUM_COMPLETED_RECORDS"} {
			t.Setenv(name, "")
		}
		result, stdout, stderr, err := runDoctorTest(t, context.Background(), projectRoot)
		if err != nil || stderr != "" || !strings.HasPrefix(stdout, "PASS platform") || !strings.HasSuffix(stdout, " info\n") {
			t.Fatalf("default human doctor: err=%v result=%+v stdout=%q stderr=%q", err, result, stdout, stderr)
		}
		for _, name := range []string{"platform", "config.runtime_dir", "config.stop_grace", "config.output_bytes", "config.completed_records", "runtime.path", "project.discovery", "environment.composition", "process.executable", "daemon"} {
			if !strings.Contains(stdout, name) {
				t.Errorf("human output missing ordered check %q: %q", name, stdout)
			}
		}
	})

	t.Run("XDG runtime fallback", func(t *testing.T) {
		t.Setenv("HUM_RUNTIME_DIR", "")
		xdg := t.TempDir()
		t.Setenv("XDG_RUNTIME_DIR", xdg)
		result, _, _, err := runDoctorTest(t, context.Background(), projectRoot, "--json")
		if err != nil || !result.OK {
			t.Fatalf("XDG fallback: err=%v result=%+v", err, result)
		}
		for _, check := range result.Checks {
			if check.Name == "config.runtime_dir" && check.Details["path"] == filepath.Join(xdg, "hum") {
				return
			}
		}
		t.Fatalf("XDG runtime path missing from checks: %+v", result.Checks)
	})

	t.Run("effective settings and absent runtime", func(t *testing.T) {
		runtimeDir := filepath.Join(t.TempDir(), "absent", "hum")
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "ignored"))
		t.Setenv("HUM_STOP_GRACE", "2s")
		t.Setenv("HUM_OUTPUT_BYTES", "65536")
		t.Setenv("HUM_COMPLETED_RECORDS", "7")
		result, stdout, stderr, err := runDoctorTest(t, context.Background(), projectRoot, "--json")
		if err != nil || !result.OK || stderr != "" {
			t.Fatalf("doctor: err=%v result=%+v stdout=%q stderr=%q", err, result, stdout, stderr)
		}
		if _, err := os.Stat(runtimeDir); !os.IsNotExist(err) {
			t.Fatalf("doctor created absent runtime path: %v", err)
		}
		assertDoctorCheck(t, result, "runtime.path", doctorInfo)
		assertDoctorCheck(t, result, "daemon", doctorInfo)
		wantOrder := []string{"platform", "config.runtime_dir", "config.stop_grace", "config.output_bytes", "config.completed_records", "runtime.path", "project.discovery", "environment.composition", "process.executable", "daemon"}
		if len(result.Checks) != len(wantOrder) {
			t.Fatalf("check count = %d, want %d: %+v", len(result.Checks), len(wantOrder), result.Checks)
		}
		for index, want := range wantOrder {
			if result.Checks[index].Name != want {
				t.Fatalf("check %d = %q, want %q", index, result.Checks[index].Name, want)
			}
		}
	})

	t.Run("flags override malformed environment", func(t *testing.T) {
		t.Setenv("HUM_STOP_GRACE", "private-invalid-stop")
		t.Setenv("HUM_OUTPUT_BYTES", "private-invalid-output")
		t.Setenv("HUM_COMPLETED_RECORDS", "private-invalid-records")
		runtimeDir := filepath.Join(t.TempDir(), "flag-runtime")
		result, stdout, stderr, err := runDoctorTest(t, context.Background(), projectRoot, "--json", "--runtime-dir", runtimeDir, "--stop-grace", "3s", "--output-bytes", "65536", "--completed-records", "9")
		if err != nil || !result.OK || strings.Contains(stdout+stderr, "private-invalid") {
			t.Fatalf("flag precedence: err=%v result=%+v stdout=%q stderr=%q", err, result, stdout, stderr)
		}
	})

	t.Run("existing runtime probe cleanup", func(t *testing.T) {
		runtimeDir := t.TempDir()
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		before, err := os.ReadDir(runtimeDir)
		if err != nil {
			t.Fatal(err)
		}
		result, _, _, runErr := runDoctorTest(t, context.Background(), projectRoot, "--json")
		if runErr != nil || !result.OK {
			t.Fatalf("doctor: err=%v result=%+v", runErr, result)
		}
		after, err := os.ReadDir(runtimeDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Fatalf("runtime probe left artifacts: before=%v after=%v", before, after)
		}
		assertDoctorCheck(t, result, "runtime.path", doctorPass)
	})

	t.Run("unusable runtime path fails", func(t *testing.T) {
		runtimePath := filepath.Join(t.TempDir(), "runtime-file")
		writeDoctorTestFile(t, runtimePath, "not a directory")
		t.Setenv("HUM_RUNTIME_DIR", runtimePath)
		result, _, _, err := runDoctorTest(t, context.Background(), projectRoot, "--json")
		var exit urfavecli.ExitCoder
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || result.OK {
			t.Fatalf("unusable runtime: err=%v result=%+v", err, result)
		}
		assertDoctorCheck(t, result, "runtime.path", doctorFail)
	})

	t.Run("insecure runtime mode fails", func(t *testing.T) {
		runtimePath := t.TempDir()
		if err := os.Chmod(runtimePath, 0o777); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HUM_RUNTIME_DIR", runtimePath)
		result, _, _, err := runDoctorTest(t, context.Background(), projectRoot, "--json")
		if err == nil || result.OK {
			t.Fatalf("insecure runtime mode passed: err=%v result=%+v", err, result)
		}
		assertDoctorCheck(t, result, "runtime.path", doctorFail)
	})

	t.Run("runtime symlink fails", func(t *testing.T) {
		parent := t.TempDir()
		target := filepath.Join(parent, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(parent, "runtime-link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HUM_RUNTIME_DIR", link)
		result, _, _, err := runDoctorTest(t, context.Background(), projectRoot, "--json")
		if err == nil || result.OK {
			t.Fatalf("runtime symlink passed: err=%v result=%+v", err, result)
		}
		assertDoctorCheck(t, result, "runtime.path", doctorFail)
	})

	t.Run("dangling runtime ancestor fails", func(t *testing.T) {
		parent := t.TempDir()
		dangling := filepath.Join(parent, "dangling")
		if err := os.Symlink(filepath.Join(parent, "missing-target"), dangling); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HUM_RUNTIME_DIR", filepath.Join(dangling, "hum"))
		result, _, _, err := runDoctorTest(t, context.Background(), projectRoot, "--json")
		var exit urfavecli.ExitCoder
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || result.OK {
			t.Fatalf("dangling runtime: err=%v result=%+v", err, result)
		}
		assertDoctorCheck(t, result, "runtime.path", doctorFail)
	})

	t.Run("warning does not fail", func(t *testing.T) {
		t.Setenv("HUM_STOP_GRACE", "0s")
		result, _, _, err := runDoctorTest(t, context.Background(), projectRoot, "--json")
		if err != nil || !result.OK || result.Summary.Warn != 1 {
			t.Fatalf("doctor warning: err=%v result=%+v", err, result)
		}
		assertDoctorCheck(t, result, "config.stop_grace", doctorWarn)
	})

	t.Run("malformed settings are aggregated and private", func(t *testing.T) {
		secret := "NOT-A-DURATION-SECRET"
		t.Setenv("HUM_STOP_GRACE", secret)
		t.Setenv("HUM_OUTPUT_BYTES", "private-output-value")
		result, stdout, stderr, err := runDoctorTest(t, context.Background(), projectRoot, "--json")
		var exit urfavecli.ExitCoder
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || result.OK || result.Summary.Fail < 2 {
			t.Fatalf("doctor failure: err=%v result=%+v", err, result)
		}
		if strings.Contains(stdout+stderr, secret) || strings.Contains(stdout+stderr, "private-output-value") {
			t.Fatalf("doctor exposed configuration value: stdout=%q stderr=%q", stdout, stderr)
		}
	})
}

func TestDoctorProjectEnvironmentAndExecutableContract(t *testing.T) {
	projectRoot := t.TempDir()
	bin := filepath.Join(projectRoot, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoctorExecutable(t, filepath.Join(bin, "app"))
	writeDoctorExecutable(t, filepath.Join(projectRoot, "ready"))
	writeDoctorTestFile(t, filepath.Join(projectRoot, ".env"), "PATH="+bin+"\nTOKEN=private-value\n")
	writeDoctorTestFile(t, filepath.Join(projectRoot, "hum.yaml"), "version: 1\nenvironment:\n  inherit: false\n  files: [.env]\nprocesses:\n  app:\n    argv: [app]\n    ready:\n      exec: [./ready]\n")

	result, stdout, stderr, err := runDoctorTest(t, context.Background(), projectRoot, "--json")
	if err != nil || !result.OK || stderr != "" {
		t.Fatalf("doctor: err=%v result=%+v stdout=%q stderr=%q", err, result, stdout, stderr)
	}
	assertDoctorCheck(t, result, "environment.composition", doctorPass)
	assertDoctorCheck(t, result, "process.executable", doctorPass)
	assertDoctorCheck(t, result, "ready.executable", doctorPass)
	if strings.Contains(stdout+stderr, "private-value") || strings.Contains(stdout+stderr, "TOKEN") {
		t.Fatalf("doctor exposed composed environment: stdout=%q stderr=%q", stdout, stderr)
	}

	fifoManifestRoot := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(fifoManifestRoot, "hum.yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	fifoManifest, _, _, fifoManifestErr := runDoctorTest(t, context.Background(), fifoManifestRoot, "--json")
	if fifoManifestErr == nil || fifoManifest.OK {
		t.Fatalf("FIFO manifest passed or blocked unexpectedly: err=%v result=%+v", fifoManifestErr, fifoManifest)
	}
	assertDoctorCheck(t, fifoManifest, "project.discovery", doctorFail)

	emptyManifestRoot := t.TempDir()
	writeDoctorTestFile(t, filepath.Join(emptyManifestRoot, "hum.yaml"), "version: 1\nprocesses: {}\n")
	emptyManifest, _, _, emptyManifestErr := runDoctorTest(t, context.Background(), emptyManifestRoot, "--json")
	if emptyManifestErr != nil || !emptyManifest.OK {
		t.Fatalf("valid empty manifest failed: err=%v result=%+v", emptyManifestErr, emptyManifest)
	}
	assertDoctorCheck(t, emptyManifest, "project.discovery", doctorPass)
	assertDoctorCheck(t, emptyManifest, "environment.composition", doctorPass)

	conventionalRoot := t.TempDir()
	conventionalBin := filepath.Join(conventionalRoot, "bin")
	if err := os.Mkdir(conventionalBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoctorExecutable(t, filepath.Join(conventionalBin, "npm"))
	writeDoctorTestFile(t, filepath.Join(conventionalRoot, "package.json"), `{"scripts":{"dev":"do-not-run"}}`)
	t.Setenv("PATH", conventionalBin)
	conventional, _, _, conventionalErr := runDoctorTest(t, context.Background(), conventionalRoot, "--json")
	if conventionalErr != nil || !conventional.OK {
		t.Fatalf("read-only conventional discovery: err=%v result=%+v", conventionalErr, conventional)
	}
	assertDoctorCheck(t, conventional, "project.discovery", doctorPass)

	emptyRoot := t.TempDir()
	marker := filepath.Join(t.TempDir(), "executed")
	for _, name := range []string{"mise", "task", "just"} {
		path := filepath.Join(conventionalBin, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho ran > "+marker+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	_, _, _, _ = runDoctorTest(t, context.Background(), emptyRoot, "--json")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("doctor executed conventional discovery command: %v", err)
	}

	privateJustRoot := t.TempDir()
	writeDoctorTestFile(t, filepath.Join(privateJustRoot, "justfile"), "[private] # hidden\n[no-cd]\ndev:\n  echo hidden\n")
	privateJust, _, _, privateJustErr := runDoctorTest(t, context.Background(), privateJustRoot, "--json")
	if privateJustErr == nil || privateJust.OK {
		t.Fatalf("private just recipe was discovered: err=%v result=%+v", privateJustErr, privateJust)
	}
	assertDoctorCheck(t, privateJust, "project.discovery", doctorFail)

	publicJustRoot := t.TempDir()
	writeDoctorTestFile(t, filepath.Join(publicJustRoot, "justfile"), "dev port='3000':\n  echo public\n")
	publicJust, _, _, publicJustErr := runDoctorTest(t, context.Background(), publicJustRoot, "--json")
	if publicJustErr != nil || !publicJust.OK {
		t.Fatalf("parameterized public Just recipe was not discovered: err=%v result=%+v", publicJustErr, publicJust)
	}

	priorityTaskRoot := t.TempDir()
	writeDoctorTestFile(t, filepath.Join(priorityTaskRoot, "Taskfile.yml"), "version: '3'\ntasks:\n  dev:\n    cmds: [echo safe]\n")
	if err := os.WriteFile(filepath.Join(priorityTaskRoot, "Taskfile.yaml"), bytes.Repeat([]byte{'x'}, (1<<20)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	priorityTask, _, _, priorityTaskErr := runDoctorTest(t, context.Background(), priorityTaskRoot, "--json")
	if priorityTaskErr != nil || !priorityTask.OK {
		t.Fatalf("ignored lower-priority Taskfile affected discovery: err=%v result=%+v", priorityTaskErr, priorityTask)
	}

	for name, declaration := range map[string]string{
		"dotted": `tasks."dev" = "echo dev"`,
		"inline": `tasks = { "dev" = "echo dev" }`,
	} {
		t.Run("mise "+name, func(t *testing.T) {
			root := t.TempDir()
			writeDoctorTestFile(t, filepath.Join(root, "mise.toml"), declaration+"\n")
			result, _, _, runErr := runDoctorTest(t, context.Background(), root, "--json")
			if runErr != nil || !result.OK {
				t.Fatalf("Mise %s declaration was not discovered: err=%v result=%+v", name, runErr, result)
			}
		})
	}

	priorityJustRoot := t.TempDir()
	writeDoctorTestFile(t, filepath.Join(priorityJustRoot, "justfile"), "other:\n  echo other\n")
	writeDoctorTestFile(t, filepath.Join(priorityJustRoot, "Justfile"), "dev:\n  echo hidden-by-priority\n")
	lowerInfo, lowerErr := os.Stat(filepath.Join(priorityJustRoot, "justfile"))
	upperInfo, upperErr := os.Stat(filepath.Join(priorityJustRoot, "Justfile"))
	if lowerErr != nil || upperErr != nil {
		t.Fatal(errors.Join(lowerErr, upperErr))
	}
	if !os.SameFile(lowerInfo, upperInfo) {
		priorityJust, _, _, priorityJustErr := runDoctorTest(t, context.Background(), priorityJustRoot, "--json")
		if priorityJustErr == nil || priorityJust.OK {
			t.Fatalf("lower-priority Justfile was discovered: err=%v result=%+v", priorityJustErr, priorityJust)
		}
		assertDoctorCheck(t, priorityJust, "project.discovery", doctorFail)
	}

	nonRegularRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(nonRegularRoot, "justfile"), 0o700); err != nil {
		t.Fatal(err)
	}
	nonRegular, _, _, nonRegularErr := runDoctorTest(t, context.Background(), nonRegularRoot, "--json")
	if nonRegularErr == nil || nonRegular.OK {
		t.Fatalf("non-regular Just declaration passed: err=%v result=%+v", nonRegularErr, nonRegular)
	}
	assertDoctorCheck(t, nonRegular, "project.discovery", doctorFail)

	largeDeclarationRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(largeDeclarationRoot, "justfile"), bytes.Repeat([]byte{'x'}, (1<<20)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	largeDeclaration, _, _, largeDeclarationErr := runDoctorTest(t, context.Background(), largeDeclarationRoot, "--json")
	if largeDeclarationErr == nil || largeDeclaration.OK {
		t.Fatalf("oversized conventional declaration passed: err=%v result=%+v", largeDeclarationErr, largeDeclaration)
	}
	assertDoctorCheck(t, largeDeclaration, "project.discovery", doctorFail)

	writeDoctorTestFile(t, filepath.Join(projectRoot, "hum.yaml"), "version: 1\nprocesses:\n  one:\n    argv: [missing-one]\n  two:\n    argv: [missing-two]\n    ready:\n      exec: [missing-ready]\n")
	failed, _, _, err := runDoctorTest(t, context.Background(), projectRoot, "--json")
	var exit urfavecli.ExitCoder
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || failed.Summary.Fail != 3 {
		t.Fatalf("multiple executable failures: err=%v result=%+v", err, failed)
	}

	strictCases := []struct {
		name       string
		manifest   string
		envFile    string
		check      string
		privateRaw string
	}{
		{name: "dependency", manifest: "version: 1\nprocesses:\n  app:\n    argv: [/bin/sh]\n    after: [missing]\n", check: "project.discovery"},
		{name: "cwd", manifest: "version: 1\nprocesses:\n  app:\n    argv: [/bin/sh]\n    cwd: missing\n", check: "project.discovery"},
		{name: "readiness", manifest: "version: 1\nprocesses:\n  app:\n    argv: [/bin/sh]\n    ready:\n      match: ready\n      exec: [/bin/sh]\n", check: "project.discovery"},
		{name: "missing environment", manifest: "version: 1\nenvironment:\n  files: [missing.env]\nprocesses:\n  app:\n    argv: [/bin/sh]\n", check: "environment.composition"},
		{name: "malformed private environment", manifest: "version: 1\nenvironment:\n  files: [.env]\nprocesses:\n  app:\n    argv: [/bin/sh]\n", envFile: "TOKEN=private-malformed-value\nBROKEN\n", check: "environment.composition", privateRaw: "private-malformed-value"},
	}
	for _, test := range strictCases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeDoctorTestFile(t, filepath.Join(root, "hum.yaml"), test.manifest)
			if test.envFile != "" {
				writeDoctorTestFile(t, filepath.Join(root, ".env"), test.envFile)
			}
			result, stdout, stderr, runErr := runDoctorTest(t, context.Background(), root, "--json")
			if runErr == nil || result.OK {
				t.Fatalf("strict invalid input passed: err=%v result=%+v", runErr, result)
			}
			assertDoctorCheck(t, result, test.check, doctorFail)
			if test.privateRaw != "" && strings.Contains(stdout+stderr, test.privateRaw) {
				t.Fatalf("strict diagnostic exposed environment value: %q %q", stdout, stderr)
			}
		})
	}

	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	canceled, _, _, cancelErr := runDoctorTest(t, canceledContext, projectRoot, "--json")
	if cancelErr == nil || canceled.OK {
		t.Fatalf("canceled doctor: err=%v result=%+v", cancelErr, canceled)
	}
	assertDoctorCheck(t, canceled, "project.discovery", doctorFail)

	oversized := strings.Repeat("x", 9<<20)
	if doctorEnvironmentsFitProtocol(manifestState{root: projectRoot, defs: []project.Definition{{Name: "large", Source: "manifest", Cwd: projectRoot, Argv: []string{"/bin/sh", oversized}}}, baseline: os.Environ()}) {
		t.Fatal("oversized process request fit protocol")
	}
}

func runDoctorTest(t *testing.T, ctx context.Context, projectRoot string, extra ...string) (doctorResult, string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	args := []string{"hum", "--project", projectRoot, "doctor"}
	args = append(args, extra...)
	err := root.Run(ctx, args)
	var result doctorResult
	if strings.Contains(strings.Join(extra, " "), "--json") {
		if decodeErr := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); decodeErr != nil {
			t.Fatalf("decode doctor JSON: %v; stdout=%q", decodeErr, stdout.String())
		}
	}
	return result, stdout.String(), stderr.String(), err
}

func assertDoctorCheck(t *testing.T, result doctorResult, name, status string) {
	t.Helper()
	for _, check := range result.Checks {
		if check.Name == name && check.Status == status {
			return
		}
	}
	t.Fatalf("missing %s %s check in %+v", status, name, result.Checks)
}

func writeDoctorTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeDoctorExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}
