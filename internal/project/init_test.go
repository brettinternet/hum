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
	wantCandidate := []Definition{{Name: "dev", Source: "package_json", Argv: []string{"npm", "run", "dev"}, Cwd: root, After: []string{}, Restart: RestartNever}}
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
	wantLoaded := []Definition{{Name: "dev", Source: "manifest", Argv: []string{"npm", "run", "dev"}, Cwd: root, After: []string{}, Restart: RestartNever}}
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
		assertInitTemplate(t, root, []string{"no candidate was detected", "# No detected candidates.", "# Add a process entry"})
		assertInitTemplateExample(t, root)
		contents, err := os.ReadFile(filepath.Join(root, "hum.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(contents), "Replace the example below with one of the detected candidates") {
			t.Fatalf("no-candidate template wrongly references detected candidates: %q", contents)
		}
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
			{Name: "dev", Source: "package_json", Argv: []string{"npm", "run", "dev"}, Cwd: root, After: []string{}, Restart: RestartNever},
			{Name: "dev", Source: "deno_json", Argv: []string{"deno", "task", "dev"}, Cwd: root, After: []string{}, Restart: RestartNever},
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
			"# Replace the example below with one of the detected candidates.",
		})
		assertInitTemplateExample(t, root)
	})
}

// assertInitTemplateExample proves the template's commented example manifest
// entry parses as a valid, standalone process declaration once uncommented,
// so following the template's own instructions cannot yield a manifest
// error (e.g. a dangling after dependency).
func assertInitTemplateExample(t *testing.T, root string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(root, "hum.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(contents), "\n")
	start := -1
	for i, line := range lines {
		if line == "# Example:" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("template missing \"# Example:\" marker: %q", contents)
	}
	var uncommented []string
	for _, line := range lines[start:] {
		if line == "version: 1" {
			break
		}
		if !strings.HasPrefix(line, "# ") {
			t.Fatalf("template example line missing comment prefix: %q", line)
		}
		uncommented = append(uncommented, strings.TrimPrefix(line, "# "))
	}
	manifest := "version: 1\nprocesses:\n" + strings.Join(uncommented, "\n") + "\n"
	exampleRoot := t.TempDir()
	writeTestManifest(t, exampleRoot, manifest)
	definitions, err := LoadDefinitions(exampleRoot)
	if err != nil {
		t.Fatalf("template example does not parse once uncommented: %v\nmanifest:\n%s", err, manifest)
	}
	if len(definitions) != 1 || definitions[0].Name != "dev" || !reflect.DeepEqual(definitions[0].Argv, []string{"command"}) {
		t.Fatalf("template example definitions = %#v, want one dev process with argv [command]", definitions)
	}
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

func TestInitManifestForceReplace(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "hum.yaml")
	original := []byte("old manifest\n")
	if err := os.WriteFile(manifestPath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, root, "package.json", `{"scripts":{"dev":"echo replacement"}}`, 0o600)
	installDiscoveryStubs(t, nil)

	oldRender, oldWrite := initRender, initWrite
	oldSync, oldClose := initSync, initClose
	oldRename := initRename
	t.Cleanup(func() {
		initRender = oldRender
		initWrite = oldWrite
		initSync = oldSync
		initClose = oldClose
		initRename = oldRename
	})
	var events []string
	var temporaryPath string
	initWrite = func(file *os.File, contents []byte) (int, error) {
		n, err := oldWrite(file, contents)
		if err == nil {
			temporaryPath = file.Name()
			events = append(events, "write")
			info, statErr := file.Stat()
			if statErr != nil {
				t.Fatalf("stat temporary manifest: %v", statErr)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("temporary manifest mode = %04o, want 0600", got)
			}
			if filepath.Dir(temporaryPath) != root {
				t.Fatalf("temporary manifest directory = %q, want %q", filepath.Dir(temporaryPath), root)
			}
		}
		return n, err
	}
	initSync = func(file *os.File) error {
		err := oldSync(file)
		if err == nil {
			events = append(events, "sync")
		}
		return err
	}
	initClose = func(file *os.File) error {
		err := oldClose(file)
		if err == nil {
			events = append(events, "close")
		}
		return err
	}
	const recreatedTemp = "unrelated file recreated after rename\n"
	initRename = func(tempPath, targetPath string) error {
		if tempPath != temporaryPath || targetPath != manifestPath {
			t.Fatalf("rename paths = (%q, %q), want (%q, %q)", tempPath, targetPath, temporaryPath, manifestPath)
		}
		if !reflect.DeepEqual(events, []string{"write", "sync", "close"}) {
			t.Fatalf("publication events = %#v, want write/sync/close before rename", events)
		}
		contents, err := os.ReadFile(tempPath)
		if err != nil {
			t.Fatalf("read closed temporary manifest: %v", err)
		}
		if !strings.Contains(string(contents), "npm") || !strings.Contains(string(contents), "package_json") {
			t.Fatalf("temporary manifest contents = %q, want replacement candidate", contents)
		}
		current, err := os.ReadFile(targetPath)
		if err != nil {
			t.Fatalf("read original manifest before rename: %v", err)
		}
		if !reflect.DeepEqual(current, original) {
			t.Fatalf("original manifest before rename = %q, want %q", current, original)
		}
		if err := oldRename(tempPath, targetPath); err != nil {
			return err
		}
		return os.WriteFile(tempPath, []byte(recreatedTemp), 0o600)
	}

	result, err := InitManifest(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != manifestPath || result.Outcome != InitOutcomeReplaced {
		t.Fatalf("result = %#v, want path %q and outcome %q", result, manifestPath, InitOutcomeReplaced)
	}
	contents, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(contents, original) || !strings.Contains(string(contents), "package_json") {
		t.Fatalf("replacement manifest contents = %q, want generated replacement", contents)
	}
	info, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("replacement manifest mode = %04o, want 0600", got)
	}
	recreated, err := os.ReadFile(temporaryPath)
	if err != nil {
		t.Fatalf("read unrelated recreated temp path: %v", err)
	}
	if string(recreated) != recreatedTemp {
		t.Fatalf("recreated temp contents = %q, want %q", recreated, recreatedTemp)
	}
	if err := os.Remove(temporaryPath); err != nil {
		t.Fatalf("remove unrelated recreated temp path: %v", err)
	}
	initAssertNoTemp(t, root)
}

func TestInitManifestForcePreservesOriginalOnFailure(t *testing.T) {
	const originalText = "original manifest bytes\x00\xff\n"

	t.Run("discovery", func(t *testing.T) {
		root, manifestPath, original := initForceFailureProject(t, originalText)
		writeDiscoveryFile(t, root, "package.json", "{\n", 0o600)
		installDiscoveryStubs(t, nil)
		initForceAssertFailure(t, root, manifestPath, original, "discovery")
	})

	t.Run("render", func(t *testing.T) {
		root, manifestPath, original := initForceFailureProject(t, originalText)
		installDiscoveryStubs(t, nil)
		oldRender := initRender
		t.Cleanup(func() { initRender = oldRender })
		initRender = func([]Definition, InitOutcome, string) ([]byte, error) {
			return nil, errors.New("injected render failure")
		}
		initForceAssertFailure(t, root, manifestPath, original, "render")
	})

	t.Run("write", func(t *testing.T) {
		root, manifestPath, original := initForceFailureProject(t, originalText)
		installDiscoveryStubs(t, nil)
		oldWrite := initWrite
		t.Cleanup(func() { initWrite = oldWrite })
		initWrite = func(*os.File, []byte) (int, error) {
			return 0, errors.New("injected write failure")
		}
		initForceAssertFailure(t, root, manifestPath, original, "write")
	})

	t.Run("sync", func(t *testing.T) {
		root, manifestPath, original := initForceFailureProject(t, originalText)
		installDiscoveryStubs(t, nil)
		oldSync := initSync
		t.Cleanup(func() { initSync = oldSync })
		initSync = func(*os.File) error { return errors.New("injected sync failure") }
		initForceAssertFailure(t, root, manifestPath, original, "sync")
	})

	t.Run("close", func(t *testing.T) {
		root, manifestPath, original := initForceFailureProject(t, originalText)
		installDiscoveryStubs(t, nil)
		oldClose := initClose
		t.Cleanup(func() { initClose = oldClose })
		first := true
		initClose = func(file *os.File) error {
			if first {
				first = false
				return errors.New("injected close failure")
			}
			return oldClose(file)
		}
		initForceAssertFailure(t, root, manifestPath, original, "close")
	})

	t.Run("rename", func(t *testing.T) {
		root, manifestPath, original := initForceFailureProject(t, originalText)
		installDiscoveryStubs(t, nil)
		oldRename := initRename
		t.Cleanup(func() { initRename = oldRename })
		initRename = func(string, string) error { return errors.New("injected rename failure") }
		initForceAssertFailure(t, root, manifestPath, original, "rename")
	})

	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		manifestPath := filepath.Join(root, "hum.yaml")
		targetPath := filepath.Join(root, "original")
		original := []byte(originalText)
		if err := os.WriteFile(targetPath, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Base(targetPath), manifestPath); err != nil {
			t.Fatal(err)
		}
		installDiscoveryStubs(t, nil)
		if _, err := InitManifest(root, true); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("symlink error = %v, want actionable symlink refusal", err)
		}
		if got, err := os.Readlink(manifestPath); err != nil || got != filepath.Base(targetPath) {
			t.Fatalf("symlink target = %q, %v; want %q", got, err, filepath.Base(targetPath))
		}
		initForceAssertBytes(t, targetPath, original)
		initAssertNoTemp(t, root)
	})

	t.Run("non-regular directory", func(t *testing.T) {
		root := t.TempDir()
		manifestPath := filepath.Join(root, "hum.yaml")
		if err := os.Mkdir(manifestPath, 0o700); err != nil {
			t.Fatal(err)
		}
		installDiscoveryStubs(t, nil)
		if _, err := InitManifest(root, true); err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("non-regular error = %v, want actionable regular-file refusal", err)
		}
		info, err := os.Stat(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Fatalf("target mode = %s, want directory", info.Mode())
		}
		initAssertNoTemp(t, root)
	})
}

func initForceFailureProject(t *testing.T, originalText string) (string, string, []byte) {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "hum.yaml")
	original := []byte(originalText)
	if err := os.WriteFile(manifestPath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	return root, manifestPath, original
}

func initForceAssertFailure(t *testing.T, root, manifestPath string, original []byte, stage string) {
	t.Helper()
	if _, err := InitManifest(root, true); err == nil || !strings.Contains(strings.ToLower(err.Error()), stage) {
		t.Fatalf("%s error = %v, want %s failure", stage, err, stage)
	}
	initForceAssertBytes(t, manifestPath, original)
	initAssertNoTemp(t, root)
}

func initForceAssertBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s bytes = %q, want %q", path, got, want)
	}
}

func initAssertNoTemp(t *testing.T, root string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, ".hum.yaml.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary manifests = %v, want none", matches)
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
