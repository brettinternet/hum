package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrNoCandidate reports that no supported conventional development entrypoint
// was found by hum init in a project root.
var ErrNoCandidate = errors.New("no conventional development candidate")

// ErrManifestMissing reports that a declaration was required but hum.yaml was absent.
var ErrManifestMissing = errors.New("manifest is missing")

// ManifestMissingError identifies the project that needs an explicit declaration.
type ManifestMissingError struct {
	Start string
	Root  string
}

func (e *ManifestMissingError) Error() string {
	if e == nil {
		return ErrManifestMissing.Error()
	}
	message := ErrManifestMissing.Error()
	if e.Start != "" && !samePath(e.Start, e.Root) {
		message += fmt.Sprintf(" in %s or its parents up to %s", e.Start, e.Root)
	} else {
		message += " in " + e.Root
	}
	return fmt.Sprintf("%s: run hum init to create hum.yaml, or use hum run NAME -- COMMAND", message)
}

func samePath(first, second string) bool {
	if filepath.Clean(first) == filepath.Clean(second) {
		return true
	}
	firstCanonical, firstErr := CanonicalPath(first)
	secondCanonical, secondErr := CanonicalPath(second)
	return firstErr == nil && secondErr == nil && firstCanonical == secondCanonical
}

func (e *ManifestMissingError) Unwrap() error { return ErrManifestMissing }

// ErrAmbiguous reports that more than one supported conventional development
// entrypoint was found in a project root.
var ErrAmbiguous = errors.New("ambiguous conventional development candidates")

// ErrConfiguration reports malformed project discovery input.
var ErrConfiguration = errors.New("project discovery configuration is malformed")

// ErrIntrospection reports malformed or failed command-backed discovery.
var ErrIntrospection = errors.New("project discovery introspection failed")

// NoCandidateError identifies a root without a supported development entrypoint.
type NoCandidateError struct {
	Root      string
	Supported []string
}

func (e *NoCandidateError) Error() string {
	if e == nil {
		return ErrNoCandidate.Error()
	}
	conventions := e.Supported
	if len(conventions) == 0 {
		conventions = supportedDiscoveryConventions
	}
	return fmt.Sprintf("%s in %s: .hum.yaml and hum.yaml are absent; run hum init to create a manifest; supported conventions: %s", ErrNoCandidate, e.Root, strings.Join(conventions, ", "))
}

func (e *NoCandidateError) Unwrap() error { return ErrNoCandidate }

// AmbiguityError identifies every source that declared the conventional dev
// process. Resolution deliberately does not apply source precedence.
type AmbiguityError struct {
	Root       string
	Sources    []string
	Candidates []Definition
}

func (e *AmbiguityError) Error() string {
	if e == nil {
		return ErrAmbiguous.Error()
	}
	return fmt.Sprintf("%s in %s: sources %s qualify for dev; run hum init to choose one", ErrAmbiguous, e.Root, strings.Join(e.Sources, ", "))
}

func (e *AmbiguityError) Unwrap() error { return ErrAmbiguous }

// ConfigurationError identifies a malformed native project declaration.
type ConfigurationError struct {
	Source string
	Path   string
	Err    error
}

func (e *ConfigurationError) Error() string {
	if e == nil {
		return ErrConfiguration.Error()
	}
	location := e.Source
	if e.Path != "" {
		location += " (" + e.Path + ")"
	}
	if location == "" {
		location = "project discovery"
	}
	if e.Source == "hum.yaml" || (e.Source != "" && e.Path == "") {
		if e.Err == nil {
			return location
		}
		message := e.Err.Error()
		message = strings.TrimPrefix(message, e.Source+": ")
		if e.Source == "hum.yaml" && e.Path != "" {
			message = strings.TrimPrefix(message, filepath.Join(e.Path, "hum.yaml")+": ")
		}
		return fmt.Sprintf("%s: %s", location, message)
	}
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", ErrConfiguration, location)
	}
	return fmt.Sprintf("%s: %s: %v", ErrConfiguration, location, e.Err)
}

func (e *ConfigurationError) Unwrap() error { return ErrConfiguration }

// IntrospectionError identifies a command-backed detector that could not
// produce a valid machine-readable answer.
type IntrospectionError struct {
	Source string
	Path   string
	Argv   []string
	Err    error
}

func (e *IntrospectionError) Error() string {
	if e == nil {
		return ErrIntrospection.Error()
	}
	location := e.Source
	if e.Path != "" {
		location += " (" + e.Path + ")"
	}
	if len(e.Argv) > 0 {
		location += " [" + strings.Join(e.Argv, " ") + "]"
	}
	if location == "" {
		location = "project discovery"
	}
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", ErrIntrospection, location)
	}
	return fmt.Sprintf("%s: %s: %v", ErrIntrospection, location, e.Err)
}

// Unwrap exposes both the introspection category and the underlying cause so
// callers can still recognise cancellation with errors.Is.
func (e *IntrospectionError) Unwrap() []error {
	if e.Err == nil {
		return []error{ErrIntrospection}
	}
	return []error{ErrIntrospection, e.Err}
}

