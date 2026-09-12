package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"hum/internal/project"
)

func TestManifestEnvironmentPreflightContract(t *testing.T) {
	t.Run("composition and one baseline snapshot", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".env"), []byte("PORT=3000\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		baseline := []string{"PATH=/bin", "DUP=first", "DUP=last"}
		manifest := manifestState{root: root, defs: []project.Definition{{Name: "api", Environment: &project.EnvironmentSpec{Inherit: true, Files: []string{".env"}, Values: map[string]*string{}, BaseDir: root, Root: root}}}}
		if err := prepareManifestEnvironments(&manifest, []string{"api"}, baseline, false); err != nil {
			t.Fatal(err)
		}
		baseline[0] = "PATH=mutated"
		if !reflect.DeepEqual(manifest.baseline, []string{"PATH=/bin", "DUP=first", "DUP=last"}) {
			t.Fatalf("baseline snapshot=%v", manifest.baseline)
		}
		if got := manifest.environments["api"]; !reflect.DeepEqual(got, []string{"DUP=last", "PATH=/bin", "PORT=3000"}) {
			t.Fatalf("prepared=%v", got)
		}
	})

	t.Run("missing required file", func(t *testing.T) {
		root := t.TempDir()
		missing := manifestState{root: root, defs: []project.Definition{{Name: "bad", Environment: &project.EnvironmentSpec{Inherit: true, Files: []string{"missing.env"}, Values: map[string]*string{}, BaseDir: root, Root: root}}}}
		if err := prepareManifestEnvironments(&missing, []string{"bad"}, []string{"PATH=/bin"}, false); err == nil {
			t.Fatal("missing environment file accepted")
		}
	})

	t.Run("dependency closure", func(t *testing.T) {
		root := t.TempDir()
		large := strings.Repeat("x", 4<<20)
		manifest := manifestState{root: root, defs: []project.Definition{
			{Name: "dependency", Environment: &project.EnvironmentSpec{Inherit: false, Values: map[string]*string{"TOO_LARGE": &large}, BaseDir: root, Root: root}},
			{Name: "api", After: []string{"dependency"}, Environment: &project.EnvironmentSpec{Inherit: true, Values: map[string]*string{}, BaseDir: root, Root: root}},
		}}
		if err := prepareManifestEnvironments(&manifest, []string{"api"}, []string{"PATH=/bin"}, false); err != nil {
			t.Fatalf("targeted preflight loaded dependency: %v", err)
		}
		if err := prepareManifestEnvironments(&manifest, []string{"api"}, []string{"PATH=/bin"}, true); err == nil || !strings.Contains(err.Error(), "4 MiB") {
			t.Fatalf("up dependency preflight error=%v", err)
		}
	})

	t.Run("raw and encoded bounds", func(t *testing.T) {
		root := t.TempDir()
		encodedLarge := strings.Repeat("\n", (4<<20)-7)
		manifest := manifestState{root: root, defs: []project.Definition{{Name: "api", Environment: &project.EnvironmentSpec{Inherit: false, Values: map[string]*string{"VALUE": &encodedLarge}, BaseDir: root, Root: root}}}}
		if err := prepareManifestEnvironments(&manifest, []string{"api"}, nil, false); err == nil || !strings.Contains(err.Error(), "request is too large") {
			t.Fatalf("encoded bound error=%v", err)
		}
		rawLarge := strings.Repeat("x", 4<<20)
		manifest.defs[0].Environment.Values["VALUE"] = &rawLarge
		if err := prepareManifestEnvironments(&manifest, []string{"api"}, nil, false); err == nil || !strings.Contains(err.Error(), "4 MiB") {
			t.Fatalf("raw bound error=%v", err)
		}
	})

	t.Run("no-op adds no preflight bound", func(t *testing.T) {
		root := t.TempDir()
		baseline := []string{"VALUE=" + strings.Repeat("x", 8<<20)}
		manifest := manifestState{root: root, defs: []project.Definition{{Name: "api"}}}
		if err := prepareManifestEnvironments(&manifest, []string{"api"}, baseline, false); err != nil {
			t.Fatalf("no-op baseline was newly bounded: %v", err)
		}
		if !reflect.DeepEqual(manifest.environments["api"], baseline) {
			t.Fatal("no-op baseline changed")
		}
	})

	for _, command := range [][]string{{"start", "api", "--no-wait", "--json"}, {"run", "api", "--detach", "--json"}, {"restart", "api", "--no-wait", "--json"}, {"up", "--no-wait", "--json"}} {
		command := command
		t.Run("zero contact "+command[0], func(t *testing.T) {
			root := stopShutdownTestProject(t)
			runtimeDir := t.TempDir()
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			writeManifestCLITestFile(t, root, "version: 1\nenvironment:\n  files: [missing.env]\nprocesses:\n  api:\n    argv: [/bin/sh, -c, 'sleep 30']\n")
			stdout, stderr, err := stopShutdownRun(t, command...)
			if err == nil || !strings.Contains(stdout+stderr+err.Error(), "missing.env") {
				t.Fatalf("command error=%v stdout=%q stderr=%q", err, stdout, stderr)
			}
			assertDownRuntimeAbsent(t, runtimeDir)
		})
	}

	t.Run("read-only ignores missing file", func(t *testing.T) {
		root := stopShutdownTestProject(t)
		runtimeDir := t.TempDir()
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		writeManifestCLITestFile(t, root, "version: 1\nenvironment:\n  files: [missing.env]\nprocesses:\n  api:\n    argv: [/bin/sh, -c, 'sleep 30']\n")
		for _, command := range [][]string{{"list", "--json"}, {"status", "api", "--json"}} {
			stdout, stderr, err := stopShutdownRun(t, command...)
			if strings.Contains(stdout+stderr, "missing.env") || err != nil && strings.Contains(err.Error(), "missing.env") {
				t.Fatalf("%s accessed environment file: err=%v stdout=%q stderr=%q", command[0], err, stdout, stderr)
			}
		}
	})
}
