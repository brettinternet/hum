//go:build !windows

package testutil

import (
	"errors"
	"syscall"
)

func binarySuffix() string              { return "" }
func runtimeTempParent() string         { return "/tmp" }
func processAlreadyGone(err error) bool { return errors.Is(err, syscall.ESRCH) }

func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func processGroupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
