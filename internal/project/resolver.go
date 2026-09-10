package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrNoCandidate reports that no supported conventional development entrypoint
// was found in a project root.
var ErrNoCandidate = errors.New("no conventional development candidate")

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
	return fmt.Sprintf("%s in %s: hum.yaml is absent; run hum init to create one; supported conventions: %s", ErrNoCandidate, e.Root, strings.Join(conventions, ", "))
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
	if e.Source == "hum.yaml" {
		// A manifest validation error already names hum.yaml through this
		// location, and its message is already prefixed by the same
		// underlying filename; repeating either as "project discovery
		// configuration is malformed: hum.yaml (path): path: message" is
		// redundant. Report the location once and strip the duplicate
		// filename prefix instead.
		if e.Err == nil {
			return location
		}
		message := e.Err.Error()
		if e.Path != "" {
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

func (e *IntrospectionError) Unwrap() error { return ErrIntrospection }

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

type discoveryLookPathFunc func(string) (string, error)
type discoveryCommandFunc func(context.Context, string, ...string) ([]byte, error)
type discoveryDetector func(context.Context, string) (Definition, bool, error)

// These package-private seams keep detector tests deterministic. Production
// resolution leaves them pointed at exec.LookPath and os/exec.
var (
	discoveryLookPath discoveryLookPathFunc = exec.LookPath
	discoveryCommand  discoveryCommandFunc  = runDiscoveryCommand
)

// ResolveDefinitions returns the explicit hum.yaml definitions when that file
// exists. Only an absent hum.yaml invokes conventional root discovery.
func ResolveDefinitions(root string) ([]Definition, error) {
	return ResolveDefinitionsContext(context.Background(), root)
}

// ResolveDefinitionsContext resolves project definitions while allowing command-backed
// discovery to be cancelled by the caller.
func ResolveDefinitionsContext(ctx context.Context, root string) ([]Definition, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	root, err := absoluteClean(root)
	if err != nil {
		return nil, &ConfigurationError{Source: "hum.yaml", Path: root, Err: err}
	}
	definitions, present, err := loadDefinitions(root)
	if err != nil {
		return nil, &ConfigurationError{Source: "hum.yaml", Path: root, Err: err}
	}
	if present {
		return definitions, nil
	}
	return discoverDefinitionsContext(ctx, root)
}

func discoverDefinitions(root string) ([]Definition, error) {
	return discoverDefinitionsContext(context.Background(), root)
}

func discoverDefinitionsContext(ctx context.Context, root string) ([]Definition, error) {
	detectors := []discoveryDetector{
		detectMise,
		detectTask,
		detectJust,
		detectMake,
		detectPackage,
		detectDeno,
		detectComposer,
		detectBinDev,
		detectMix,
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
		return nil, &NoCandidateError{
			Root:      root,
			Supported: append([]string(nil), supportedDiscoveryConventions...),
		}
	}
	if len(candidates) > 1 {
		sources := make([]string, len(candidates))
		for i, candidate := range candidates {
			sources[i] = candidate.Source
		}
		return nil, &AmbiguityError{
			Root:       root,
			Sources:    sources,
			Candidates: append([]Definition(nil), candidates...),
		}
	}
	return candidates, nil
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

// discoveryCommandTimeout bounds each introspection command so a hung task
// runner cannot stall CLI resolution or the single-threaded MCP server.
var discoveryCommandTimeout = 10 * time.Second

func runDiscoveryCommand(ctx context.Context, root string, argv ...string) ([]byte, error) {
	if len(argv) == 0 || argv[0] == "" {
		return nil, errors.New("empty discovery command")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	commandCtx, cancel := context.WithTimeout(ctx, discoveryCommandTimeout)
	defer cancel()
	command := exec.CommandContext(commandCtx, argv[0], argv[1:]...)
	command.Dir = root
	command.WaitDelay = time.Second
	out, err := command.Output()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if commandCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("%s did not finish within %s", strings.Join(argv, " "), discoveryCommandTimeout)
	}
	return out, err
}

func commandOutput(ctx context.Context, root, source, path string, skipEmptyFailure bool, argv ...string) ([]byte, bool, error) {
	lookup := discoveryLookPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	if _, err := lookup(argv[0]); err != nil {
		return nil, false, nil
	}
	run := discoveryCommand
	if run == nil {
		run = runDiscoveryCommand
	}
	output, err := run(ctx, root, argv...)
	if err != nil {
		// Mise and Task commonly report that their declaration file is
		// absent with an empty failure. Task reserves exit code 100 for a
		// missing Taskfile and may also write a diagnostic to stdout.
		// A declared Justfile or mix.exs, however, must surface a failed
		// introspection.
		if skipEmptyFailure && (len(bytes.TrimSpace(output)) == 0 || source == "task" && commandExitCode(err) == 100) {
			return nil, false, nil
		}
		return nil, false, &IntrospectionError{Source: source, Path: path, Argv: append([]string(nil), argv...), Err: err}
	}
	return output, true, nil
}

func commandExitCode(err error) int {
	type exitCoder interface {
		ExitCode() int
	}
	var coder exitCoder
	if errors.As(err, &coder) {
		return coder.ExitCode()
	}
	return -1
}

func detectMise(ctx context.Context, root string) (Definition, bool, error) {
	output, ran, err := commandOutput(ctx, root, "mise", "", true, "mise", "tasks", "--local", "--json")
	if err != nil || !ran {
		return Definition{}, false, err
	}
	found, err := parseTaskJSON("mise", output)
	if err != nil {
		return Definition{}, false, &IntrospectionError{Source: "mise", Argv: []string{"mise", "tasks", "--local", "--json"}, Err: err}
	}
	if !found {
		return Definition{}, false, nil
	}
	return discoveredDefinition(root, "mise", "mise", "run", "dev"), true, nil
}

func detectTask(ctx context.Context, root string) (Definition, bool, error) {
	output, ran, err := commandOutput(ctx, root, "task", "", true, "task", "--dir", root, "--list-all", "--json")
	if err != nil || !ran {
		return Definition{}, false, err
	}
	found, err := parseTaskJSON("task", output)
	if err != nil {
		return Definition{}, false, &IntrospectionError{Source: "task", Argv: []string{"task", "--dir", root, "--list-all", "--json"}, Err: err}
	}
	if !found {
		return Definition{}, false, nil
	}
	return discoveredDefinition(root, "task", "task", "dev"), true, nil
}

func parseTaskJSON(source string, output []byte) (bool, error) {
	value, err := decodeJSON(output)
	if err != nil {
		return false, fmt.Errorf("%s output is not valid JSON: %w", source, err)
	}
	entries, err := taskEntries(value)
	if err != nil {
		return false, fmt.Errorf("%s output has invalid task records: %w", source, err)
	}
	for _, entry := range entries {
		name, aliases, err := taskEntryNames(entry)
		if err != nil {
			return false, fmt.Errorf("%s output has invalid task record: %w", source, err)
		}
		if name == "dev" {
			return true, nil
		}
		for _, alias := range aliases {
			if alias == "dev" {
				return true, nil
			}
		}
	}
	return false, nil
}

func taskEntries(value any) ([]any, error) {
	switch typed := value.(type) {
	case []any:
		return typed, nil
	case map[string]any:
		if raw, ok := typed["tasks"]; ok {
			entries, ok := raw.([]any)
			if !ok {
				return nil, errors.New("tasks must be an array")
			}
			return entries, nil
		}
		if raw, ok := typed["data"]; ok {
			return taskEntries(raw)
		}
		// Some versions expose a name-keyed task map. Accept it without
		// inspecting any task body or metadata.
		entries := make([]any, 0, len(typed))
		for name, record := range typed {
			if _, ok := record.(map[string]any); !ok {
				return nil, errors.New("task map values must be objects")
			}
			entries = append(entries, map[string]any{"name": name})
		}
		return entries, nil
	default:
		return nil, errors.New("top-level value must be an array or object")
	}
}

func taskEntryNames(value any) (string, []string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil, nil
	case map[string]any:
		raw, ok := typed["name"]
		if !ok {
			return "", nil, errors.New("missing name")
		}
		name, ok := raw.(string)
		if !ok {
			return "", nil, errors.New("name must be a string")
		}
		rawAliases, ok := typed["aliases"]
		if !ok {
			return name, nil, nil
		}
		values, ok := rawAliases.([]any)
		if !ok {
			return "", nil, errors.New("aliases must be an array")
		}
		aliases := make([]string, len(values))
		for i, value := range values {
			alias, ok := value.(string)
			if !ok {
				return "", nil, errors.New("aliases must contain strings")
			}
			aliases[i] = alias
		}
		return name, aliases, nil
	default:
		return "", nil, errors.New("record must be an object")
	}
}

func detectJust(ctx context.Context, root string) (Definition, bool, error) {
	path, present, err := rootFile(root, []string{"justfile", "Justfile", ".justfile"}, "just")
	if err != nil || !present {
		return Definition{}, false, err
	}
	argv := []string{"just", "--unstable", "--dump", "--dump-format", "json", "--justfile", path}
	output, ran, err := commandOutput(ctx, root, "just", path, false, argv...)
	if err != nil || !ran {
		return Definition{}, false, err
	}
	found, err := parseJustJSON(output)
	if err != nil {
		return Definition{}, false, &IntrospectionError{Source: "just", Path: path, Argv: append([]string(nil), argv...), Err: err}
	}
	if !found {
		return Definition{}, false, nil
	}
	return discoveredDefinition(root, "just", "just", "dev"), true, nil
}

func parseJustJSON(output []byte) (bool, error) {
	value, err := decodeJSON(output)
	if err != nil {
		return false, fmt.Errorf("just dump is not valid JSON: %w", err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return false, errors.New("just dump must be an object")
	}
	raw, ok := object["recipes"]
	if !ok {
		return false, errors.New("just dump is missing recipes")
	}
	switch recipes := raw.(type) {
	case map[string]any:
		return justRecipeMapHasPublicDev(recipes)
	case []any:
		return justRecipeArrayHasPublicDev(recipes)
	default:
		return false, errors.New("just recipes must be an object")
	}
}

func justRecipeMapHasPublicDev(recipes map[string]any) (bool, error) {
	publicDev := false
	for name, rawRecipe := range recipes {
		recipe, ok := rawRecipe.(map[string]any)
		if !ok {
			return false, fmt.Errorf("just recipe %q must be an object", name)
		}
		private, err := justRecipePrivate(recipe)
		if err != nil {
			return false, fmt.Errorf("just recipe %q %w", name, err)
		}
		if name == "dev" && !private {
			publicDev = true
		}
	}
	return publicDev, nil
}

func justRecipeArrayHasPublicDev(recipes []any) (bool, error) {
	publicDev := false
	devSeen := false
	for _, rawRecipe := range recipes {
		recipe, ok := rawRecipe.(map[string]any)
		if !ok {
			return false, errors.New("just recipe must be an object")
		}
		rawName, ok := recipe["name"]
		if !ok {
			return false, errors.New("just recipe is missing name")
		}
		name, ok := rawName.(string)
		if !ok {
			return false, errors.New("just recipe name must be a string")
		}
		private, err := justRecipePrivate(recipe)
		if err != nil {
			return false, fmt.Errorf("just recipe %q %w", name, err)
		}
		if name != "dev" {
			continue
		}
		if devSeen {
			return false, errors.New("just dump contains duplicate dev recipes")
		}
		devSeen = true
		publicDev = !private
	}
	return publicDev, nil
}

func justRecipePrivate(recipe map[string]any) (bool, error) {
	rawPrivate, ok := recipe["private"]
	if !ok {
		return false, nil
	}
	private, ok := rawPrivate.(bool)
	if !ok {
		return false, errors.New("private must be a boolean")
	}
	return private, nil
}

func detectMake(_ context.Context, root string) (Definition, bool, error) {
	path, present, err := rootFile(root, []string{"GNUmakefile", "makefile", "Makefile"}, "make")
	if err != nil || !present {
		return Definition{}, false, err
	}
	contents, readErr := os.ReadFile(path)
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

func detectPackage(_ context.Context, root string) (Definition, bool, error) {
	path, present, err := rootFile(root, []string{"package.json"}, "package_json")
	if err != nil || !present {
		return Definition{}, false, err
	}
	contents, err := os.ReadFile(path)
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

func detectDeno(_ context.Context, root string) (Definition, bool, error) {
	paths, err := rootFiles(root, []string{"deno.json", "deno.jsonc"}, "deno_json")
	if err != nil {
		return Definition{}, false, err
	}
	found := false
	for _, path := range paths {
		contents, readErr := os.ReadFile(path)
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

func detectComposer(_ context.Context, root string) (Definition, bool, error) {
	path, present, err := rootFile(root, []string{"composer.json"}, "composer_json")
	if err != nil || !present {
		return Definition{}, false, err
	}
	contents, err := os.ReadFile(path)
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
	path := filepath.Join(root, "bin", "dev")
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
	if info.Mode().Perm()&0o111 == 0 {
		return Definition{}, false, nil
	}
	return discoveredDefinition(root, "bin_dev", "./bin/dev"), true, nil
}

func detectMix(_ context.Context, root string) (Definition, bool, error) {
	path, present, err := rootFile(root, []string{"mix.exs"}, "mix")
	if err != nil || !present {
		return Definition{}, false, err
	}
	contents, err := os.ReadFile(path)
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

func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("trailing JSON value")
		}
		return nil, err
	}
	return value, nil
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
