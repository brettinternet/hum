package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type discoveryStub struct {
	output []byte
	err    error
}

type discoveryExitError int

func (err discoveryExitError) Error() string { return fmt.Sprintf("exit status %d", err) }
func (err discoveryExitError) ExitCode() int { return int(err) }

func installDiscoveryStubs(t *testing.T, _ map[string]discoveryStub) *[]string {
	t.Helper()
	// Runtime resolution no longer invokes discovery. Keep this helper as a
	// compatibility seam for init detector cases; an empty call log proves that
	// no project subprocess was consulted.
	return &[]string{}
}

func writeDiscoveryFile(t *testing.T, root, name, contents string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func wantDiscoveredDefinition(t *testing.T, definitions []Definition, root, source string, argv ...string) {
	t.Helper()
	want := []Definition{{Name: "dev", Source: source, Argv: argv, Cwd: root, After: []string{}, Restart: RestartNever}}
	if !reflect.DeepEqual(definitions, want) {
		t.Fatalf("definitions = %#v, want %#v", definitions, want)
	}
}

func TestNearestManifestSelection(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, root, "hum.yaml", "version: 1\nprocesses:\n  top:\n    argv: [top]\n", 0o600)
	web := filepath.Join(root, "apps", "web")
	src := filepath.Join(web, "src")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, root, "apps/web/hum.yaml", "version: 1\nprocesses:\n  web:\n    argv: [web]\n", 0o600)

	selection, present, err := DefaultManifestSelection(root, root)
	if err != nil || !present || selection.Relative != "hum.yaml" {
		t.Fatalf("root selection = %#v, present=%t, err=%v", selection, present, err)
	}
	selection, present, err = DefaultManifestSelection(src, root)
	if err != nil || !present || selection.Relative != "apps/web/hum.yaml" {
		t.Fatalf("nearest selection = %#v, present=%t, err=%v; want apps/web/hum.yaml", selection, present, err)
	}
	definitions, err := ResolveDefinitionsContext(context.Background(), src, root)
	if err != nil || len(definitions) != 1 || definitions[0].Name != "web" || definitions[0].Source != "manifest:apps/web/hum.yaml" {
		t.Fatalf("nearest definitions = %#v, err=%v", definitions, err)
	}

	writeDiscoveryFile(t, root, "apps/web/.hum.yaml", "version: 1\nprocesses:\n  private:\n    argv: [private]\n", 0o600)
	selection, present, err = DefaultManifestSelection(src, root)
	if err != nil || !present || selection.Relative != "apps/web/.hum.yaml" {
		t.Fatalf("private selection = %#v, present=%t, err=%v; want .hum.yaml in nearest directory", selection, present, err)
	}
	writeDiscoveryFile(t, root, "apps/web/.hum.yaml", "version: 1\nprocesses:\n  broken: [\n", 0o600)
	if _, err := ResolveDefinitionsContext(context.Background(), src, root); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("malformed nearer manifest error = %v, want ErrConfiguration", err)
	}

	noGitParent := t.TempDir()
	writeDiscoveryFile(t, noGitParent, "hum.yaml", "version: 1\nprocesses: {}\n", 0o600)
	noGit := filepath.Join(noGitParent, "nested")
	if err := os.Mkdir(noGit, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, present, err := DefaultManifestSelection(noGit, noGit); err != nil || present {
		t.Fatalf("no-Git selection = present %t, err %v; must not search above project root", present, err)
	}
	if _, err := ResolveDefinitionsContext(context.Background(), noGit, noGit); !errors.Is(err, ErrManifestMissing) || err.Error() != (&ManifestMissingError{Start: noGit, Root: noGit}).Error() {
		t.Fatalf("no-Git parent manifest error = %v, want manifest_missing only in %s", err, noGit)
	}

	missingRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(missingRoot, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	rootMissing := (&ManifestMissingError{Start: missingRoot, Root: missingRoot}).Error()
	wantRootMissing := fmt.Sprintf("manifest is missing in %s: run hum init to create hum.yaml, or use hum run NAME -- COMMAND", missingRoot)
	if rootMissing != wantRootMissing {
		t.Fatalf("root missing error = %q, want unchanged %q", rootMissing, wantRootMissing)
	}
	missingStart := filepath.Join(missingRoot, "apps", "web")
	if err := os.MkdirAll(missingStart, 0o700); err != nil {
		t.Fatal(err)
	}
	missing, err := ResolveDefinitionsContext(context.Background(), missingStart, missingRoot)
	if err == nil || missing != nil {
		t.Fatalf("nested missing definitions = %#v, err=%v", missing, err)
	}
	wantNestedMissing := fmt.Sprintf("manifest is missing in %s or its parents up to %s: run hum init to create hum.yaml, or use hum run NAME -- COMMAND", missingStart, missingRoot)
	if err.Error() != wantNestedMissing {
		t.Fatalf("nested missing error = %q, want %q", err.Error(), wantNestedMissing)
	}
}

func TestAlternateManifestSelection(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "version: 1\nprocesses:\n  default:\n    argv: [default]\n    cwd: sub\n")
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	alternate := filepath.Join(root, "hum.dev.yaml")
	if err := os.WriteFile(alternate, []byte("version: 1\nprocesses:\n  dev:\n    argv: [dev]\n    cwd: sub\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	selection, err := ResolveManifestPath(root, root, "hum.dev.yaml")
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := CanonicalPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Relative != "hum.dev.yaml" || selection.Source != "manifest:hum.dev.yaml" || selection.Root != canonicalRoot {
		t.Fatalf("selection = %#v", selection)
	}
	definitions, err := ResolveExplicitDefinitions(context.Background(), selection)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || definitions[0].Name != "dev" || definitions[0].Source != "manifest:hum.dev.yaml" || definitions[0].Cwd != filepath.Join(canonicalRoot, "sub") {
		t.Fatalf("definitions = %#v", definitions)
	}
	if _, err := ResolveManifestPath(root, root, "missing.yaml"); err == nil {
		t.Fatal("missing alternate manifest unexpectedly succeeded")
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("version: 1\nprocesses: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveManifestPath(root, root, outside); err == nil {
		t.Fatal("outside alternate manifest unexpectedly succeeded")
	}
	escaped := filepath.Join(root, "escaped.yaml")
	if err := os.Symlink(outside, escaped); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveManifestPath(root, root, escaped); err == nil {
		t.Fatal("symlink-escaped alternate manifest unexpectedly succeeded")
	}
	insideLink := filepath.Join(root, "manifest-link.yaml")
	if err := os.Symlink(alternate, insideLink); err != nil {
		t.Fatal(err)
	}
	linked, err := ResolveManifestPath(root, root, insideLink)
	if err != nil {
		t.Fatal(err)
	}
	if linked.Relative != "hum.dev.yaml" || linked.Source != "manifest:hum.dev.yaml" {
		t.Fatalf("symlink source identity = %#v", linked)
	}
	directory := filepath.Join(root, "manifest-dir")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveManifestPath(root, root, directory); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("non-regular manifest error = %v", err)
	}
}

func TestExplicitManifestSelection(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "service"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := writeDiscoveryFile(t, root, "hum.dev.yaml", "version: 1\nprocesses:\n  dev:\n    argv: [echo, dev]\n    cwd: service\n", 0o600)
	selection, err := ResolveManifestPath(root, "", path)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := CanonicalPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Root != canonicalRoot || selection.Relative != "hum.dev.yaml" || selection.Source != "manifest:hum.dev.yaml" {
		t.Fatalf("selection = %#v", selection)
	}
	defs, err := ResolveExplicitDefinitions(context.Background(), selection)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Source != selection.Source || defs[0].Cwd != filepath.Join(canonicalRoot, "service") {
		t.Fatalf("definitions = %#v", defs)
	}
	missing := filepath.Join(root, "missing.yaml")
	if _, err := ResolveManifestPath(root, root, missing); err == nil {
		t.Fatal("missing manifest accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("version: 1\nprocesses: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveManifestPath(root, root, outside); err == nil {
		t.Fatal("outside manifest accepted")
	}
	link := filepath.Join(root, "escaped.yaml")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveManifestPath(root, root, link); err == nil {
		t.Fatal("escaped symlink accepted")
	}
}

// TestManifestReadsAreBounded keeps an untrusted checkout from exhausting
// memory through any manifest load path, including shell completion.
func TestManifestReadsAreBounded(t *testing.T) {
	root := t.TempDir()
	oversized := "version: 1\nprocesses: {}\n#" + strings.Repeat("x", 1<<20) + "\n"
	writeTestManifest(t, root, oversized)
	path := writeDiscoveryFile(t, root, "hum.dev.yaml", oversized, 0o600)
	selection, err := ResolveManifestPath(root, root, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveExplicitDefinitions(context.Background(), selection); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("explicit oversized manifest error = %v", err)
	}
	if _, err := LoadDefinitions(root); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("default oversized manifest error = %v", err)
	}
}

func TestResolveExplicit(t *testing.T) {
	t.Run("valid manifest is authoritative", func(t *testing.T) {
		root := t.TempDir()
		writeTestManifest(t, root, "version: 1\nprocesses:\n  web:\n    argv: [go, run, ./web]\n")
		writeDiscoveryFile(t, root, "Makefile", "dev:\n\t@touch should-not-run\n", 0o600)
		installDiscoveryStubs(t, nil)

		definitions, err := ResolveDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		want := []Definition{{Name: "web", Source: "manifest:hum.yaml", Argv: []string{"go", "run", "./web"}, Cwd: root, After: []string{}, Restart: RestartNever}}
		if !reflect.DeepEqual(definitions, want) {
			t.Fatalf("definitions = %#v, want %#v", definitions, want)
		}
	})

	t.Run("empty manifest is authoritative", func(t *testing.T) {
		root := t.TempDir()
		writeTestManifest(t, root, "version: 1\nprocesses: {}\n")
		writeDiscoveryFile(t, root, "bin/dev", "#!/bin/sh\nexit 0\n", 0o700)
		installDiscoveryStubs(t, nil)

		definitions, err := ResolveDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		if definitions == nil || len(definitions) != 0 {
			t.Fatalf("definitions = %#v, want non-nil empty definitions", definitions)
		}
	})

	t.Run("invalid manifest does not fall back", func(t *testing.T) {
		root := t.TempDir()
		writeTestManifest(t, root, "version: 1\nprocesses:\n  web: [not, a, process]\n")
		writeDiscoveryFile(t, root, "package.json", `{"scripts":{"dev":"echo should-not-run"}}`, 0o600)
		installDiscoveryStubs(t, nil)

		_, err := ResolveDefinitions(root)
		if err == nil || !strings.Contains(err.Error(), "hum.yaml") {
			t.Fatalf("error = %v, want authoritative hum.yaml error", err)
		}
		var configuration *ConfigurationError
		if !errors.As(err, &configuration) {
			t.Fatalf("error = %v, want ConfigurationError", err)
		}
	})

	t.Run("manifest errors name the path once without a discovery prefix", func(t *testing.T) {
		root := t.TempDir()
		writeTestManifest(t, root, "version: 1\nprocesses:\n  a:\n    argv: [a]\n    autostart: true\n")
		installDiscoveryStubs(t, nil)

		_, err := ResolveDefinitions(root)
		if err == nil {
			t.Fatal("ResolveDefinitions succeeded, want unknown-key error")
		}
		if !errors.Is(err, ErrConfiguration) {
			t.Fatalf("error = %v, want errors.Is ErrConfiguration", err)
		}
		var configuration *ConfigurationError
		if !errors.As(err, &configuration) {
			t.Fatalf("error = %v, want ConfigurationError", err)
		}
		want := fmt.Sprintf("hum.yaml (%s): process %q: unknown key %q (valid keys: after, argv, cwd, ready, restart, tty)", root, "a", "autostart")
		if err.Error() != want {
			t.Fatalf("error = %q, want %q", err.Error(), want)
		}
		if strings.Contains(err.Error(), "project discovery") {
			t.Fatalf("error = %q, want no project discovery prefix", err.Error())
		}
		if strings.Count(err.Error(), root) != 1 {
			t.Fatalf("error = %q, want root %q named exactly once", err.Error(), root)
		}
	})
}

func TestInitTaskRunnerSources(t *testing.T) {
	t.Run("mise", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "mise.toml", "[tasks.dev]\nrun = \"echo body\"\n", 0o600)
		calls := installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "mise", "mise", "run", "dev")
		if len(*calls) != 0 {
			t.Fatalf("calls = %#v, want no subprocess calls", *calls)
		}
	})

	t.Run("task", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "Taskfile.yml", "version: '3'\ntasks:\n  dev:\n    cmds: [echo body]\n", 0o600)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "task", "task", "dev")
	})

	t.Run("task alias", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "Taskfile.yml", "version: '3'\ntasks:\n  start:\n    aliases: [dev]\n", 0o600)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "task", "task", "dev")
	})

	t.Run("missing Taskfile is ignored without introspection", func(t *testing.T) {
		root := t.TempDir()
		calls := installDiscoveryStubs(t, map[string]discoveryStub{
			"task": {output: []byte("Taskfile not found\n"), err: discoveryExitError(100)},
		})
		_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		var noCandidate *NoCandidateError
		if !errors.As(err, &noCandidate) {
			t.Fatalf("error = %v, want NoCandidateError", err)
		}
		if len(*calls) != 0 {
			t.Fatalf("calls = %#v, want no subprocess calls", *calls)
		}
	})

	t.Run("public just recipe", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "justfile", "dev:\n\techo body\n", 0o600)
		calls := installDiscoveryStubs(t, map[string]discoveryStub{"just": {output: []byte(`{"recipes":{"dev":{"private":false,"body":["echo body"]}}}`)}})
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "just", "just", "dev")
		if len(*calls) != 0 {
			t.Fatalf("calls = %#v, want no subprocess calls", *calls)
		}
	})

	t.Run("private keyed dev recipe is excluded", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "justfile", "[private]\ndev:\n\techo body\n", 0o600)
		calls := installDiscoveryStubs(t, map[string]discoveryStub{"just": {output: []byte(`{"recipes":{"dev":{"private":true,"body":["echo body"]}}}`)}})

		_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err == nil {
			t.Fatal("private Just dev recipe unexpectedly produced a candidate")
		}
		var noCandidate *NoCandidateError
		if !errors.As(err, &noCandidate) {
			t.Fatalf("error = %v, want NoCandidateError", err)
		}
		if !errors.Is(err, ErrNoCandidate) {
			t.Fatalf("error = %v, want ErrNoCandidate", err)
		}
		if noCandidate.Root != root {
			t.Fatalf("no-candidate root = %q, want %q", noCandidate.Root, root)
		}
		if len(*calls) != 0 {
			t.Fatalf("calls = %#v, want no subprocess calls", *calls)
		}
	})

	t.Run("dynamic recipe metadata is ignored in favor of literal syntax", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "justfile", "dev:\n\techo body\n", 0o600)
		calls := installDiscoveryStubs(t, map[string]discoveryStub{"just": {output: []byte(`{"recipes":{"dev":{"private":"false","body":["echo body"]}}}`)}})

		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatalf("read-only detection error = %v", err)
		}
		wantDiscoveredDefinition(t, definitions, root, "just", "just", "dev")
		if len(*calls) != 0 {
			t.Fatalf("calls = %#v, want no subprocess calls", *calls)
		}
	})

	t.Run("literal make target without execution", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "Makefile", "dev: deps\n\t@touch should-not-run\npattern%:\n\t@touch should-not-run\n", 0o600)
		calls := installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "make", "make", "dev")
		if len(*calls) != 0 {
			t.Fatalf("calls = %#v, want no command execution for Make", *calls)
		}
		if _, err := os.Stat(filepath.Join(root, "should-not-run")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recipe marker exists; Make body was executed")
		}
	})

	t.Run("literal double-colon make target without execution", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "Makefile", "dev::\n\t@touch should-not-run\npattern%:\n\t@touch should-not-run\n", 0o600)
		calls := installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "make", "make", "dev")
		if len(*calls) != 0 {
			t.Fatalf("calls = %#v, want no command execution for Make", *calls)
		}
		if _, err := os.Stat(filepath.Join(root, "should-not-run")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recipe marker exists; Make body was executed")
		}
	})

	t.Run("recipe directive text does not suppress top-level dev target", func(t *testing.T) {
		root := t.TempDir()
		makefile := "other:\n\tdefine dev:\n\t@touch recipe-should-not-run\n\ndev:\n\t@touch should-not-run\n"
		writeDiscoveryFile(t, root, "Makefile", makefile, 0o600)
		calls := installDiscoveryStubs(t, nil)

		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "make", "make", "dev")
		if len(*calls) != 0 {
			t.Fatalf("calls = %#v, want no command execution for Make", *calls)
		}
		for _, marker := range []string{"recipe-should-not-run", "should-not-run"} {
			if _, err := os.Stat(filepath.Join(root, marker)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("recipe marker %q exists; Make body was executed", marker)
			}
		}
	})

	t.Run("recipe-only directive text is not a dev target", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "Makefile", "other:\n\tdefine dev:\n\t@touch should-not-run\n", 0o600)
		calls := installDiscoveryStubs(t, nil)

		_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err == nil {
			t.Fatal("recipe-only directive text unexpectedly produced a candidate")
		}
		var noCandidate *NoCandidateError
		if !errors.As(err, &noCandidate) {
			t.Fatalf("error = %v, want NoCandidateError", err)
		}
		if !errors.Is(err, ErrNoCandidate) {
			t.Fatalf("error = %v, want ErrNoCandidate", err)
		}
		if noCandidate.Root != root {
			t.Fatalf("no-candidate root = %q, want %q", noCandidate.Root, root)
		}
		if len(*calls) != 0 {
			t.Fatalf("calls = %#v, want no command execution for Make", *calls)
		}
		if _, err := os.Stat(filepath.Join(root, "should-not-run")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recipe marker exists; Make body was executed")
		}
	})

	t.Run("selected GNUmakefile dev target", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "GNUmakefile", "dev:\n\t@touch should-not-run\n", 0o600)
		calls := installDiscoveryStubs(t, nil)

		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "make", "make", "dev")
		if len(*calls) != 0 {
			t.Fatalf("calls = %#v, want no command execution for Make", *calls)
		}
		if _, err := os.Stat(filepath.Join(root, "should-not-run")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recipe marker exists; Make body was executed")
		}
	})

	for _, lower := range []string{"Makefile", "makefile"} {
		t.Run("GNUmakefile takes precedence over "+lower, func(t *testing.T) {
			root := t.TempDir()
			writeDiscoveryFile(t, root, "GNUmakefile", "other:\n\t@touch GNUmakefile-should-not-run\n", 0o600)
			writeDiscoveryFile(t, root, lower, "dev:\n\t@touch lower-should-not-run\n", 0o600)
			calls := installDiscoveryStubs(t, nil)

			_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
			if err == nil {
				t.Fatal("ResolveDefinitions unexpectedly succeeded from a lower-priority Makefile")
			}
			var noCandidate *NoCandidateError
			if !errors.As(err, &noCandidate) {
				t.Fatalf("error = %v, want NoCandidateError", err)
			}
			if !errors.Is(err, ErrNoCandidate) {
				t.Fatalf("error = %v, want ErrNoCandidate", err)
			}
			if noCandidate.Root != root {
				t.Fatalf("no-candidate root = %q, want %q", noCandidate.Root, root)
			}
			if len(*calls) != 0 {
				t.Fatalf("calls = %#v, want no command execution for Make", *calls)
			}
			for _, marker := range []string{"GNUmakefile-should-not-run", "lower-should-not-run"} {
				if _, err := os.Stat(filepath.Join(root, marker)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("recipe marker %q exists; Make body was executed", marker)
				}
			}
		})
	}

	for _, directive := range []string{"include", "-include", "sinclude"} {
		t.Run(directive+" operand containing dev:", func(t *testing.T) {
			root := t.TempDir()
			writeDiscoveryFile(t, root, "Makefile", fmt.Sprintf("%s dev: fragment.mk\n", directive), 0o600)
			calls := installDiscoveryStubs(t, nil)

			_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
			if err == nil {
				t.Fatalf("%s directive was treated as a dev target", directive)
			}
			var noCandidate *NoCandidateError
			if !errors.As(err, &noCandidate) {
				t.Fatalf("error = %v, want NoCandidateError", err)
			}
			if !errors.Is(err, ErrNoCandidate) {
				t.Fatalf("error = %v, want ErrNoCandidate", err)
			}
			if noCandidate.Root != root {
				t.Fatalf("no-candidate root = %q, want %q", noCandidate.Root, root)
			}
			if len(*calls) != 0 {
				t.Fatalf("calls = %#v, want no command execution for Make", *calls)
			}
		})
	}

	t.Run("make directives with dev operands are not targets", func(t *testing.T) {
		for _, test := range []struct {
			name        string
			declaration string
		}{
			{name: "ifdef", declaration: "ifdef dev:\nendif\n"},
			{name: "ifndef", declaration: "ifndef dev:\nendif\n"},
			{name: "ifeq", declaration: "ifeq dev: value\nendif\n"},
			{name: "ifneq", declaration: "ifneq dev: value\nendif\n"},
			{name: "else", declaration: "ifdef other\nelse ifdef dev:\nendif\n"},
			{name: "endif", declaration: "ifdef dev\nendif dev:\n"},
			{name: "define", declaration: "define dev:\nendef\n"},
			{name: "endef", declaration: "define helper\nendef dev:\n"},
			{name: "undefine", declaration: "undefine dev:\n"},
			{name: "override", declaration: "override dev: = value\n"},
			{name: "export", declaration: "export dev:\n"},
			{name: "unexport", declaration: "unexport dev:\n"},
			{name: "private", declaration: "private dev: = value\n"},
			{name: "vpath", declaration: "vpath dev: %\n"},
		} {
			t.Run(test.name, func(t *testing.T) {
				root := t.TempDir()
				writeDiscoveryFile(t, root, "Makefile", test.declaration, 0o600)
				calls := installDiscoveryStubs(t, nil)

				_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
				if err == nil {
					t.Fatalf("%s directive was treated as a dev target", test.name)
				}
				var noCandidate *NoCandidateError
				if !errors.As(err, &noCandidate) {
					t.Fatalf("error = %v, want NoCandidateError", err)
				}
				if !errors.Is(err, ErrNoCandidate) {
					t.Fatalf("error = %v, want ErrNoCandidate", err)
				}
				if noCandidate.Root != root {
					t.Fatalf("no-candidate root = %q, want %q", noCandidate.Root, root)
				}
				if len(*calls) != 0 {
					t.Fatalf("calls = %#v, want no command execution for Make", *calls)
				}
			})
		}
	})

	t.Run("make variable assignments are not targets", func(t *testing.T) {
		for _, test := range []struct {
			name        string
			declaration string
		}{
			{name: "single-colon assignment", declaration: "dev := value\n"},
			{name: "double-colon assignment", declaration: "dev ::= value\n"},
			{name: "target-specific assignment", declaration: "dev: FOO = bar\n"},
		} {
			t.Run(test.name, func(t *testing.T) {
				root := t.TempDir()
				writeDiscoveryFile(t, root, "Makefile", test.declaration, 0o600)
				installDiscoveryStubs(t, nil)
				_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
				var noCandidate *NoCandidateError
				if !errors.As(err, &noCandidate) {
					t.Fatalf("error = %v, want NoCandidateError", err)
				}
			})
		}
	})

	t.Run("unavailable runners are skipped", func(t *testing.T) {
		root := t.TempDir()
		installDiscoveryStubs(t, nil)
		_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		var noCandidate *NoCandidateError
		if !errors.As(err, &noCandidate) {
			t.Fatalf("error = %v, want NoCandidateError", err)
		}
	})

	t.Run("malformed task file is visible without introspection", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "Taskfile.yml", "tasks: [not-a-map]\n", 0o600)
		calls := installDiscoveryStubs(t, map[string]discoveryStub{"task": {output: []byte("should not run")}})
		_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		var configuration *ConfigurationError
		if !errors.As(err, &configuration) {
			t.Fatalf("error = %v, want ConfigurationError", err)
		}
		if !strings.Contains(err.Error(), "task") {
			t.Fatalf("error = %v, want source-specific task detail", err)
		}
		if len(*calls) != 0 {
			t.Fatalf("calls = %#v, want no subprocess calls", *calls)
		}
	})
}

