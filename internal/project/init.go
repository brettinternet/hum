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
)

// InitResult describes the manifest produced or encountered by InitManifest.
type InitResult struct {
	Path       string
	Outcome    InitOutcome
	Candidates []Definition
}

// ErrManifestExists reports that the project already has a hum.yaml manifest.
var ErrManifestExists = errors.New("hum.yaml already exists")

// initWrite and initLink keep post-create failure and publication-race tests deterministic.
var (
	initWrite = func(file *os.File, contents []byte) (int, error) {
		return file.Write(contents)
	}
	initLink = os.Link
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
// changed, and no discovered command is launched.
func InitManifest(start string) (InitResult, error) {
	root, err := DiscoverProjectRoot(start)
	if err != nil {
		return InitResult{}, fmt.Errorf("hum init: discover project root: %w", err)
	}
	path := filepath.Join(root, "hum.yaml")

	if _, err := os.Lstat(path); err == nil {
		return InitResult{Path: path, Outcome: InitOutcomeExists}, &ManifestExistsError{Path: path}
	} else if !errors.Is(err, os.ErrNotExist) {
		return InitResult{}, fmt.Errorf("hum init: inspect %s: %w", path, err)
	}

	candidates, outcome, reason, err := initCandidates(root)
	if err != nil {
		return InitResult{}, err
	}
	contents := renderInitManifest(candidates, outcome, reason)

	file, err := os.CreateTemp(root, ".hum.yaml.tmp-*")
	if err != nil {
		return InitResult{}, fmt.Errorf("hum init: create temporary manifest in %s: %w", root, err)
	}
	tempPath := file.Name()
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		_ = os.Remove(tempPath)
	}()

	written, err := initWrite(file, contents)
	if err != nil {
		return InitResult{}, fmt.Errorf("hum init: write %s: %w", path, err)
	}
	if written != len(contents) {
		return InitResult{}, fmt.Errorf("hum init: write %s: %w", path, io.ErrShortWrite)
	}
	if err := file.Sync(); err != nil {
		return InitResult{}, fmt.Errorf("hum init: sync %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return InitResult{}, fmt.Errorf("hum init: close %s: %w", path, err)
	}
	closed = true

	if err := initLink(tempPath, path); err != nil {
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

func renderInitManifest(candidates []Definition, outcome InitOutcome, reason string) []byte {
	var document strings.Builder
	if outcome == InitOutcomeTemplate {
		fmt.Fprintf(&document, "# hum init did not generate a process entry: %s.\n", reason)
		if len(candidates) == 0 {
			document.WriteString("# No detected candidates.\n")
		} else {
			document.WriteString("# Detected candidates:\n")
			for _, candidate := range candidates {
				fmt.Fprintf(&document, "# - source: %s\n", candidate.Source)
				fmt.Fprintf(&document, "#   argv: %s\n", formatYAMLSequence(candidate.Argv))
			}
		}
		document.WriteString("# Replace the example below with one of the detected candidates.\n")
		document.WriteString("# Example:\n")
		document.WriteString("#   \"dev\":\n")
		document.WriteString("#     argv:\n")
		document.WriteString("#       - \"command\"\n")
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
		if definition.Ready != nil {
			ready := *definition.Ready
			cloned[i].Ready = &ready
		}
	}
	return cloned
}