var supportedDiscoveryConventions = []string{
	"mise task dev",
	"Task task dev",
	"Just recipe dev",
	"Makefile dev target",
	"package.json scripts.dev",
	"deno.json/deno.jsonc tasks.dev",
	"composer.json scripts.dev",
	"executable bin/dev",
	"mix phx.server",
}

var supportedDiscoverySources = []string{
	"mise",
	"task",
	"just",
	"make",
	"package_json",
	"deno_json",
	"composer_json",
	"bin_dev",
	"mix",
}

type discoveryReadFileFunc func(string) ([]byte, error)
type discoveryDetector func(context.Context, string) (Definition, bool, error)

// ManifestSelection identifies one validated manifest and its project scope.
type ManifestSelection struct {
	Root     string
	Path     string
	Relative string
	Source   string
}

// ResolveManifestPath validates an explicit manifest selector. Relative paths
// are based at invocationDir; the selected file and every symlink it follows
// must remain inside the selected project.
func ResolveManifestPath(invocationDir, projectRoot, filename string) (ManifestSelection, error) {
	if filename == "" {
		return ManifestSelection{}, errors.New("--file requires a non-empty path")
	}
	invocation, err := absoluteClean(invocationDir)
	if err != nil {
		return ManifestSelection{}, fmt.Errorf("manifest path: %w", err)
	}
	candidate := filename
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(invocation, candidate)
	}
	candidate, err = absoluteClean(candidate)
	if err != nil {
		return ManifestSelection{}, fmt.Errorf("manifest path %q: %w", filename, err)
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return ManifestSelection{}, fmt.Errorf("manifest file %q: %w", filename, err)
	}
	resolved, err = absoluteClean(resolved)
	if err != nil {
		return ManifestSelection{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return ManifestSelection{}, fmt.Errorf("manifest file %q: %w", filename, err)
	}
	if !info.Mode().IsRegular() {
		return ManifestSelection{}, fmt.Errorf("manifest file %q is not a regular file", filename)
	}
	var root string
	if projectRoot != "" {
		root, err = CanonicalPath(projectRoot)
		if err != nil {
			return ManifestSelection{}, fmt.Errorf("project root: %w", err)
		}
	} else {
		root, err = DiscoverProjectRoot(filepath.Dir(resolved))
		if err != nil {
			return ManifestSelection{}, err
		}
	}
	resolvedRoot, err := CanonicalPath(root)
	if err != nil {
		return ManifestSelection{}, err
	}
	if !pathWithin(resolvedRoot, resolved) {
		return ManifestSelection{}, fmt.Errorf("manifest file %q is outside project root %q", filename, resolvedRoot)
	}
	// Identity follows the resolved file so symlink spellings cannot create
	// multiple source identities for the same manifest.
	relative, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || filepath.IsAbs(relative) || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return ManifestSelection{}, fmt.Errorf("manifest file %q is outside project root %q", filename, resolvedRoot)
	}
	relative = filepath.ToSlash(filepath.Clean(relative))
	return ManifestSelection{Root: resolvedRoot, Path: resolved, Relative: relative, Source: "manifest:" + relative}, nil
}

// DefaultManifestSelection searches from start up to and including root. The
// nearest directory containing either conventional manifest wins; within one
// directory, a private .hum.yaml is authoritative over hum.yaml. The selected
// file is validated using the same containment and regular-file checks as an
// explicit --file selection.
func DefaultManifestSelection(start, root string) (ManifestSelection, bool, error) {
	start, err := absoluteClean(start)
	if err != nil {
		return ManifestSelection{}, true, &ConfigurationError{Source: ".hum.yaml", Path: root, Err: err}
	}
	root, err = absoluteClean(root)
	if err != nil {
		return ManifestSelection{}, true, &ConfigurationError{Source: ".hum.yaml", Path: root, Err: err}
	}
	if !pathWithin(root, start) {
		return ManifestSelection{}, true, &ConfigurationError{Source: ".hum.yaml", Path: root, Err: fmt.Errorf("manifest search directory %q is outside project root", start)}
	}
	for directory := start; ; directory = filepath.Dir(directory) {
		for _, filename := range []string{".hum.yaml", "hum.yaml"} {
			candidate := filepath.Join(directory, filename)
			if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return ManifestSelection{}, true, &ConfigurationError{Source: filename, Path: root, Err: fmt.Errorf("%s: inspect: %w", candidate, err)}
			}
			selection, err := ResolveManifestPath(directory, root, filename)
			if err != nil {
				display := manifestDisplayRelative(root, candidate)
				return ManifestSelection{}, true, &ConfigurationError{Source: display, Path: root, Err: err}
			}
			// Keep the lexical spelling for default child cwd and filesystem
			// paths; source identity and containment come from the resolved path.
			selection.Path = candidate
			if filename == "hum.yaml" && directory == root && selection.Relative == "hum.yaml" {
				// Preserve the runtime's historical root-manifest identity.
				selection.Source = "manifest"
			}
			return selection, true, nil
		}
		if directory == root {
			break
		}
	}
	return ManifestSelection{}, false, nil
}

func manifestDisplayRelative(root, filename string) string {
	relative, err := filepath.Rel(root, filename)
	if err != nil {
		return filepath.Base(filename)
	}
	return filepath.ToSlash(filepath.Clean(relative))
}