func TestInitEcosystemSources(t *testing.T) {
	t.Run("package manager metadata wins", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "package.json", `{"packageManager":"bun@1.2.3","scripts":{"dev":"echo body"}}`, 0o600)
		writeDiscoveryFile(t, root, "package-lock.json", "{}", 0o600)
		installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "package_json", "bun", "run", "dev")
	})

	for _, test := range []struct {
		name     string
		lockfile string
		runner   string
	}{
		{name: "bun text lock", lockfile: "bun.lock", runner: "bun"},
		{name: "bun binary lock", lockfile: "bun.lockb", runner: "bun"},
		{name: "pnpm lock", lockfile: "pnpm-lock.yaml", runner: "pnpm"},
		{name: "yarn lock", lockfile: "yarn.lock", runner: "yarn"},
		{name: "npm lock", lockfile: "package-lock.json", runner: "npm"},
		{name: "npm shrinkwrap", lockfile: "npm-shrinkwrap.json", runner: "npm"},
		{name: "npm default", runner: "npm"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeDiscoveryFile(t, root, "package.json", `{"scripts":{"dev":"echo body"}}`, 0o600)
			if test.lockfile != "" {
				writeDiscoveryFile(t, root, test.lockfile, "lock", 0o600)
			}
			installDiscoveryStubs(t, nil)
			definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
			if err != nil {
				t.Fatal(err)
			}
			wantDiscoveredDefinition(t, definitions, root, "package_json", test.runner, "run", "dev")
		})
	}

	t.Run("package lock conflicts are configuration errors", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "package.json", `{"scripts":{"dev":"echo body"}}`, 0o600)
		writeDiscoveryFile(t, root, "pnpm-lock.yaml", "lock", 0o600)
		writeDiscoveryFile(t, root, "yarn.lock", "lock", 0o600)
		installDiscoveryStubs(t, nil)
		_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		var configuration *ConfigurationError
		if !errors.As(err, &configuration) || !strings.Contains(err.Error(), "pnpm") || !strings.Contains(err.Error(), "yarn") {
			t.Fatalf("error = %v, want typed lockfile conflict", err)
		}
	})

	t.Run("null dev values are configuration errors", func(t *testing.T) {
		for _, test := range []struct {
			name     string
			filename string
			source   string
			contents string
		}{
			{name: "package", filename: "package.json", source: "package_json", contents: `{"scripts":{"dev":null}}`},
			{name: "deno", filename: "deno.json", source: "deno_json", contents: `{"tasks":{"dev":null}}`},
			{name: "composer", filename: "composer.json", source: "composer_json", contents: `{"scripts":{"dev":null}}`},
		} {
			t.Run(test.name, func(t *testing.T) {
				root := t.TempDir()
				writeDiscoveryFile(t, root, test.filename, test.contents, 0o600)
				installDiscoveryStubs(t, nil)

				_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
				var configuration *ConfigurationError
				if err == nil || !errors.As(err, &configuration) {
					t.Fatalf("error = %v, want ConfigurationError", err)
				}
				if configuration.Source != test.source {
					t.Fatalf("configuration source = %q, want %q", configuration.Source, test.source)
				}
				if configuration.Path != filepath.Join(root, test.filename) {
					t.Fatalf("configuration path = %q, want %q", configuration.Path, filepath.Join(root, test.filename))
				}
				if !strings.HasPrefix(err.Error(), "project discovery configuration is malformed: ") {
					t.Fatalf("error = %q, want discovery configuration prefix", err)
				}
			})
		}
	})

	t.Run("deno jsonc", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "deno.jsonc", "{\n // no task body is read\n \"tasks\": {\"dev\": \"echo body\",},\n}\n", 0o600)
		installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "deno_json", "deno", "task", "dev")
	})

	t.Run("deno jsonc block comment", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "deno.jsonc", "{\n  \"tasks\": {\n    /* block comment with slash / and star * before closing */\n    \"dev\": \"echo body\",\n  },\n}\n", 0o600)
		installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "deno_json", "deno", "task", "dev")
	})

	t.Run("deno jsonc block comment preserves token boundaries", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "deno.jsonc", "{\n  \"version\": 1/* split */2,\n  \"tasks\": {\"dev\": \"echo body\"}\n}\n", 0o600)
		installDiscoveryStubs(t, nil)
		_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		var configuration *ConfigurationError
		if !errors.As(err, &configuration) {
			t.Fatalf("error = %v, want ConfigurationError", err)
		}
	})

	t.Run("composer", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "composer.json", `{"scripts":{"dev":["echo body"]}}`, 0o600)
		installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "composer_json", "composer", "run-script", "dev")
	})

	t.Run("executable bin dev", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "bin/"+binDevExecutableName, "#!/bin/sh\ntouch should-not-run\n", 0o700)
		installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "bin_dev", "./bin/"+binDevExecutableName)
	})

	t.Run("confirmed mix phoenix task", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "mix.exs", "defmodule App.MixProject do\n  defp deps, do: [{:phoenix, \"~> 1.7\"}]\nend\n", 0o600)
		calls := installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "mix", "mix", "phx.server")
		if len(*calls) != 0 {
			t.Fatalf("Mix discovery executed commands: %#v", *calls)
		}
	})

	t.Run("confirmed mix phoenix task with ANSI output", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "mix.exs", "defmodule App.MixProject do\n  # \\x1b[32mphx.server\\x1b[0m output is not consulted\n  defp deps, do: [{:phoenix, \"~> 1.7\"}]\nend\n", 0o600)
		calls := installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "mix", "mix", "phx.server")
		if len(*calls) != 0 {
			t.Fatalf("Mix discovery executed commands: %#v", *calls)
		}
	})

	t.Run("commented and quoted phoenix dependencies fail closed", func(t *testing.T) {
		for _, contents := range []string{
			"# {:phoenix, \"~> 1.7\"}\n",
			"value = \"{:phoenix, ~> 1.7}\"\n",
			"value = '''{:phoenix, ~> 1.7}'''\n",
		} {
			root := t.TempDir()
			writeDiscoveryFile(t, root, "mix.exs", contents, 0o600)
			installDiscoveryStubs(t, nil)
			_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
			var noCandidate *NoCandidateError
			if !errors.As(err, &noCandidate) {
				t.Fatalf("ResolveDefinitions(%q) error = %v, want NoCandidateError", contents, err)
			}
		}
	})
}

