package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hum/internal/daemon"
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

func TestManifestMissingUp(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeDiscoveredBin(t, root)
	_, _, err := stopShutdownRun(t, "up")
	if !errors.Is(err, project.ErrManifestMissing) {
		t.Fatalf("up error = %v, want manifest_missing", err)
	}
	assertRuntimeDirEmpty(t, runtimeDir)
}

func TestManifestMissingStart(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeDiscoveredBin(t, root)
	_, _, err := stopShutdownRun(t, "start", "dev")
	if !errors.Is(err, project.ErrManifestMissing) {
		t.Fatalf("start error = %v, want manifest_missing", err)
	}
	assertRuntimeDirEmpty(t, runtimeDir)
}

func TestManifestMissingList(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeDiscoveredBin(t, root)
	stdout, stderr, err := stopShutdownRun(t, "list", "--json")
	if err != nil {
		t.Fatalf("list without daemon: %v (stderr: %s)", err, stderr)
	}
	var listed listJSON
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Processes) != 0 {
		t.Fatalf("list returned %d processes, want empty", len(listed.Processes))
	}
	if _, err := os.Stat(runtimeDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only list touched runtime directory: %v", err)
	}
}

func TestManifestMissingListReturnsRetainedRecords(t *testing.T) {
	root := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	client, err := daemon.Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Start(context.Background(), daemon.StartRequest{Name: "retained", Root: root, Cwd: root, Source: "ad_hoc", Argv: []string{"/bin/sh", "-c", "sleep 30"}}); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	stdout, stderr, err := stopShutdownRun(t, "list", "--json")
	if err != nil || !strings.Contains(stdout, `"name":"retained"`) {
		t.Fatalf("retained list: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
}

func TestManifestMissingStatusRemainsRuntimeOnly(t *testing.T) {
	_ = stopShutdownTestProject(t)
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	if stdout, stderr, err := stopShutdownRun(t, "status", "--json"); err != nil {
		t.Fatalf("aggregate status failed: %v; stdout=%q stderr=%q", err, stdout, stderr)
	}
	_, _, err := stopShutdownRun(t, "status", "missing", "--json")
	if err == nil || errors.Is(err, project.ErrManifestMissing) {
		t.Fatalf("named status error = %v, want existing not-found semantics", err)
	}
	if stdout, stderr, err := stopShutdownRun(t, "list", "--all", "--json"); err != nil {
		t.Fatalf("list --all changed: %v; stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout, stderr, err := stopShutdownRun(t, "--global", "list", "--json"); err != nil {
		t.Fatalf("global list changed: %v; stdout=%q stderr=%q", err, stdout, stderr)
	}
	assertRuntimeDirEmpty(t, runtimeDir)
}

func TestManifestMissingCommandMatrix(t *testing.T) {
	commands := [][]string{
		{"up"},
		{"start", "dev"},
		{"restart", "dev"},
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
			if !errors.Is(err, project.ErrManifestMissing) {
				t.Fatalf("%s error = %v, want manifest_missing", strings.Join(args, " "), err)
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
		if !errors.Is(err, project.ErrManifestMissing) {
			t.Fatalf("start dev error = %v, want manifest_missing", err)
		}
		assertRuntimeDirEmpty(t, runtimeDir)
	})

	t.Run("no-candidate up remains inert", func(t *testing.T) {
		_ = stopShutdownTestProject(t)
		runtimeDir := filepath.Join(t.TempDir(), "runtime")
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

		stdout, stderr, err := stopShutdownRun(t, "up")
		if err == nil {
			t.Fatalf("no-candidate up unexpectedly succeeded: stdout %q stderr %q", stdout, stderr)
		}
		if !errors.Is(err, project.ErrManifestMissing) {
			t.Fatalf("no-candidate up error = %v, want manifest_missing", err)
		}
		if stdout != "" || stderr != "" {
			t.Fatalf("no-candidate up output = stdout %q stderr %q, want empty (harness does not print the returned error)", stdout, stderr)
		}
		assertRuntimeDirEmpty(t, runtimeDir)
	})

	strictNoCandidateCommands := [][]string{
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
			if !errors.Is(err, project.ErrManifestMissing) {
				t.Fatalf("%s error = %v, want manifest_missing", strings.Join(args, " "), err)
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
		if !errors.Is(err, project.ErrManifestMissing) {
			t.Fatalf("start dev error = %v, want manifest_missing", err)
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
		if !errors.Is(err, project.ErrManifestMissing) {
			t.Fatalf("start dev error = %v, want manifest_missing", err)
		}
		assertRuntimeDirEmpty(t, runtimeDir)
	})

	t.Run("malformed conventional files are ignored at runtime", func(t *testing.T) {
		for _, test := range []struct {
			name     string
			filename string
			contents string
			args     []string
		}{
			{name: "null package script", filename: "package.json", contents: `{"scripts":{"dev":null}}`, args: []string{"start", "dev"}},
			{name: "target-specific make assignment", filename: "Makefile", contents: "dev: FOO = bar\n", args: []string{"up"}},
		} {
			t.Run(test.name, func(t *testing.T) {
				root := stopShutdownTestProject(t)
				if err := os.WriteFile(filepath.Join(root, test.filename), []byte(test.contents), 0o600); err != nil {
					t.Fatalf("write %s: %v", test.filename, err)
				}
				runtimeDir := filepath.Join(t.TempDir(), "runtime")
				t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

				stdout, stderr, err := stopShutdownRun(t, test.args...)
				if !errors.Is(err, project.ErrManifestMissing) {
					t.Fatalf("%s error = %v, want ErrManifestMissing; stdout=%q stderr=%q", strings.Join(test.args, " "), err, stdout, stderr)
				}
				assertRuntimeDirEmpty(t, runtimeDir)
			})
		}
	})
}