// ResolveExplicitDefinitions loads exactly the selected file and never invokes
// conventional discovery.
func ResolveExplicitDefinitions(ctx context.Context, selection ManifestSelection) ([]Definition, error) {
	_ = ctx
	return LoadDefinitionsFile(selection.Root, selection.Path, selection.Relative, selection.Source)
}

// ResolveDefinitions returns the effective default manifest definitions.
func ResolveDefinitions(root string) ([]Definition, error) {
	return ResolveDefinitionsContext(context.Background(), root, root)
}

// ResolveDefinitionsContext resolves the effective default manifest by
// searching from start up to root. An absent default manifest is actionable
// instead of triggering conventional discovery.
func ResolveDefinitionsContext(ctx context.Context, start, root string) ([]Definition, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	start, err := absoluteClean(start)
	if err != nil {
		return nil, &ConfigurationError{Source: ".hum.yaml", Path: root, Err: err}
	}
	root, err = absoluteClean(root)
	if err != nil {
		return nil, &ConfigurationError{Source: ".hum.yaml", Path: root, Err: err}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	selection, present, err := DefaultManifestSelection(start, root)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, &ManifestMissingError{Start: start, Root: root}
	}
	contents, readErr := readDiscoveryDeclaration(selection.Path)
	if readErr != nil {
		return nil, &ConfigurationError{Source: selection.Relative, Path: root, Err: fmt.Errorf("%s: open: %w", selection.Path, readErr)}
	}
	display := selection.Path
	if selection.Relative == ".hum.yaml" {
		display = selection.Relative
	}
	definitions, parseErr := parseDefinitions(root, contents, filepath.Dir(selection.Path), display, selection.Source)
	if parseErr != nil {
		return nil, &ConfigurationError{Source: selection.Relative, Path: root, Err: parseErr}
	}
	return runtimeManifestDefinitions(definitions), nil
}

func runtimeManifestDefinitions(definitions []Definition) []Definition {
	for index := range definitions {
		if definitions[index].Source == "manifest" {
			definitions[index].Source = "manifest:hum.yaml"
		}
	}
	return definitions
}

// ResolveExplicitDefinitionsReadOnly loads exactly the selected manifest with
// the same bounded, nonblocking file semantics used by diagnostic discovery.
func ResolveExplicitDefinitionsReadOnly(selection ManifestSelection) ([]Definition, error) {
	contents, err := readDiscoveryDeclaration(selection.Path)
	if err != nil {
		return nil, err
	}
	return parseDefinitions(selection.Root, contents, filepath.Dir(selection.Path), selection.Relative, selection.Source)
}

// ResolveDefinitionsReadOnly resolves the effective default manifest and
// conservative conventional init candidates without running project-owned
// commands. It is intended for observational diagnostics and initialization.
func ResolveDefinitionsReadOnly(ctx context.Context, start, root string) ([]Definition, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	start, err := absoluteClean(start)
	if err != nil {
		return nil, &ConfigurationError{Source: ".hum.yaml", Path: root, Err: err}
	}
	root, err = absoluteClean(root)
	if err != nil {
		return nil, &ConfigurationError{Source: ".hum.yaml", Path: root, Err: err}
	}
	selection, present, err := DefaultManifestSelection(start, root)
	if err != nil {
		return nil, err
	}
	if present {
		contents, readErr := readDiscoveryDeclaration(selection.Path)
		if readErr != nil {
			if selection.Relative == ".hum.yaml" {
				return nil, &ConfigurationError{Source: ".hum.yaml", Path: root, Err: fmt.Errorf("%s: open: %w", selection.Path, readErr)}
			}
			return nil, &ConfigurationError{Source: "hum.yaml", Path: root, Err: fmt.Errorf("%s: open: %w", selection.Path, readErr)}
		}
		display := selection.Path
		if selection.Relative == ".hum.yaml" {
			display = selection.Relative
		}
		definitions, parseErr := parseDefinitions(root, contents, filepath.Dir(selection.Path), display, selection.Source)
		if parseErr != nil {
			if selection.Relative == ".hum.yaml" {
				return nil, &ConfigurationError{Source: ".hum.yaml", Path: root, Err: parseErr}
			}
			return nil, &ConfigurationError{Source: "hum.yaml", Path: root, Err: parseErr}
		}
		return runtimeManifestDefinitions(definitions), nil
	}
	return resolveReadOnlyCandidates(ctx, root)
}

func resolveReadOnlyCandidates(ctx context.Context, root string) ([]Definition, error) {
	if err := validateReadOnlyDiscoveryFiles(root); err != nil {
		return nil, err
	}
	detectors := []discoveryDetector{
		detectMiseReadOnly,
		detectTaskReadOnly,
		detectJustReadOnly,
		detectMakeReadOnly,
		detectPackageReadOnly,
		detectDenoReadOnly,
		detectComposerReadOnly,
		detectBinDev,
		detectMixReadOnly,
	}
	candidates := make([]Definition, 0, len(detectors))
	for _, detector := range detectors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		candidate, found, err := detector(ctx, root)
		if err != nil {
			return nil, err
		}
		if found {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return nil, &NoCandidateError{Root: root, Supported: append([]string(nil), supportedDiscoveryConventions...)}
	}
	if len(candidates) > 1 {
		sources := make([]string, len(candidates))
		for index, candidate := range candidates {
			sources[index] = candidate.Source
		}
		return nil, &AmbiguityError{Root: root, Sources: sources, Candidates: candidates}
	}
	return candidates, nil
}

