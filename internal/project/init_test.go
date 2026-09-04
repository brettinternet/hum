package project

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInitSingleCandidate(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	start := filepath.Join(root, "nested", "leaf")
	if err := os.MkdirAll(start, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, root, "package.json", `{"scripts":{"dev":"echo ready: with spaces"}}`, 0o600)
	installDiscoveryStubs(t, nil)

	result, err := InitManifest(start)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "hum.yaml")
	if result.Path != manifestPath {
		t.Fatalf("path = %q, want %q", result.Path, manifestPath)
	}
	if result.Outcome != InitOutcomeGenerated {
		t.Fatalf("outcome = %q, want %q", result.Outcome, InitOutcomeGenerated)
	}
	wantCandidate := []Definition{{Name: "dev", Source: "package_json", Argv: []string{"npm", "run", "dev"}, Cwd: root}}
	if !reflect.DeepEqual(result.Candidates, wantCandidate) {
		t.Fatalf("candidates = %#v, want %#v", result.Candidates, wantCandidate)
	}

	contents, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, expected := range []string{
		"  \"dev\":\n",
		"    # source: package_json\n",
		"      - \"npm\"\n",
		"      - \"run\"\n",
		"      - \"dev\"\n",
		"    # ready:\n",
		"    #   match: \"Local:\"\n",
		"    #   timeout: 30s\n",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("manifest = %q, want substring %q", text, expected)
		}
	}
	info, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("manifest mode = %o, want 600", got)
	}

	definitions, err := LoadDefinitions(root)
	if err != nil {
		t.Fatal(err)
	}
	wantLoaded := []Definition{{Name: "dev", Source: "manifest", Argv: []string{"npm", "run", "dev"}, Cwd: root}}
	if !reflect.DeepEqual(definitions, wantLoaded) {
		t.Fatalf("loaded definitions = %#v, want %#v", definitions, wantLoaded)
	}
}

func TestInitTemplates(t *testing.T) {
	t.Run("no candidate", func(t *testing.T) {
		root := t.TempDir()
		installDiscoveryStubs(t, nil)

		result, err := InitManifest(root)
		if err != nil {
			t.Fatal(err)
		}
		if result.Path != filepath.Join(root, "hum.yaml") {
			t.Fatalf("path = %q, want %q", result.Path, filepath.Join(root, "hum.yaml"))
		}
		if result.Outcome != InitOutcomeTemplate {
			t.Fatalf("outcome = %q, want %q", result.Outcome, InitOutcomeTemplate)
		}
		if result.Candidates == nil || len(result.Candidates) != 0 {
			t.Fatalf("candidates = %#v, want non-nil empty slice", result.Candidates)
		}
		assertInitTemplate(t, root, []string{"no candidate was detected", "# No detected candidates."})
	})

	t.Run("ambiguous candidates", func(t *testing.T) {
		root := t.TempDir()
		writeDiscoveryFile(t, root, "package.json", `{"scripts":{"dev":"echo package"}}`, 0o600)
		writeDiscoveryFile(t, root, "deno.json", `{"tasks":{"dev":"echo deno"}}`, 0o600)
		installDiscoveryStubs(t, nil)

		result, err := InitManifest(root)
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != InitOutcomeTemplate {
			t.Fatalf("outcome = %q, want %q", result.Outcome, InitOutcomeTemplate)
		}
		wantCandidates := []Definition{
			{Name: "dev", Source: "package_json", Argv: []string{"npm", "run", "dev"}, Cwd: root},
			{Name: "dev", Source: "deno_json", Argv: []string{"deno", "task", "dev"}, Cwd: root},
		}
		if !reflect.DeepEqual(result.Candidates, wantCandidates) {
			t.Fatalf("candidates = %#v, want %#v", result.Candidates, wantCandidates)
		}
		assertInitTemplate(t, root, []string{
			"ambiguous candidates were detected",
			"# - source: package_json",
			"#   argv: [\"npm\", \"run\", \"dev\"]",
			"# - source: deno_json",
			"#   argv: [\"deno\", \"task\", \"dev\"]",
		})
	})
}

func assertInitTemplate(t *testing.T, root string, expected []string) {
	t.Helper()
	manifestPath := filepath.Join(root, "hum.yaml")
	contents, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, substring := range expected {
		if !strings.Contains(text, substring) {
			t.Fatalf("template = %q, want substring %q", text, substring)
		}
	}
	if got := strings.Count(text, "#   \"dev\":\n"); got != 1 {
		t.Fatalf("template example entries = %d, want 1", got)
	}
	definitions, err := LoadDefinitions(root)
	if err != nil {
		t.Fatalf("template does not pass strict parser: %v", err)
	}
	if definitions == nil || len(definitions) != 0 {
		t.Fatalf("template definitions = %#v, want non-nil empty slice", definitions)
	}
}

