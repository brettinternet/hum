// Package project resolves project roots and strict project manifests.
package project

import (
	"fmt"
	"os"
	"path/filepath"
)

// DiscoverProjectRoot returns the nearest ancestor containing a .git
// directory or worktree file. If no marker exists, it returns the absolute,
// cleaned cwd.
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
		if info, statErr := os.Stat(marker); statErr == nil {
			if info.IsDir() || info.Mode().IsRegular() {
				return filepath.Clean(probe), nil
			}
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