const maxReadOnlyDiscoveryFileBytes int64 = 1 << 20

func readDiscoveryDeclaration(path string) ([]byte, error) {
	file, err := openDiscoveryDeclaration(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("declaration is not a regular file")
	}
	if info.Size() > maxReadOnlyDiscoveryFileBytes {
		return nil, errors.New("declaration exceeds the 1 MiB diagnostic limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxReadOnlyDiscoveryFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxReadOnlyDiscoveryFileBytes {
		return nil, errors.New("declaration exceeds the 1 MiB diagnostic limit")
	}
	return data, nil
}

func validateReadOnlyDiscoveryFiles(root string) error {
	groups := []struct {
		source string
		names  []string
		all    bool
	}{
		{source: "mise", names: []string{"mise.toml", ".mise.toml", filepath.Join(".config", "mise.toml")}, all: true},
		{source: "task", names: []string{"Taskfile.yml", "Taskfile.yaml", "taskfile.yml", "taskfile.yaml"}},
		{source: "just", names: []string{"justfile", "Justfile", ".justfile"}},
		{source: "make", names: []string{"GNUmakefile", "makefile", "Makefile"}},
		{source: "package_json", names: []string{"package.json"}},
		{source: "deno_json", names: []string{"deno.json", "deno.jsonc"}, all: true},
		{source: "composer_json", names: []string{"composer.json"}},
		{source: "mix", names: []string{"mix.exs"}},
	}
	for _, group := range groups {
		paths, err := rootFiles(root, group.names, group.source)
		if err != nil {
			return err
		}
		if !group.all && len(paths) > 1 {
			paths = paths[:1]
		}
		for _, path := range paths {
			info, err := os.Stat(path)
			if err != nil {
				return configurationError(group.source, path, err)
			}
			if info.Size() > maxReadOnlyDiscoveryFileBytes {
				return configurationError(group.source, path, errors.New("declaration exceeds the 1 MiB diagnostic limit"))
			}
		}
	}
	return nil
}

var (
	justRecipeName   = regexp.MustCompile(`^([A-Za-z0-9_-]+)`)
	miseInlineDevKey = regexp.MustCompile(`(?:^|,)\s*(?:"dev"|'dev'|dev)\s*=`)
)

func detectMiseReadOnly(_ context.Context, root string) (Definition, bool, error) {
	paths, err := rootFiles(root, []string{"mise.toml", ".mise.toml", filepath.Join(".config", "mise.toml")}, "mise")
	if err != nil {
		return Definition{}, false, err
	}
	for _, path := range paths {
		data, err := readDiscoveryDeclaration(path)
		if err != nil {
			return Definition{}, false, configurationError("mise", path, err)
		}
		if miseDeclaresDev(data) {
			return discoveredDefinition(root, "mise", "mise", "run", "dev"), true, nil
		}
	}
	return Definition{}, false, nil
}

func miseDeclaresDev(data []byte) bool {
	inTasks := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			table := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			parts := strings.Split(table, ".")
			if len(parts) == 2 && tomlKey(parts[0]) == "tasks" && tomlKey(parts[1]) == "dev" {
				return true
			}
			inTasks = len(parts) == 1 && tomlKey(parts[0]) == "tasks"
			continue
		}
		name, value, assignment := strings.Cut(line, "=")
		if !assignment {
			continue
		}
		parts := strings.Split(strings.TrimSpace(name), ".")
		if inTasks && len(parts) == 1 && tomlKey(parts[0]) == "dev" || len(parts) == 2 && tomlKey(parts[0]) == "tasks" && tomlKey(parts[1]) == "dev" {
			return true
		}
		value = strings.TrimSpace(value)
		if len(parts) == 1 && tomlKey(parts[0]) == "tasks" && strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}") && miseInlineDevKey.MatchString(strings.TrimSpace(value[1:len(value)-1])) {
			return true
		}
	}
	return false
}

func tomlKey(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '\'' && value[len(value)-1] == '\'' || value[0] == '"' && value[len(value)-1] == '"') {
		return value[1 : len(value)-1]
	}
	return value
}

func detectTaskReadOnly(_ context.Context, root string) (Definition, bool, error) {
	path, present, err := rootFile(root, []string{"Taskfile.yml", "Taskfile.yaml", "taskfile.yml", "taskfile.yaml"}, "task")
	if err != nil || !present {
		return Definition{}, false, err
	}
	data, err := readDiscoveryDeclaration(path)
	if err != nil {
		return Definition{}, false, configurationError("task", path, err)
	}
	var document struct {
		Tasks map[string]struct {
			Aliases []string `yaml:"aliases"`
		} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return Definition{}, false, configurationError("task", path, err)
	}
	for name, task := range document.Tasks {
		if name == "dev" || slicesContain(task.Aliases, "dev") {
			return discoveredDefinition(root, "task", "task", "dev"), true, nil
		}
	}
	return Definition{}, false, nil
}

