//go:build darwin || linux

package integration

import (
	"os"
	"syscall"
	"testing"
)

func lifecycleKill(pid int) error {
	return syscall.Kill(pid, syscall.SIGKILL)
}

func lifecycleAssertDetachedSession(t *testing.T, pid int) {
	t.Helper()
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("get detached daemon process group: %v", err)
	}
	if pgid != pid {
		t.Fatalf("detached daemon PGID = %d, want session/process-group leader PID %d", pgid, pid)
	}
	sid := lifecycleProcessSessionID(t, pid)
	callerSID := lifecycleProcessSessionID(t, os.Getpid())
	if sid != pid {
		t.Fatalf("detached daemon SID = %d, want PID %d from setsid", sid, pid)
	}
	if sid == callerSID {
		t.Fatalf("detached daemon SID = %d is caller session %d", sid, callerSID)
	}
}
