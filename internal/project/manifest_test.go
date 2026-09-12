package project

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestManifestEnvironmentContract(t *testing.T) {
	root := t.TempDir()

	t.Run("complete manifest", func(t *testing.T) {
		writeTestManifest(t, root, "version: 1\nenvironment:\n  inherit: false\n  files: [.env]\nprocesses:\n  api:\n    argv: [bun, run, api]\n    env:\n      PORT: \"3000\"\n      EMPTY: \"\"\n      LITERAL: '$NAME'\n      OLD_URL: null\n")
		defs, err := LoadDefinitions(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(defs) != 1 || defs[0].Environment == nil || defs[0].Environment.Inherit {
			t.Fatalf("environment=%#v", defs)
		}
		values := defs[0].Environment.Values
		if values["OLD_URL"] != nil || *values["PORT"] != "3000" || *values["EMPTY"] != "" || *values["LITERAL"] != "$NAME" {
			t.Fatalf("values=%#v", values)
		}
	})

	t.Run("defaults are no-op", func(t *testing.T) {
		for _, environment := range []string{"", "environment: {}\n"} {
			writeTestManifest(t, root, "version: 1\n"+environment+"processes:\n  api:\n    argv: [api]\n    env: {}\n")
			defs, err := LoadDefinitions(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(defs) != 1 || defs[0].Environment != nil && !environmentNoop(defs[0].Environment) {
				t.Fatalf("default environment=%#v", defs)
			}
		}
	})

	invalid := []struct {
		name string
		body string
	}{
		{name: "environment scalar", body: "environment: true\nprocesses: {}\n"},
		{name: "unknown environment key", body: "environment: {optional: true}\nprocesses: {}\n"},
		{name: "inherit string", body: "environment: {inherit: 'true'}\nprocesses: {}\n"},
		{name: "inherit integer", body: "environment: {inherit: 1}\nprocesses: {}\n"},
		{name: "files scalar", body: "environment: {files: .env}\nprocesses: {}\n"},
		{name: "empty file", body: "environment: {files: ['']}\nprocesses: {}\n"},
		{name: "absolute file", body: "environment: {files: [/tmp/private.env]}\nprocesses: {}\n"},
		{name: "root file", body: "environment: {files: [.]}\nprocesses: {}\n"},
		{name: "escaping file", body: "environment: {files: [../private.env]}\nprocesses: {}\n"},
		{name: "too many files", body: "environment: {files: [a,b,c,d,e,f,g,h,i,j,k,l,m,n,o,p,q]}\nprocesses: {}\n"},
		{name: "env sequence", body: "processes:\n  api: {argv: [api], env: []}\n"},
		{name: "env numeric", body: "processes:\n  api: {argv: [api], env: {PORT: 3000}}\n"},
		{name: "env boolean", body: "processes:\n  api: {argv: [api], env: {DEBUG: true}}\n"},
		{name: "env object", body: "processes:\n  api: {argv: [api], env: {KEY: {secret: SECRET_VALUE}}}\n"},
		{name: "invalid key", body: "processes:\n  api: {argv: [api], env: {BAD-KEY: SECRET_VALUE}}\n"},
		{name: "duplicate key", body: "processes:\n  api:\n    argv: [api]\n    env:\n      KEY: first\n      KEY: SECRET_VALUE\n"},
		{name: "unknown process key", body: "processes:\n  api: {argv: [api], environment: SECRET_VALUE}\n"},
		{name: "malformed YAML", body: "processes:\n  api: {argv: [api], env: {KEY: SECRET_VALUE\n"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			writeTestManifest(t, root, "version: 1\n"+test.body)
			_, err := LoadDefinitions(root)
			if err == nil {
				t.Fatal("invalid manifest accepted")
			}
			if strings.Contains(err.Error(), "SECRET_VALUE") {
				t.Fatalf("diagnostic leaked environment value: %q", err)
			}
		})
	}
}

func writeTestManifest(t *testing.T, root, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStopGraceManifest(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		raw  string
		want *time.Duration
		err  bool
	}{
		{name: "inherited", raw: "", want: nil},
		{name: "positive", raw: "stop_grace: 2s", want: durationPtr(2 * time.Second)},
		{name: "sub-second", raw: "stop_grace: 250ms", want: durationPtr(250 * time.Millisecond)},
		{name: "explicit zero", raw: "stop_grace: 0s", want: durationPtr(0)},
		{name: "bare zero", raw: "stop_grace: 0", err: true},
		{name: "malformed", raw: "stop_grace: nope", err: true},
		{name: "negative", raw: "stop_grace: -1s", err: true},
		{name: "wrong type", raw: "stop_grace: 1", err: true},
		{name: "unknown", raw: "grace: 1s", err: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := "version: 1\nprocesses:\n  api:\n    argv: [api]\n"
			if test.raw != "" {
				body += "    " + test.raw + "\n"
			}
			writeTestManifest(t, root, body)
			definitions, err := LoadDefinitions(root)
			if test.err {
				if err == nil {
					t.Fatal("LoadDefinitions succeeded")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := definitions[0].StopGrace
			if (got == nil) != (test.want == nil) || got != nil && *got != *test.want {
				t.Fatalf("StopGrace = %v, want %v", got, test.want)
			}
		})
	}
}

func durationPtr(value time.Duration) *time.Duration { return &value }
func TestExecutableReadinessManifest(t *testing.T) {
	cases := []struct {
		name, ready string
		wantErr     bool
	}{
		{"match", "match: ready", false},
		{"exec", "exec: [task, health]", false},
		{"positive durations", "exec: [task, health]\n      interval: 250ms\n      timeout: 2.5s", false},
		{"both", "match: ready\n      exec: [task]", true},
		{"neither", "timeout: 1s", true},
		{"empty argv", "exec: []", true},
		{"empty arg", `exec: [""]`, true},
		{"match interval", "match: ready\n      interval: 1s", true},
		{"zero interval", "exec: [task]\n      interval: 0s", true},
		{"zero timeout", "exec: [task]\n      timeout: 0s", true},
		{"negative timeout", "match: ready\n      timeout: -1s", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestManifest(t, root, "version: 1\nprocesses:\n  web:\n    argv: [server]\n    ready:\n      "+tc.ready+"\n")
			defs, err := LoadDefinitions(root)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v, wantErr=%v", err, tc.wantErr)
			}
			if err == nil {
				if tc.name == "exec" {
					if !reflect.DeepEqual(defs[0].Ready.Exec, []string{"task", "health"}) {
						t.Fatalf("exec argv=%#v, want exact parsed argv", defs[0].Ready.Exec)
					}
				}
				if tc.name == "positive durations" {
					if defs[0].Ready.Interval != 250*time.Millisecond || defs[0].Ready.Timeout != 2500*time.Millisecond {
						t.Fatalf("durations=%s/%s", defs[0].Ready.Interval, defs[0].Ready.Timeout)
					}
				} else if defs[0].Ready.Timeout != 30*time.Second {
					t.Fatalf("timeout=%s", defs[0].Ready.Timeout)
				}
				if tc.name == "exec" && defs[0].Ready.Interval != time.Second {
					t.Fatalf("interval=%s", defs[0].Ready.Interval)
				}
			}
		})
	}
	root := t.TempDir()
	writeTestManifest(t, root, "version: 1\nprocesses:\n  db:\n    argv: [db]\n    ready:\n      exec: [health, db]\n  api:\n    argv: [api]\n    after: [db]\n")
	if _, err := LoadDefinitions(root); err != nil {
		t.Fatalf("exec readiness dependency: %v", err)
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
			Ready:  &ReadyDefinition{Match: "listening", Timeout: 2 * time.Second}, After: []string{}, Restart: RestartNever,
		},
		{
			Name:   "web",
			Source: "manifest",
			Argv:   []string{"go", "run", "./web"},
			Cwd:    filepath.Join(root, "web"),
			Ready:  &ReadyDefinition{Match: "ready:", Timeout: 30 * time.Second}, After: []string{}, Restart: RestartNever,
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
			manifest:  "version: 1\nprocesses:\n  web:\n    argv: [go]\n    autostart: true\n",
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

func TestAfterManifest(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, `version: 1
processes:
  web:
    argv: [web]
    after: [api]
  api:
    argv: [api]
    after: [db, queue]
    ready: {match: api-ready}
  db:
    argv: [db]
    ready: {match: db-ready}
  queue:
    argv: [queue]
    ready: {match: queue-ready}
  worker:
    argv: [worker]
    after: []
`)
	definitions, err := LoadDefinitions(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{definitions[0].Name, definitions[1].Name, definitions[2].Name, definitions[3].Name, definitions[4].Name}; !reflect.DeepEqual(got, []string{"api", "db", "queue", "web", "worker"}) {
		t.Fatalf("definition order = %v", got)
	}
	byName := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		byName[definition.Name] = definition
	}
	if !reflect.DeepEqual(byName["api"].After, []string{"db", "queue"}) {
		t.Fatalf("api after = %#v", byName["api"].After)
	}
	if len(byName["web"].After) != 1 || byName["web"].After[0] != "api" {
		t.Fatalf("web after = %#v", byName["web"].After)
	}
	if byName["worker"].After == nil || len(byName["worker"].After) != 0 {
		t.Fatalf("empty after = %#v, want an empty slice", byName["worker"].After)
	}
	cloned := cloneDefinitions(definitions)
	cloned[3].After[0] = "mutated"
	if byName["web"].After[0] != "api" {
		t.Fatal("definition clone aliases after slice")
	}
	if discovered := discoveredDefinition(root, "package_json", "npm", "run", "dev"); discovered.After == nil || len(discovered.After) != 0 {
		t.Fatalf("discovered after = %#v, want empty", discovered.After)
	}

	generated := renderInitManifest([]Definition{{Name: "dev", Source: "test", Argv: []string{"dev"}}}, InitOutcomeGenerated, "")
	if !strings.Contains(string(generated), "# after: [db]") {
		t.Fatalf("generated init omitted inert after example: %s", generated)
	}
	writeTestManifest(t, root, string(generated))
	if _, err := LoadDefinitions(root); err != nil {
		t.Fatalf("generated init manifest is invalid: %v", err)
	}
	// The template (unlike the single-candidate manifest above) has no real
	// process to name in an after dependency, so it omits the after example
	// entirely rather than offering one that fails validation ("dependency
	// %q must declare ready") the moment a reader follows the template's own
	// instruction to uncomment it.
	template := renderInitManifest(nil, InitOutcomeTemplate, "no candidate")
	if strings.Contains(string(template), "after") {
		t.Fatalf("template init example references after without a valid target: %s", template)
	}
	writeTestManifest(t, root, string(template))
	if _, err := LoadDefinitions(root); err != nil {
		t.Fatalf("template init manifest is invalid: %v", err)
	}

	cases := []struct {
		name string
		body string
		want []string
	}{
		{"unknown", "after: [missing]", []string{"process \"web\".after[0]", "unknown process"}},
		{"duplicate", "after: [db, db]", []string{"process \"web\".after[1]", "duplicate"}},
		{"self", "after: [web]", []string{"process \"web\".after[0]", "itself"}},
		{"without readiness", "after: [db]", []string{"process \"web\".after[0]", "declare ready"}},
		{"non-list", "after: db", []string{"process \"web\".after", "sequence"}},
		{"non-string", "after: [db, 7]", []string{"process \"web\".after[1]", "string"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := "version: 1\nprocesses:\n  web:\n    argv: [web]\n    " + test.body + "\n  db:\n    argv: [db]\n"
			if test.name == "without readiness" {
				body += ""
			}
			writeTestManifest(t, root, body)
			_, err := LoadDefinitions(root)
			if err == nil {
				t.Fatal("manifest unexpectedly accepted invalid after")
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %q, want %q", err, want)
				}
			}
		})
	}
	cycles := []struct {
		name  string
		chain string
	}{
		{"two", "a: [b]\n  b: [a]"},
		{"three", "a: [b]\n  b: [c]\n  c: [a]"},
		{"longer", "a: [b]\n  b: [c]\n  c: [d]\n  d: [e]\n  e: [a]"},
	}
	for _, test := range cycles {
		t.Run("cycle-"+test.name, func(t *testing.T) {
			lines := strings.Split(test.chain, "\n")
			var body strings.Builder
			body.WriteString("version: 1\nprocesses:\n")
			for _, line := range lines {
				parts := strings.SplitN(strings.TrimSpace(line), ": ", 2)
				fmt.Fprintf(&body, "  %s:\n    argv: [%s]\n    ready: {match: ready}\n    after: %s\n", parts[0], parts[0], parts[1])
			}
			writeTestManifest(t, root, body.String())
			_, err := LoadDefinitions(root)
			if err == nil || !strings.Contains(err.Error(), "cycle") || !strings.Contains(err.Error(), "after") {
				t.Fatalf("cycle error = %v", err)
			}
		})
	}
}

func TestTTYManifest(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "version: 1\nprocesses:\n  dev:\n    argv: [dev]\n    tty: true\n")
	definitions, err := LoadDefinitions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || !definitions[0].TTY {
		t.Fatalf("definitions = %+v", definitions)
	}
	for _, value := range []string{"\"true\"", "yes", "1", "null"} {
		writeTestManifest(t, root, "version: 1\nprocesses:\n  dev:\n    argv: [dev]\n    tty: "+value+"\n")
		if _, err := LoadDefinitions(root); err == nil || !strings.Contains(err.Error(), "process \"dev\"") || !strings.Contains(err.Error(), "tty") {
			t.Fatalf("tty=%s error = %v", value, err)
		}
	}
}