func detectJustReadOnly(_ context.Context, root string) (Definition, bool, error) {
	path, present, err := rootFile(root, []string{"justfile", "Justfile", ".justfile"}, "just")
	if err != nil || !present {
		return Definition{}, false, err
	}
	data, err := readDiscoveryDeclaration(path)
	if err != nil {
		return Definition{}, false, configurationError("just", path, err)
	}
	private := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			for _, attribute := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"), ",") {
				if strings.TrimSpace(attribute) == "private" {
					private = true
				}
			}
			continue
		}
		match := justRecipeName.FindStringSubmatch(trimmed)
		if len(match) == 0 {
			private = false
			continue
		}
		remainder := strings.TrimSpace(strings.TrimPrefix(trimmed, match[1]))
		isRecipe := strings.Contains(remainder, ":") && !strings.HasPrefix(remainder, ":=")
		if isRecipe && match[1] == "dev" && !private {
			return discoveredDefinition(root, "just", "just", "dev"), true, nil
		}
		private = false
	}
	return Definition{}, false, nil
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func discoveredDefinition(root, source string, argv ...string) Definition {
	return Definition{
		Name:    "dev",
		Source:  source,
		Argv:    append([]string(nil), argv...),
		Cwd:     root,
		Ready:   nil,
		After:   []string{},
		Restart: RestartNever,
	}
}

func detectMakeReadOnly(ctx context.Context, root string) (Definition, bool, error) {
	return detectMakeWithReader(ctx, root, readDiscoveryDeclaration)
}

func detectMakeWithReader(_ context.Context, root string, readFile discoveryReadFileFunc) (Definition, bool, error) {
	path, present, err := rootFile(root, []string{"GNUmakefile", "makefile", "Makefile"}, "make")
	if err != nil || !present {
		return Definition{}, false, err
	}
	contents, readErr := readFile(path)
	if readErr != nil {
		return Definition{}, false, configurationError("make", path, readErr)
	}
	if makeDeclaresDev(contents) {
		return discoveredDefinition(root, "make", "make", "dev"), true, nil
	}
	return Definition{}, false, nil
}

func makeDeclaresDev(contents []byte) bool {
	inDefine := false
	for _, line := range strings.Split(string(contents), "\n") {
		if line == "" || line[0] == '\t' {
			continue
		}
		// A leading space is also skipped conservatively because GNU make
		// accepts indented recipe forms.
		trimmed := strings.TrimSpace(stripMakeComment(line))
		if trimmed == "" {
			continue
		}
		directive := makeDirectiveKind(trimmed)
		if inDefine {
			if directive == "endef" {
				inDefine = false
			}
			continue
		}
		if directive != "" {
			if directive == "define" {
				inDefine = true
			}
			continue
		}
		if line[0] == '\t' || line[0] == ' ' {
			continue
		}
		line = trimmed
		colon := makeRuleColon(line)
		if colon < 0 || makeAssignmentColon(line, colon) || makeTargetSpecificAssignment(line, colon) {
			continue
		}
		left := strings.TrimSpace(line[:colon])
		if left == "" || strings.ContainsAny(left, "=;") {
			continue
		}
		for _, target := range strings.Fields(left) {
			if target == "dev" {
				return true
			}
		}
	}
	return false
}

func makeDirectiveKind(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	first := fields[0]
	for _, directive := range []string{"include", "-include", "sinclude", "ifdef", "ifndef", "ifeq", "ifneq", "else", "endif", "define", "endef", "undefine", "vpath", "load", "-load"} {
		if makeDirectiveToken(first, directive) {
			if directive == "-load" {
				return "load"
			}
			return directive
		}
	}
	for _, modifier := range []string{"override", "export", "unexport", "private"} {
		if !makeDirectiveToken(first, modifier) {
			continue
		}
		for _, field := range fields[1:] {
			if makeDirectiveToken(field, "define") {
				return "define"
			}
			if makeDirectiveToken(field, "endef") {
				return "endef"
			}
			if makeDirectiveToken(field, "undefine") {
				return "undefine"
			}
		}
		return modifier
	}
	return ""
}

func makeDirectiveToken(token, directive string) bool {
	if token == directive {
		return true
	}
	if !strings.HasPrefix(token, directive) || len(token) == len(directive) {
		return false
	}
	switch token[len(directive)] {
	case '(', ':':
		return true
	default:
		return false
	}
}

func stripMakeComment(line string) string {
	for i := range len(line) {
		if line[i] != '#' {
			continue
		}
		backslashes := 0
		for j := i - 1; j >= 0 && line[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			return line[:i]
		}
	}
	return line
}

