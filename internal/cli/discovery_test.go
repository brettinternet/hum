package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hum/internal/project"
)

func writeDiscoveredBin(t *testing.T, root string) {
	t.Helper()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatalf("create bin directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bin, "dev"), []byte("#!/bin/sh\nsleep 30\n"), 0o700); err != nil {
		t.Fatalf("write bin/dev: %v", err)
	}
}

func assertDiscoveryDefinition(t *testing.T, definition manifestLaunchResult, outcome string) {
	t.Helper()
	if definition.Name != "dev" {
		t.Fatalf("definition name = %q, want dev", definition.Name)
	}
	if definition.Source != "bin_dev" {
		t.Fatalf("definition source = %q, want bin_dev", definition.Source)
	}
	if !reflect.DeepEqual(definition.Argv, []string{"./bin/dev"}) {
		t.Fatalf("definition argv = %#v, want [./bin/dev]", definition.Argv)
	}
	if definition.Outcome != outcome {
		t.Fatalf("definition outcome = %q, want %q", definition.Outcome, outcome)
	}
}

func assertRuntimeDirEmpty(t *testing.T, runtimeDir string) {
	t.Helper()
	entries, err := os.ReadDir(runtimeDir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("read runtime directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("runtime directory mutated: %#v", entries)
	}
}

func TestDiscoveredUp(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeDiscoveredBin(t, root)

	stdout, stderr, err := stopShutdownRun(t, "up", "--json")
	if err != nil {
		t.Fatalf("discovered up: %v (stderr: %s)", err, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 1 {
		t.Fatalf("discovered up returned %d results, want 1: %s", len(results), stdout)
	}
	assertDiscoveryDefinition(t, results[0], "running_unverified")
	if results[0].Readiness != "running_unverified" {
		t.Fatalf("discovered up readiness = %q, want running_unverified", results[0].Readiness)
	}
	if results[0].ReadyCursor != nil {
		t.Fatalf("discovered up unexpectedly reported ready cursor %v", results[0].ReadyCursor)
	}

	stdout, stderr, err = stopShutdownRun(t, "status", "--json", "dev")
	if err != nil {
		t.Fatalf("discovered status: %v (stderr: %s)", err, stderr)
	}
	var status statusJSON
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("decode discovered status: %v", err)
	}
	if status.Source != "bin_dev" || !reflect.DeepEqual(status.Argv, []string{"./bin/dev"}) {
		t.Fatalf("discovered status identity = %+v", status)
	}
	if status.Readiness != "running_unverified" {
		t.Fatalf("discovered status readiness = %q, want running_unverified", status.Readiness)
	}
}

func TestDiscoveredStart(t *testing.T) {
	root := stopShutdownTestProject(t)
	_, runtimeDir := stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeDiscoveredBin(t, root)

	stdout, stderr, err := stopShutdownRun(t, "start", "--json", "dev")
	if err != nil {
		t.Fatalf("discovered start: %v (stderr: %s)", err, stderr)
	}
	results := manifestCLILaunchResults(t, stdout)
	if len(results) != 1 {
		t.Fatalf("discovered start returned %d results, want 1: %s", len(results), stdout)
	}
	assertDiscoveryDefinition(t, results[0], "running_unverified")
	if results[0].Readiness != "running_unverified" {
		t.Fatalf("discovered start readiness = %q, want running_unverified", results[0].Readiness)
	}

	if _, _, err := stopShutdownRun(t, "stop", "dev"); err != nil {
		t.Fatalf("stop discovered process: %v", err)
	}
	stdout, stderr, err = stopShutdownRun(t, "run", "--detach", "--json", "dev")
	if err != nil {
		t.Fatalf("run discovered definition: %v (stderr: %s)", err, stderr)
	}
	var runResultValue runResult
	if err := json.Unmarshal([]byte(stdout), &runResultValue); err != nil {
		t.Fatalf("decode discovered run: %v", err)
	}
	if runResultValue.Name != "dev" || runResultValue.Source != "bin_dev" || !reflect.DeepEqual(runResultValue.Argv, []string{"./bin/dev"}) {
		t.Fatalf("discovered run identity = %+v", runResultValue)
	}
	if runResultValue.Outcome != "running_unverified" || runResultValue.Readiness != "running_unverified" || runResultValue.ReadyCursor != nil {
		t.Fatalf("discovered run readiness = %+v", runResultValue)
	}

	stdout, stderr, err = stopShutdownRun(t, "restart", "--json", "dev")
	if err != nil {
		t.Fatalf("restart discovered definition: %v (stderr: %s)", err, stderr)
	}
	var restarted restartResult
	if err := json.Unmarshal([]byte(stdout), &restarted); err != nil {
		t.Fatalf("decode discovered restart: %v", err)
	}
	if restarted.Source != "bin_dev" || !reflect.DeepEqual(restarted.Argv, []string{"./bin/dev"}) {
		t.Fatalf("discovered restart identity = %+v", restarted)
	}
	if restarted.Readiness != "running_unverified" || restarted.ReadyCursor != nil {
		t.Fatalf("discovered restart readiness = %q cursor %v", restarted.Readiness, restarted.ReadyCursor)
	}
}

func TestDiscoveredList(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeDiscoveredBin(t, root)

	stdout, stderr, err := stopShutdownRun(t, "list", "--json")
	if err != nil {
		t.Fatalf("discovered list without daemon: %v (stderr: %s)", err, stderr)
	}
	var listed listJSON
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("decode discovered list: %v", err)
	}
	if len(listed.Processes) != 1 {
		t.Fatalf("discovered list returned %d processes, want 1", len(listed.Processes))
	}
	process := listed.Processes[0]
	if process.Name != "dev" || process.Source != "bin_dev" || process.State != "stopped" || !reflect.DeepEqual(process.Argv, []string{"./bin/dev"}) {
		t.Fatalf("discovered stopped process = %+v", process)
	}
	if _, err := os.Stat(runtimeDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only discovered list touched runtime directory: %v", err)
	}

	_, runtimeDir = stopShutdownTestServer(t, 2*time.Second)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	if _, _, err := stopShutdownRun(t, "start", "--no-wait", "dev"); err != nil {
		t.Fatalf("start discovered process for list: %v", err)
	}
	stdout, stderr, err = stopShutdownRun(t, "list")
	if err != nil {
		t.Fatalf("discovered human list: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "source=bin_dev") || !strings.Contains(stdout, "argv=./bin/dev") || !strings.Contains(stdout, "readiness=running_unverified") {
		t.Fatalf("discovered human list omitted metadata: %q", stdout)
	}
}

func TestDiscoveryErrors(t *testing.T) {
	commands := [][]string{
		{"up"},
		{"start", "dev"},
		{"run", "dev"},
		{"restart", "dev"},
		{"status", "dev"},
		{"logs", "dev"},
		{"wait", "dev"},
	}
	for _, args := range commands {
		name := strings.Join(args, "-")
		t.Run(name, func(t *testing.T) {
			root := stopShutdownTestProject(t)
			if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\n"), 0o600); err != nil {
				t.Fatalf("write malformed package.json: %v", err)
			}
			runtimeDir := filepath.Join(t.TempDir(), "runtime")
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

			_, _, err := stopShutdownRun(t, args...)
			if err == nil {
				t.Fatalf("%s unexpectedly succeeded", strings.Join(args, " "))
			}
			var configuration *project.ConfigurationError
			if !errors.As(err, &configuration) {
				t.Fatalf("%s error = %v, want ConfigurationError", strings.Join(args, " "), err)
			}
			assertRuntimeDirEmpty(t, runtimeDir)
		})
	}
	t.Run("malformed deno jsonc does not mutate runtime state", func(t *testing.T) {
		root := stopShutdownTestProject(t)
		if err := os.WriteFile(filepath.Join(root, "deno.jsonc"), []byte("{\n  \"version\": 1/* split */2,\n  \"tasks\": {\"dev\": \"echo body\"}\n}\n"), 0o600); err != nil {
			t.Fatalf("write malformed deno.jsonc: %v", err)
		}
		runtimeDir := filepath.Join(t.TempDir(), "runtime")
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

		_, _, err := stopShutdownRun(t, "start", "dev")
		if err == nil {
			t.Fatal("start dev unexpectedly succeeded")
		}
		var configuration *project.ConfigurationError
		if !errors.As(err, &configuration) {
			t.Fatalf("start dev error = %v, want ConfigurationError", err)
		}
		assertRuntimeDirEmpty(t, runtimeDir)
	})

	strictNoCandidateCommands := [][]string{
		{"up"},
		{"start", "dev"},
	}
	for _, args := range strictNoCandidateCommands {
		name := "no-candidate-" + strings.Join(args, "-")
		t.Run(name, func(t *testing.T) {
			_ = stopShutdownTestProject(t)
			runtimeDir := filepath.Join(t.TempDir(), "runtime")
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

			_, _, err := stopShutdownRun(t, args...)
			if err == nil {
				t.Fatalf("%s unexpectedly succeeded", strings.Join(args, " "))
			}
			var noCandidate *project.NoCandidateError
			if !errors.As(err, &noCandidate) {
				t.Fatalf("%s error = %v, want NoCandidateError", strings.Join(args, " "), err)
			}
			assertRuntimeDirEmpty(t, runtimeDir)
		})
	}
	t.Run("strict start ignores lower-priority and include declarations", func(t *testing.T) {
		root := stopShutdownTestProject(t)
		if err := os.WriteFile(filepath.Join(root, "GNUmakefile"), []byte("include dev: fragment.mk\n"), 0o600); err != nil {
			t.Fatalf("write GNUmakefile: %v", err)
		}
		marker := filepath.Join(root, "should-not-run")
		if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("dev:\n\t@touch should-not-run\n"), 0o600); err != nil {
			t.Fatalf("write Makefile: %v", err)
		}
		runtimeDir := filepath.Join(t.TempDir(), "runtime")
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		// A pre-fix detector can launch the lower-priority Makefile. Ensure a
		// failing assertion still retires that daemon before test cleanup.
		t.Cleanup(func() {
			_, _, _ = stopShutdownRun(t, "shutdown", "--stop-processes")
		})

		_, _, err := stopShutdownRun(t, "start", "dev")
		if err == nil {
			t.Fatal("start dev unexpectedly succeeded from false Make discovery")
		}
		var noCandidate *project.NoCandidateError
		if !errors.As(err, &noCandidate) {
			t.Fatalf("start dev error = %v, want NoCandidateError", err)
		}
		if noCandidate.Root != root {
			t.Fatalf("no-candidate root = %q, want %q", noCandidate.Root, root)
		}
		assertRuntimeDirEmpty(t, runtimeDir)
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Make recipe marker exists; strict discovery executed Make: %v", err)
		}
	})

	t.Run("strict start ignores directive-only Make input", func(t *testing.T) {
		root := stopShutdownTestProject(t)
		makefile := strings.Join([]string{
			"ifdef dev:",
			"endif",
			"ifndef dev:",
			"endif",
			"ifeq dev: value",
			"endif",
			"ifneq dev: value",
			"endif",
			"ifdef other",
			"else ifdef dev:",
			"endif dev:",
			"define dev:",
			"endef",
			"define helper",
			"endef dev:",
			"undefine dev:",
			"override dev: = value",
			"export dev:",
			"unexport dev:",
			"private dev: = value",
			"vpath dev: %",
		}, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte(makefile), 0o600); err != nil {
			t.Fatalf("write directive-only Makefile: %v", err)
		}
		runtimeDir := filepath.Join(t.TempDir(), "runtime")
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		// A pre-fix detector can launch a daemon for a false Make candidate.
		// Ensure a failing assertion still retires that daemon before cleanup.
		t.Cleanup(func() {
			_, _, _ = stopShutdownRun(t, "shutdown", "--stop-processes")
		})

		_, _, err := stopShutdownRun(t, "start", "dev")
		if err == nil {
			t.Fatal("start dev unexpectedly succeeded from directive-only Make input")
		}
		var noCandidate *project.NoCandidateError
		if !errors.As(err, &noCandidate) {
			t.Fatalf("start dev error = %v, want NoCandidateError", err)
		}
		if noCandidate.Root != root {
			t.Fatalf("no-candidate root = %q, want %q", noCandidate.Root, root)
		}
		assertRuntimeDirEmpty(t, runtimeDir)
	})

	t.Run("representative strict launch discovery errors", func(t *testing.T) {
		for _, test := range []struct {
			name     string
			filename string
			contents string
			args     []string
			kind     string
			source   string
		}{
			{
				name:     "null package script",
				filename: "package.json",
				contents: `{"scripts":{"dev":null}}`,
				args:     []string{"start", "dev"},
				kind:     "configuration",
				source:   "package_json",
			},
			{
				name:     "target-specific make assignment",
				filename: "Makefile",
				contents: "dev: FOO = bar\n",
				args:     []string{"up"},
				kind:     "no-candidate",
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				root := stopShutdownTestProject(t)
				if err := os.WriteFile(filepath.Join(root, test.filename), []byte(test.contents), 0o600); err != nil {
					t.Fatalf("write %s: %v", test.filename, err)
				}
				runtimeDir := filepath.Join(t.TempDir(), "runtime")
				t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

				_, _, err := stopShutdownRun(t, test.args...)
				if err == nil {
					t.Fatalf("%s unexpectedly succeeded", strings.Join(test.args, " "))
				}
				switch test.kind {
				case "configuration":
					var configuration *project.ConfigurationError
					if !errors.As(err, &configuration) {
						t.Fatalf("%s error = %v, want ConfigurationError", strings.Join(test.args, " "), err)
					}
					if configuration.Source != test.source {
						t.Fatalf("configuration source = %q, want %q", configuration.Source, test.source)
					}
					if configuration.Path != filepath.Join(root, test.filename) {
						t.Fatalf("configuration path = %q, want %q", configuration.Path, filepath.Join(root, test.filename))
					}
				case "no-candidate":
					var noCandidate *project.NoCandidateError
					if !errors.As(err, &noCandidate) {
						t.Fatalf("%s error = %v, want NoCandidateError", strings.Join(test.args, " "), err)
					}
					if noCandidate.Root != root {
						t.Fatalf("no-candidate root = %q, want %q", noCandidate.Root, root)
					}
				default:
					t.Fatalf("unknown expected error kind %q", test.kind)
				}
				assertRuntimeDirEmpty(t, runtimeDir)
			})
		}
	})
}
