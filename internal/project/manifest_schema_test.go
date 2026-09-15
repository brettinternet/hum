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
	environment := schemaObject(t, schema, "properties", "environment")
	assertClosedSchemaObject(t, "environment", environment)
	assertSchemaKeys(t, "environment", environment, map[string]struct{}{"inherit": {}, "files": {}})
	inherit := schemaObject(t, environment, "properties", "inherit")
	if inherit["type"] != "boolean" || inherit["default"] != true {
		t.Fatalf("environment.inherit schema = %#v", inherit)
	}
	files := schemaObject(t, environment, "properties", "files")
	if files["type"] != "array" || files["maxItems"] != float64(maxEnvironmentFiles) {
		t.Fatalf("environment.files schema = %#v", files)
	}
	fileItem := schemaObject(t, files, "items")
	filePattern, _ := fileItem["pattern"].(string)
	if fileItem["type"] != "string" || fileItem["minLength"] != float64(1) || !strings.Contains(filePattern, "u0000") {
		t.Fatalf("environment.files item schema = %#v", fileItem)
	}

	process := schemaObject(t, schema, "$defs", "process")
	assertClosedSchemaObject(t, "process", process)
	assertSchemaKeys(t, "process", process, processFields)
	assertRequiredKeys(t, "process", process, "argv")
	processEnvironment := schemaObject(t, process, "properties", "env")
	if processEnvironment["type"] != "object" {
		t.Fatalf("process.env schema = %#v", processEnvironment)
	}
	envNames := schemaObject(t, processEnvironment, "propertyNames")
	assertSchemaString(t, envNames, "pattern", environmentKeyPattern.String())
	envValues := schemaObject(t, processEnvironment, "additionalProperties")
	variants, ok := envValues["anyOf"].([]any)
	if !ok || len(variants) != 2 {
		t.Fatalf("process.env values schema = %#v", envValues)
	}
	envValuePattern, _ := variants[0].(map[string]any)["pattern"].(string)
	assertNULOnlyPattern(t, "process.env value", envValuePattern, "", "plain", "line\nbreak")
	assertNULOnlyPattern(t, "environment.files item", filePattern, "sub/.env", "line\nbreak")

	readiness := schemaObject(t, schema, "$defs", "readiness")
	assertClosedSchemaObject(t, "readiness", readiness)
	assertSchemaKeys(t, "readiness", readiness, readyFields)
	httpPattern, _ := schemaObject(t, readiness, "properties", "http")["pattern"].(string)
	tcpPattern, _ := schemaObject(t, readiness, "properties", "tcp")["pattern"].(string)
	httpRE, err := regexp.Compile(httpPattern)
	if err != nil {
		t.Fatalf("HTTP schema pattern: %v", err)
	}
	tcpRE, err := regexp.Compile(tcpPattern)
	if err != nil {
		t.Fatalf("TCP schema pattern: %v", err)
	}
	for _, test := range []struct{ name, http, tcp string }{
		{"valid compressed", "http://[2001:db8::1]/ready", "[2001:db8::1]:443"},
		{"valid mapped", "http://[::ffff:192.0.2.1]/ready", "[::ffff:192.0.2.1]:443"},
		{"malformed prefix", "http://[:::1]/ready", "[:::1]:443"},
		{"malformed short", "http://[1:2]/ready", "[1:2]:443"},
	} {
		httpSchema, tcpSchema := httpRE.MatchString(test.http), tcpRE.MatchString(test.tcp)
		httpParsed, tcpParsed := validateHTTPTarget(test.http) == nil, validateTCPTarget(test.tcp) == nil
		if httpSchema != httpParsed || tcpSchema != tcpParsed {
			t.Errorf("%s schema/parser parity: HTTP %v/%v TCP %v/%v", test.name, httpSchema, httpParsed, tcpSchema, tcpParsed)
		}
	}
	for _, test := range []struct {
		name, method string
		value        any
		want         bool
	}{
		{"empty HTTP", "http", "", false}, {"empty TCP", "tcp", "", false},
		{"non-string HTTP", "http", 123, false}, {"valid HTTP", "http", "http://127.0.0.1:80/", true},
		{"valid TCP", "tcp", "[::1]:80", true},
	} {
		if got := schemaReadinessTargetAccepts(readiness, test.method, test.value); got != test.want {
			t.Errorf("schema readiness %s %q accepted=%v want=%v", test.name, test.value, got, test.want)
		}
	}

	assertReadinessDocumentCorpus(t, readiness)

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
	if !ok || len(oneOf) != 4 || !requiredOnly(oneOf[0], "match") || !requiredOnly(oneOf[1], "exec") || !requiredOnly(oneOf[2], "http") || !requiredOnly(oneOf[3], "tcp") {
		t.Fatalf("readiness oneOf = %#v, want exactly one readiness method", readiness["oneOf"])
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

type readinessSchemaCase struct {
	name   string
	ready  string
	fields map[string]any
}

func assertReadinessDocumentCorpus(t *testing.T, readiness map[string]any) {
	t.Helper()
	cases := []readinessSchemaCase{
		{"match", "match: ready", map[string]any{"match": "ready"}},
		{"exec", "exec: [probe]", map[string]any{"exec": []any{"probe"}}},
		{"http IPv4 explicit", "http: http://127.0.0.1:8080/ready", map[string]any{"http": "http://127.0.0.1:8080/ready"}},
		{"http localhost default", "http: https://localhost/ready", map[string]any{"http": "https://localhost/ready"}},
		{"http compressed IPv6", "http: http://[2001:db8::1]/ready", map[string]any{"http": "http://[2001:db8::1]/ready"}},
		{"http mapped IPv6", "http: http://[::ffff:192.0.2.1]/ready", map[string]any{"http": "http://[::ffff:192.0.2.1]/ready"}},
		{"tcp IPv4 explicit", "tcp: 127.0.0.1:8080", map[string]any{"tcp": "127.0.0.1:8080"}},
		{"tcp localhost", "tcp: localhost:8080", map[string]any{"tcp": "localhost:8080"}},
		{"tcp compressed IPv6", "tcp: \"[2001:db8::1]:443\"", map[string]any{"tcp": "[2001:db8::1]:443"}},
		{"tcp mapped IPv6", "tcp: \"[::ffff:192.0.2.1]:443\"", map[string]any{"tcp": "[::ffff:192.0.2.1]:443"}},
		{"http empty", "http: \"\"", map[string]any{"http": ""}}, {"http non-string", "http: 123", map[string]any{"http": 123}},
		{"tcp empty", "tcp: \"\"", map[string]any{"tcp": ""}}, {"tcp non-string", "tcp: 123", map[string]any{"tcp": 123}},
		{"http port zero", "http: http://127.0.0.1:0/", map[string]any{"http": "http://127.0.0.1:0/"}}, {"http port max", "http: http://127.0.0.1:65536/", map[string]any{"http": "http://127.0.0.1:65536/"}}, {"http port nonnumeric", "http: http://127.0.0.1:abc/", map[string]any{"http": "http://127.0.0.1:abc/"}},
		{"http bad scheme", "http: ftp://localhost/", map[string]any{"http": "ftp://localhost/"}}, {"http bad host", "http: http://example.com/", map[string]any{"http": "http://example.com/"}}, {"http userinfo", "http: http://user@localhost/", map[string]any{"http": "http://user@localhost/"}}, {"http fragment", "http: http://localhost/#ready", map[string]any{"http": "http://localhost/#ready"}},
		{"tcp missing port", "tcp: 127.0.0.1", map[string]any{"tcp": "127.0.0.1"}}, {"tcp port zero", "tcp: 127.0.0.1:0", map[string]any{"tcp": "127.0.0.1:0"}}, {"tcp port max", "tcp: 127.0.0.1:65536", map[string]any{"tcp": "127.0.0.1:65536"}}, {"tcp port nonnumeric", "tcp: 127.0.0.1:abc", map[string]any{"tcp": "127.0.0.1:abc"}}, {"tcp bad host", "tcp: example.com:80", map[string]any{"tcp": "example.com:80"}}, {"tcp malformed IPv6", "tcp: \"[:::1]:80\"", map[string]any{"tcp": "[:::1]:80"}}, {"http malformed IPv6", "http: http://[:::1]:80/", map[string]any{"http": "http://[:::1]:80/"}},
		{"match and exec", "match: ready\n      exec: [probe]", map[string]any{"match": "ready", "exec": []any{"probe"}}}, {"match and http", "match: ready\n      http: http://127.0.0.1:1/", map[string]any{"match": "ready", "http": "http://127.0.0.1:1/"}}, {"match and tcp", "match: ready\n      tcp: 127.0.0.1:1", map[string]any{"match": "ready", "tcp": "127.0.0.1:1"}}, {"exec and http", "exec: [probe]\n      http: http://127.0.0.1:1/", map[string]any{"exec": []any{"probe"}, "http": "http://127.0.0.1:1/"}}, {"exec and tcp", "exec: [probe]\n      tcp: 127.0.0.1:1", map[string]any{"exec": []any{"probe"}, "tcp": "127.0.0.1:1"}}, {"http and tcp", "http: http://127.0.0.1:1/\n      tcp: 127.0.0.1:1", map[string]any{"http": "http://127.0.0.1:1/", "tcp": "127.0.0.1:1"}},
		{"match interval", "match: ready\n      interval: 10ms", map[string]any{"match": "ready", "interval": "10ms"}}, {"exec interval", "exec: [probe]\n      interval: 10ms", map[string]any{"exec": []any{"probe"}, "interval": "10ms"}}, {"http interval", "http: http://127.0.0.1:1/\n      interval: 10ms", map[string]any{"http": "http://127.0.0.1:1/", "interval": "10ms"}}, {"tcp interval", "tcp: 127.0.0.1:1\n      interval: 10ms", map[string]any{"tcp": "127.0.0.1:1", "interval": "10ms"}},
		{"unknown property", "match: ready\n      extra: true", map[string]any{"match": "ready", "extra": true}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestManifest(t, root, "version: 1\nprocesses:\n  web:\n    argv: [server]\n    ready:\n      "+test.ready+"\n")
			_, parseErr := LoadDefinitions(root)
			schemaAccepted := schemaReadinessDocumentAccepts(readiness, test.fields)
			if schemaAccepted != (parseErr == nil) {
				t.Fatalf("schema/parser disagreement: schema=%v parser=%v error=%v document=%#v", schemaAccepted, parseErr == nil, parseErr, test.fields)
			}
		})
	}
}