func makeRuleColon(line string) int {
	depth := 0
	escaped := false
	for i := range len(line) {
		c := line[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		switch c {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func makeAssignmentColon(line string, colon int) bool {
	if colon < 0 || colon >= len(line) {
		return false
	}
	end := colon
	for end < len(line) && line[end] == ':' {
		end++
	}
	return end > colon && end < len(line) && line[end] == '='
}
func makeTargetSpecificAssignment(line string, colon int) bool {
	if colon < 0 || colon >= len(line) {
		return false
	}
	value := strings.TrimSpace(strings.TrimLeft(line[colon+1:], ":"))
	for {
		stripped := false
		for _, modifier := range []string{"override", "export", "unexport", "private"} {
			if !strings.HasPrefix(value, modifier) {
				continue
			}
			if len(value) > len(modifier) && !isMakeSpace(value[len(modifier)]) {
				continue
			}
			value = strings.TrimSpace(value[len(modifier):])
			stripped = true
			break
		}
		if !stripped {
			break
		}
	}
	return makeVariableAssignment(value)
}

func makeVariableAssignment(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	end := len(value)
	for i := range len(value) {
		if isMakeSpace(value[i]) {
			end = i
			break
		}
	}
	first := value[:end]
	if index := makeAssignmentOperatorIndex(first); index > 0 && makeVariableName(first[:index]) {
		return true
	}
	remainder := value[end:]
	for len(remainder) > 0 && isMakeSpace(remainder[0]) {
		remainder = remainder[1:]
	}
	return makeAssignmentOperatorPrefix(remainder) > 0 && makeVariableName(first)
}

func makeAssignmentOperatorIndex(value string) int {
	for i := range len(value) {
		if makeAssignmentOperatorPrefix(value[i:]) > 0 {
			return i
		}
	}
	return -1
}

func makeAssignmentOperatorPrefix(value string) int {
	switch {
	case strings.HasPrefix(value, ":::="):
		return 4
	case strings.HasPrefix(value, "::="):
		return 3
	case strings.HasPrefix(value, ":="), strings.HasPrefix(value, "+="),
		strings.HasPrefix(value, "?="), strings.HasPrefix(value, "!="):
		return 2
	case strings.HasPrefix(value, "="):
		return 1
	default:
		return 0
	}
}

func makeVariableName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for i := range len(value) {
		switch value[i] {
		case ' ', '\t', '\r', '\n', '$', '(', ')', ';', '#', ':', '=':
			return false
		}
	}
	return true
}

func isMakeSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func detectPackageReadOnly(ctx context.Context, root string) (Definition, bool, error) {
	return detectPackageWithReader(ctx, root, readDiscoveryDeclaration)
}

func detectPackageWithReader(_ context.Context, root string, readFile discoveryReadFileFunc) (Definition, bool, error) {
	path, present, err := rootFile(root, []string{"package.json"}, "package_json")
	if err != nil || !present {
		return Definition{}, false, err
	}
	contents, err := readFile(path)
	if err != nil {
		return Definition{}, false, configurationError("package_json", path, err)
	}
	object, err := decodeJSONObject(contents)
	if err != nil {
		return Definition{}, false, configurationError("package_json", path, err)
	}
	packageManager, hasPackageManager, err := packageManagerValue(object)
	if err != nil {
		return Definition{}, false, configurationError("package_json", path, err)
	}
	rawScripts, hasScripts := object["scripts"]
	if !hasScripts {
		return Definition{}, false, nil
	}
	var scripts map[string]json.RawMessage
	if err := json.Unmarshal(rawScripts, &scripts); err != nil || scripts == nil {
		if err == nil {
			err = errors.New("scripts must be an object")
		}
		return Definition{}, false, configurationError("package_json", path, err)
	}
	rawDev, hasDev := scripts["dev"]
	if !hasDev {
		return Definition{}, false, nil
	}
	var script *string
	if err := json.Unmarshal(rawDev, &script); err != nil || script == nil {
		if err == nil {
			err = errors.New("scripts.dev must be a string")
		}
		return Definition{}, false, configurationError("package_json", path, fmt.Errorf("scripts.dev must be a string: %w", err))
	}
	_ = script // The body is intentionally never parsed or executed.

	runner := packageManager
	if !hasPackageManager {
		runner, err = packageRunnerFromLockfiles(root)
		if err != nil {
			return Definition{}, false, configurationError("package_json", path, err)
		}
	}
	return discoveredDefinition(root, "package_json", runner, "run", "dev"), true, nil
}

func packageManagerValue(object map[string]json.RawMessage) (string, bool, error) {
	raw, ok := object["packageManager"]
	if !ok {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", true, errors.New("packageManager must be a string")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", true, errors.New("packageManager must not be empty")
	}
	name := strings.TrimSpace(strings.SplitN(value, "@", 2)[0])
	switch name {
	case "bun", "pnpm", "yarn", "npm":
		return name, true, nil
	default:
		return "", true, fmt.Errorf("unsupported packageManager %q", value)
	}
}

var packageLockfileFamilies = []struct {
	name  string
	files []string
}{
	{name: "bun", files: []string{"bun.lock", "bun.lockb"}},
	{name: "pnpm", files: []string{"pnpm-lock.yaml"}},
	{name: "yarn", files: []string{"yarn.lock"}},
	{name: "npm", files: []string{"package-lock.json", "npm-shrinkwrap.json"}},
}

func packageRunnerFromLockfiles(root string) (string, error) {
	families := make([]string, 0, len(packageLockfileFamilies))
	for _, family := range packageLockfileFamilies {
		for _, filename := range family.files {
			path := filepath.Join(root, filename)
			info, err := os.Stat(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return "", fmt.Errorf("cannot inspect lockfile %q: %w", filename, err)
			}
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("lockfile %q is not a regular file", filename)
			}
			families = append(families, family.name)
			break
		}
	}
	if len(families) == 0 {
		return "npm", nil
	}
	if len(families) > 1 {
		return "", fmt.Errorf("conflicting lockfile families: %s", strings.Join(families, ", "))
	}
	return families[0], nil
}

func detectDenoReadOnly(ctx context.Context, root string) (Definition, bool, error) {
	return detectDenoWithReader(ctx, root, readDiscoveryDeclaration)
}

func detectDenoWithReader(_ context.Context, root string, readFile discoveryReadFileFunc) (Definition, bool, error) {
	paths, err := rootFiles(root, []string{"deno.json", "deno.jsonc"}, "deno_json")
	if err != nil {
		return Definition{}, false, err
	}
	found := false
	for _, path := range paths {
		contents, readErr := readFile(path)
		if readErr != nil {
			return Definition{}, false, configurationError("deno_json", path, readErr)
		}
		if filepath.Ext(path) == ".jsonc" {
			contents, readErr = normalizeJSONC(contents)
			if readErr != nil {
				return Definition{}, false, configurationError("deno_json", path, readErr)
			}
		}
		object, decodeErr := decodeJSONObject(contents)
		if decodeErr != nil {
			return Definition{}, false, configurationError("deno_json", path, decodeErr)
		}
		rawTasks, hasTasks := object["tasks"]
		if !hasTasks {
			continue
		}
		var tasks map[string]json.RawMessage
		if err := json.Unmarshal(rawTasks, &tasks); err != nil || tasks == nil {
			if err == nil {
				err = errors.New("tasks must be an object")
			}
			return Definition{}, false, configurationError("deno_json", path, err)
		}
		rawDev, hasDev := tasks["dev"]
		if !hasDev {
			continue
		}
		if err := validateTaskValue(rawDev); err != nil {
			return Definition{}, false, configurationError("deno_json", path, fmt.Errorf("tasks.dev: %w", err))
		}
		found = true
	}
	if found {
		return discoveredDefinition(root, "deno_json", "deno", "task", "dev"), true, nil
	}
	return Definition{}, false, nil
}

func validateTaskValue(raw json.RawMessage) error {
	var stringValue *string
	if err := json.Unmarshal(raw, &stringValue); err == nil && stringValue != nil {
		return nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err == nil && list != nil {
		for _, rawValue := range list {
			var stringValue *string
			if err := json.Unmarshal(rawValue, &stringValue); err != nil || stringValue == nil {
				return errors.New("task must be a string or array of strings")
			}
		}
		return nil
	}
	return errors.New("task must be a string or array of strings")
}

func detectComposerReadOnly(ctx context.Context, root string) (Definition, bool, error) {
	return detectComposerWithReader(ctx, root, readDiscoveryDeclaration)
}

func detectComposerWithReader(_ context.Context, root string, readFile discoveryReadFileFunc) (Definition, bool, error) {
	path, present, err := rootFile(root, []string{"composer.json"}, "composer_json")
	if err != nil || !present {
		return Definition{}, false, err
	}
	contents, err := readFile(path)
	if err != nil {
		return Definition{}, false, configurationError("composer_json", path, err)
	}
	object, err := decodeJSONObject(contents)
	if err != nil {
		return Definition{}, false, configurationError("composer_json", path, err)
	}
	rawScripts, hasScripts := object["scripts"]
	if !hasScripts {
		return Definition{}, false, nil
	}
	var scripts map[string]json.RawMessage
	if err := json.Unmarshal(rawScripts, &scripts); err != nil || scripts == nil {
		if err == nil {
			err = errors.New("scripts must be an object")
		}
		return Definition{}, false, configurationError("composer_json", path, err)
	}
	rawDev, hasDev := scripts["dev"]
	if !hasDev {
		return Definition{}, false, nil
	}
	if err := validateTaskValue(rawDev); err != nil {
		return Definition{}, false, configurationError("composer_json", path, fmt.Errorf("scripts.dev: %w", err))
	}
	return discoveredDefinition(root, "composer_json", "composer", "run-script", "dev"), true, nil
}

func detectBinDev(_ context.Context, root string) (Definition, bool, error) {
	path := filepath.Join(root, "bin", binDevExecutableName)
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, lstatErr := os.Lstat(path); errors.Is(lstatErr, os.ErrNotExist) {
				return Definition{}, false, nil
			}
		}
		return Definition{}, false, configurationError("bin_dev", path, err)
	}
	if !info.Mode().IsRegular() {
		return Definition{}, false, configurationError("bin_dev", path, errors.New("bin/dev is not a regular file"))
	}
	if !binDevExecutable(info) {
		return Definition{}, false, nil
	}
	return discoveredDefinition(root, "bin_dev", "./bin/"+binDevExecutableName), true, nil
}

func detectMixReadOnly(ctx context.Context, root string) (Definition, bool, error) {
	return detectMixWithReader(ctx, root, readDiscoveryDeclaration)
}

func detectMixWithReader(_ context.Context, root string, readFile discoveryReadFileFunc) (Definition, bool, error) {
	path, present, err := rootFile(root, []string{"mix.exs"}, "mix")
	if err != nil || !present {
		return Definition{}, false, err
	}
	contents, err := readFile(path)
	if err != nil {
		return Definition{}, false, configurationError("mix", path, err)
	}
	if !mixPhoenixDependency.Match(stripElixirCommentsAndStrings(contents)) {
		return Definition{}, false, nil
	}
	return discoveredDefinition(root, "mix", "mix", "phx.server"), true, nil
}

var mixPhoenixDependency = regexp.MustCompile(`\{\s*:phoenix\s*,`)

// stripElixirCommentsAndStrings keeps static Mix detection from accepting a
// dependency name that appears only in a comment or quoted value. Dynamic
// dependency declarations deliberately fail closed and require hum.yaml.
func stripElixirCommentsAndStrings(contents []byte) []byte {
	cleaned := append([]byte(nil), contents...)
	for index := 0; index < len(cleaned); {
		switch cleaned[index] {
		case '#':
			for index < len(cleaned) && cleaned[index] != '\n' {
				cleaned[index] = ' '
				index++
			}
		case '\'', '"':
			quote := cleaned[index]
			triple := index+2 < len(cleaned) && cleaned[index+1] == quote && cleaned[index+2] == quote
			width := 1
			if triple {
				width = 3
			}
			for count := 0; count < width; count++ {
				cleaned[index+count] = ' '
			}
			index += width
			for index < len(cleaned) {
				if cleaned[index] == '\\' && !triple {
					cleaned[index] = ' '
					index++
					if index < len(cleaned) {
						cleaned[index] = ' '
						index++
					}
					continue
				}
				if cleaned[index] == quote {
					endWidth := 1
					if triple {
						if index+2 >= len(cleaned) || cleaned[index+1] != quote || cleaned[index+2] != quote {
							cleaned[index] = ' '
							index++
							continue
						}
						endWidth = 3
					}
					for count := 0; count < endWidth; count++ {
						cleaned[index+count] = ' '
					}
					index += endWidth
					break
				}
				if cleaned[index] != '\n' {
					cleaned[index] = ' '
				}
				index++
			}
		default:
			index++
		}
	}
	return cleaned
}

func decodeJSONObject(data []byte) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, errors.New("top-level value must be a JSON object")
	}
	return object, nil
}

