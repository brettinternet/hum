package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultReadyTimeout = 30 * time.Second

type yamlEntry struct {
	name  string
	value *yaml.Node
}

// RestartPolicy controls whether a declared process is relaunched after an
// unexpected exit. The zero value is intentionally not used for parsed
// definitions: parsers and all runtime adapters normalize it to RestartNever.
type RestartPolicy string

const (
	RestartNever     RestartPolicy = "never"
	RestartOnFailure RestartPolicy = "on-failure"
)

var (
	manifestFields = map[string]struct{}{
		"version":     {},
		"environment": {},
		"processes":   {},
	}
	processFields = map[string]struct{}{
		"argv":       {},
		"cwd":        {},
		"ready":      {},
		"after":      {},
		"tty":        {},
		"restart":    {},
		"stop_grace": {},
		"env":        {},
	}
	readyFields = map[string]struct{}{
		"match":    {},
		"exec":     {},
		"interval": {},
		"timeout":  {},
	}
)

// IsManifestSource identifies records originating from a hum manifest.
func IsManifestSource(source string) bool {
	return source == "manifest" || source == "hum.yaml" || strings.HasPrefix(source, "manifest:") || strings.HasPrefix(source, "hum.yaml:")
}

// Definition is one named process declared by a project manifest.
type Definition struct {
	Name string
	// Environment is sensitive launch configuration and is never serialized in
	// process snapshots or response models.
	Environment *EnvironmentSpec `json:"-"`
	Source      string
	Argv        []string
	Cwd         string
	Ready       *ReadyDefinition
	After       []string
	TTY         bool
	Restart     RestartPolicy
	StopGrace   *time.Duration
}

// ReadyDefinition describes the output expression or direct executable and
// timeout used to determine whether a manifest process is ready.
type ReadyDefinition struct {
	Match    string
	Exec     []string
	Interval time.Duration
	Timeout  time.Duration
}

// LoadDefinitions reads the one supported project manifest, hum.yaml, below
// root. A missing hum.yaml is equivalent to an empty manifest.
func LoadDefinitions(root string) ([]Definition, error) {
	root, err := absoluteClean(root)
	if err != nil {
		return nil, fmt.Errorf("hum.yaml: project root: %w", err)
	}
	definitions, _, err := loadDefinitions(root)
	return definitions, err
}

// LoadDefinitionsFile parses exactly filename as a complete manifest. The
// filename must already have been validated by ResolveManifestPath; display is
// the stable project-relative name used in diagnostics and source identity.
func LoadDefinitionsFile(root, filename, display, source string) ([]Definition, error) {
	root, err := absoluteClean(root)
	if err != nil {
		return nil, fmt.Errorf("%s: project root: %w", display, err)
	}
	if display == "" {
		display = filepath.Base(filename)
	}
	if source == "" {
		source = "manifest:" + filepath.ToSlash(display)
	}
	definitions, err := loadDefinitionsFile(root, filename, display, source)
	return definitions, err
}

// loadDefinitions parses hum.yaml and reports whether the manifest was
// present. The presence bit is kept private so LoadDefinitions can retain its
// historical missing-manifest behavior while ResolveDefinitions can make a
// present manifest authoritative.
func loadDefinitions(root string) ([]Definition, bool, error) {
	filename := filepath.Join(root, "hum.yaml")
	if _, err := os.Lstat(filename); errors.Is(err, os.ErrNotExist) {
		return []Definition{}, false, nil
	} else if err != nil {
		return nil, true, fmt.Errorf("%s: open: %w", filename, err)
	}
	definitions, err := loadDefinitionsFile(root, filename, filename, "manifest")
	return definitions, true, err
}

