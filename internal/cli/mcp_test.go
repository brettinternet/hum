package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hum/internal/app"
	"hum/internal/project"
)

func TestReadinessHTTPMCPAdapter(t *testing.T) {
	for _, method := range []string{"http", "tcp"} {
		target := "http://127.0.0.1:1/readyz"
		if method == "tcp" {
			target = "[::1]:1"
		}
		got := mcpProcess(app.Process{Readiness: &app.Readiness{Method: method, Target: target, State: app.ReadinessStarting}})
		if got.Readiness == nil || got.Readiness.Method != method || got.Readiness.Target != target {
			t.Fatalf("%s MCP readiness = %#v", method, got.Readiness)
		}
	}
}

func TestMCPHelp(t *testing.T) {
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("dev", "unknown", &output, &errorOutput)
	if err := root.Run(context.Background(), []string{"hum", "mcp", "--help"}); err != nil {
		t.Fatalf("mcp help: %v", err)
	}
	help := strings.ToLower(output.String())
	for _, want := range []string{
		"stdio", "one-time", "project_root", "absolute existing", "start and up", "resolved",
		"ad_hoc", "hum run", "daemon shutdown or replacement", "argv-based environment activation",
		"run, serve, and shutdown are not mcp tools",
		"64", "-32001", "-32600", "-32800", "notifications/cancelled", "serialized", "parent cancellation",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("mcp help missing %q: %q", want, output.String())
		}
	}
}

func TestManifestlessCommandsDoNotExecuteMix(t *testing.T) {
	for _, command := range []string{"list", "status", "completion", "init"} {
		t.Run(command, func(t *testing.T) {
			root := stopShutdownTestProject(t)
			runtimeDir := filepath.Join(t.TempDir(), "runtime")
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			sentinel := filepath.Join(root, "mix-evaluated")
			if err := os.WriteFile(filepath.Join(root, "mix.exs"), []byte(fmt.Sprintf("File.write!(%q, \"evaluated\")\ndefmodule App.MixProject do\n  defp deps, do: [{:phoenix, \"~> 1.7\"}]\nend\n", sentinel)), 0o600); err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, "mix"), []byte("#!/bin/sh\n: > \"$SENTINEL\"\nexit 99\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SENTINEL", sentinel)
			t.Setenv("PATH", bin)

			switch command {
			case "list":
				if _, _, err := stopShutdownRun(t, "list", "--json"); err != nil {
					t.Fatalf("list: %v", err)
				}
			case "status":
				_, _, err := stopShutdownRun(t, "status", "dev", "--json")
				if err == nil || errors.Is(err, project.ErrManifestMissing) {
					t.Fatalf("status error = %v, want not-found", err)
				}
			case "completion":
				if _, _, err := runCompletionForTest(t, "start", "--generate-shell-completion"); err != nil {
					t.Fatalf("completion: %v", err)
				}
			case "init":
				if _, _, err := stopShutdownRun(t, "init", "--json"); err != nil {
					t.Fatalf("init: %v", err)
				}
			}
			if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s evaluated mix.exs or invoked Mix: %v", command, err)
			}
		})
	}
}

func TestManifestResolutionCancellationDoesNotLaunchCommand(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "mise-executed")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "mise"), []byte("#!/bin/sh\n: > \"$SENTINEL\"\nexit 99\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("SENTINEL", marker)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (mcpResolver{}).Resolve(ctx, root)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("resolver error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resolution launched mise: %v", err)
	}
}

func TestManifestlessProjectDoesNotExecuteConventionalSources(t *testing.T) {
	root := stopShutdownTestProject(t)
	sentinel := filepath.Join(root, "mix-evaluated")
	if err := os.WriteFile(filepath.Join(root, "mix.exs"), []byte(fmt.Sprintf("File.write!(%q, \"evaluated\")\n", sentinel)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := stopShutdownRun(t, "list", "--json")
	if err != nil {
		t.Fatalf("manifestless list failed: %v", err)
	}
	if _, statErr := os.Stat(sentinel); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("manifestless inspection evaluated conventional source: %v", statErr)
	}
}

func TestManifestMissingMCPExplicitSelection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte("version: 1\nprocesses:\n  default:\n    argv: [default]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hum.dev.yaml"), []byte("version: 1\nprocesses:\n  dev:\n    argv: [dev]\n    cwd: sub\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	resolver := mcpResolver{}
	selected, err := resolver.Resolve(context.Background(), root)
	if err != nil || len(selected.Definitions) != 1 || selected.Definitions[0].Name != "default" {
		t.Fatalf("omitted manifest = %#v, err=%v", selected, err)
	}
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte("version: 1\nprocesses:\n  broken: [not, a, process]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), root); err == nil || errors.Is(err, project.ErrManifestMissing) {
		t.Fatalf("invalid default manifest fell back: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte("version: 1\nprocesses:\n  default:\n    argv: [default]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	selected, err = resolver.ResolveManifest(context.Background(), root, "hum.dev.yaml")
	if err != nil || len(selected.Definitions) != 1 || selected.Definitions[0].Name != "dev" || selected.Definitions[0].Source != "manifest:hum.dev.yaml" || selected.Definitions[0].Cwd != filepath.Join(selected.Root, "sub") {
		t.Fatalf("explicit manifest = %#v, err=%v", selected, err)
	}
	absolute, err := resolver.ResolveManifest(context.Background(), root, filepath.Join(root, "hum.dev.yaml"))
	if err != nil || absolute.Definitions[0].Source != "manifest:hum.dev.yaml" {
		t.Fatalf("absolute manifest = %#v, err=%v", absolute, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("version: 1\nprocesses: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ResolveManifest(context.Background(), root, outside); err == nil {
		t.Fatal("outside manifest accepted")
	}
	invalid := filepath.Join(root, "invalid.yaml")
	if err := os.WriteFile(invalid, []byte("version: 1\nprocesses:\n  broken: [not, a, process]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ResolveManifest(context.Background(), root, invalid); err == nil || errors.Is(err, project.ErrManifestMissing) {
		t.Fatalf("invalid explicit manifest fell back: %v", err)
	}
	t.Run("explicit selection disables discovery", func(t *testing.T) {
		discoveryRoot := t.TempDir()
		sentinel := filepath.Join(discoveryRoot, "discovery-ran")
		bin := t.TempDir()
		mise := filepath.Join(bin, "mise")
		if err := os.WriteFile(mise, []byte(fmt.Sprintf("#!/bin/sh\ntouch %q\nprintf '%s'\n", sentinel, `[{"name":"dev"}]`)), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin)
		explicit := filepath.Join(discoveryRoot, "hum.alt.yaml")
		if err := os.WriteFile(explicit, []byte("version: 1\nprocesses:\n  alt:\n    argv: [alt]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		resolved, err := resolver.ResolveManifest(context.Background(), discoveryRoot, explicit)
		if err != nil || len(resolved.Definitions) != 1 || resolved.Definitions[0].Name != "alt" {
			t.Fatalf("explicit resolution=%#v err=%v", resolved, err)
		}
		if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("explicit resolution invoked discovery: %v", err)
		}
	})
}
