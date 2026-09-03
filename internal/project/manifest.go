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

var (
	manifestFields = map[string]struct{}{
		"version":   {},
		"processes": {},
	}
	processFields = map[string]struct{}{
		"argv":  {},
		"cwd":   {},
		"ready": {},
	}
	readyFields = map[string]struct{}{
		"match":   {},
		"timeout": {},
	}
)

// Definition is one named process declared by a project manifest.
type Definition struct {
	Name   string
	Source string
	Argv   []string
	Cwd    string
	Ready  *ReadyDefinition
}

// ReadyDefinition describes the output expression and timeout used to
// determine whether a manifest process is ready.
type ReadyDefinition struct {
	Match   string
	Timeout time.Duration
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

// loadDefinitions parses hum.yaml and reports whether the manifest was
// present. The presence bit is kept private so LoadDefinitions can retain its
// historical missing-manifest behavior while ResolveDefinitions can make a
// present manifest authoritative.
func loadDefinitions(root string) ([]Definition, bool, error) {
	filename := filepath.Join(root, "hum.yaml")
	file, err := os.Open(filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, lstatErr := os.Lstat(filename); errors.Is(lstatErr, os.ErrNotExist) {
				return []Definition{}, false, nil
			}
		}
		return nil, true, fmt.Errorf("%s: open: %w", filename, err)
	}
	defer file.Close()

	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, true, fmt.Errorf("%s: read: %w", filename, err)
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(contents, []byte("\xef\xbb\xbf")))
	if len(trimmed) > 0 && json.Valid(trimmed) {
		return nil, true, manifestError(filename, "manifest", "unsupported format: JSON is not supported")
	}

	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, true, manifestError(filename, "manifest", "document is empty")
		}
		return nil, true, fmt.Errorf("%s: decode YAML: %w", filename, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return nil, true, manifestError(filename, "manifest", "multiple YAML documents are not allowed")
	} else if !errors.Is(err, io.EOF) {
		return nil, true, fmt.Errorf("%s: decode YAML: multiple documents are not allowed: %w", filename, err)
	}

	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 || document.Content[0] == nil {
		return nil, true, manifestError(filename, "manifest", "document must contain exactly one value")
	}
	rootNode := document.Content[0]
	if err := rejectForbidden(filename, "manifest", rootNode); err != nil {
		return nil, true, err
	}
	entries, err := decodeMapping(filename, "manifest", rootNode, manifestFields)
	if err != nil {
		return nil, true, err
	}
	fields := make(map[string]*yaml.Node, len(entries))
	for _, entry := range entries {
		fields[entry.name] = entry.value
	}

	versionNode, ok := fields["version"]
	if !ok {
		return nil, true, manifestError(filename, "manifest", "missing key %q", "version")
	}
	if err := parseVersion(filename, versionNode); err != nil {
		return nil, true, err
	}
	processesNode, ok := fields["processes"]
	if !ok {
		return nil, true, manifestError(filename, "manifest", "missing key %q", "processes")
	}
	definitions, err := parseProcesses(root, filename, processesNode)
	if err != nil {
		return nil, true, err
	}
	return definitions, true, nil
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

func parseProcesses(root, filename string, node *yaml.Node) ([]Definition, error) {
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
		definition, err := parseProcess(root, filename, context, entry.value)
		if err != nil {
			return nil, err
		}
		definition.Name = entry.name
		definition.Source = "manifest"
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions, nil
}

func parseProcess(root, filename, context string, node *yaml.Node) (Definition, error) {
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
	return Definition{Argv: argv, Cwd: cwd, Ready: ready}, nil
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
	matchNode, ok := fields["match"]
	if !ok {
		return nil, manifestError(filename, readyContext, "missing key %q", "match")
	}
	if !isStringScalar(matchNode) {
		return nil, manifestError(filename, readyContext, "match must be a string")
	}
	match := matchNode.Value
	if _, err := regexp.Compile(match); err != nil {
		return nil, manifestError(filename, readyContext, "invalid match regular expression %q: %v", match, err)
	}

	timeout := defaultReadyTimeout
	if timeoutNode, ok := fields["timeout"]; ok {
		if !isStringScalar(timeoutNode) {
			return nil, manifestError(filename, readyContext, "timeout must be a duration string")
		}
		parsed, err := time.ParseDuration(timeoutNode.Value)
		if err != nil {
			return nil, manifestError(filename, readyContext, "invalid timeout %q: %v", timeoutNode.Value, err)
		}
		if parsed < 0 {
			return nil, manifestError(filename, readyContext, "timeout must not be negative")
		}
		if parsed == 0 {
			return nil, manifestError(filename, readyContext, "timeout must be positive")
		}
		timeout = parsed
	}
	return &ReadyDefinition{Match: match, Timeout: timeout}, nil
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
				return nil, manifestError(filename, context, "unknown key %q", name)
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