func loadDefinitionsFile(root, filename, display, source string) ([]Definition, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("%s: open: %w", display, err)
	}
	defer file.Close()
	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("%s: read: %w", display, err)
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(contents, []byte("\xef\xbb\xbf")))
	if len(trimmed) > 0 && json.Valid(trimmed) {
		return nil, manifestError(display, "manifest", "unsupported format: JSON is not supported")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, manifestError(display, "manifest", "document is empty")
		}
		return nil, manifestError(display, "manifest", "invalid YAML")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return nil, manifestError(display, "manifest", "multiple YAML documents are not allowed")
	} else if !errors.Is(err, io.EOF) {
		return nil, manifestError(display, "manifest", "multiple YAML documents are not allowed")
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 || document.Content[0] == nil {
		return nil, manifestError(display, "manifest", "document must contain exactly one value")
	}
	rootNode := document.Content[0]
	if err := rejectForbidden(display, "manifest", rootNode); err != nil {
		return nil, err
	}
	entries, err := decodeMapping(display, "manifest", rootNode, manifestFields)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]*yaml.Node, len(entries))
	for _, entry := range entries {
		fields[entry.name] = entry.value
	}
	versionNode, ok := fields["version"]
	if !ok {
		return nil, manifestError(display, "manifest", "missing key %q", "version")
	}
	if err := parseVersion(display, versionNode); err != nil {
		return nil, err
	}
	var environment *EnvironmentSpec
	if environmentNode, ok := fields["environment"]; ok {
		environment, err = parseEnvironment(root, filepath.Dir(filename), display, environmentNode)
		if err != nil {
			return nil, err
		}
	}
	processesNode, ok := fields["processes"]
	if !ok {
		return nil, manifestError(display, "manifest", "missing key %q", "processes")
	}
	return parseProcesses(root, display, processesNode, source, environment)
}

func parseVersion(filename string, node *yaml.Node) error {
	if node == nil || node.Kind != yaml.ScalarNode || node.ShortTag() != "!!int" {
		return manifestError(filename, "manifest.version", "must be integer 1")
	}
	var version int
	if err := node.Decode(&version); err != nil {
		return manifestError(filename, "manifest.version", "must be integer 1: %v", err)
	}
	if version != 1 {
		return manifestError(filename, "manifest.version", "unsupported version %d", version)
	}
	return nil
}

