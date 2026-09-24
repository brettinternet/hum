package project

import "os"

const binDevExecutableName = "dev.exe"

func binDevExecutable(info os.FileInfo) bool { return info.Mode().IsRegular() }

// Windows has no Unix FIFOs; the regular-file check in the caller excludes
// special files before any declaration bytes are read.
func openDiscoveryDeclaration(path string) (*os.File, error) {
	return os.Open(path)
}
