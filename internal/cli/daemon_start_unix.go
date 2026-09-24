//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
	"time"
)

func configureDetachedDaemon(child *exec.Cmd) {
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func cancelDetachedDaemon(child *exec.Cmd, reaped *reapedChild) {
	if child == nil || child.Process == nil || child.Process.Pid <= 0 || reaped.exited() {
		return
	}
	_ = syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
}

func terminateDetachedDaemon(child *exec.Cmd, reaped *reapedChild) {
	if child == nil || child.Process == nil || child.Process.Pid <= 0 || reaped.exited() {
		return
	}
	pid := child.Process.Pid
	// Setsid makes the child both a session leader and process-group leader.
	// TERM lets a daemon that reached its signal-aware serve path remove its
	// ownership artifacts. Fall back to KILL if startup itself is wedged.
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-reaped.done:
	case <-time.After(daemonTerminationGrace):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-reaped.done
	}
}
