//go:build windows

package cli

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureDetachedDaemon(child *exec.Cmd) {
	child.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
}

func cancelDetachedDaemon(child *exec.Cmd, reaped *reapedChild) {
	terminateDetachedDaemon(child, reaped)
}

func terminateDetachedDaemon(child *exec.Cmd, reaped *reapedChild) {
	if child == nil || child.Process == nil || reaped.exited() {
		return
	}
	// Windows has no process-group signal equivalent. Kill through the process
	// handle and wait for the launch-time reaper so cancellation cannot return
	// while a detached daemon remains alive.
	_ = child.Process.Kill()
	<-reaped.done
}
