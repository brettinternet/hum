//go:build darwin

package process

import (
	"os"

	"golang.org/x/sys/unix"
)

// FIONREAD is _IOR('f', 127, int) in Darwin's sys/filio.h.
const ttyBytesAvailable = 0x4004667f

func ttyPendingBytes(master *os.File) (int, error) {
	return unix.IoctlGetInt(int(master.Fd()), ttyBytesAvailable)
}
