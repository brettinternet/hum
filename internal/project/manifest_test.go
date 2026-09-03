package project

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeTestManifest(t *testing.T, root, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDefinitionsManifest(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"api", "web"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestManifest(t, root, `version: 1
processes:
  web:
    argv: [go, run, ./web]
    cwd: ./web/../web
    ready:
      match: "ready:"
  api:
    argv:
      - go
      - run
      - ./api
      - --port=8080
    ready:
      match: "listening"
      timeout: 2s
`)

	definitions, err := LoadDefinitions(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []Definition{
		{
			Name:   "api",
			Source: "manifest",
			Argv:   []string{"go", "run", "./api", "--port=8080"},
			Cwd:    root,
			Ready:  &ReadyDefinition{Match: "listening", Timeout: 2 * time.Second},
		},
		{
			Name:   "web",
			Source: "manifest",
			Argv:   []string{"go", "run", "./web"},
			Cwd:    filepath.Join(root, "web"),
			Ready:  &ReadyDefinition{Match: "ready:", Timeout: 30 * time.Second},
		},
	}
	if !reflect.DeepEqual(definitions, want) {
		t.Fatalf("definitions = %#v, want %#v", definitions, want)
	}

	definitions[0].Argv[0] = "mutated"
	definitions[0].Ready.Match = "mutated"
	again, err := LoadDefinitions(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("reloaded definitions = %#v, want fresh normalized values %#v", again, want)
	}
}

func TestLoadDefinitionsIgnoresAlternateManifestNames(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"hum.json", "hum.toml", "hum.yml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("not a supported manifest"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	definitions, err := LoadDefinitions(root)
	if err != nil {
		t.Fatal(err)
	}
	if definitions == nil || len(definitions) != 0 {
		t.Fatalf("definitions = %#v, want a non-nil empty result", definitions)
	}
}

func TestLoadDefinitionsEmptyProcesses(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "version: 1\nprocesses: {}\n")
	definitions, err := LoadDefinitions(root)
	if err != nil {
		t.Fatal(err)
	}
	if definitions == nil || len(definitions) != 0 {
		t.Fatalf("definitions = %#v, want a non-nil empty result", definitions)
	}
}

func TestLoadDefinitionsAcceptsYAMLFlowSyntax(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "{version: 1, processes: {}}\n")
	definitions, err := LoadDefinitions(root)
	if err != nil {
		t.Fatal(err)
	}
	if definitions == nil || len(definitions) != 0 {
		t.Fatalf("definitions = %#v, want a non-nil empty result", definitions)
	}
}

