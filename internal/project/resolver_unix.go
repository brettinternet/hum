//go:build !windows

package project

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

const binDevExecutableName = "dev"

func binDevExecutable(info os.FileInfo) bool { return info.Mode().Perm()&0o111 != 0 }

func openDiscoveryDeclaration(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open declaration file")
	}
	return file, nil
}
