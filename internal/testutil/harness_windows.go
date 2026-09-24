//go:build windows

package testutil

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/windows"
	"hum/internal/process"
)

func binarySuffix() string              { return ".exe" }
func runtimeTempParent() string         { return "" }
func runtimePath(dir string) string     { return filepath.Join(dir, "runtime") }
func processAlreadyGone(err error) bool { return errors.Is(err, windows.ERROR_INVALID_PARAMETER) }

func ProcessAlive(pid int) bool       { return process.ProcessGroupAlive(pid) }
func processGroupAlive(pgid int) bool { return process.ProcessGroupAlive(pgid) }
