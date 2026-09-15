//go:build darwin || linux

package integration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hum/internal/testutil"
)

const zeroConfigWorkflowTimeout = 15 * time.Second

func TestZeroConfigManifestRequired(t *testing.T) {
	fixture := integrationFixture(t)
	hum := integrationHum(t)
	cases := []struct {
		name  string
		setup func(*testing.T, string, string)
	}{
		{"mise", func(t *testing.T, root, marker string) {
			writeZeroConfigFile(t, filepath.Join(root, "mise.toml"), "[tasks.dev]\nrun = \"touch "+marker+"\"\n")
		}},
		{"task", func(t *testing.T, root, marker string) {
			writeZeroConfigFile(t, filepath.Join(root, "Taskfile.yml"), "version: '3'\ntasks:\n  dev:\n    cmds: [touch "+marker+"]\n")
		}},
		{"just", func(t *testing.T, root, marker string) {
			writeZeroConfigFile(t, filepath.Join(root, "justfile"), "dev:\n  touch "+marker+"\n")
		}},
		{"make", func(t *testing.T, root, marker string) {
			writeZeroConfigFile(t, filepath.Join(root, "Makefile"), "dev:\n\ttouch "+marker+"\n")
		}},
		{"package-json", func(t *testing.T, root, marker string) {
			writeZeroConfigFile(t, filepath.Join(root, "package.json"), fmt.Sprintf(`{"scripts":{"dev":"touch %s"}}`, marker))
		}},
		{"deno", func(t *testing.T, root, marker string) {
			writeZeroConfigFile(t, filepath.Join(root, "deno.json"), fmt.Sprintf(`{"tasks":{"dev":"touch %s"}}`, marker))
		}},
		{"composer", func(t *testing.T, root, marker string) {
			writeZeroConfigFile(t, filepath.Join(root, "composer.json"), fmt.Sprintf(`{"scripts":{"dev":"touch %s"}}`, marker))
		}},
		{"bin-dev", func(t *testing.T, root, marker string) {
			writeZeroConfigExecutable(t, filepath.Join(root, "bin", "dev"), "touch "+marker)
		}},
		{"mix", func(t *testing.T, root, marker string) {
			writeZeroConfigFile(t, filepath.Join(root, "mix.exs"), "defmodule App.MixProject do\n  def project, do: [deps: [{:phoenix, \"~> 1.7\"}]]\nend\n")
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := zeroConfigCanonicalTempDir(t)
			marker := filepath.Join(root, "must-not-run")
			testCase.setup(t, root, marker)
			shimDir := t.TempDir()
			introspectionMarker := filepath.Join(root, "introspection-must-not-run")
			for _, executable := range []string{"mise", "task", "just", "mix"} {
				writeZeroConfigExecutable(t, filepath.Join(shimDir, executable), ": > "+zeroConfigShellQuote(introspectionMarker)+"\nexit 99")
			}
			runtimeDir := testutil.RuntimeDir(t)
			env := testutil.RuntimeEnv(runtimeDir, "PATH="+shimDir+string(os.PathListSeparator)+"/usr/bin:/bin", "HUM_STOP_GRACE=1s")
			up := testutil.Run(t, hum, root, env, "up", "--json")
			if up.Code == 0 || !strings.Contains(up.Stdout, "manifest_missing") {
				t.Fatalf("up result = code %d stdout=%q stderr=%q, want manifest_missing", up.Code, up.Stdout, up.Stderr)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("runtime source executed candidate: %v", err)
			}
			if _, err := os.Stat(introspectionMarker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("runtime source launched introspection executable: %v", err)
			}
			run := testutil.Run(t, hum, root, env, "run", "dev", "--detach", "--json", "--", fixture, "stream", filepath.Join(root, "ad-hoc-ran"))
			if run.Code != 0 || run.Err != nil {
				t.Fatalf("ad-hoc run failed: code=%d err=%v stdout=%q stderr=%q", run.Code, run.Err, run.Stdout, run.Stderr)
			}
			testutil.WaitForFile(t, filepath.Join(root, "ad-hoc-ran")+".started", zeroConfigWorkflowTimeout)
			_ = testutil.Run(t, hum, root, env, "stop", "dev")
			_ = testutil.Run(t, hum, root, env, "shutdown", "--stop-processes")
		})
	}
}

func zeroConfigCanonicalTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("canonicalize project directory %q: %v", dir, err)
	}
	return filepath.Clean(canonical)
}

func writeZeroConfigFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func zeroConfigAssertSuccess(t *testing.T, result testutil.Result, operation string) {
	t.Helper()
	if result.Code != 0 || result.Err != nil || result.Stderr != "" {
		t.Fatalf("%s failed: code=%d err=%v stdout=%q stderr=%q", operation, result.Code, result.Err, result.Stdout, result.Stderr)
	}
}

func zeroConfigAssertNoCandidateExecution(t *testing.T, launchLog, bodyMarker string) {
	t.Helper()
	if _, err := os.Stat(bodyMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate body executed during inspection: %v", err)
	}
	if _, err := os.Stat(launchLog); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate launch log was created during inspection: %v", err)
	}
}

func zeroConfigAssertNoDaemon(t *testing.T, runtimeDir string) {
	t.Helper()
	for _, name := range []string{"hum.sock", "hum.pid", "hum.ready"} {
		if _, err := os.Stat(filepath.Join(runtimeDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inspection started daemon artifact %q: %v", name, err)
		}
	}
}

func zeroConfigShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func writeZeroConfigExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o700); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}