func parseProcesses(root, filename string, node *yaml.Node, source string, manifestEnvironment *EnvironmentSpec) ([]Definition, error) {
	entries, err := decodeMapping(filename, "processes", node, nil)
	if err != nil {
		return nil, err
	}
	definitions := make([]Definition, 0, len(entries))
	for _, entry := range entries {
		if !validName(entry.name) {
			return nil, manifestError(filename, "processes", "invalid process name %q (want [A-Za-z0-9][A-Za-z0-9._-]{0,63})", entry.name)
		}
		context := fmt.Sprintf("process %q", entry.name)
		definition, err := parseProcess(root, filename, context, entry.value, manifestEnvironment)
		if err != nil {
			return nil, err
		}
		definition.Name = entry.name
		definition.Source = source
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	if err := validateAfterGraph(filename, definitions); err != nil {
		return nil, err
	}
	return definitions, nil
}

func validateAfterGraph(filename string, definitions []Definition) error {
	byName := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		byName[definition.Name] = definition
	}
	for _, definition := range definitions {
		seen := make(map[string]int, len(definition.After))
		for index, dependency := range definition.After {
			context := fmt.Sprintf("process %q.after[%d]", definition.Name, index)
			if previous, ok := seen[dependency]; ok {
				return manifestError(filename, context, "duplicate dependency %q (already declared at index %d)", dependency, previous)
			}
			seen[dependency] = index
		}
		for index, dependency := range definition.After {
			context := fmt.Sprintf("process %q.after[%d]", definition.Name, index)
			if dependency == definition.Name {
				return manifestError(filename, context, "process cannot depend on itself")
			}
			dependencyDefinition, ok := byName[dependency]
			if !ok {
				return manifestError(filename, context, "unknown process %q", dependency)
			}
			if dependencyDefinition.Ready == nil {
				return manifestError(filename, context, "dependency %q must declare ready", dependency)
			}
		}
	}

	state := make(map[string]uint8, len(definitions))
	stack := make([]string, 0, len(definitions))
	var visit func(string, string, int) error
	visit = func(name, edgeOwner string, edgeIndex int) error {
		switch state[name] {
		case 2:
			return nil
		case 1:
			cycleStart := 0
			for index, item := range stack {
				if item == name {
					cycleStart = index
					break
				}
			}
			cycle := append([]string(nil), stack[cycleStart:]...)
			cycle = append(cycle, name)
			context := fmt.Sprintf("process %q.after[%d]", edgeOwner, edgeIndex)
			return manifestError(filename, context, "dependency cycle: %s", strings.Join(cycle, " -> "))
		}
		state[name] = 1
		stack = append(stack, name)
		definition := byName[name]
		for index, dependency := range definition.After {
			if err := visit(dependency, name, index); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = 2
		return nil
	}
	for _, definition := range definitions {
		if err := visit(definition.Name, "", -1); err != nil {
			return err
		}
	}
	return nil
}

func parseProcess(root, filename, context string, node *yaml.Node, manifestEnvironment *EnvironmentSpec) (Definition, error) {
	entries, err := decodeMapping(filename, context, node, processFields)
	if err != nil {
		return Definition{}, err
	}
	fields := make(map[string]*yaml.Node, len(entries))
	for _, entry := range entries {
		fields[entry.name] = entry.value
	}
	argvNode, ok := fields["argv"]
	if !ok {
		return Definition{}, manifestError(filename, context, "missing key %q", "argv")
	}
	argv, err := parseArgv(filename, context, argvNode)
	if err != nil {
		return Definition{}, err
	}

	cwd := root
	if cwdNode, ok := fields["cwd"]; ok {
		if !isStringScalar(cwdNode) {
			return Definition{}, manifestError(filename, context, "cwd must be a string")
		}
		cwd, err = normalizeCwd(root, filename, context, cwdNode.Value)
		if err != nil {
			return Definition{}, err
		}
	}

	var ready *ReadyDefinition
	if readyNode, ok := fields["ready"]; ok {
		ready, err = parseReady(filename, context, readyNode)
		if err != nil {
			return Definition{}, err
		}
	}
	after := []string{}
	if afterNode, ok := fields["after"]; ok {
		after, err = parseAfter(filename, context, afterNode)
		if err != nil {
			return Definition{}, err
		}
	}
	tty := false
	if ttyNode, ok := fields["tty"]; ok {
		if ttyNode == nil || ttyNode.Kind != yaml.ScalarNode || ttyNode.ShortTag() != "!!bool" {
			return Definition{}, manifestError(filename, context, "tty must be a boolean")
		}
		if err := ttyNode.Decode(&tty); err != nil {
			return Definition{}, manifestError(filename, context, "tty must be a boolean: %v", err)
		}
	}
	restart := RestartNever
	if restartNode, ok := fields["restart"]; ok {
		if !isStringScalar(restartNode) {
			return Definition{}, manifestError(filename, context, "restart must be a string (never or on-failure)")
		}
		restart = RestartPolicy(restartNode.Value)
		if restart != RestartNever && restart != RestartOnFailure {
			return Definition{}, manifestError(filename, context, "restart %q is invalid (want never or on-failure)", restartNode.Value)
		}
	}
	var processEnvironment map[string]*string
	if envNode, ok := fields["env"]; ok {
		processEnvironment, err = parseProcessEnvironment(filename, context+".env", envNode)
		if err != nil {
			return Definition{}, err
		}
	}
	var stopGrace *time.Duration
	if stopGraceNode, ok := fields["stop_grace"]; ok {
		if !isStringScalar(stopGraceNode) {
			return Definition{}, manifestError(filename, context, "stop_grace must be a duration string")
		}
		parsed, parseErr := time.ParseDuration(stopGraceNode.Value)
		if parseErr != nil {
			return Definition{}, manifestError(filename, context, "invalid stop_grace %q: %v", stopGraceNode.Value, parseErr)
		}
		if parsed < 0 {
			return Definition{}, manifestError(filename, context, "stop_grace must not be negative")
		}
		stopGrace = &parsed
	}
	var environment *EnvironmentSpec
	if manifestEnvironment != nil {
		merged := *manifestEnvironment
		merged.Files = append([]string(nil), manifestEnvironment.Files...)
		merged.Values = processEnvironment
		if merged.Values == nil {
			merged.Values = map[string]*string{}
		}
		environment = &merged
	} else if processEnvironment != nil {
		environment = &EnvironmentSpec{Inherit: true, Values: processEnvironment}
	}
	return Definition{Argv: argv, Cwd: cwd, Ready: ready, After: after, TTY: tty, Restart: restart, StopGrace: stopGrace, Environment: environment}, nil
}

func parseEnvironment(root, base, filename string, node *yaml.Node) (*EnvironmentSpec, error) {
	entries, err := decodeMapping(filename, "environment", node, map[string]struct{}{"inherit": {}, "files": {}})
	if err != nil {
		return nil, err
	}
	spec := &EnvironmentSpec{Inherit: true, Values: map[string]*string{}, BaseDir: base, Root: root}
	for _, entry := range entries {
		switch entry.name {
		case "inherit":
			if entry.value == nil || entry.value.Kind != yaml.ScalarNode || entry.value.ShortTag() != "!!bool" {
				return nil, manifestError(filename, "environment.inherit", "must be a boolean")
			}
			if err := entry.value.Decode(&spec.Inherit); err != nil {
				return nil, manifestError(filename, "environment.inherit", "must be a boolean")
			}
		case "files":
			if !isSequenceNode(entry.value) {
				return nil, manifestError(filename, "environment.files", "must be a sequence of non-empty relative strings")
			}
			if len(entry.value.Content) > maxEnvironmentFiles {
				return nil, manifestError(filename, "environment.files", "must contain at most %d entries", maxEnvironmentFiles)
			}
			for index, item := range entry.value.Content {
				if !isStringScalar(item) || item.Value == "" || filepath.IsAbs(item.Value) || strings.IndexByte(item.Value, 0) >= 0 {
					return nil, manifestError(filename, fmt.Sprintf("environment.files[%d]", index), "must be a non-empty relative path")
				}
				clean := filepath.Clean(filepath.Join(base, item.Value))
				if clean == base || !pathWithin(root, clean) {
					return nil, manifestError(filename, fmt.Sprintf("environment.files[%d]", index), "path escapes the project root")
				}
				spec.Files = append(spec.Files, item.Value)
			}
		}
	}
	return spec, nil
}

func parseProcessEnvironment(filename, context string, node *yaml.Node) (map[string]*string, error) {
	entries, err := decodeMapping(filename, context, node, nil)
	if err != nil {
		return nil, err
	}
	values := make(map[string]*string, len(entries))
	for _, entry := range entries {
		if !environmentKeyPattern.MatchString(entry.name) || strings.IndexByte(entry.name, 0) >= 0 {
			return nil, manifestError(filename, context, "invalid environment key")
		}
		if entry.value == nil || entry.value.ShortTag() == "!!null" {
			values[entry.name] = nil
			continue
		}
		if !isStringScalar(entry.value) || strings.IndexByte(entry.value.Value, 0) >= 0 {
			return nil, manifestError(filename, context, "environment values must be strings or null")
		}
		value := entry.value.Value
		values[entry.name] = &value
	}
	return values, nil
}

func parseAfter(filename, context string, node *yaml.Node) ([]string, error) {
	afterContext := context + ".after"
	if node == nil || !isSequenceNode(node) {
		return nil, manifestError(filename, afterContext, "must be a sequence of strings")
	}
	after := make([]string, len(node.Content))
	for index, item := range node.Content {
		if !isStringScalar(item) {
			return nil, manifestError(filename, fmt.Sprintf("%s[%d]", afterContext, index), "must be a string")
		}
		after[index] = item.Value
	}
	return after, nil
}

func parseArgv(filename, context string, node *yaml.Node) ([]string, error) {
	if node == nil || !isSequenceNode(node) {
		return nil, manifestError(filename, context, "argv must be a non-empty sequence of strings")
	}
	if len(node.Content) == 0 {
		return nil, manifestError(filename, context, "argv must be a non-empty sequence of strings")
	}
	argv := make([]string, len(node.Content))
	for i, item := range node.Content {
		if !isStringScalar(item) {
			return nil, manifestError(filename, context, "argv[%d] must be a string", i)
		}
		if item.Value == "" {
			return nil, manifestError(filename, context, "argv[%d] must not be empty", i)
		}
		argv[i] = item.Value
	}
	return argv, nil
}

func parseReady(filename, context string, node *yaml.Node) (*ReadyDefinition, error) {
	readyContext := context + ".ready"
	entries, err := decodeMapping(filename, readyContext, node, readyFields)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]*yaml.Node, len(entries))
	for _, entry := range entries {
		fields[entry.name] = entry.value
	}
	matchNode, hasMatch := fields["match"]
	execNode, hasExec := fields["exec"]
	if hasMatch == hasExec {
		return nil, manifestError(filename, readyContext, "requires exactly one of %q or %q", "match", "exec")
	}
	definition := &ReadyDefinition{Timeout: defaultReadyTimeout}
	if hasMatch {
		if !isStringScalar(matchNode) {
			return nil, manifestError(filename, readyContext, "match must be a string")
		}
		definition.Match = matchNode.Value
		if _, err := regexp.Compile(definition.Match); err != nil {
			return nil, manifestError(filename, readyContext, "invalid match regular expression %q: %v", definition.Match, err)
		}
	} else {
		definition.Exec, err = parseArgv(filename, readyContext+".exec", execNode)
		if err != nil {
			return nil, err
		}
		definition.Interval = time.Second
		if intervalNode, ok := fields["interval"]; ok {
			if !isStringScalar(intervalNode) {
				return nil, manifestError(filename, readyContext, "interval must be a duration string")
			}
			parsed, parseErr := time.ParseDuration(intervalNode.Value)
			if parseErr != nil || parsed <= 0 {
				return nil, manifestError(filename, readyContext, "interval must be a positive duration")
			}
			definition.Interval = parsed
		}
	}
	if timeoutNode, ok := fields["timeout"]; ok {
		if !isStringScalar(timeoutNode) {
			return nil, manifestError(filename, readyContext, "timeout must be a duration string")
		}
		parsed, parseErr := time.ParseDuration(timeoutNode.Value)
		if parseErr != nil {
			return nil, manifestError(filename, readyContext, "invalid timeout %q: %v", timeoutNode.Value, parseErr)
		}
		if parsed <= 0 {
			return nil, manifestError(filename, readyContext, "timeout must be positive")
		}
		definition.Timeout = parsed
	}
	if hasMatch {
		if _, hasInterval := fields["interval"]; hasInterval {
			return nil, manifestError(filename, readyContext, "interval is only valid with exec")
		}
	}
	return definition, nil
}