func schemaReadinessDocumentAccepts(readiness map[string]any, document map[string]any) bool {
	properties, ok := readiness["properties"].(map[string]any)
	if !ok {
		return false
	}
	for key, value := range document {
		property, ok := properties[key].(map[string]any)
		if !ok {
			return false
		}
		switch property["type"] {
		case "string":
			text, ok := value.(string)
			if !ok {
				return false
			}
			if pattern, ok := property["pattern"].(string); ok && !schemaPatternAccepts(pattern, text) {
				return false
			}
		case "array":
			items, ok := value.([]any)
			if !ok || len(items) < int(property["minItems"].(float64)) {
				return false
			}
			itemSchema := property["items"].(map[string]any)
			for _, item := range items {
				text, ok := item.(string)
				if !ok || len(text) < int(itemSchema["minLength"].(float64)) {
					return false
				}
			}
		default:
			return false
		}
	}
	oneOf, ok := readiness["oneOf"].([]any)
	if !ok {
		return false
	}
	matches := 0
	for _, raw := range oneOf {
		required := raw.(map[string]any)["required"].([]any)
		if len(required) == 1 {
			if _, present := document[required[0].(string)]; present {
				matches++
			}
		}
	}
	if matches != 1 {
		return false
	}
	if _, present := document["interval"]; present {
		if _, ok := document["exec"]; !ok {
			if _, ok := document["http"]; !ok {
				if _, ok := document["tcp"]; !ok {
					return false
				}
			}
		}
	}
	return true
}

