package project

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

func installDiscoveryStubs(t *testing.T, stubs map[string]discoveryStub) *[]string {
	t.Helper()
	oldLookPath := discoveryLookPath
	oldCommand := discoveryCommand
	calls := []string{}
	discoveryLookPath = func(name string) (string, error) {
		if _, ok := stubs[name]; ok {
			return filepath.Join("/fake", name), nil
		}
		return "", exec.ErrNotFound
	}
	discoveryCommand = func(_ string, argv ...string) ([]byte, error) {
		if len(argv) == 0 {
			return nil, errors.New("empty test argv")
		}
		calls = append(calls, strings.Join(argv, " "))
		stub, ok := stubs[argv[0]]
		if !ok {
			return nil, fmt.Errorf("unexpected unavailable command %q", argv[0])
		}
		return append([]byte(nil), stub.output...), stub.err
	}
	t.Cleanup(func() {
		discoveryLookPath = oldLookPath
		discoveryCommand = oldCommand
	})
	return &calls
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
	want := []Definition{{Name: "dev", Source: source, Argv: argv, Cwd: root}}
	if !reflect.DeepEqual(definitions, want) {
		t.Fatalf("definitions = %#v, want %#v", definitions, want)
	}
}

func wantJustDumpCall(t *testing.T, calls *[]string, justfile string) {
	t.Helper()
	want := strings.Join([]string{"just", "--unstable", "--dump", "--dump-format", "json", "--justfile", justfile}, " ")
	if !reflect.DeepEqual(*calls, []string{want}) {
		t.Fatalf("calls = %#v, want %#v", *calls, []string{want})
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
		want := []Definition{{Name: "web", Source: "manifest", Argv: []string{"go", "run", "./web"}, Cwd: root}}
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
}

func TestDiscoverTaskRunnerDev(t *testing.T) {
	t.Run("mise", func(t *testing.T) {
		root := t.TempDir()
		installDiscoveryStubs(t, map[string]discoveryStub{"mise": {output: []byte(`[{"name":"dev","description":"echo body"}]`)}})
		definitions, err := ResolveDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "mise", "mise", "run", "dev")
	})

	t.Run("task", func(t *testing.T) {
		root := t.TempDir()
		installDiscoveryStubs(t, map[string]discoveryStub{"task": {output: []byte(`{"tasks":[{"name":"dev","desc":"echo body"}]}`)}})
		definitions, err := ResolveDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "task", "task", "dev")
	})

	t.Run("missing Taskfile with diagnostic output", func(t *testing.T) {
		root := t.TempDir()
		installDiscoveryStubs(t, map[string]discoveryStub{
			"task": {output: []byte("Taskfile not found\n"), err: discoveryExitError(100)},
		})
		_, err := ResolveDefinitions(root)
		var noCandidate *NoCandidateError
		if !errors.As(err, &noCandidate) {
			t.Fatalf("error = %v, want NoCandidateError", err)
		}
	})

	t.Run("public just recipe", func(t *testing.T) {
		root := t.TempDir()
		justfile := writeDiscoveryFile(t, root, "justfile", "dev:\n\techo body\n", 0o600)
		calls := installDiscoveryStubs(t, map[string]discoveryStub{"just": {output: []byte(`{"recipes":{"dev":{"private":false,"body":["echo body"]}}}`)}})
		definitions, err := ResolveDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "just", "just", "dev")
		wantJustDumpCall(t, calls, justfile)
	})

	t.Run("private keyed dev recipe is excluded", func(t *testing.T) {
		root := t.TempDir()
		justfile := writeDiscoveryFile(t, root, "justfile", "[private]\ndev:\n\techo body\n", 0o600)
		calls := installDiscoveryStubs(t, map[string]discoveryStub{"just": {output: []byte(`{"recipes":{"dev":{"private":true,"body":["echo body"]}}}`)}})

		_, err := ResolveDefinitions(root)
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
		wantJustDumpCall(t, calls, justfile)
	})

	t.Run("malformed keyed recipe metadata is an introspection error", func(t *testing.T) {
		root := t.TempDir()
		justfile := writeDiscoveryFile(t, root, "justfile", "dev:\n\techo body\n", 0o600)
		calls := installDiscoveryStubs(t, map[string]discoveryStub{"just": {output: []byte(`{"recipes":{"dev":{"private":"false","body":["echo body"]}}}`)}})

		_, err := ResolveDefinitions(root)
		var introspection *IntrospectionError
		if err == nil || !errors.As(err, &introspection) {
			t.Fatalf("error = %v, want IntrospectionError", err)
		}
		if introspection.Source != "just" {
			t.Fatalf("introspection source = %q, want just", introspection.Source)
		}
		if introspection.Path != justfile {
			t.Fatalf("introspection path = %q, want %q", introspection.Path, justfile)
		}
		wantJustDumpCall(t, calls, justfile)
	})

	t.Run("literal make target without execution", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "Makefile", "dev: deps\n\t@touch should-not-run\npattern%:\n\t@touch should-not-run\n", 0o600)
		calls := installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitions(root)
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
		definitions, err := ResolveDefinitions(root)
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

		definitions, err := ResolveDefinitions(root)
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

		_, err := ResolveDefinitions(root)
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

		definitions, err := ResolveDefinitions(root)
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

			_, err := ResolveDefinitions(root)
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

			_, err := ResolveDefinitions(root)
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

				_, err := ResolveDefinitions(root)
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
				_, err := ResolveDefinitions(root)
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
		_, err := ResolveDefinitions(root)
		var noCandidate *NoCandidateError
		if !errors.As(err, &noCandidate) {
			t.Fatalf("error = %v, want NoCandidateError", err)
		}
	})

	t.Run("malformed introspection is visible", func(t *testing.T) {
		root := t.TempDir()
		installDiscoveryStubs(t, map[string]discoveryStub{"mise": {output: []byte("not json")}})
		_, err := ResolveDefinitions(root)
		var introspection *IntrospectionError
		if !errors.As(err, &introspection) {
			t.Fatalf("error = %v, want IntrospectionError", err)
		}
		if !strings.Contains(err.Error(), "mise") {
			t.Fatalf("error = %v, want source-specific mise detail", err)
		}
	})
}

