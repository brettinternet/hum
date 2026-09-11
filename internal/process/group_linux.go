//go:build linux

package process

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func processGroupAlive(pgid int) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, fmt.Errorf("read procfs: %w", err)
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false, fmt.Errorf("read procfs stat for pid %s: %w", entry.Name(), err)
		}
		closeParen := strings.LastIndexByte(string(data), ')')
		if closeParen < 0 || closeParen+1 >= len(data) {
			continue
		}
		fields := strings.Fields(string(data[closeParen+1:]))
		if len(fields) < 3 {
			continue
		}
		memberPGID, err := strconv.Atoi(fields[2])
		if err == nil && memberPGID == pgid && fields[0] != "Z" {
			return true, nil
		}
	}

	return false, nil
}
