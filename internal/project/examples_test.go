package project

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExampleManifestsLoad keeps the published examples accepted by the real
// parser and environment composition, so they cannot drift from the schema.
func TestExampleManifestsLoad(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	manifests, err := filepath.Glob(filepath.Join(repo, "examples", "*", "hum.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) == 0 {
		t.Fatal("no example manifests found")
	}
	for _, manifest := range manifests {
		name := filepath.Base(filepath.Dir(manifest))
		t.Run(name, func(t *testing.T) {
			if _, err := os.Stat(filepath.Join(filepath.Dir(manifest), "README.md")); err != nil {
				t.Fatalf("example README: %v", err)
			}
			definitions, err := LoadDefinitionsFile(repo, manifest, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(definitions) == 0 {
				t.Fatal("example declares no processes")
			}
			if _, err := PrepareEnvironments(definitions, nil); err != nil {
				t.Fatal(err)
			}
		})
	}

	t.Run("hum.example.yaml", func(t *testing.T) {
		contents, err := os.ReadFile(filepath.Join(repo, "hum.example.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		// The reference manifest names a web/ cwd that exists only in a real project.
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "web"), 0o700); err != nil {
			t.Fatal(err)
		}
		writeTestManifest(t, root, string(contents))
		if _, err := LoadDefinitions(root); err != nil {
			t.Fatal(err)
		}
	})
}
