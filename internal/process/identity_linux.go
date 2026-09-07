//go:build linux

package process

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// bootIdentity reads a value that changes on every boot. Procfs start times
// count clock ticks since boot, so without a boot-invariant component a
// pre-reboot identity can match an unrelated post-boot process that happens to
// hold the same PID and to have started in the same tick.
var bootIdentity = sync.OnceValues(func() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("read boot id: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", errors.New("read boot id: value is empty")
	}
	return value, nil
})

// processStartIdentity reads Linux's monotonically increasing procfs start
// time (the 22nd field in /proc/<pid>/stat), qualified by the current boot so
// the identity cannot match across a reboot. The command name is wrapped in
// parentheses and may itself contain spaces or parentheses, so parsing starts
// after the final closing parenthesis rather than using strings.Fields on the
// whole file.
func processStartIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("process start identity: invalid pid %d", pid)
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", os.ErrNotExist
		}
		return "", fmt.Errorf("read procfs stat for pid %d: %w", pid, err)
	}
	closeParen := strings.LastIndexByte(string(data), ')')
	if closeParen < 0 || closeParen+1 >= len(data) {
		return "", fmt.Errorf("parse procfs stat for pid %d: missing command terminator", pid)
	}
	fields := strings.Fields(string(data[closeParen+1:]))
	// The suffix begins at field 3 (state), therefore field 22 (starttime) is
	// suffix index 19.
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return "", fmt.Errorf("parse procfs stat for pid %d: missing start time", pid)
	}
	value, err := strconv.ParseUint(fields[startTimeIndex], 10, 64)
	if err != nil || value == 0 {
		if err == nil {
			err = errors.New("start time is zero")
		}
		return "", fmt.Errorf("parse procfs start time for pid %d: %w", pid, err)
	}
	boot, err := bootIdentity()
	if err != nil {
		return "", err
	}
	return "procfs:" + boot + ":" + strconv.FormatUint(value, 10), nil
}