func TestLoadDefinitionsStrictRejections(t *testing.T) {
	cases := []struct {
		name      string
		manifest  string
		entry     string
		setup     func(t *testing.T, root string)
		wantError string
	}{
		{
			name:      "empty document",
			manifest:  "",
			wantError: "document is empty",
		},
		{
			name:      "JSON document",
			manifest:  `{"version":1,"processes":{}}`,
			wantError: "unsupported format",
		},
		{
			name:      "BOM-prefixed JSON document",
			manifest:  "\xef\xbb\xbf \n{\"version\":1,\"processes\":{}}",
			wantError: "unsupported format",
		},
		{
			name:      "root is not a mapping",
			manifest:  "[]\n",
			wantError: "manifest",
		},
		{
			name:      "missing version",
			manifest:  "processes: {}\n",
			wantError: "version",
		},
		{
			name:      "missing processes",
			manifest:  "version: 1\n",
			wantError: "processes",
		},
		{
			name:      "unsupported version",
			manifest:  "version: 2\nprocesses: {}\n",
			wantError: "unsupported version",
		},
		{
			name:      "string version",
			manifest:  "version: \"1\"\nprocesses: {}\n",
			wantError: "version",
		},
		{
			name:      "unknown root key",
			manifest:  "version: 1\nprocesses: {}\nextra: true\n",
			wantError: "unknown key",
		},
		{
			name:      "duplicate root key",
			manifest:  "version: 1\nversion: 1\nprocesses: {}\n",
			wantError: "duplicate key",
		},
		{
			name:      "multiple documents",
			manifest:  "---\nversion: 1\nprocesses: {}\n---\nversion: 1\nprocesses: {}\n",
			wantError: "multiple",
		},
		{
			name:      "scalar shell command",
			manifest:  "version: 1\nprocesses:\n  web: npm run dev\n",
			entry:     "web",
			wantError: "mapping",
		},
		{
			name:      "unknown process key",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    env: {}\n",
			entry:     "web",
			wantError: "unknown key",
		},
		{
			name:      "duplicate process key",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    argv: [run]\n",
			entry:     "web",
			wantError: "duplicate key",
		},
		{
			name:      "missing argv",
			manifest:  "version: 1\nprocesses:\n  web:\n    cwd: .\n",
			entry:     "web",
			wantError: "argv",
		},
		{
			name:      "scalar argv",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: go run web\n",
			entry:     "web",
			wantError: "sequence",
		},
		{
			name:      "empty argv",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: []\n",
			entry:     "web",
			wantError: "non-empty",
		},
		{
			name:      "empty argv element",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go, \"\"]\n",
			entry:     "web",
			wantError: "must not be empty",
		},
		{
			name:      "non-string argv element",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go, 7]\n",
			entry:     "web",
			wantError: "must be a string",
		},
		{
			name:      "non-string cwd",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    cwd: 7\n",
			entry:     "web",
			wantError: "cwd",
		},
		{
			name:      "invalid name leading punctuation",
			manifest:  "version: 1\nprocesses:\n  _web:\n    argv: [go]\n",
			entry:     "_web",
			wantError: "invalid process name",
		},
		{
			name:      "invalid name too long",
			manifest:  "version: 1\nprocesses:\n  aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa:\n    argv: [go]\n",
			entry:     "aaaa",
			wantError: "invalid process name",
		},
		{
			name:      "invalid absolute cwd",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    cwd: /tmp\n",
			entry:     "web",
			wantError: "relative",
		},
		{
			name:      "lexical cwd escape",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    cwd: ../outside\n",
			entry:     "web",
			wantError: "escapes",
		},
		{
			name:      "missing cwd directory",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    cwd: missing\n",
			entry:     "web",
			wantError: "existing directory",
		},
		{
			name:     "cwd is not a directory",
			manifest: "version: 1\nprocesses:\n  web:\n    argv: [go]\n    cwd: file\n",
			entry:    "web",
			setup: func(t *testing.T, root string) {
				if err := os.WriteFile(filepath.Join(root, "file"), []byte("file"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: "not a directory",
		},
		{
			name:      "ready is not a mapping",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    ready: ready\n",
			entry:     "web",
			wantError: "mapping",
		},
		{
			name:      "missing ready match",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    ready: {timeout: 1s}\n",
			entry:     "web",
			wantError: "match",
		},
		{
			name:      "unknown ready key",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    ready: {match: ok, retries: 2}\n",
			entry:     "web",
			wantError: "unknown key",
		},
		{
			name:      "duplicate ready key",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    ready:\n      match: ok\n      match: still-ok\n",
			entry:     "web",
			wantError: "duplicate key",
		},
		{
			name:      "non-string ready match",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    ready: {match: 7}\n",
			entry:     "web",
			wantError: "match",
		},
		{
			name:      "invalid ready regex",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    ready: {match: \"[\"}\n",
			entry:     "web",
			wantError: "regular expression",
		},
		{
			name:      "invalid ready duration",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    ready: {match: ok, timeout: later}\n",
			entry:     "web",
			wantError: "invalid timeout",
		},
		{
			name:      "negative ready duration",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    ready: {match: ok, timeout: -1s}\n",
			entry:     "web",
			wantError: "negative",
		},
		{
			name:      "zero ready duration",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    ready: {match: ok, timeout: 0s}\n",
			entry:     "web",
			wantError: "positive",
		},
		{
			name:      "negative zero ready duration",
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    ready: {match: ok, timeout: -0s}\n",
			entry:     "web",
			wantError: "positive",
		},
		{
			name:      "alias",
			manifest:  "version: 1\nprocesses:\n  base: &base\n    argv: [go]\n  web: *base\n",
			entry:     "web",
			wantError: "aliases",
		},
		{
			name:      "merge key",
			manifest:  "version: 1\nprocesses:\n  web:\n    <<: {argv: [go]}\n    argv: [go]\n",
			entry:     "web",
			wantError: "merge",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if test.setup != nil {
				test.setup(t, root)
			}
			writeTestManifest(t, root, test.manifest)
			_, err := LoadDefinitions(root)
			if err == nil {
				t.Fatal("LoadDefinitions succeeded; want strict manifest error")
			}
			if !strings.Contains(err.Error(), "hum.yaml") {
				t.Fatalf("error = %q, want hum.yaml", err)
			}
			if test.entry != "" && !strings.Contains(err.Error(), test.entry) {
				t.Fatalf("error = %q, want failing entry %q", err, test.entry)
			}
			if test.wantError != "" && !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestLoadDefinitionsCwdSymlinkResolution(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	writeTestManifest(t, root, "version: 1\nprocesses:\n  web:\n    argv: [go]\n    cwd: outside\n")
	_, err := LoadDefinitions(root)
	if err == nil || !strings.Contains(err.Error(), "web") || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("error = %v, want symlink escape naming web and outside", err)
	}
}

func TestLoadDefinitionsAcceptsRootRelativeCwdSymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	writeTestManifest(t, root, "version: 1\nprocesses:\n  web:\n    argv: [go]\n    cwd: link\n")
	definitions, err := LoadDefinitions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || definitions[0].Cwd != filepath.Join(root, "link") {
		t.Fatalf("definitions = %#v, want lexical symlink cwd", definitions)
	}
}