func normalizeJSONC(data []byte) ([]byte, error) {
	withoutComments, err := stripJSONComments(data)
	if err != nil {
		return nil, err
	}
	return stripJSONTrailingCommas(withoutComments), nil
}

func stripJSONComments(data []byte) ([]byte, error) {
	const (
		jsonNormal = iota
		jsonString
		jsonLineComment
		jsonBlockComment
	)
	state := jsonNormal
	escaped := false
	output := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch state {
		case jsonNormal:
			if c == '"' {
				state = jsonString
				output = append(output, c)
			} else if c == '/' && i+1 < len(data) && data[i+1] == '/' {
				state = jsonLineComment
				i++
			} else if c == '/' && i+1 < len(data) && data[i+1] == '*' {
				state = jsonBlockComment
				output = append(output, ' ')
				i++
			} else {
				output = append(output, c)
			}
		case jsonString:
			output = append(output, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				state = jsonNormal
			}
		case jsonLineComment:
			if c == '\n' {
				state = jsonNormal
				output = append(output, c)
			}
		case jsonBlockComment:
			if c == '*' && i+1 < len(data) && data[i+1] == '/' {
				state = jsonNormal
				i++
			} else if c == '\n' {
				output = append(output, c)
			}
		}
	}
	if state == jsonBlockComment {
		return nil, errors.New("unterminated block comment")
	}
	return output, nil
}

