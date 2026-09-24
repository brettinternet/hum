//go:build linux

package process

import (
	"os"

	"golang.org/x/sys/unix"
)

func ttyPendingBytes(master *os.File) (int, error) {
	return unix.IoctlGetInt(int(master.Fd()), unix.TIOCINQ)
}