func normalizeCwd(root, filename, context, value string) (string, error) {
	if filepath.IsAbs(value) {
		return "", manifestError(filename, context, "cwd %q must be relative to the project root", value)
	}
	candidate := filepath.Clean(filepath.Join(root, value))
	if !pathWithin(root, candidate) {
		return "", manifestError(filename, context, "cwd %q escapes the project root", value)
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", manifestError(filename, context, "cwd %q is not an existing directory: %v", value, err)
	}
	if !info.IsDir() {
		return "", manifestError(filename, context, "cwd %q is not a directory", value)
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", manifestError(filename, context, "cannot resolve project root for cwd: %v", err)
	}
	resolvedCwd, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", manifestError(filename, context, "cannot resolve cwd %q: %v", value, err)
	}
	if !pathWithin(resolvedRoot, resolvedCwd) {
		return "", manifestError(filename, context, "cwd %q resolves outside the project root", value)
	}
	return candidate, nil
}

func decodeMapping(filename, context string, node *yaml.Node, allowed map[string]struct{}) ([]yamlEntry, error) {
	if node == nil || !isMappingNode(node) {
		return nil, manifestError(filename, context, "must be a mapping")
	}
	if len(node.Content)%2 != 0 {
		return nil, manifestError(filename, context, "mapping is malformed")
	}
	entries := make([]yamlEntry, 0, len(node.Content)/2)
	seen := make(map[string]struct{}, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		if key == nil || key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" {
			return nil, manifestError(filename, context, "mapping keys must be strings")
		}
		name := key.Value
		if name == "<<" {
			return nil, manifestError(filename, context, "merge keys are not allowed")
		}
		if _, exists := seen[name]; exists {
			return nil, manifestError(filename, context, "duplicate key %q", name)
		}
		seen[name] = struct{}{}
		if allowed != nil {
			if _, known := allowed[name]; !known {
				validKeys := allowed
				// Keep diagnostics stable: additive launch-only keys are omitted
				// from the legacy valid-key hint.
				_, hasStopGrace := allowed["stop_grace"]
				_, hasEnv := allowed["env"]
				if hasStopGrace || hasEnv {
					validKeys = make(map[string]struct{}, len(allowed)-1)
					for key := range allowed {
						if key != "stop_grace" && key != "env" {
							validKeys[key] = struct{}{}
						}
					}
				}
				return nil, manifestError(filename, context, "unknown key %q (valid keys: %s)", name, sortedKeys(validKeys))
			}
		}
		entries = append(entries, yamlEntry{name: name, value: node.Content[i+1]})
	}
	return entries, nil
}

