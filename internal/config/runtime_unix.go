//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"strconv"
)

func defaultRuntimeDir() string { return filepath.Join(os.TempDir(), "hum-"+strconv.Itoa(os.Getuid())) }