func TestInitDiscoveryTemplates(t *testing.T) {
	t.Run("collects every supported source", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "mise.toml", "[tasks.dev]\nrun = \"echo body\"\n", 0o600)
		writeDiscoveryFile(t, root, "Taskfile.yml", "version: '3'\ntasks:\n  dev:\n    cmds: [echo body]\n", 0o600)
		writeDiscoveryFile(t, root, "justfile", "dev:\n\techo body\n", 0o600)
		writeDiscoveryFile(t, root, "Makefile", "dev:\n\t@touch should-not-run\n", 0o600)
		writeDiscoveryFile(t, root, "package.json", `{"scripts":{"dev":"echo body"}}`, 0o600)
		writeDiscoveryFile(t, root, "deno.json", `{"tasks":{"dev":"echo body"}}`, 0o600)
		writeDiscoveryFile(t, root, "composer.json", `{"scripts":{"dev":"echo body"}}`, 0o600)
		writeDiscoveryFile(t, root, "bin/"+binDevExecutableName, "#!/bin/sh\n", 0o700)
		writeDiscoveryFile(t, root, "mix.exs", "defmodule App.MixProject do\n  defp deps, do: [{:phoenix, \"~> 1.7\"}]\nend\n", 0o600)
		installDiscoveryStubs(t, map[string]discoveryStub{
			"mise": {output: []byte(`[{"name":"dev"}]`)},
			"task": {output: []byte(`{"tasks":[{"name":"dev"}]}`)},
			"just": {output: []byte(`{"recipes":{"dev":{"private":false}}}`)},
		})

		_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		var ambiguity *AmbiguityError
		if !errors.As(err, &ambiguity) {
			t.Fatalf("error = %v, want AmbiguityError", err)
		}
		for _, source := range supportedDiscoverySources {
			if !strings.Contains(err.Error(), source) {
				t.Fatalf("error = %v, want source %q", err, source)
			}
		}
		if !strings.Contains(err.Error(), "hum init") {
			t.Fatalf("error = %v, want hum init guidance", err)
		}
	})

	t.Run("one candidate succeeds", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "deno.json", `{"tasks":{"dev":"echo body"}}`, 0o600)
		installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "deno_json", "deno", "task", "dev")
	})

	t.Run("no candidate is actionable", func(t *testing.T) {
		root := t.TempDir()
		installDiscoveryStubs(t, nil)
		_, err := ResolveDefinitionsReadOnly(context.Background(), root, root)
		if !errors.Is(err, ErrNoCandidate) {
			t.Fatalf("error = %v, want NoCandidateError", err)
		}
		for _, text := range []string{"hum.yaml", "hum init", root} {
			if !strings.Contains(err.Error(), text) {
				t.Fatalf("error = %v, want actionable detail %q", err, text)
			}
		}
	})
}