func rejectForbidden(filename, context string, node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode || node.Alias != nil {
		return manifestError(filename, context, "aliases are not allowed")
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if err := rejectForbidden(filename, context, key); err != nil {
				return err
			}
			if key != nil && key.Kind == yaml.ScalarNode && key.Value == "<<" {
				return manifestError(filename, context, "merge keys are not allowed")
			}
			childContext := context
			if key != nil && key.Kind == yaml.ScalarNode {
				childContext = contextForChild(context, key.Value)
			}
			var value *yaml.Node
			if i+1 < len(node.Content) {
				value = node.Content[i+1]
			}
			if err := rejectForbidden(filename, childContext, value); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range node.Content {
		if err := rejectForbidden(filename, context, child); err != nil {
			return err
		}
	}
	return nil
}

func contextForChild(parent, key string) string {
	if parent == "manifest" && key == "processes" {
		return "processes"
	}
	if parent == "processes" {
		return fmt.Sprintf("process %q", key)
	}
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func sortedKeys(keys map[string]struct{}) string {
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func manifestError(filename, context, format string, args ...any) error {
	message := fmt.Sprintf(format, args...)
	if context != "" {
		message = context + ": " + message
	}
	return fmt.Errorf("%s: %s", filename, message)
}

func isStringScalar(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str"
}

func isMappingNode(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.MappingNode && node.ShortTag() == "!!map"
}

func isSequenceNode(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.SequenceNode && node.ShortTag() == "!!seq"
}

func pathWithin(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func validName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	isAlphaNum := func(c byte) bool {
		return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
	}
	if !isAlphaNum(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !isAlphaNum(c) && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}
