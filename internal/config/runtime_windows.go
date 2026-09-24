//go:build windows

package config

import (
	"os"
	"path/filepath"
)

func defaultRuntimeDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "hum-runtime")
}
