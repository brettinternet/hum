package project

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// InitOutcome describes what hum init did with the project manifest.
type InitOutcome string

const (
	InitOutcomeGenerated InitOutcome = "generated"
	InitOutcomeTemplate  InitOutcome = "template"
	InitOutcomeExists    InitOutcome = "exists"
	InitOutcomeReplaced  InitOutcome = "replaced"
)

// InitResult describes the manifest produced or encountered by InitManifest.
type InitResult struct {
	Path       string
	Outcome    InitOutcome
	Candidates []Definition
}

// ErrManifestExists reports that the project already has a hum.yaml manifest.
var ErrManifestExists = errors.New("hum.yaml already exists")

// initWrite, initSync, initClose, initLink, and initRename keep
// post-create failure and publication tests deterministic.
var (
	initRender = func(candidates []Definition, outcome InitOutcome, reason string) ([]byte, error) {
		return renderInitManifest(candidates, outcome, reason), nil
	}
	initWrite = func(file *os.File, contents []byte) (int, error) {
		return file.Write(contents)
	}
	initSync   = func(file *os.File) error { return file.Sync() }
	initClose  = func(file *os.File) error { return file.Close() }
	initLink   = os.Link
	initRename = os.Rename
)

// ManifestExistsError identifies the manifest that prevented initialization.
type ManifestExistsError struct {
	Path string
}

func (e *ManifestExistsError) Error() string {
	if e == nil {
		return ErrManifestExists.Error()
	}
	return fmt.Sprintf("%s: %s", ErrManifestExists, e.Path)
}

func (e *ManifestExistsError) Unwrap() error { return ErrManifestExists }

// InitManifest resolves the nearest project root and creates hum.yaml from
// conventional development discovery. Existing manifests are never read or
// changed unless force is true, and no discovered command is launched.
// The optional force argument preserves the original no-force call shape for
// project callers while allowing hum init --force to publish replacements.
func InitManifest(start string, force ...bool) (InitResult, error) {
	root, err := DiscoverProjectRootLexical(start)
	if err != nil {
		return InitResult{}, fmt.Errorf("hum init: discover project root: %w", err)
	}
	path := filepath.Join(root, "hum.yaml")
	forceReplace := len(force) != 0 && force[0]

	existing, err := initForceDestination(path, forceReplace)
	if err != nil {
		return InitResult{}, err
	}
	if existing && !forceReplace {
		return InitResult{Path: path, Outcome: InitOutcomeExists}, &ManifestExistsError{Path: path}
	}

	candidates, outcome, reason, err := initCandidates(root)
	if err != nil {
		return InitResult{}, err
	}
	contents, err := initRender(candidates, outcome, reason)
	if err != nil {
		return InitResult{}, fmt.Errorf("hum init: render %s: %w", path, err)
	}

	file, err := os.CreateTemp(root, ".hum.yaml.tmp-*")
	if err != nil {
		return InitResult{}, fmt.Errorf("hum init: create temporary manifest in %s: %w", root, err)
	}
	tempPath := file.Name()
	closed := false
	published := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		if !published {
			_ = os.Remove(tempPath)
		}
	}()

	written, err := initWrite(file, contents)
	if err != nil {
		return InitResult{}, fmt.Errorf("hum init: write %s: %w", path, err)
	}
	if written != len(contents) {
		return InitResult{}, fmt.Errorf("hum init: write %s: %w", path, io.ErrShortWrite)
	}
	if err := initSync(file); err != nil {
		return InitResult{}, fmt.Errorf("hum init: sync %s: %w", path, err)
	}
	if err := initClose(file); err != nil {
		return InitResult{}, fmt.Errorf("hum init: close %s: %w", path, err)
	}
	closed = true

	if forceReplace {
		finalExisting, err := initForceDestination(path, true)
		if err != nil {
			return InitResult{}, err
		}
		if err := initRename(tempPath, path); err != nil {
			return InitResult{}, fmt.Errorf("hum init: publish %s: %w", path, err)
		}
		published = true
		if existing || finalExisting {
			outcome = InitOutcomeReplaced
		}
	} else if err := initLink(tempPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return InitResult{Path: path, Outcome: InitOutcomeExists}, &ManifestExistsError{Path: path}
		}
		return InitResult{}, fmt.Errorf("hum init: publish %s: %w", path, err)
	}

	return InitResult{
		Path:       path,
		Outcome:    outcome,
		Candidates: cloneDefinitions(candidates),
	}, nil
}

