package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	manifestSchemaID        = "https://raw.githubusercontent.com/brettinternet/hum/main/hum.schema.json"
	manifestSchemaDraft     = "https://json-schema.org/draft/2020-12/schema"
	manifestNamePattern     = "^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$"
	durationPattern         = "^[+]?(?:[0-9]+(?:\\.[0-9]+)?(?:ns|us|µs|μs|ms|s|m|h))+$"
	positiveDurationPattern = "^(?=.*[1-9])[+]?(?:[0-9]+(?:\\.[0-9]+)?(?:ns|us|µs|μs|ms|s|m|h))+$"
)

func TestManifestSchemaContract(t *testing.T) {
	schema := readManifestSchema(t)
	assertSchemaString(t, schema, "$schema", manifestSchemaDraft)
	assertSchemaString(t, schema, "$id", manifestSchemaID)
	assertClosedSchemaObject(t, "manifest", schema)
	assertSchemaKeys(t, "manifest", schema, manifestFields)
	assertRequiredKeys(t, "manifest", schema, "processes", "version")

	processes := schemaObject(t, schema, "properties", "processes")
	propertyNames := schemaObject(t, processes, "propertyNames")
	assertSchemaString(t, propertyNames, "pattern", manifestNamePattern)
	process := schemaObject(t, schema, "$defs", "process")
	assertClosedSchemaObject(t, "process", process)
	assertSchemaKeys(t, "process", process, processFields)
	assertRequiredKeys(t, "process", process, "argv")
	readiness := schemaObject(t, schema, "$defs", "readiness")
	assertClosedSchemaObject(t, "readiness", readiness)
	assertSchemaKeys(t, "readiness", readiness, readyFields)

	namePattern := regexp.MustCompile(manifestNamePattern)
	for _, test := range []struct {
		name string
		want bool
	}{
		{name: "web", want: true},
		{name: "api.v2-worker", want: true},
		{name: "_web", want: false},
		{name: strings.Repeat("a", 65), want: false},
	} {
		if schemaAccepts := namePattern.MatchString(test.name); schemaAccepts != test.want {
			t.Errorf("schema process name %q accepted = %v, want %v", test.name, schemaAccepts, test.want)
		}
		if parserAccepts := validName(test.name); parserAccepts != test.want {
			t.Errorf("parser process name %q accepted = %v, want %v", test.name, parserAccepts, test.want)
		}
	}

	stopGrace := schemaObject(t, process, "properties", "stop_grace")
	assertSchemaString(t, stopGrace, "pattern", durationPattern)
	for _, field := range []string{"interval", "timeout"} {
		duration := schemaObject(t, readiness, "properties", field)
		assertSchemaString(t, duration, "pattern", positiveDurationPattern)
	}
	for _, test := range []struct {
		value        string
		allowZero    bool
		wantAccepted bool
	}{
		{value: "1h30m", wantAccepted: true},
		{value: "+250ms", wantAccepted: true},
		{value: "0s", allowZero: true, wantAccepted: true},
		{value: "0s", wantAccepted: false},
		{value: "-1s", allowZero: true, wantAccepted: false},
		{value: "one second", wantAccepted: false},
	} {
		pattern := positiveDurationPattern
		if test.allowZero {
			pattern = durationPattern
		}
		if got := schemaDurationMatches(pattern, test.value); got != test.wantAccepted {
			t.Errorf("schema duration %q accepted = %v, want %v", test.value, got, test.wantAccepted)
		}
		parsed, err := time.ParseDuration(test.value)
		parserAccepts := err == nil && (parsed > 0 || test.allowZero && parsed == 0)
		if parserAccepts != test.wantAccepted {
			t.Errorf("parser duration %q accepted = %v, want %v", test.value, parserAccepts, test.wantAccepted)
		}
	}

	oneOf, ok := readiness["oneOf"].([]any)
	if !ok || len(oneOf) != 2 || !requiredOnly(oneOf[0], "match") || !requiredOnly(oneOf[1], "exec") {
		t.Fatalf("readiness oneOf = %#v, want exactly match or exec", readiness["oneOf"])
	}

	for _, test := range []struct {
		name     string
		manifest string
		wantErr  bool
	}{
		{
			name:     "all accepted keys and compound durations",
			manifest: "version: 1\nprocesses:\n  db:\n    argv: [db]\n    ready:\n      match: ready\n  api.v2:\n    argv: [api]\n    cwd: .\n    ready:\n      exec: [health]\n      interval: 1m30s\n      timeout: 2m15s\n    after: [db]\n    tty: true\n    restart: on-failure\n    stop_grace: 1m5s\n",
		},
		{name: "unknown manifest key", manifest: "version: 1\nprocesses: {}\nextra: true\n", wantErr: true},
		{name: "unknown process key", manifest: "version: 1\nprocesses:\n  web:\n    argv: [web]\n    extra: true\n", wantErr: true},
		{name: "unknown readiness key", manifest: "version: 1\nprocesses:\n  web:\n    argv: [web]\n    ready:\n      match: ready\n      extra: true\n", wantErr: true},
		{name: "invalid process name", manifest: "version: 1\nprocesses:\n  _web:\n    argv: [web]\n", wantErr: true},
		{name: "zero positive duration", manifest: "version: 1\nprocesses:\n  web:\n    argv: [web]\n    ready:\n      exec: [health]\n      timeout: 0s\n", wantErr: true},
		{name: "neither readiness probe", manifest: "version: 1\nprocesses:\n  web:\n    argv: [web]\n    ready:\n      timeout: 1s\n", wantErr: true},
		{name: "both readiness probes", manifest: "version: 1\nprocesses:\n  web:\n    argv: [web]\n    ready:\n      match: ready\n      exec: [health]\n", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestManifest(t, root, test.manifest)
			_, err := LoadDefinitions(root)
			if (err != nil) != test.wantErr {
				t.Fatalf("LoadDefinitions() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func readManifestSchema(t *testing.T) map[string]any {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "hum.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(contents, &schema); err != nil {
		t.Fatalf("decode hum.schema.json: %v", err)
	}
	return schema
}

func schemaObject(t *testing.T, root map[string]any, path ...string) map[string]any {
	t.Helper()
	current := root
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("schema path %s is not an object", strings.Join(path, "."))
		}
		current = next
	}
	return current
}

func assertSchemaString(t *testing.T, object map[string]any, key, want string) {
	t.Helper()
	if got, ok := object[key].(string); !ok || got != want {
		t.Fatalf("schema %s = %#v, want %q", key, object[key], want)
	}
}

func assertClosedSchemaObject(t *testing.T, name string, object map[string]any) {
	t.Helper()
	if closed, ok := object["additionalProperties"].(bool); !ok || closed {
		t.Fatalf("%s additionalProperties = %#v, want false", name, object["additionalProperties"])
	}
}

func assertSchemaKeys(t *testing.T, name string, object map[string]any, parserKeys map[string]struct{}) {
	t.Helper()
	properties := schemaObject(t, object, "properties")
	schemaKeys := make([]string, 0, len(properties))
	for key := range properties {
		schemaKeys = append(schemaKeys, key)
	}
	acceptedKeys := make([]string, 0, len(parserKeys))
	for key := range parserKeys {
		acceptedKeys = append(acceptedKeys, key)
	}
	sort.Strings(schemaKeys)
	sort.Strings(acceptedKeys)
	if !reflect.DeepEqual(schemaKeys, acceptedKeys) {
		t.Fatalf("%s schema keys = %v, parser keys = %v", name, schemaKeys, acceptedKeys)
	}
}

func assertRequiredKeys(t *testing.T, name string, object map[string]any, want ...string) {
	t.Helper()
	values, ok := object["required"].([]any)
	if !ok {
		t.Fatalf("%s required = %#v, want %v", name, object["required"], want)
	}
	got := make([]string, 0, len(values))
	for _, value := range values {
		key, ok := value.(string)
		if !ok {
			t.Fatalf("%s required contains non-string %#v", name, value)
		}
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s required = %v, want %v", name, got, want)
	}
}

func requiredOnly(value any, key string) bool {
	object, ok := value.(map[string]any)
	if !ok || len(object) != 1 {
		return false
	}
	required, ok := object["required"].([]any)
	return ok && len(required) == 1 && required[0] == key
}

func schemaDurationMatches(pattern, value string) bool {
	positive := strings.Contains(pattern, "(?=.*[1-9])")
	if positive && !strings.ContainsAny(value, "123456789") {
		return false
	}
	goPattern := strings.Replace(pattern, "(?=.*[1-9])", "", 1)
	return regexp.MustCompile(goPattern).MatchString(value)
}