func schemaPatternAccepts(pattern, value string) bool {
	if strings.Contains(pattern, "(?=.*[1-9])") && !strings.ContainsAny(value, "123456789") {
		return false
	}
	pattern = strings.Replace(pattern, "(?=.*[1-9])", "", 1)
	return regexp.MustCompile(pattern).MatchString(value)
}

func schemaReadinessTargetAccepts(readiness map[string]any, method string, value any) bool {
	properties, ok := readiness["properties"].(map[string]any)
	if !ok {
		return false
	}
	property, ok := properties[method].(map[string]any)
	if !ok || property["type"] != "string" {
		return false
	}
	text, ok := value.(string)
	if !ok {
		return false
	}
	pattern, ok := property["pattern"].(string)
	if !ok || !regexp.MustCompile(pattern).MatchString(text) {
		return false
	}
	oneOf, ok := readiness["oneOf"].([]any)
	if !ok {
		return false
	}
	matches := 0
	for _, raw := range oneOf {
		variant, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		required, ok := variant["required"].([]any)
		if ok && len(required) == 1 && required[0] == method {
			matches++
		}
	}
	return matches == 1
}

func schemaDurationMatches(pattern, value string) bool {
	positive := strings.Contains(pattern, "(?=.*[1-9])")
	if positive && !strings.ContainsAny(value, "123456789") {
		return false
	}
	goPattern := strings.Replace(pattern, "(?=.*[1-9])", "", 1)
	return regexp.MustCompile(goPattern).MatchString(value)
}

// assertNULOnlyPattern proves a schema pattern rejects only NUL, so editors do
// not flag values the strict parser accepts (a `.`-based lookahead would
// wrongly reject newlines).
func assertNULOnlyPattern(t *testing.T, name, pattern string, accepted ...string) {
	t.Helper()
	if !strings.Contains(pattern, "[^\\u0000]") {
		t.Fatalf("%s pattern %q must exclude only NUL via a character class", name, pattern)
	}
	// Go regexp lacks ECMA lookahead, so drop the leading path guards and keep
	// the NUL-exclusion body under test.
	body := regexp.MustCompile(`\(\?![^)]*\)`).ReplaceAllString(pattern, "")
	compiled := regexp.MustCompile(strings.ReplaceAll(body, "\\u0000", "\\x{0}"))
	for _, value := range accepted {
		if !compiled.MatchString(value) {
			t.Errorf("%s pattern rejected %q, which the parser accepts", name, value)
		}
		if compiled.MatchString(value + "\x00") {
			t.Errorf("%s pattern accepted a NUL in %q", name, value)
		}
	}
}