func TestDiscoverEcosystemDev(t *testing.T) {
	t.Run("package manager metadata wins", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "package.json", `{"packageManager":"bun@1.2.3","scripts":{"dev":"echo body"}}`, 0o600)
		writeDiscoveryFile(t, root, "package-lock.json", "{}", 0o600)
		installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitions(root)
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
			definitions, err := ResolveDefinitions(root)
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
		_, err := ResolveDefinitions(root)
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

				_, err := ResolveDefinitions(root)
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
			})
		}
	})

	t.Run("deno jsonc", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "deno.jsonc", "{\n // no task body is read\n \"tasks\": {\"dev\": \"echo body\",},\n}\n", 0o600)
		installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "deno_json", "deno", "task", "dev")
	})

	t.Run("deno jsonc block comment", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "deno.jsonc", "{\n  \"tasks\": {\n    /* block comment with slash / and star * before closing */\n    \"dev\": \"echo body\",\n  },\n}\n", 0o600)
		installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "deno_json", "deno", "task", "dev")
	})

	t.Run("deno jsonc block comment preserves token boundaries", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "deno.jsonc", "{\n  \"version\": 1/* split */2,\n  \"tasks\": {\"dev\": \"echo body\"}\n}\n", 0o600)
		installDiscoveryStubs(t, nil)
		_, err := ResolveDefinitions(root)
		var configuration *ConfigurationError
		if !errors.As(err, &configuration) {
			t.Fatalf("error = %v, want ConfigurationError", err)
		}
	})

	t.Run("composer", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "composer.json", `{"scripts":{"dev":["echo body"]}}`, 0o600)
		installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "composer_json", "composer", "run-script", "dev")
	})

	t.Run("executable bin dev", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "bin/dev", "#!/bin/sh\ntouch should-not-run\n", 0o700)
		installDiscoveryStubs(t, nil)
		definitions, err := ResolveDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "bin_dev", "./bin/dev")
	})

	t.Run("confirmed mix phoenix task", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "mix.exs", "defmodule App.MixProject do\n  # body is never evaluated by resolution\nend\n", 0o600)
		installDiscoveryStubs(t, map[string]discoveryStub{"mix": {output: []byte("app.start\nphx.server\n")}})
		definitions, err := ResolveDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "mix", "mix", "phx.server")
	})

	t.Run("confirmed mix phoenix task with ANSI output", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "mix.exs", "defmodule App.MixProject do\n  # body is never evaluated by resolution\nend\n", 0o600)
		installDiscoveryStubs(t, map[string]discoveryStub{"mix": {output: []byte("\x1b[32mphx.\x1b[0mserver\n")}})
		definitions, err := ResolveDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "mix", "mix", "phx.server")
	})
}

func TestDiscoveryAmbiguity(t *testing.T) {
	t.Run("collects every supported source", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "justfile", "dev:\n\techo body\n", 0o600)
		writeDiscoveryFile(t, root, "Makefile", "dev:\n\t@touch should-not-run\n", 0o600)
		writeDiscoveryFile(t, root, "package.json", `{"scripts":{"dev":"echo body"}}`, 0o600)
		writeDiscoveryFile(t, root, "deno.json", `{"tasks":{"dev":"echo body"}}`, 0o600)
		writeDiscoveryFile(t, root, "composer.json", `{"scripts":{"dev":"echo body"}}`, 0o600)
		writeDiscoveryFile(t, root, "bin/dev", "#!/bin/sh\n", 0o700)
		writeDiscoveryFile(t, root, "mix.exs", "defmodule App.MixProject do end\n", 0o600)
		installDiscoveryStubs(t, map[string]discoveryStub{
			"mise": {output: []byte(`[{"name":"dev"}]`)},
			"task": {output: []byte(`{"tasks":[{"name":"dev"}]}`)},
			"just": {output: []byte(`{"recipes":{"dev":{"private":false}}}`)},
			"mix":  {output: []byte("phx.server\n")},
		})

		_, err := ResolveDefinitions(root)
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
		definitions, err := ResolveDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		wantDiscoveredDefinition(t, definitions, root, "deno_json", "deno", "task", "dev")
	})

	t.Run("no candidate is actionable", func(t *testing.T) {
		root := t.TempDir()
		installDiscoveryStubs(t, nil)
		_, err := ResolveDefinitions(root)
		var noCandidate *NoCandidateError
		if !errors.As(err, &noCandidate) {
			t.Fatalf("error = %v, want NoCandidateError", err)
		}
		for _, text := range append([]string{"hum.yaml", "hum init"}, supportedDiscoveryConventions...) {
			if !strings.Contains(err.Error(), text) {
				t.Fatalf("error = %v, want actionable detail %q", err, text)
			}
		}
	})
}