func TestPrivateManifest(t *testing.T) {
	manifest := func(name, command string) string {
		return fmt.Sprintf("version: 1\nprocesses:\n  %s:\n    argv: [%s]\n", name, command)
	}
	valid := func(t *testing.T, root string, wantName, wantSource string) {
		t.Helper()
		for _, resolve := range []struct {
			name string
			load func() ([]Definition, error)
		}{
			{"ResolveDefinitionsContext", func() ([]Definition, error) { return ResolveDefinitionsContext(context.Background(), root, root) }},
			{"ResolveDefinitionsReadOnly", func() ([]Definition, error) { return ResolveDefinitionsReadOnly(context.Background(), root, root) }},
			{"LoadDefinitions", func() ([]Definition, error) { return LoadDefinitions(root) }},
		} {
			t.Run(resolve.name, func(t *testing.T) {
				defs, err := resolve.load()
				expectedSource := wantSource
				if wantSource == "manifest" && resolve.name != "LoadDefinitions" {
					expectedSource = "manifest:hum.yaml"
				}
				if err != nil || len(defs) != 1 || defs[0].Name != wantName || defs[0].Source != expectedSource {
					t.Fatalf("definitions=%#v err=%v, want %s/%s", defs, err, wantName, expectedSource)
				}
			})
		}
	}
	invalid := func(t *testing.T, root string) {
		t.Helper()
		for _, resolve := range []struct {
			name string
			load func() ([]Definition, error)
		}{
			{"ResolveDefinitionsContext", func() ([]Definition, error) { return ResolveDefinitionsContext(context.Background(), root, root) }},
			{"ResolveDefinitionsReadOnly", func() ([]Definition, error) { return ResolveDefinitionsReadOnly(context.Background(), root, root) }},
			{"LoadDefinitions", func() ([]Definition, error) { return LoadDefinitions(root) }},
		} {
			t.Run(resolve.name, func(t *testing.T) {
				_, err := resolve.load()
				var configuration *ConfigurationError
				if err == nil || !errors.As(err, &configuration) || configuration.Source != ".hum.yaml" || strings.Contains(err.Error(), "fallback") {
					t.Fatalf("error=%v, want private ConfigurationError", err)
				}
			})
		}
	}
	t.Run("PrivateWins", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "hum.yaml", manifest("shared", "shared"), 0o600)
		writeDiscoveryFile(t, root, ".hum.yaml", manifest("private", "private"), 0o600)
		valid(t, root, "private", "manifest:.hum.yaml")
	})
	t.Run("PrivateAlone", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, ".hum.yaml", manifest("private", "private"), 0o600)
		valid(t, root, "private", "manifest:.hum.yaml")
	})
	t.Run("PrivateSymlinkRootKeepsLexicalPaths", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, ".hum.yaml", "version: 1\nenvironment:\n  files: [.env]\nprocesses:\n  private:\n    argv: [private]\n", 0o600)
		writeDiscoveryFile(t, root, ".env", "TOKEN=value\n", 0o600)
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(root, alias); err != nil {
			t.Fatal(err)
		}
		definitions, err := ResolveDefinitionsContext(context.Background(), alias, alias)
		if err != nil || len(definitions) != 1 || definitions[0].Environment == nil {
			t.Fatalf("definitions=%#v err=%v", definitions, err)
		}
		if definitions[0].Environment.Root != alias || definitions[0].Environment.BaseDir != alias {
			t.Fatalf("environment=%#v, want lexical alias %q", definitions[0].Environment, alias)
		}
	})
	t.Run("SharedAlone", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "hum.yaml", manifest("shared", "shared"), 0o600)
		valid(t, root, "shared", "manifest")
	})
	t.Run("NoMerge", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "hum.yaml", manifest("shared", "shared"), 0o600)
		writeDiscoveryFile(t, root, ".hum.yaml", manifest("private", "private"), 0o600)
		defs, err := ResolveDefinitionsContext(context.Background(), root, root)
		if err != nil || len(defs) != 1 || defs[0].Name != "private" {
			t.Fatalf("definitions=%#v err=%v, want private only", defs, err)
		}
	})
	t.Run("InvalidPrivateNoFallback", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "hum.yaml", manifest("shared", "shared"), 0o600)
		writeDiscoveryFile(t, root, ".hum.yaml", "not: a manifest\n", 0o600)
		invalid(t, root)
	})
	t.Run("UnreadablePrivateNoFallback", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "hum.yaml", manifest("shared", "shared"), 0o600)
		if runtime.GOOS == "windows" {
			// Chmod cannot deny reads on Windows; a non-file private path
			// still proves resolution must not fall back to hum.yaml.
			if err := os.Mkdir(filepath.Join(root, ".hum.yaml"), 0o700); err != nil {
				t.Fatal(err)
			}
		} else {
			path := writeDiscoveryFile(t, root, ".hum.yaml", manifest("private", "private"), 0)
			t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
		}
		invalid(t, root)
	})
	t.Run("SymlinkPrivateNoFallback", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "hum.yaml", manifest("shared", "shared"), 0o600)
		outside := filepath.Join(t.TempDir(), "outside.yaml")
		if err := os.WriteFile(outside, []byte(manifest("outside", "outside")), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, ".hum.yaml")); err != nil {
			t.Fatal(err)
		}
		invalid(t, root)
	})
	t.Run("DirectoryPrivateNoFallback", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "hum.yaml", manifest("shared", "shared"), 0o600)
		if err := os.Mkdir(filepath.Join(root, ".hum.yaml"), 0o700); err != nil {
			t.Fatal(err)
		}
		invalid(t, root)
	})
	t.Run("ExplicitFileWins", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "hum.yaml", manifest("shared", "shared"), 0o600)
		writeDiscoveryFile(t, root, ".hum.yaml", manifest("private", "private"), 0o600)
		writeDiscoveryFile(t, root, "alternate.yaml", manifest("alternate", "alternate"), 0o600)
		selection, err := ResolveManifestPath(root, root, "alternate.yaml")
		if err != nil {
			t.Fatal(err)
		}
		defs, err := ResolveExplicitDefinitions(context.Background(), selection)
		if err != nil || len(defs) != 1 || defs[0].Name != "alternate" || defs[0].Source != "manifest:alternate.yaml" {
			t.Fatalf("definitions=%#v err=%v", defs, err)
		}
	})
}

