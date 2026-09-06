package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestartPolicyManifest(t *testing.T) {
	root := t.TempDir()
	for _, value := range []string{"never", "on-failure"} {
		writeTestManifest(t, root, "version: 1\nprocesses:\n  api:\n    argv: [server]\n    restart: "+value+"\n")
		definitions, err := LoadDefinitions(root)
		if err != nil {
			t.Fatalf("restart %s: %v", value, err)
		}
		if len(definitions) != 1 || string(definitions[0].Restart) != value {
			t.Fatalf("restart %s definitions = %#v", value, definitions)
		}
	}
	writeTestManifest(t, root, "version: 1\nprocesses:\n  api:\n    argv: [server]\n")
	definitions, err := LoadDefinitions(root)
	if err != nil || len(definitions) != 1 || definitions[0].Restart != RestartNever {
		t.Fatalf("default restart = %#v, err %v", definitions, err)
	}

	for _, value := range []string{"always", "on-success", "1", "null"} {
		writeTestManifest(t, root, "version: 1\nprocesses:\n  api:\n    argv: [server]\n    restart: "+value+"\n")
		_, err := LoadDefinitions(root)
		if err == nil || !strings.Contains(err.Error(), "hum.yaml") || !strings.Contains(err.Error(), `process "api"`) || !strings.Contains(err.Error(), "restart") {
			t.Fatalf("invalid restart %s error = %v", value, err)
		}
	}
	writeTestManifest(t, root, "version: 1\nprocesses:\n  api:\n    argv: [server]\n    restart: 7\n")
	if _, err := LoadDefinitions(root); err == nil || !strings.Contains(err.Error(), "process \"api\"") || !strings.Contains(err.Error(), "restart") {
		t.Fatalf("non-string restart error = %v", err)
	}

	// Conventional discovery is hard to invoke without external tools; its
	// constructor is the single policy boundary and is covered directly here.
	discovered := discoveredDefinition(root, "package_json", "npm", "run", "dev")
	if discovered.Restart != RestartNever {
		t.Fatalf("discovered restart = %q", discovered.Restart)
	}

	// Both generated and template init manifests keep the example inert.
	if err := os.Remove(filepath.Join(root, "hum.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"dev":"server"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	installDiscoveryStubs(t, nil)
	result, err := InitManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "# restart: on-failure") {
		t.Fatalf("generated template omitted inert restart example: %s", contents)
	}
	if _, err := LoadDefinitions(root); err != nil {
		t.Fatalf("generated manifest invalid: %v", err)
	}
}
