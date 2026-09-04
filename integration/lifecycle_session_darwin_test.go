//go:build darwin

package integration

import (
	"syscall"
	"testing"
)

func lifecycleProcessSessionID(t *testing.T, pid int) int {
	t.Helper()
	sid, err := syscall.Getsid(pid)
	if err != nil {
		t.Fatalf("read process session ID for %d: %v", pid, err)
	}
	if sid <= 0 {
		t.Fatalf("process session ID for %d = %d: want positive session ID", pid, sid)
	}
	return sid
}
