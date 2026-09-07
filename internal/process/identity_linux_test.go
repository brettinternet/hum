//go:build linux

package process

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestProcessStartIdentityIsBootQualified pins the boot-invariant component.
// Procfs start times count ticks since boot and restart near zero after one,
// so a tick count alone lets a recorded identity match an unrelated post-boot
// process that reuses its PID, which is exactly the signal that must never be
// sent to a process hum does not own.
func TestProcessStartIdentityIsBootQualified(t *testing.T) {
	identity, err := ProcessStartIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("process start identity: %v", err)
	}
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatalf("read boot id: %v", err)
	}
	boot := strings.TrimSpace(string(data))
	if boot == "" {
		t.Fatal("boot id is empty")
	}
	prefix := "procfs:" + boot + ":"
	if !strings.HasPrefix(identity, prefix) {
		t.Fatalf("identity %q is not qualified by boot id %q", identity, boot)
	}
	ticks, err := strconv.ParseUint(strings.TrimPrefix(identity, prefix), 10, 64)
	if err != nil || ticks == 0 {
		t.Fatalf("identity %q does not end in a procfs start time: %v", identity, err)
	}
}