func stripJSONTrailingCommas(data []byte) []byte {
	const (
		jsonNormal = iota
		jsonString
	)
	state := jsonNormal
	escaped := false
	output := make([]byte, 0, len(data))
	for i := range len(data) {
		c := data[i]
		if state == jsonString {
			output = append(output, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				state = jsonNormal
			}
			continue
		}
		if c == '"' {
			state = jsonString
			output = append(output, c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(data) && isJSONSpace(data[j]) {
				j++
			}
			if j < len(data) && (data[j] == ']' || data[j] == '}') {
				continue
			}
		}
		output = append(output, c)
	}
	return output
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func configurationError(source, path string, err error) error {
	return &ConfigurationError{Source: source, Path: path, Err: err}
}

func rootFile(root string, names []string, source string) (string, bool, error) {
	paths, err := rootFiles(root, names, source)
	if err != nil || len(paths) == 0 {
		return "", false, err
	}
	return paths[0], true, nil
}

func rootFiles(root string, names []string, source string) ([]string, error) {
	paths := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if _, lstatErr := os.Lstat(path); errors.Is(lstatErr, os.ErrNotExist) {
					continue
				}
			}
			return nil, configurationError(source, path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, configurationError(source, path, errors.New("declaration is not a regular file"))
		}
		paths = append(paths, path)
	}
	return paths, nil
}
