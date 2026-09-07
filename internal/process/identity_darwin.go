//go:build darwin

package process

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// processStartIdentity reads the native Darwin process table. Unlike a PID
// liveness probe, kern.proc.pid includes the process creation timestamp needed
// to distinguish a recycled PID.
func processStartIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("process start identity: invalid pid %d", pid)
	}
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if errors.Is(err, unix.ESRCH) || errors.Is(err, os.ErrNotExist) {
			return "", os.ErrNotExist
		}
		return "", fmt.Errorf("read native process table for pid %d: %w", pid, err)
	}
	if proc == nil || proc.Proc.P_pid != int32(pid) {
		return "", fmt.Errorf("read native process table for pid %d: process identity unavailable", pid)
	}
	start := proc.Proc.P_starttime
	if start.Sec == 0 && start.Usec == 0 {
		return "", fmt.Errorf("read native process table for pid %d: start time is zero", pid)
	}
	return fmt.Sprintf("kinfo:%d:%d", start.Sec, start.Usec), nil
}
