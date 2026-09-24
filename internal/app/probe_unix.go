//go:build !windows

package app

import (
	"context"
	"os/exec"
	"syscall"
)

const windowsProbe = false
const windowsStop = false

func configureProbe(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProbe(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func stopOrphanChild(child Child) error {
	_ = child.Signal(syscall.SIGKILL)
	return nil
}

func runWindowsReadinessProbe(context.Context, []string, string, []string, int) (string, error) {
	panic("Windows readiness probe on Unix")
}
