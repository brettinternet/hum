//go:build linux

package integration

import (
	"syscall"
	"testing"
)

func lifecycleProcessSessionID(t *testing.T, pid int) int {
	t.Helper()
	sid, _, errno := syscall.Syscall(syscall.SYS_GETSID, uintptr(pid), 0, 0)
	if errno != 0 {
		t.Fatalf("read process session ID for %d: %v", pid, errno)
	}
	if sid == 0 {
		t.Fatalf("process session ID for %d = %d: want positive session ID", pid, sid)
	}
	return int(sid)
}