func TestResolveDefinitionsManifestOnlyDoesNotDiscover(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryFile(t, root, "package.json", `{"scripts":{"dev":"sleep 30"}}`, 0o600)
	_, err := ResolveDefinitionsContext(context.Background(), root, root)
	if !errors.Is(err, ErrManifestMissing) {
		t.Fatalf("runtime resolution error = %v, want manifest missing", err)
	}
}

func TestResolveDefinitionsManifestOnlyIgnoresRunnerFiles(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryFile(t, root, "Taskfile.yml", "tasks:\n  dev:\n    cmds: [sleep 30]\n", 0o600)
	_, err := ResolveDefinitions(root)
	if !errors.Is(err, ErrManifestMissing) {
		t.Fatalf("runtime resolution error = %v, want manifest missing", err)
	}
}

func TestIntrospectionErrorExposesItsCause(t *testing.T) {
	err := &IntrospectionError{Source: "just", Err: context.Canceled}
	if !errors.Is(err, ErrIntrospection) || !errors.Is(err, context.Canceled) {
		t.Fatalf("introspection error hides its category or cause: %v", err)
	}
	if !errors.Is(&IntrospectionError{Source: "just"}, ErrIntrospection) {
		t.Fatal("introspection error without a cause lost its category")
	}
}

func TestResolveDefinitionsManifestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	_, err := ResolveDefinitionsContext(ctx, root, root)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("resolution error = %v, want context.Canceled", err)
	}
}