func TestInitExistingManifest(t *testing.T) {
	tests := []struct {
		name string
		make func(t *testing.T, path string) []byte
	}{
		{
			name: "regular file",
			make: func(t *testing.T, path string) []byte {
				contents := []byte("not a manifest, and must stay byte-for-byte unchanged\n")
				if err := os.WriteFile(path, contents, 0o640); err != nil {
					t.Fatal(err)
				}
				return contents
			},
		},
		{
			name: "dangling symlink",
			make: func(t *testing.T, path string) []byte {
				if err := os.Symlink("missing-hum-yaml", path); err != nil {
					t.Fatal(err)
				}
				return nil
			},
		},
		{
			name: "nonregular directory",
			make: func(t *testing.T, path string) []byte {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				return nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			manifestPath := filepath.Join(root, "hum.yaml")
			original := test.make(t, manifestPath)
			calls := installDiscoveryStubs(t, map[string]discoveryStub{
				"mise": {output: []byte(`[{"name":"dev"}]`)},
			})

			result, err := InitManifest(root)
			if err == nil {
				t.Fatal("InitManifest unexpectedly succeeded")
			}
			var exists *ManifestExistsError
			if !errors.As(err, &exists) {
				t.Fatalf("error = %v, want ManifestExistsError", err)
			}
			if !errors.Is(err, ErrManifestExists) {
				t.Fatalf("error = %v, want ErrManifestExists", err)
			}
			if exists.Path != manifestPath {
				t.Fatalf("existing path = %q, want %q", exists.Path, manifestPath)
			}
			if result.Path != manifestPath || result.Outcome != InitOutcomeExists {
				t.Fatalf("result = %#v, want path %q and outcome %q", result, manifestPath, InitOutcomeExists)
			}
			if len(*calls) != 0 {
				t.Fatalf("discovery calls = %#v, want none", *calls)
			}

			if test.name == "regular file" {
				contents, readErr := os.ReadFile(manifestPath)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if !reflect.DeepEqual(contents, original) {
					t.Fatalf("manifest bytes = %q, want %q", contents, original)
				}
			} else {
				if _, statErr := os.Lstat(manifestPath); statErr != nil {
					t.Fatalf("existing %s disappeared: %v", test.name, statErr)
				}
			}
		})
	}
}

func TestInitManifestRace(t *testing.T) {
	root := t.TempDir()
	installDiscoveryStubs(t, nil)
	manifestPath := filepath.Join(root, "hum.yaml")
	raced := []byte("raced\n")

	oldLink := initLink
	t.Cleanup(func() {
		initLink = oldLink
	})
	initLink = func(tempPath, targetPath string) error {
		if err := os.WriteFile(targetPath, raced, 0o600); err != nil {
			return err
		}
		return os.Link(tempPath, targetPath)
	}

	result, err := InitManifest(root)
	if err == nil {
		t.Fatal("InitManifest unexpectedly succeeded after a publication race")
	}
	var exists *ManifestExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("error = %v, want ManifestExistsError", err)
	}
	if result.Path != manifestPath || result.Outcome != InitOutcomeExists {
		t.Fatalf("result = %#v, want raced existing result", result)
	}
	contents, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(contents, raced) {
		t.Fatalf("raced manifest = %q, want original bytes", contents)
	}
}

func TestInitWriteFailureLeavesNoManifest(t *testing.T) {
	root := t.TempDir()
	installDiscoveryStubs(t, nil)
	manifestPath := filepath.Join(root, "hum.yaml")

	oldWrite := initWrite
	t.Cleanup(func() {
		initWrite = oldWrite
	})
	initWrite = func(*os.File, []byte) (int, error) {
		return 0, errors.New("injected write failure")
	}

	if _, err := InitManifest(root); err == nil {
		t.Fatal("InitManifest unexpectedly succeeded after an injected write failure")
	}
	if _, statErr := os.Lstat(manifestPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("hum.yaml stat error = %v, want not exists", statErr)
	}
}

func TestInitDiscoveryFailureLeavesNoManifest(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryFile(t, root, "package.json", "{\n", 0o600)
	installDiscoveryStubs(t, nil)

	result, err := InitManifest(root)
	if err == nil {
		t.Fatal("InitManifest unexpectedly succeeded")
	}
	var configuration *ConfigurationError
	if !errors.As(err, &configuration) {
		t.Fatalf("error = %v, want ConfigurationError", err)
	}
	if !reflect.DeepEqual(result, InitResult{}) {
		t.Fatalf("result = %#v, want zero result", result)
	}
	if _, statErr := os.Lstat(filepath.Join(root, "hum.yaml")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("hum.yaml stat error = %v, want not exists", statErr)
	}
}