func initForceDestination(path string, force bool) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("hum init: inspect %s: %w", path, err)
	}
	if !force {
		return true, nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("hum init: refusing to replace %s: target is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("hum init: refusing to replace %s: target is not a regular file", path)
	}
	return true, nil
}

func initCandidates(root string) ([]Definition, InitOutcome, string, error) {
	candidates, err := discoverDefinitions(root)
	if err == nil {
		return candidates, InitOutcomeGenerated, "", nil
	}

	var noCandidate *NoCandidateError
	if errors.As(err, &noCandidate) {
		return []Definition{}, InitOutcomeTemplate, "no candidate was detected", nil
	}
	var ambiguity *AmbiguityError
	if errors.As(err, &ambiguity) {
		return cloneDefinitions(ambiguity.Candidates), InitOutcomeTemplate, "ambiguous candidates were detected", nil
	}
	return nil, "", "", err
}

const manifestSchemaDirective = "# yaml-language-server: $schema=https://raw.githubusercontent.com/brettinternet/hum/main/hum.schema.json\n"

func renderInitManifest(candidates []Definition, outcome InitOutcome, reason string) []byte {
	var document strings.Builder
	document.WriteString(manifestSchemaDirective)
	if outcome == InitOutcomeTemplate {
		fmt.Fprintf(&document, "# hum init did not generate a process entry: %s.\n", reason)
		if len(candidates) == 0 {
			document.WriteString("# No detected candidates.\n")
			document.WriteString("# Add a process entry below; replace the example command with your own.\n")
		} else {
			document.WriteString("# Detected candidates:\n")
			for _, candidate := range candidates {
				fmt.Fprintf(&document, "# - source: %s\n", candidate.Source)
				fmt.Fprintf(&document, "#   argv: %s\n", formatYAMLSequence(candidate.Argv))
			}
			document.WriteString("# Replace the example below with one of the detected candidates.\n")
		}
		document.WriteString("# Example:\n")
		document.WriteString("#   \"dev\":\n")
		document.WriteString("#     argv:\n")
		document.WriteString("#       - \"command\"\n")
		document.WriteString("#     # restart: on-failure\n")
		document.WriteString("version: 1\n")
		document.WriteString("processes: {}\n")
		return []byte(document.String())
	}

	candidate := candidates[0]
	document.WriteString("version: 1\n")
	document.WriteString("processes:\n")
	fmt.Fprintf(&document, "  %s:\n", quoteYAML(candidate.Name))
	fmt.Fprintf(&document, "    # source: %s\n", candidate.Source)
	document.WriteString("    argv:\n")
	for _, arg := range candidate.Argv {
		fmt.Fprintf(&document, "      - %s\n", quoteYAML(arg))
	}
	document.WriteString("    # ready:\n")
	document.WriteString("    #   match: \"Local:\"\n")
	document.WriteString("    #   timeout: 30s\n")
	document.WriteString("    # restart: on-failure\n")
	document.WriteString("    # after: [db]\n")
	return []byte(document.String())
}

func formatYAMLSequence(values []string) string {
	var document strings.Builder
	document.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			document.WriteString(", ")
		}
		document.WriteString(quoteYAML(value))
	}
	document.WriteByte(']')
	return document.String()
}

func quoteYAML(value string) string { return strconv.Quote(value) }

func cloneDefinitions(definitions []Definition) []Definition {
	cloned := make([]Definition, len(definitions))
	for i, definition := range definitions {
		cloned[i] = definition
		cloned[i].Argv = append([]string(nil), definition.Argv...)
		cloned[i].After = make([]string, len(definition.After))
		copy(cloned[i].After, definition.After)
		if definition.Ready != nil {
			ready := *definition.Ready
			cloned[i].Ready = &ready
		}
	}
	return cloned
}
