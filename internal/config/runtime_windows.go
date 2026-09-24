//go:build windows

package config

import (
	"os"
	"path/filepath"
)

func defaultRuntimeDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		var err error
		if base, err = os.UserConfigDir(); err != nil {
			// A relative base would give each working directory its own runtime.
			base = os.TempDir()
		}
	}
	return filepath.Join(base, "hum-runtime")
}
