// Package project resolves project roots and strict project manifests.
package project

import (
	"fmt"
	"os"
	"path/filepath"
)

// CanonicalPath returns the absolute physical path for an existing path. For
// a removed path it returns the cleaned absolute spelling; callers that need
// to address retained state can then compare that spelling with a known root.
func CanonicalPath(path string) (string, error) {
	absolute, err := absoluteClean(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve physical path: %w", err)
	}
	// A removed worktree cannot be passed to EvalSymlinks as a whole. Resolve
	// its nearest existing ancestor, then append the missing lexical suffix so
	// aliases such as /var/... and /private/var/... still compare equally.
	missing := []string{}
	probe := absolute
	for {
		parent := filepath.Dir(probe)
		missing = append(missing, filepath.Base(probe))
		if parent == probe {
			return absolute, nil
		}
		resolved, evalErr := filepath.EvalSymlinks(parent)
		if evalErr == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(evalErr) {
			return "", fmt.Errorf("resolve physical path: %w", evalErr)
		}
		probe = parent
	}
}

// CanonicalProjectRoot is a descriptive alias for CanonicalPath.
func CanonicalProjectRoot(path string) (string, error) { return CanonicalPath(path) }

// LexicalPath returns an absolute, cleaned path without resolving symlinks.
// It is intended for child process cwd values.
func LexicalPath(path string) (string, error) { return absoluteClean(path) }

// Scope is the identity root and lexical child cwd selected for an invocation.
type Scope struct {
	Root string
	Cwd  string
}

// Resolve returns a canonical project identity while retaining the lexical cwd.
func Resolve(cwd string) (Scope, error) {
	lexical, err := absoluteClean(cwd)
	if err != nil {
		return Scope{}, err
	}
	root, err := DiscoverProjectRoot(lexical)
	if err != nil {
		return Scope{}, err
	}
	return Scope{Root: root, Cwd: lexical}, nil
}

// DiscoverProjectRoot returns the canonical physical path of the nearest
// ancestor containing a .git directory or worktree file. If no marker exists,
// it returns the canonical physical cwd (or its cleaned absolute spelling if
// the path has been removed).
func DiscoverProjectRoot(cwd string) (string, error) {
	path, err := absoluteClean(cwd)
	if err != nil {
		return "", err
	}
	probe := path
	if info, statErr := os.Stat(probe); statErr == nil && !info.IsDir() {
		probe = filepath.Dir(probe)
	}
	for {
		marker := filepath.Join(probe, ".git")
		if info, statErr := os.Stat(marker); statErr == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return CanonicalPath(probe)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return CanonicalPath(path)
		}
		probe = parent
	}
}

// DiscoverProjectRootLexical returns the nearest project root without
// resolving symlinks. It is used only for filesystem operations whose paths
// are intentionally presented in the caller's lexical namespace (notably
// init output and child cwd).
func DiscoverProjectRootLexical(cwd string) (string, error) {
	path, err := absoluteClean(cwd)
	if err != nil {
		return "", err
	}
	probe := path
	if info, statErr := os.Stat(probe); statErr == nil && !info.IsDir() {
		probe = filepath.Dir(probe)
	}
	for {
		marker := filepath.Join(probe, ".git")
		if info, statErr := os.Stat(marker); statErr == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return filepath.Clean(probe), nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return filepath.Clean(path), nil
		}
		probe = parent
	}
}

// ProjectRoot is a concise alias for DiscoverProjectRoot.
func ProjectRoot(cwd string) (string, error) { return DiscoverProjectRoot(cwd) }

func absoluteClean(path string) (string, error) {
	if path == "" {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("current directory: %w", err)
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("absolute cwd: %w", err)
	}
	return filepath.Clean(absolute), nil
}
